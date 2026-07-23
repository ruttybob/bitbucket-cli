package commands

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ruttybob/bkt-axi/internal/app"
	"github.com/ruttybob/bkt-axi/internal/axi"
	"github.com/ruttybob/bkt-axi/internal/bitbucket"
)

// issue.go implements the `issue` noun (Bitbucket Cloud only). Data Center's
// issue tracker was removed in modern releases, so a DC host gets a clear
// Cloud-only error from the adapter layer.

// NewIssueCmd builds the `issue` noun and its verbs.
func NewIssueCmd() *app.Command {
	return &app.Command{
		Name:  "issue",
		Short: "Work with issues (Cloud)",
		Long:  "List, inspect, create, and mutate Bitbucket Cloud repository issues.",
		Children: []*app.Command{
			newIssueListCmd(),
			newIssueViewCmd(),
			newIssueCreateCmd(),
			newIssueEditCmd(),
			newIssueCloseCmd(),
			newIssueReopenCmd(),
			newIssueCommentCmd(),
			newIssueAttachmentCmd(),
		},
	}
}

var issueExtraFields = []fieldEntry{
	{Token: "kind", Column: axi.Field{Key: "kind", Extractor: axi.Pluck("kind")}},
	{Token: "assignee", Column: axi.Field{Key: "assignee", Extractor: axi.Pluck("assignee")}},
	{Token: "reporter", Column: axi.Field{Key: "reporter", Extractor: axi.Pluck("reporter")}},
	{Token: "created", Column: axi.Field{Key: "created", Extractor: axi.RelativeTime(axi.Pluck("created_at"))}},
	{Token: "updated", Column: axi.Field{Key: "updated", Extractor: axi.RelativeTime(axi.Pluck("updated_at"))}},
}

func issueListSchema(extras []axi.Field) []axi.Field {
	return schemaColumns([]axi.Field{
		{Key: "id", Extractor: axi.Pluck("id")},
		{Key: "title", Extractor: axi.Pluck("title")},
		{Key: "state", Extractor: axi.Pluck("state")},
		{Key: "priority", Extractor: axi.Pluck("priority")},
	}, extras)
}

func issueDetailSchema(full bool) []axi.Field {
	return []axi.Field{
		{Key: "id", Extractor: axi.Pluck("id")},
		{Key: "title", Extractor: axi.Pluck("title")},
		{Key: "state", Extractor: axi.Pluck("state")},
		{Key: "kind", Extractor: axi.Pluck("kind")},
		{Key: "priority", Extractor: axi.Pluck("priority")},
		{Key: "assignee", Extractor: axi.Pluck("assignee")},
		{Key: "reporter", Extractor: axi.Pluck("reporter")},
		{Key: "content", Extractor: axi.Const("")}, // overwritten per-render for truncation
		{Key: "url", Extractor: axi.Pluck("url")},
		{Key: "created", Extractor: axi.RelativeTime(axi.Pluck("created_at"))},
		{Key: "updated", Extractor: axi.RelativeTime(axi.Pluck("updated_at"))},
	}
}

func newIssueListCmd() *app.Command {
	flags := append(app.FlagSet{
		{Name: "state", Type: app.FlagString, Default: "", Desc: "Filter by state: new, open, resolved, closed, all"},
		{Name: "limit", Type: app.FlagInt, Default: 50, Desc: "Maximum number of issues to show (1-100)"},
		{Name: "fields", Type: app.FlagString, Default: "", Desc: "Extra columns (comma-sep): kind,assignee,reporter,created,updated"},
	}, selectorFlags()...)
	return &app.Command{
		Name:    "list",
		Aliases: []string{"ls"},
		Short:   "List issues in the current repository",
		Long:    "List Bitbucket Cloud issues for the resolved repository. The default schema is {id,title,state,priority}; use --fields to add columns.",
		Flags:   flags,
		MinArgs: 0, MaxArgs: 0,
		Examples: []app.Example{
			{Cmd: "bkt-axi issue list", What: "open issues in the current repo"},
			{Cmd: "bkt-axi issue list --state all --fields assignee,created", What: "all issues with assignee and age"},
		},
		Run: runIssueList,
	}
}

func runIssueList(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --workspace) or set a context")
	}
	extras, err := resolveExtraFields(ctx.Flags.String("fields"), "issue list", issueExtraFields)
	if err != nil {
		return err
	}
	schema := issueListSchema(extras)

	result, err := client.ListIssues(context.Background(), scope, bitbucket.IssueListOptions{
		State: ctx.Flags.String("state"),
		Limit: ctx.Flags.Int("limit"),
	})
	if err != nil {
		return err
	}
	if len(result.Issues) == 0 {
		state := strings.ToLower(strings.TrimSpace(ctx.Flags.String("state")))
		if state == "" {
			state = "open"
		}
		emitEmpty(ctx, "issues", fmt.Sprintf("0 %s issues in %s", state, scope), []string{
			"Run `bkt-axi issue list --state all` to include resolved and closed issues",
			"Run `bkt-axi issue create --title \"...\"` to add an issue",
		})
		return nil
	}
	count := any(result.Shown)
	help := []string{"Run `bkt-axi issue view <id>` for an issue's details"}
	if result.MoreAvailable {
		count = fmt.Sprintf("%d shown (more available)", result.Shown)
		help = append([]string{"Raise --limit to see more results"}, help...)
	}
	emitList(ctx, "issues", toAny(result.Issues), schema, count, help)
	return nil
}

func newIssueViewCmd() *app.Command {
	flags := append(app.FlagSet{
		{Name: "full", Type: app.FlagBool, Default: false, Desc: "Show the full, untruncated content"},
		{Name: "comments", Type: app.FlagBool, Default: false, Desc: "Include the comment thread"},
	}, selectorFlags()...)
	return &app.Command{
		Name:    "view",
		Short:   "Show details for an issue",
		Long:    "Display an issue's full state with a truncated content body (use --full for the complete body and --comments for the thread).",
		Flags:   flags,
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{{Cmd: "bkt-axi issue view 7", What: "details for issue #7"}},
		Run:      runIssueView,
	}
}

func runIssueView(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --workspace) or set a context")
	}
	id, idErr := parseID(ctx.Args[0])
	if idErr != nil {
		return axi.UsageError(fmt.Sprintf("invalid issue id %q: %s", ctx.Args[0], idErr))
	}
	issue, err := client.GetIssue(context.Background(), scope, id)
	if err != nil {
		return err
	}
	full := ctx.Flags.Bool("full")
	content := issue.Content
	if !full {
		content = axi.TruncateBody(content, axi.DefaultBodyBudget)
	}
	schema := issueDetailSchema(full)
	// Inject the (possibly truncated) content as a constant extractor.
	for i := range schema {
		if schema[i].Key == "content" {
			schema[i].Extractor = axi.Const(content)
		}
	}
	help := []string(nil)
	if !full && prWasTruncated(issue.Content) {
		help = append(help, fmt.Sprintf("Run `bkt-axi issue view %d --full` to see the complete content", id))
	}

	var comments []bitbucket.Comment
	if ctx.Flags.Bool("comments") {
		comments, err = client.ListIssueComments(context.Background(), scope, id)
		if err != nil {
			return err
		}
	}

	// Build the doc: the issue detail, plus optional comments + help.
	kvs := []axi.KV{{Key: "issue", Value: axi.NewObject(axi.Ordered(*issue, schema)...)}}
	payload := map[string]any{"issue": axi.Extract(*issue, schema)}
	if comments != nil {
		kvs = append(kvs, axi.KV{Key: "comments", Value: commentRows(comments)})
		payload["comments"] = comments
	}
	if len(help) > 0 {
		kvs = append(kvs, axi.KV{Key: "help", Value: axi.HelpRows(help)})
		payload["help"] = help
	}
	emit(ctx, payload, axi.Marshal(axi.NewObject(kvs...)))
	return nil
}

func newIssueCreateCmd() *app.Command {
	flags := append(app.FlagSet{
		{Name: "title", Type: app.FlagString, Default: "", Desc: "Issue title (required)"},
		{Name: "body", Type: app.FlagString, Default: "", Desc: "Issue body (description)"},
		{Name: "body-file", Type: app.FlagString, Default: "", Desc: "Read issue body from a file (- for stdin)"},
		{Name: "kind", Type: app.FlagString, Default: "", Desc: "Issue kind: bug, enhancement, proposal, task"},
		{Name: "priority", Type: app.FlagString, Default: "", Desc: "Issue priority: trivial, minor, major, critical, blocker"},
		{Name: "assignee", Type: app.FlagString, Default: "", Desc: "Assignee UUID/account id"},
	}, selectorFlags()...)
	return &app.Command{
		Name:    "create",
		Short:   "Create an issue",
		Long:    "Create a new Bitbucket Cloud issue. --title is required.",
		Flags:   flags,
		MinArgs: 0, MaxArgs: 0,
		Examples: []app.Example{{Cmd: "bkt-axi issue create --title \"Fix login\" --kind bug --priority major", What: "create a major bug"}},
		Run:      runIssueCreate,
	}
}

func runIssueCreate(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --workspace) or set a context")
	}
	title := strings.TrimSpace(ctx.Flags.String("title"))
	if title == "" {
		return axi.UsageError("`issue create` requires --title")
	}
	body, err := bodyFromFlags(ctx)
	if err != nil {
		return err
	}
	issue, err := client.CreateIssue(context.Background(), scope, bitbucket.IssueCreateInput{
		Title:    title,
		Content:  body,
		Kind:     ctx.Flags.String("kind"),
		Priority: ctx.Flags.String("priority"),
		Assignee: ctx.Flags.String("assignee"),
	})
	if err != nil {
		return err
	}
	emitDetail(ctx, "issue", *issue, issueDetailSchema(true), []string{
		fmt.Sprintf("Run `bkt-axi issue view %d` to see full details", issue.ID),
	})
	return nil
}

func newIssueEditCmd() *app.Command {
	flags := append(app.FlagSet{
		{Name: "title", Type: app.FlagString, Default: "", Desc: "New title"},
		{Name: "body", Type: app.FlagString, Default: "", Desc: "New body (description)"},
		{Name: "body-file", Type: app.FlagString, Default: "", Desc: "Read new body from a file (- for stdin)"},
		{Name: "kind", Type: app.FlagString, Default: "", Desc: "New kind: bug, enhancement, proposal, task"},
		{Name: "priority", Type: app.FlagString, Default: "", Desc: "New priority: trivial, minor, major, critical, blocker"},
		{Name: "assignee", Type: app.FlagString, Default: "", Desc: "New assignee UUID/account id (empty to clear)"},
	}, selectorFlags()...)
	return &app.Command{
		Name:    "edit",
		Aliases: []string{"update"},
		Short:   "Edit an issue",
		Long:    "Update an issue's title, body, kind, priority, or assignee. Only flags you set are changed.",
		Flags:   flags,
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{{Cmd: "bkt-axi issue edit 7 --priority critical", What: "raise issue #7 to critical"}},
		Run:      runIssueEdit,
	}
}

func runIssueEdit(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --workspace) or set a context")
	}
	id, idErr := parseID(ctx.Args[0])
	if idErr != nil {
		return axi.UsageError(fmt.Sprintf("invalid issue id %q: %s", ctx.Args[0], idErr))
	}
	var in bitbucket.IssueEditInput
	changed := false
	if ctx.Flags.Changed("title") {
		t := ctx.Flags.String("title")
		in.Title = &t
		changed = true
	}
	if ctx.Flags.Changed("body") || ctx.Flags.Changed("body-file") {
		b, berr := bodyFromFlags(ctx)
		if berr != nil {
			return berr
		}
		in.Content = &b
		changed = true
	}
	if ctx.Flags.Changed("kind") {
		k := ctx.Flags.String("kind")
		in.Kind = &k
		changed = true
	}
	if ctx.Flags.Changed("priority") {
		p := ctx.Flags.String("priority")
		in.Priority = &p
		changed = true
	}
	if ctx.Flags.Changed("assignee") {
		a := ctx.Flags.String("assignee")
		in.Assignee = &a
		changed = true
	}
	if !changed {
		return axi.UsageError("`issue edit` requires at least one of --title, --body, --body-file, --kind, --priority, --assignee")
	}
	issue, err := client.UpdateIssue(context.Background(), scope, id, in)
	if err != nil {
		return err
	}
	emitDetail(ctx, "issue", *issue, issueDetailSchema(true), nil)
	return nil
}

func newIssueCloseCmd() *app.Command {
	return &app.Command{
		Name:    "close",
		Short:   "Close (resolve) an issue",
		Long:    "Resolve an issue. Idempotent: a no-op when the issue is already in a terminal state.",
		Flags:   selectorFlags(),
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{{Cmd: "bkt-axi issue close 7", What: "resolve issue #7"}},
		Run:      runIssueClose,
	}
}

func runIssueClose(ctx *app.Context) error {
	return runIssueStateChange(ctx, "close")
}

func newIssueReopenCmd() *app.Command {
	return &app.Command{
		Name:    "reopen",
		Short:   "Reopen an issue",
		Long:    "Reopen an issue. Idempotent: a no-op when the issue is already active.",
		Flags:   selectorFlags(),
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{{Cmd: "bkt-axi issue reopen 7", What: "reopen issue #7"}},
		Run:      runIssueReopen,
	}
}

func runIssueReopen(ctx *app.Context) error {
	return runIssueStateChange(ctx, "reopen")
}

func runIssueStateChange(ctx *app.Context, verb string) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --workspace) or set a context")
	}
	id, idErr := parseID(ctx.Args[0])
	if idErr != nil {
		return axi.UsageError(fmt.Sprintf("invalid issue id %q: %s", ctx.Args[0], idErr))
	}
	var (
		issue   *bitbucket.Issue
		changed bool
	)
	if verb == "close" {
		issue, changed, err = client.CloseIssue(context.Background(), scope, id)
	} else {
		issue, changed, err = client.ReopenIssue(context.Background(), scope, id)
	}
	if err != nil {
		return err
	}
	if !changed {
		emitConfirmation(ctx, fmt.Sprintf("issue #%d already %s (no-op)", id, verbTargetState(verb)))
		return nil
	}
	emitDetail(ctx, "issue", *issue, issueDetailSchema(false), nil)
	return nil
}

// verbTargetState returns the no-op description for a close/reopen.
func verbTargetState(verb string) string {
	if verb == "close" {
		return "resolved"
	}
	return "open"
}

func newIssueCommentCmd() *app.Command {
	flags := append(app.FlagSet{
		{Name: "body", Type: app.FlagString, Default: "", Desc: "Comment body (required)"},
		{Name: "body-file", Type: app.FlagString, Default: "", Desc: "Read comment body from a file (- for stdin)"},
	}, selectorFlags()...)
	return &app.Command{
		Name:    "comment",
		Short:   "Comment on an issue",
		Long:    "Add a comment to an issue. --body or --body-file is required.",
		Flags:   flags,
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{{Cmd: "bkt-axi issue comment 7 --body \"Looks done\"", What: "comment on issue #7"}},
		Run:      runIssueComment,
	}
}

func runIssueComment(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --workspace) or set a context")
	}
	id, idErr := parseID(ctx.Args[0])
	if idErr != nil {
		return axi.UsageError(fmt.Sprintf("invalid issue id %q: %s", ctx.Args[0], idErr))
	}
	body, err := bodyFromFlags(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(body) == "" {
		return axi.UsageError("`issue comment` requires --body or --body-file")
	}
	if err := client.CreateIssueComment(context.Background(), scope, id, body); err != nil {
		return err
	}
	emitConfirmation(ctx, fmt.Sprintf("comment added to issue #%d", id))
	return nil
}

// --- issue attachment ---------------------------------------------------

func newIssueAttachmentCmd() *app.Command {
	return &app.Command{
		Name:  "attachment",
		Short: "Manage issue attachments",
		Long:  "List, upload, download, and delete attachments on a Bitbucket Cloud issue.",
		Children: []*app.Command{
			{Name: "list", Aliases: []string{"ls"}, Short: "List attachments on an issue",
				Flags: selectorFlags(), MinArgs: 1, MaxArgs: 1, Run: runIssueAttachmentList},
			{Name: "upload", Short: "Upload a file to an issue",
				Flags: selectorFlags(), MinArgs: 2, MaxArgs: 2, Run: runIssueAttachmentUpload},
			{Name: "download", Short: "Download an attachment",
				Flags: selectorFlags(), MinArgs: 2, MaxArgs: 2, Run: runIssueAttachmentDownload},
			{Name: "delete", Short: "Delete an attachment",
				Flags: selectorFlags(), MinArgs: 2, MaxArgs: 2, Run: runIssueAttachmentDelete},
		},
	}
}

func runIssueAttachmentList(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --workspace) or set a context")
	}
	id, idErr := parseID(ctx.Args[0])
	if idErr != nil {
		return axi.UsageError(fmt.Sprintf("invalid issue id %q: %s", ctx.Args[0], idErr))
	}
	atts, err := client.ListIssueAttachments(context.Background(), scope, id)
	if err != nil {
		return err
	}
	schema := []axi.Field{
		{Key: "name", Extractor: axi.Pluck("name")},
		{Key: "url", Extractor: axi.Pluck("url")},
	}
	if len(atts) == 0 {
		emitEmpty(ctx, "attachments", fmt.Sprintf("0 attachments on issue #%d", id), []string{
			fmt.Sprintf("Run `bkt-axi issue attachment upload %d <file>` to add an attachment", id),
		})
		return nil
	}
	emitList(ctx, "attachments", toAny(atts), schema, len(atts), nil)
	return nil
}

func runIssueAttachmentUpload(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --workspace) or set a context")
	}
	id, idErr := parseID(ctx.Args[0])
	if idErr != nil {
		return axi.UsageError(fmt.Sprintf("invalid issue id %q: %s", ctx.Args[0], idErr))
	}
	path := ctx.Args[1]
	f, err := os.Open(path)
	if err != nil {
		return axi.Errorf("cannot open %q: %s", path, err)
	}
	defer f.Close()
	att, err := client.UploadIssueAttachment(context.Background(), scope, id, baseName(path), f)
	if err != nil {
		return err
	}
	emitConfirmation(ctx, fmt.Sprintf("uploaded %s to issue #%d", att.Name, id))
	return nil
}

func runIssueAttachmentDownload(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --workspace) or set a context")
	}
	id, idErr := parseID(ctx.Args[0])
	if idErr != nil {
		return axi.UsageError(fmt.Sprintf("invalid issue id %q: %s", ctx.Args[0], idErr))
	}
	name := ctx.Args[1]
	if err := client.DownloadIssueAttachment(context.Background(), scope, id, name, ctx.Out()); err != nil {
		return err
	}
	return nil
}

func runIssueAttachmentDelete(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --workspace) or set a context")
	}
	id, idErr := parseID(ctx.Args[0])
	if idErr != nil {
		return axi.UsageError(fmt.Sprintf("invalid issue id %q: %s", ctx.Args[0], idErr))
	}
	name := ctx.Args[1]
	if err := client.DeleteIssueAttachment(context.Background(), scope, id, name); err != nil {
		return err
	}
	emitConfirmation(ctx, fmt.Sprintf("deleted %s from issue #%d", name, id))
	return nil
}

// baseName returns the final path element, used as the upload filename.
func baseName(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}
