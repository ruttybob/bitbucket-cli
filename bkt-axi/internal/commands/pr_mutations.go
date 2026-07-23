package commands

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ruttybob/bkt-axi/internal/app"
	"github.com/ruttybob/bkt-axi/internal/axi"
	"github.com/ruttybob/bkt-axi/internal/bitbucket"
	"github.com/ruttybob/bkt-axi/internal/git"
)

// pr_mutations.go implements the Phase 2 pull-request mutation commands. Each
// resolves the unified client and scope, calls the normalized mutation
// adapter, and renders a minimal TOON confirm. Mutations are idempotent: an
// already-in-target-state operation is a clean exit-0 no-op with an
// "(already — no-op)" suffix on the state field (AXI §6).

// prDiffBudget is the tail length kept when a diff is truncated.
const prDiffBudget = 8000

// validMergeStrategies are the friendly strategy aliases accepted on --strategy.
var validMergeStrategies = []string{"squash", "merge", "rebase"}

// newPRMutationChildren builds the Phase 2 PR mutation verbs.
func newPRMutationChildren() []*app.Command {
	return []*app.Command{
		newPRCreateCmd(),
		newPREditCmd(),
		newPRDiffCmd(),
		newPRCheckoutCmd(),
		newPRApproveCmd(),
		newPRMergeCmd(),
		newPRDeclineCmd(),
		newPRReopenCmd(),
		newPRCommentCmd(),
	}
}

// --- shared flag/option helpers ------------------------------------------

// csvFlag splits a comma-separated flag value into trimmed, non-empty parts.
func csvFlag(ctx *app.Context, name string) []string {
	raw := strings.TrimSpace(ctx.Flags.String(name))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// textFromFlags resolves a text body from a primary flag and a companion
// --<prefix>-file flag. The file flag takes precedence; setting both is an
// error. name is the primary flag (e.g. "body"); the file flag is name+"-file".
func textFromFlags(ctx *app.Context, name string) (string, error) {
	body := strings.TrimSpace(ctx.Flags.String(name))
	fileFlag := name + "-file"
	file := strings.TrimSpace(ctx.Flags.String(fileFlag))
	if body != "" && file != "" {
		return "", axi.UsageError(fmt.Sprintf("--%s and --%s are mutually exclusive", name, fileFlag))
	}
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return "", axi.Errorf("read --%s %q: %s", fileFlag, file, err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	return body, nil
}

// noopSuffix appends the idempotent no-op marker when already is true.
func noopSuffix(state string, already bool) string {
	if already {
		return state + " (already — no-op)"
	}
	return state
}

// emitPRConfirm renders the minimal pull-request confirm: id, title, state,
// url. stateLabel already carries the no-op suffix when applicable.
func emitPRConfirm(ctx *app.Context, pr *bitbucket.PR, stateLabel string) {
	doc := axi.NewObject(
		axi.KV{Key: "pull_request", Value: axi.NewObject(
			axi.KV{Key: "id", Value: pr.ID},
			axi.KV{Key: "title", Value: pr.Title},
			axi.KV{Key: "state", Value: stateLabel},
			axi.KV{Key: "url", Value: pr.URL},
		)},
	)
	payload := map[string]any{
		"pull_request": map[string]any{
			"id":    pr.ID,
			"title": pr.Title,
			"state": stateLabel,
			"url":   pr.URL,
		},
	}
	emit(ctx, payload, axi.Marshal(doc))
}

// --- pr create -----------------------------------------------------------

func newPRCreateCmd() *app.Command {
	flags := append(app.FlagSet{
		{Name: "title", Type: app.FlagString, Default: "", Desc: "Pull request title (defaults to the last commit subject)"},
		{Name: "body", Type: app.FlagString, Default: "", Desc: "Pull request description"},
		{Name: "body-file", Type: app.FlagString, Default: "", Desc: "Read description from a file (overrides --body)"},
		{Name: "source", Type: app.FlagString, Default: "", Desc: "Source branch (defaults to the current branch)"},
		{Name: "target", Type: app.FlagString, Default: "", Desc: "Target branch (defaults to the repository default branch)"},
		{Name: "reviewer", Type: app.FlagString, Default: "", Desc: "Reviewer identity (comma-separated)"},
		{Name: "close-source", Type: app.FlagBool, Default: false, Desc: "Close the source branch after merge"},
		{Name: "draft", Type: app.FlagBool, Default: false, Desc: "Create as a draft pull request"},
		{Name: "with-default-reviewers", Type: app.FlagBool, Default: false, Desc: "Add the repository's default reviewers"},
		{Name: "source-project", Type: app.FlagString, Default: "", Desc: "Data Center: source project key (for cross-repo PRs)"},
		{Name: "source-repo", Type: app.FlagString, Default: "", Desc: "Data Center: source repository slug (for cross-repo PRs)"},
	}, selectorFlags()...)
	return &app.Command{
		Name:    "create",
		Short:   "Create a pull request",
		Long:    "Create a pull request from the source branch into the target branch. --title defaults to the last commit subject; --source defaults to the current branch; --target defaults to the repository default branch.",
		Flags:   flags,
		MinArgs: 0, MaxArgs: 0,
		Examples: []app.Example{
			{Cmd: "bkt-axi pr create", What: "create a PR from the current branch into the default branch"},
			{Cmd: "bkt-axi pr create --source feature/x --target main --reviewer alice,bob", What: "create with explicit branches and reviewers"},
			{Cmd: "bkt-axi pr create --draft --body-file PR.md", What: "create a draft PR with a description from a file"},
		},
		Run: runPRCreate,
	}
}

func runPRCreate(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --workspace or --project) or set a context")
	}

	title := strings.TrimSpace(ctx.Flags.String("title"))
	if title == "" {
		title = git.LastCommitSubject("")
	}

	body, err := textFromFlags(ctx, "body")
	if err != nil {
		return err
	}

	source := strings.TrimSpace(ctx.Flags.String("source"))
	if source == "" {
		source = git.CurrentBranch("")
	}
	if source == "" {
		return axi.UsageError("--source is required (could not infer the current branch); pass --source <branch>")
	}

	target := strings.TrimSpace(ctx.Flags.String("target"))
	if target == "" {
		if t, terr := client.DefaultBranch(context.Background(), scope); terr == nil && t != "" {
			target = t
		}
	}
	if target == "" {
		return axi.UsageError("--target is required (could not read the repository default branch); pass --target <branch>")
	}

	if title == "" {
		return axi.UsageError("--title is required (could not infer a commit subject); pass --title \"<title>\"")
	}

	pr, err := client.CreatePR(context.Background(), scope, bitbucket.CreatePRInput{
		Title:            title,
		Description:      body,
		SourceBranch:     source,
		TargetBranch:     target,
		CloseSource:      ctx.Flags.Bool("close-source"),
		Draft:            ctx.Flags.Bool("draft"),
		Reviewers:        csvFlag(ctx, "reviewer"),
		DefaultReviewers: ctx.Flags.Bool("with-default-reviewers"),
		SourceProjectKey: ctx.Flags.String("source-project"),
		SourceRepoSlug:   ctx.Flags.String("source-repo"),
	})
	if err != nil {
		return err
	}
	emitPRConfirm(ctx, pr, pr.State)
	return nil
}

// --- pr edit -------------------------------------------------------------

func newPREditCmd() *app.Command {
	flags := append(app.FlagSet{
		{Name: "title", Type: app.FlagString, Default: "", Desc: "New pull request title"},
		{Name: "body", Type: app.FlagString, Default: "", Desc: "New pull request description"},
		{Name: "body-file", Type: app.FlagString, Default: "", Desc: "Read description from a file (overrides --body)"},
		{Name: "reviewer", Type: app.FlagString, Default: "", Desc: "Reviewer identity to add (comma-separated)"},
		{Name: "remove-reviewer", Type: app.FlagString, Default: "", Desc: "Reviewer identity to remove (comma-separated)"},
		{Name: "with-default-reviewers", Type: app.FlagBool, Default: false, Desc: "Add the repository's default reviewers"},
		{Name: "publish", Type: app.FlagBool, Default: false, Desc: "Publish (un-draft) the pull request"},
	}, selectorFlags()...)
	return &app.Command{
		Name:    "edit",
		Short:   "Edit a pull request",
		Long:    "Edit a pull request's title, description, reviewers, or draft state. At least one change flag is required.",
		Flags:   flags,
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{
			{Cmd: "bkt-axi pr edit 42 --title \"New title\"", What: "rename pull request #42"},
			{Cmd: "bkt-axi pr edit 42 --reviewer alice --remove-reviewer bob", What: "swap reviewers"},
			{Cmd: "bkt-axi pr edit 42 --publish", What: "publish (un-draft) the pull request"},
		},
		Run: runPREdit,
	}
}

func runPREdit(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --workspace or --project) or set a context")
	}

	id, idErr := parseID(ctx.Args[0])
	if idErr != nil {
		return axi.UsageError(fmt.Sprintf("invalid pull request id %q: %s", ctx.Args[0], idErr))
	}

	body, err := textFromFlags(ctx, "body")
	if err != nil {
		return err
	}
	title := strings.TrimSpace(ctx.Flags.String("title"))

	in := bitbucket.UpdatePRInput{
		ReviewersAdd:     csvFlag(ctx, "reviewer"),
		ReviewersRemove:  csvFlag(ctx, "remove-reviewer"),
		DefaultReviewers: ctx.Flags.Bool("with-default-reviewers"),
		Publish:          ctx.Flags.Bool("publish"),
	}
	if ctx.Flags.Changed("title") {
		t := title
		in.Title = &t
	}
	if ctx.Flags.Changed("body") || ctx.Flags.Changed("body-file") {
		d := body
		in.Description = &d
	}

	if in.Title == nil && in.Description == nil && len(in.ReviewersAdd) == 0 &&
		len(in.ReviewersRemove) == 0 && !in.DefaultReviewers && !in.Publish {
		return axi.UsageError("at least one of --title, --body/--body-file, --reviewer, --remove-reviewer, --with-default-reviewers, --publish is required")
	}

	pr, err := client.UpdatePR(context.Background(), scope, id, in)
	if err != nil {
		return err
	}
	emitPRConfirm(ctx, pr, pr.State)
	return nil
}

// --- pr diff -------------------------------------------------------------

func newPRDiffCmd() *app.Command {
	flags := append(app.FlagSet{
		{Name: "full", Type: app.FlagBool, Default: false, Desc: "Show the full, untruncated diff"},
	}, selectorFlags()...)
	return &app.Command{
		Name:    "diff",
		Short:   "Show the diff for a pull request",
		Long:    "Display the unified diff for a pull request, truncated to the last 8000 bytes by default. Use --full for the complete diff.",
		Flags:   flags,
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{
			{Cmd: "bkt-axi pr diff 42", What: "show the (tail-truncated) diff for pull request #42"},
			{Cmd: "bkt-axi pr diff 42 --full", What: "show the complete diff"},
		},
		Run: runPRDiff,
	}
}

func runPRDiff(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --workspace or --project) or set a context")
	}

	id, idErr := parseID(ctx.Args[0])
	if idErr != nil {
		return axi.UsageError(fmt.Sprintf("invalid pull request id %q: %s", ctx.Args[0], idErr))
	}

	var buf strings.Builder
	if err := client.PRDiff(context.Background(), scope, id, &buf); err != nil {
		return err
	}
	diff := strings.TrimRight(buf.String(), "\n")

	// Large-content contract (see AGENTS.md / commit diff): default renders a
	// tail-truncated preview; --full writes the complete diff to a temp file and
	// emits full_path so it never floods stdout.
	full := ctx.Flags.Bool("full")
	help := []string(nil)
	var diffValue, fullPath string
	if full {
		path, werr := writeTempOutput("bkt-axi-pr-diff-*.diff", diff)
		if werr != nil {
			return axi.Errorf("failed to write diff to temp file: %s", werr)
		}
		fullPath = path
		diffValue = diff
		help = append(help, "Read the complete output: `cat "+fullPath+"`")
	} else {
		diffValue = axi.TruncateTail(diff, prDiffBudget)
		if axi.ExceedsBudget(diff, prDiffBudget) {
			help = append(help, fmt.Sprintf("Run `bkt-axi pr diff %d --full` to write the complete diff to a file", id))
		}
	}

	docFields := []axi.KV{
		{Key: "pull_request", Value: id},
		{Key: "diff", Value: diffValue},
	}
	if fullPath != "" {
		docFields = append(docFields, axi.KV{Key: "full_path", Value: fullPath})
	}
	if len(help) > 0 {
		docFields = append(docFields, axi.KV{Key: "help", Value: axi.HelpRows(help)})
	}
	payload := map[string]any{"pull_request": id, "diff": diffValue}
	if fullPath != "" {
		payload["full_path"] = fullPath
	}
	if len(help) > 0 {
		payload["help"] = help
	}
	emit(ctx, payload, axi.Marshal(axi.NewObject(docFields...)))
	return nil
}

// --- pr checkout ---------------------------------------------------------

func newPRCheckoutCmd() *app.Command {
	return &app.Command{
		Name:    "checkout",
		Short:   "Checkout a pull request branch locally",
		Long:    "Fetch and checkout a pull request's head into a local branch named pr/<id>. Fork pull requests fetch from a temporary fork remote.",
		Flags:   selectorFlags(),
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{
			{Cmd: "bkt-axi pr checkout 42", What: "checkout pull request #42 into a local branch"},
		},
		Run: runPRCheckout,
	}
}

func runPRCheckout(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --workspace or --project) or set a context")
	}

	id, idErr := parseID(ctx.Args[0])
	if idErr != nil {
		return axi.UsageError(fmt.Sprintf("invalid pull request id %q: %s", ctx.Args[0], idErr))
	}

	ssh := originIsSSH("")
	ref, err := client.PRCheckout(context.Background(), scope, id, ssh)
	if err != nil {
		return err
	}

	localBranch := fmt.Sprintf("pr/%d", id)
	remote := "origin"

	// Fork pull requests need a remote pointing at the source repository.
	if ref.IsFork && ref.CloneURL != "" {
		remote = "fork/" + scope.RepoSlug
		if existing := git.LookupRemoteURL("", remote); existing != "" {
			if existing != ref.CloneURL {
				if _, e := git.Run("", "remote", "set-url", remote, ref.CloneURL); e != nil {
					return axi.Errorf("update fork remote %q: %s", remote, e)
				}
			}
		} else if _, e := git.Run("", "remote", "add", remote, ref.CloneURL); e != nil {
			return axi.Errorf("add fork remote %q: %s", remote, e)
		}
	}

	refspec := fmt.Sprintf("%s:%s", ref.FetchRef, localBranch)
	if _, e := git.Run("", "fetch", remote, refspec); e != nil {
		return axi.Errorf("fetch pull request #%d: %s", id, e)
	}
	if e := git.RunClean("", "checkout", localBranch); e != nil {
		return axi.Errorf("checkout %s: %s", localBranch, e)
	}

	doc := axi.NewObject(
		axi.KV{Key: "checkout", Value: axi.NewObject(
			axi.KV{Key: "branch", Value: localBranch},
			axi.KV{Key: "source", Value: ref.Branch},
			axi.KV{Key: "remote", Value: remote},
		)},
	)
	payload := map[string]any{
		"checkout": map[string]any{
			"branch": localBranch,
			"source": ref.Branch,
			"remote": remote,
		},
	}
	emit(ctx, payload, axi.Marshal(doc))
	return nil
}

// originIsSSH reports whether the origin remote uses SSH, so fork clones pick
// a matching protocol. Defaults to HTTPS when origin is absent or unreadable.
func originIsSSH(dir string) bool {
	u := git.LookupRemoteURL(dir, "origin")
	return strings.HasPrefix(u, "git@") || strings.HasPrefix(u, "ssh://")
}

// --- pr approve ----------------------------------------------------------

func newPRApproveCmd() *app.Command {
	return &app.Command{
		Name:    "approve",
		Short:   "Approve a pull request",
		Long:    "Record your approval on a pull request. Idempotent: approving a pull request you have already approved is a no-op.",
		Flags:   selectorFlags(),
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{
			{Cmd: "bkt-axi pr approve 42", What: "approve pull request #42"},
		},
		Run: runPRApprove,
	}
}

func runPRApprove(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --workspace or --project) or set a context")
	}

	id, idErr := parseID(ctx.Args[0])
	if idErr != nil {
		return axi.UsageError(fmt.Sprintf("invalid pull request id %q: %s", ctx.Args[0], idErr))
	}

	res, err := client.ApprovePR(context.Background(), scope, id)
	if err != nil {
		return err
	}
	emitPRConfirm(ctx, res.PR, noopSuffix("approved", res.Already))
	return nil
}

// --- pr merge ------------------------------------------------------------

func newPRMergeCmd() *app.Command {
	flags := append(app.FlagSet{
		{Name: "strategy", Type: app.FlagString, Default: "", Desc: "Merge strategy: squash, merge, or rebase (default: server default)"},
		{Name: "close-source", Type: app.FlagBool, Default: false, Desc: "Close the source branch after merging"},
		{Name: "message", Type: app.FlagString, Default: "", Desc: "Merge commit message"},
		{Name: "message-file", Type: app.FlagString, Default: "", Desc: "Read merge message from a file (overrides --message)"},
		{Name: "auto", Type: app.FlagBool, Default: false, Desc: "Enable auto-merge (Cloud; reserved for a later phase)"},
	}, selectorFlags()...)
	return &app.Command{
		Name:    "merge",
		Short:   "Merge a pull request",
		Long:    "Merge a pull request. Idempotent: merging an already-merged pull request is a no-op. --strategy accepts squash, merge, or rebase.",
		Flags:   flags,
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{
			{Cmd: "bkt-axi pr merge 42", What: "merge pull request #42 with the server default strategy"},
			{Cmd: "bkt-axi pr merge 42 --strategy squash --close-source", What: "squash-merge and close the source branch"},
		},
		Run: runPRMerge,
	}
}

func runPRMerge(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --workspace or --project) or set a context")
	}

	id, idErr := parseID(ctx.Args[0])
	if idErr != nil {
		return axi.UsageError(fmt.Sprintf("invalid pull request id %q: %s", ctx.Args[0], idErr))
	}

	strategy := strings.TrimSpace(strings.ToLower(ctx.Flags.String("strategy")))
	if strategy != "" && !contains(validMergeStrategies, strategy) {
		return axi.UsageError(fmt.Sprintf("unknown --strategy %q for `pr merge`", strategy),
			validMergeStrategies...)
	}

	message, err := textFromFlags(ctx, "message")
	if err != nil {
		return err
	}

	res, err := client.MergePR(context.Background(), scope, id, bitbucket.MergePRInput{
		Strategy:    strategy,
		CloseSource: ctx.Flags.Bool("close-source"),
		Message:     message,
		Auto:        ctx.Flags.Bool("auto"),
	})
	if err != nil {
		return err
	}
	emitPRConfirm(ctx, res.PR, noopSuffix(res.PR.State, res.Already))
	return nil
}

// --- pr decline ----------------------------------------------------------

func newPRDeclineCmd() *app.Command {
	flags := append(app.FlagSet{
		{Name: "message", Type: app.FlagString, Default: "", Desc: "Decline reason"},
	}, selectorFlags()...)
	return &app.Command{
		Name:    "decline",
		Short:   "Decline a pull request",
		Long:    "Decline (reject) a pull request. Idempotent: declining an already-declined pull request is a no-op.",
		Flags:   flags,
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{
			{Cmd: "bkt-axi pr decline 42 --message \"Not needed\"", What: "decline pull request #42 with a reason"},
		},
		Run: runPRDecline,
	}
}

func runPRDecline(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --workspace or --project) or set a context")
	}

	id, idErr := parseID(ctx.Args[0])
	if idErr != nil {
		return axi.UsageError(fmt.Sprintf("invalid pull request id %q: %s", ctx.Args[0], idErr))
	}

	res, err := client.DeclinePR(context.Background(), scope, id, ctx.Flags.String("message"))
	if err != nil {
		return err
	}
	emitPRConfirm(ctx, res.PR, noopSuffix(res.PR.State, res.Already))
	return nil
}

// --- pr reopen -----------------------------------------------------------

func newPRReopenCmd() *app.Command {
	return &app.Command{
		Name:    "reopen",
		Short:   "Reopen a declined pull request",
		Long:    "Reopen a previously declined pull request. Idempotent: reopening an already-open pull request is a no-op.",
		Flags:   selectorFlags(),
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{
			{Cmd: "bkt-axi pr reopen 42", What: "reopen pull request #42"},
		},
		Run: runPRReopen,
	}
}

func runPRReopen(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --workspace or --project) or set a context")
	}

	id, idErr := parseID(ctx.Args[0])
	if idErr != nil {
		return axi.UsageError(fmt.Sprintf("invalid pull request id %q: %s", ctx.Args[0], idErr))
	}

	res, err := client.ReopenPR(context.Background(), scope, id)
	if err != nil {
		return err
	}
	emitPRConfirm(ctx, res.PR, noopSuffix(res.PR.State, res.Already))
	return nil
}

// --- pr comment ----------------------------------------------------------

func newPRCommentCmd() *app.Command {
	flags := append(app.FlagSet{
		{Name: "body", Type: app.FlagString, Default: "", Desc: "Comment body (required)"},
		{Name: "body-file", Type: app.FlagString, Default: "", Desc: "Read comment body from a file (overrides --body)"},
	}, selectorFlags()...)
	return &app.Command{
		Name:    "comment",
		Short:   "Comment on a pull request",
		Long:    "Add a top-level comment to a pull request. --body (or --body-file) is required.",
		Flags:   flags,
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{
			{Cmd: "bkt-axi pr comment 42 --body \"Looks good\"", What: "comment on pull request #42"},
		},
		Run: runPRComment,
	}
}

func runPRComment(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --workspace or --project) or set a context")
	}

	id, idErr := parseID(ctx.Args[0])
	if idErr != nil {
		return axi.UsageError(fmt.Sprintf("invalid pull request id %q: %s", ctx.Args[0], idErr))
	}

	body, err := textFromFlags(ctx, "body")
	if err != nil {
		return err
	}
	if strings.TrimSpace(body) == "" {
		return axi.UsageError("--body (or --body-file) is required")
	}

	comment, err := client.CommentPR(context.Background(), scope, id, body)
	if err != nil {
		return err
	}

	cid := comment.ID
	doc := axi.NewObject(
		axi.KV{Key: "comment", Value: axi.NewObject(
			axi.KV{Key: "id", Value: cid},
			axi.KV{Key: "pull_request", Value: id},
		)},
	)
	payload := map[string]any{
		"comment": map[string]any{
			"id":           cid,
			"pull_request": id,
		},
	}
	emit(ctx, payload, axi.Marshal(doc))
	return nil
}

// contains reports whether ss contains s (case-sensitive).
func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
