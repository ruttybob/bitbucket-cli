package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ruttybob/bkt-axi/internal/app"
	"github.com/ruttybob/bkt-axi/internal/axi"
	"github.com/ruttybob/bkt-axi/internal/bitbucket"
)

// pr.go implements the Phase 0 pull-request commands: `pr list` and
// `pr view`. Each resolves the unified client and scope from flags/context,
// calls the normalized adapter, and renders TOON (or the --json/--yaml escape
// hatch). The cloud/dc switch lives entirely inside the adapter.

// selectorFlags are the host/scope overrides every data command declares so an
// agent can target a specific context/host/repo without editing config.
func selectorFlags() app.FlagSet {
	return app.FlagSet{
		{Name: "context", Type: app.FlagString, Default: "", Desc: "Named configuration context to use"},
		{Name: "host", Type: app.FlagString, Default: "", Desc: "Host key or URL override"},
		{Name: "workspace", Type: app.FlagString, Default: "", Desc: "Bitbucket Cloud workspace override"},
		{Name: "project", Type: app.FlagString, Default: "", Desc: "Bitbucket Data Center project key override"},
		{Name: "repo", Type: app.FlagString, Default: "", Desc: "Repository slug override"},
	}
}

// NewPRCmd builds the `pr` noun and its Phase 0 verbs.
func NewPRCmd() *app.Command {
	return &app.Command{
		Name:  "pr",
		Short: "Work with pull requests",
		Long:  "List and inspect pull requests across Bitbucket Cloud and Data Center.",
		Children: append([]*app.Command{
			newPRListCmd(),
			newPRViewCmd(),
			newPRReviewerCmd(),
			newPRTaskCmd(),
			newPRSuggestionsCmd(),
			newPRChecksCmd(),
		}, newPRMutationChildren()...),
	}
}

// --- pr reviewer --------------------------------------------------------

func newPRReviewerCmd() *app.Command {
	return &app.Command{
		Name:  "reviewer",
		Short: "Manage pull request reviewers",
		Long:  "List, add, and remove reviewers on a pull request. Add/remove are idempotent.",
		Children: []*app.Command{
			{Name: "list", Aliases: []string{"ls"}, Short: "List reviewers",
				Flags: selectorFlags(), MinArgs: 1, MaxArgs: 1, Run: runPRReviewerList},
			{Name: "add", Short: "Add a reviewer",
				Flags: selectorFlags(), MinArgs: 2, MaxArgs: 2, Run: runPRReviewerAdd},
			{Name: "remove", Aliases: []string{"rm"}, Short: "Remove a reviewer",
				Flags: selectorFlags(), MinArgs: 2, MaxArgs: 2, Run: runPRReviewerRemove},
		},
	}
}

var reviewerSchema = []axi.Field{
	{Key: "name", Extractor: axi.Pluck("name")},
	{Key: "state", Extractor: axi.Pluck("state")},
	{Key: "approved", Extractor: axi.BoolYesNo(axi.Pluck("approved"))},
}

func runPRReviewerList(ctx *app.Context) error {
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
	reviewers, err := client.ListPRReviewers(context.Background(), scope, id)
	if err != nil {
		return err
	}
	if len(reviewers) == 0 {
		emitEmpty(ctx, "reviewers", fmt.Sprintf("0 reviewers on pull request #%d", id), []string{
			fmt.Sprintf("Run `bkt-axi pr reviewer add %d <user>` to request a review", id),
		})
		return nil
	}
	emitList(ctx, "reviewers", toAny(reviewers), reviewerSchema, len(reviewers), nil)
	return nil
}

func runPRReviewerAdd(ctx *app.Context) error {
	return runPRReviewerMutate(ctx, "add")
}

func runPRReviewerRemove(ctx *app.Context) error {
	return runPRReviewerMutate(ctx, "remove")
}

func runPRReviewerMutate(ctx *app.Context, verb string) error {
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
	user := ctx.Args[1]
	var (
		changed bool
		merr    error
	)
	if verb == "add" {
		changed, merr = client.AddPRReviewer(context.Background(), scope, id, user)
	} else {
		changed, merr = client.RemovePRReviewer(context.Background(), scope, id, user)
	}
	if merr != nil {
		return merr
	}
	if !changed {
		emitConfirmation(ctx, fmt.Sprintf("%s is already %s pull request #%d (no-op)", user, reviewerStatusWord(verb), id))
		return nil
	}
	emitConfirmation(ctx, fmt.Sprintf("%s %s on pull request #%d", verb, user, id))
	return nil
}

func reviewerStatusWord(verb string) string {
	if verb == "add" {
		return "a reviewer on"
	}
	return "absent from"
}

// --- pr task (DC) -------------------------------------------------------

func newPRTaskCmd() *app.Command {
	return &app.Command{
		Name:  "task",
		Short: "Manage pull request tasks (Data Center)",
		Long:  "List, create, complete, and reopen tasks on a pull request (Data Center). Complete/reopen are idempotent.",
		Children: []*app.Command{
			{Name: "list", Aliases: []string{"ls"}, Short: "List tasks",
				Flags: selectorFlags(), MinArgs: 1, MaxArgs: 1, Run: runPRTaskList},
			{Name: "create", Short: "Create a task",
				Flags: selectorFlags(), MinArgs: 2, MaxArgs: 2, Run: runPRTaskCreate},
			{Name: "complete", Short: "Complete (resolve) a task",
				Flags: selectorFlags(), MinArgs: 2, MaxArgs: 2, Run: runPRTaskComplete},
			{Name: "reopen", Short: "Reopen a task",
				Flags: selectorFlags(), MinArgs: 2, MaxArgs: 2, Run: runPRTaskReopen},
		},
	}
}

var taskSchema = []axi.Field{
	{Key: "id", Extractor: axi.Pluck("id")},
	{Key: "state", Extractor: axi.Pluck("state")},
	{Key: "text", Extractor: axi.Pluck("text")},
	{Key: "author", Extractor: axi.Pluck("author")},
}

func runPRTaskList(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --project) or set a context")
	}
	id, idErr := parseID(ctx.Args[0])
	if idErr != nil {
		return axi.UsageError(fmt.Sprintf("invalid pull request id %q: %s", ctx.Args[0], idErr))
	}
	tasks, err := client.ListPRTasks(context.Background(), scope, id)
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		emitEmpty(ctx, "tasks", fmt.Sprintf("0 tasks on pull request #%d", id), []string{
			fmt.Sprintf("Run `bkt-axi pr task create %d \"<text>\"` to add a task", id),
		})
		return nil
	}
	emitList(ctx, "tasks", toAny(tasks), taskSchema, len(tasks), nil)
	return nil
}

func runPRTaskCreate(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --project) or set a context")
	}
	id, idErr := parseID(ctx.Args[0])
	if idErr != nil {
		return axi.UsageError(fmt.Sprintf("invalid pull request id %q: %s", ctx.Args[0], idErr))
	}
	task, err := client.CreatePRTask(context.Background(), scope, id, ctx.Args[1])
	if err != nil {
		return err
	}
	emitDetail(ctx, "task", *task, taskSchema, nil)
	return nil
}

func runPRTaskComplete(ctx *app.Context) error {
	return runPRTaskState(ctx, true)
}

func runPRTaskReopen(ctx *app.Context) error {
	return runPRTaskState(ctx, false)
}

func runPRTaskState(ctx *app.Context, resolve bool) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --project) or set a context")
	}
	id, idErr := parseID(ctx.Args[0])
	if idErr != nil {
		return axi.UsageError(fmt.Sprintf("invalid pull request id %q: %s", ctx.Args[0], idErr))
	}
	taskID, tErr := parseID(ctx.Args[1])
	if tErr != nil {
		return axi.UsageError(fmt.Sprintf("invalid task id %q: %s", ctx.Args[1], tErr))
	}
	var (
		task    *bitbucket.PullRequestTask
		changed bool
	)
	if resolve {
		task, changed, err = client.CompletePRTask(context.Background(), scope, id, taskID)
	} else {
		task, changed, err = client.ReopenPRTask(context.Background(), scope, id, taskID)
	}
	if err != nil {
		return err
	}
	verb := "completed"
	target := "resolved"
	if !resolve {
		verb = "reopened"
		target = "open"
	}
	if !changed {
		emitConfirmation(ctx, fmt.Sprintf("task #%d already %s (no-op)", taskID, target))
		return nil
	}
	emitDetail(ctx, "task", *task, taskSchema, nil)
	_ = verb
	return nil
}

// --- pr suggestions (DC) ------------------------------------------------

func newPRSuggestionsCmd() *app.Command {
	return &app.Command{
		Name:  "suggestions",
		Short: "Manage code suggestions (Data Center)",
		Long:  "List and apply inline code suggestions on a pull request (Data Center). Apply is idempotent.",
		Children: []*app.Command{
			{Name: "list", Aliases: []string{"ls"}, Short: "List suggestions",
				Flags: selectorFlags(), MinArgs: 1, MaxArgs: 1, Run: runPRSuggestionsList},
			{Name: "apply", Short: "Apply a suggestion",
				Flags: selectorFlags(), MinArgs: 3, MaxArgs: 3, Run: runPRSuggestionsApply},
		},
	}
}

var suggestionSchema = []axi.Field{
	{Key: "id", Extractor: axi.Pluck("id")},
	{Key: "comment_id", Extractor: axi.Pluck("comment_id")},
	{Key: "applied", Extractor: axi.BoolYesNo(axi.Pluck("applied"))},
	{Key: "text", Extractor: axi.Pluck("text")},
}

func runPRSuggestionsList(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --project) or set a context")
	}
	id, idErr := parseID(ctx.Args[0])
	if idErr != nil {
		return axi.UsageError(fmt.Sprintf("invalid pull request id %q: %s", ctx.Args[0], idErr))
	}
	sugs, err := client.ListPRSuggestions(context.Background(), scope, id)
	if err != nil {
		return err
	}
	if len(sugs) == 0 {
		emitEmpty(ctx, "suggestions", fmt.Sprintf("0 suggestions on pull request #%d", id), nil)
		return nil
	}
	emitList(ctx, "suggestions", toAny(sugs), suggestionSchema, len(sugs), nil)
	return nil
}

func runPRSuggestionsApply(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --project) or set a context")
	}
	prID, pErr := parseID(ctx.Args[0])
	if pErr != nil {
		return axi.UsageError(fmt.Sprintf("invalid pull request id %q: %s", ctx.Args[0], pErr))
	}
	commentID, cErr := parseID(ctx.Args[1])
	if cErr != nil {
		return axi.UsageError(fmt.Sprintf("invalid comment id %q: %s", ctx.Args[1], cErr))
	}
	suggestionID, sErr := parseID(ctx.Args[2])
	if sErr != nil {
		return axi.UsageError(fmt.Sprintf("invalid suggestion id %q: %s", ctx.Args[2], sErr))
	}
	changed, err := client.ApplyPRSuggestion(context.Background(), scope, prID, commentID, suggestionID)
	if err != nil {
		return err
	}
	if !changed {
		emitConfirmation(ctx, fmt.Sprintf("suggestion #%d already applied (no-op)", suggestionID))
		return nil
	}
	emitConfirmation(ctx, fmt.Sprintf("applied suggestion #%d on pull request #%d", suggestionID, prID))
	return nil
}

// --- pr checks (cross-platform) ----------------------------------------

func newPRChecksCmd() *app.Command {
	return &app.Command{
		Name:    "checks",
		Short:   "Show CI/build status for a pull request",
		Long:    "Show build/CI statuses for a pull request's head commit (Data Center build-status or Cloud commit-status).",
		Flags:   selectorFlags(),
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{{Cmd: "bkt-axi pr checks 42", What: "statuses for PR #42's head commit"}},
		Run:      runPRChecks,
	}
}

func runPRChecks(ctx *app.Context) error {
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
	statuses, err := client.PRChecks(context.Background(), scope, id)
	if err != nil {
		return err
	}
	items := make([]any, len(statuses))
	for i := range statuses {
		items[i] = statuses[i]
	}
	return emitStatuses(ctx, items, fmt.Sprintf("pull request #%d", id))
}

func newPRListCmd() *app.Command {
	flags := append(app.FlagSet{
		{Name: "state", Type: app.FlagString, Default: "open", Desc: "Filter by state: open, merged, declined, all"},
		{Name: "mine", Type: app.FlagBool, Default: false, Desc: "Only pull requests you authored"},
		{Name: "reviewer", Type: app.FlagString, Default: "", Desc: "Only pull requests needing review by a user (identity)"},
		{Name: "limit", Type: app.FlagInt, Default: 50, Desc: "Maximum number of pull requests to show (1-100)"},
		{Name: "fields", Type: app.FlagString, Default: "", Desc: "Extra columns (comma-sep): author,branch,target,draft,url,reviewers,created,checks"},
	}, selectorFlags()...)
	return &app.Command{
		Name:    "list",
		Aliases: []string{"ls"},
		Short:   "List pull requests in the current repository",
		Long:    "List pull requests for the resolved repository. The default schema is {id,title,state,review}; use --fields to add columns.",
		Flags:   flags,
		MinArgs: 0, MaxArgs: 0,
		Examples: []app.Example{
			{Cmd: "bkt-axi pr list", What: "open pull requests in the current repo"},
			{Cmd: "bkt-axi pr list --mine", What: "only pull requests you authored"},
			{Cmd: "bkt-axi pr list --fields author,branch --state all", What: "all PRs with author and source branch"},
		},
		Run: runPRList,
	}
}

func newPRViewCmd() *app.Command {
	flags := append(app.FlagSet{
		{Name: "full", Type: app.FlagBool, Default: false, Desc: "Show the full, untruncated description"},
		{Name: "comments", Type: app.FlagBool, Default: false, Desc: "Include the comment thread"},
	}, selectorFlags()...)
	return &app.Command{
		Name:    "view",
		Short:   "Show details for a pull request",
		Long:    "Display a pull request's full state with a truncated description (use --full for the complete body and --comments for the thread).",
		Flags:   flags,
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{
			{Cmd: "bkt-axi pr view 42", What: "details for pull request #42"},
			{Cmd: "bkt-axi pr view 42 --full", What: "include the complete description"},
			{Cmd: "bkt-axi pr view 42 --comments", What: "include the comment thread"},
		},
		Run: runPRView,
	}
}

// prExtraFields maps a --fields token to its schema column. The keys are the
// only tokens accepted; anything else is rejected with exit 2.
var prExtraFields = map[string]axi.Field{
	"author":    {Key: "author", Extractor: axi.Pluck("author")},
	"branch":    {Key: "branch", Extractor: axi.Pluck("from")},
	"target":    {Key: "target", Extractor: axi.Pluck("to")},
	"draft":     {Key: "draft", Extractor: axi.Pluck("draft")},
	"url":       {Key: "url", Extractor: axi.Pluck("url")},
	"reviewers": {Key: "reviewers", Extractor: axi.JoinArray(axi.Pluck("reviewers"), " ")},
	"created":   {Key: "created", Extractor: axi.RelativeTime(axi.Pluck("created_at"))},
	"checks":    {Key: "checks", Extractor: axi.Pluck("checks")},
}

func prListSchema(extra []string) ([]axi.Field, error) {
	schema := []axi.Field{
		{Key: "id", Extractor: axi.Pluck("id")},
		{Key: "title", Extractor: axi.Pluck("title")},
		{Key: "state", Extractor: axi.Pluck("state")},
		{Key: "review", Extractor: axi.Pluck("review")},
	}
	seen := map[string]bool{"id": true, "title": true, "state": true, "review": true}
	for _, name := range extra {
		name = strings.TrimSpace(strings.ToLower(name))
		if name == "" {
			continue
		}
		if seen[name] {
			continue
		}
		f, ok := prExtraFields[name]
		if !ok {
			e := axi.UsageError(fmt.Sprintf("unknown --fields value `%s` for `pr list`", name))
			e.Suggestions = []string{"allowed --fields values: author, branch, target, draft, url, reviewers, created, checks"}
			return nil, e
		}
		schema = append(schema, f)
		seen[name] = true
	}
	return schema, nil
}

func runPRList(ctx *app.Context) error {
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

	var extras []string
	if raw := strings.TrimSpace(ctx.Flags.String("fields")); raw != "" {
		extras = strings.Split(raw, ",")
	}
	schema, err := prListSchema(extras)
	if err != nil {
		return err
	}

	opts := bitbucket.PRListOptions{
		State:    ctx.Flags.String("state"),
		Reviewer: strings.TrimSpace(ctx.Flags.String("reviewer")),
		Limit:    ctx.Flags.Int("limit"),
	}
	if ctx.Flags.Bool("mine") {
		id, _, idErr := client.CurrentUser(context.Background())
		if idErr != nil {
			return idErr
		}
		opts.Mine = id
	}

	result, err := client.ListPRs(context.Background(), scope, opts)
	if err != nil {
		return err
	}

	stateWord := strings.ToLower(strings.TrimSpace(ctx.Flags.String("state")))
	if stateWord == "" {
		stateWord = "open"
	}
	scopeStr := scope.String()

	// Definitive empty state (AXI §5).
	if len(result.PRs) == 0 {
		msg := fmt.Sprintf("0 %s pull requests in %s", stateWord, scopeStr)
		doc := axi.NewObject(
			axi.KV{Key: "pull_requests", Value: msg},
			axi.KV{Key: "help", Value: axi.HelpRows(axi.EmptyPRList())},
		)
		payload := map[string]any{"pull_requests": msg}
		emit(ctx, payload, axi.Marshal(doc))
		return nil
	}

	count := any(result.Shown) // bare int renders as `count: 2`
	if result.MoreAvailable {
		count = fmt.Sprintf("%d shown (more available)", result.Shown)
	}

	items := toAny(result.PRs)
	doc := axi.NewObject(
		axi.KV{Key: "count", Value: count},
		axi.KV{Key: "pull_requests", Value: axi.Rows(items, schema)},
		axi.KV{Key: "help", Value: axi.HelpRows(axi.AfterPRList(result.MoreAvailable))},
	)
	payload := listPayload(count, items, schema, axi.AfterPRList(result.MoreAvailable))
	emit(ctx, payload, axi.Marshal(doc))
	return nil
}

func runPRView(ctx *app.Context) error {
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

	pr, err := client.GetPR(context.Background(), scope, id)
	if err != nil {
		return err
	}

	full := ctx.Flags.Bool("full")
	desc := pr.Description
	if !full {
		desc = axi.TruncateBody(desc, axi.DefaultBodyBudget)
	}

	// Detail schema: the full normalized model in a stable order.
	schema := []axi.Field{
		{Key: "id", Extractor: axi.Pluck("id")},
		{Key: "title", Extractor: axi.Pluck("title")},
		{Key: "state", Extractor: axi.Pluck("state")},
		{Key: "draft", Extractor: axi.Pluck("draft")},
		{Key: "author", Extractor: axi.Pluck("author")},
		{Key: "from", Extractor: axi.Pluck("from")},
		{Key: "to", Extractor: axi.Pluck("to")},
		{Key: "review", Extractor: axi.Pluck("review")},
		{Key: "checks", Extractor: axi.Pluck("checks")},
		{Key: "description", Extractor: axi.Const(desc)},
		{Key: "url", Extractor: axi.Pluck("url")},
		{Key: "created", Extractor: axi.RelativeTime(axi.Pluck("created_at"))},
		{Key: "reviewers", Extractor: axi.JoinArray(axi.Pluck("reviewers"), " ")},
	}
	help := []string(nil)
	if !full && prWasTruncated(pr.Description) {
		help = append(help, fmt.Sprintf("Run `bkt-axi pr view %d --full` to see the complete description", id))
	}

	fields := []axi.KV{
		{Key: "pull_request", Value: axi.NewObject(axi.Ordered(*pr, schema)...)},
	}

	var comments []bitbucket.Comment
	if ctx.Flags.Bool("comments") {
		comments, err = client.ListComments(context.Background(), scope, id)
		if err != nil {
			return err
		}
		fields = append(fields, axi.KV{Key: "comments", Value: commentRows(comments)})
	}
	if len(help) > 0 {
		fields = append(fields, axi.KV{Key: "help", Value: axi.HelpRows(help)})
	}

	doc := axi.NewObject(fields...)
	payload := detailPayload(pr, desc, comments, help)
	emit(ctx, payload, axi.Marshal(doc))
	return nil
}

func commentRows(comments []bitbucket.Comment) []axi.Object {
	out := make([]axi.Object, 0, len(comments))
	schema := []axi.Field{
		{Key: "id", Extractor: axi.Pluck("id")},
		{Key: "author", Extractor: axi.Pluck("author")},
		{Key: "state", Extractor: axi.Pluck("state")},
		{Key: "text", Extractor: axi.Pluck("text")},
	}
	for i := range comments {
		out = append(out, axi.NewObject(axi.Ordered(comments[i], schema)...))
	}
	return out
}

func prWasTruncated(s string) bool {
	return len(strings.TrimSpace(s)) > axi.DefaultBodyBudget
}

func parseID(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("must be a positive integer")
	}
	return n, nil
}

// listPayload builds the JSON/YAML payload mirroring the TOON list doc.
func listPayload(count any, items []any, schema []axi.Field, help []string) map[string]any {
	rows := make([]map[string]any, 0, len(items))
	for _, it := range items {
		rows = append(rows, axi.Extract(it, schema))
	}
	out := map[string]any{"count": count, "pull_requests": rows}
	if len(help) > 0 {
		out["help"] = help
	}
	return out
}

// detailPayload builds the JSON/YAML payload mirroring the TOON detail doc.
func detailPayload(pr *bitbucket.PR, desc string, comments []bitbucket.Comment, help []string) map[string]any {
	detail := map[string]any{
		"id":          pr.ID,
		"title":       pr.Title,
		"state":       pr.State,
		"draft":       pr.Draft,
		"author":      pr.Author,
		"from":        pr.From,
		"to":          pr.To,
		"review":      pr.Review,
		"checks":      pr.Checks,
		"description": desc,
		"url":         pr.URL,
		"created_at":  pr.CreatedAt.Format(time.RFC3339),
		"reviewers":   pr.Reviewers,
	}
	out := map[string]any{"pull_request": detail}
	if comments != nil {
		out["comments"] = comments
	}
	if len(help) > 0 {
		out["help"] = help
	}
	return out
}
