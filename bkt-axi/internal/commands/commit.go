package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruttybob/bkt-axi/internal/app"
	"github.com/ruttybob/bkt-axi/internal/axi"
)

// commit.go implements the Phase 1 commit commands: `commit view`, `commit
// diff`, and `commit status`. The cloud/dc switch lives entirely inside the
// adapter.

// commitDiffBudget is the tail-preview size for `commit diff` without --full.
const commitDiffBudget = 8000

// NewCommitCmd builds the `commit` noun and its Phase 1 verbs.
func NewCommitCmd() *app.Command {
	return &app.Command{
		Name:  "commit",
		Short: "Work with commits",
		Long:  "Inspect commits, compare them, and read build statuses across Bitbucket Cloud and Data Center.",
		Children: []*app.Command{
			newCommitViewCmd(),
			newCommitDiffCmd(),
			newCommitStatusCmd(),
		},
	}
}

func newCommitViewCmd() *app.Command {
	flags := append(app.FlagSet{
		{Name: "full", Type: app.FlagBool, Default: false, Desc: "Show the full, untruncated message"},
		{Name: "fields", Type: app.FlagString, Default: "", Desc: "Extra columns (comma-sep): parents,author_email,committed"},
	}, selectorFlags()...)
	return &app.Command{
		Name:    "view",
		Short:   "Show details for a commit",
		Long:    "Display a commit's full state with a truncated message (use --full for the complete message).",
		Flags:   flags,
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{
			{Cmd: "bkt-axi commit view abc1234", What: "details for the commit"},
			{Cmd: "bkt-axi commit view abc1234 --full", What: "include the complete commit message"},
		},
		Run: runCommitView,
	}
}

func newCommitDiffCmd() *app.Command {
	flags := append(app.FlagSet{
		{Name: "full", Type: app.FlagBool, Default: false, Desc: "Write the complete diff to a temp file and print its path"},
	}, selectorFlags()...)
	return &app.Command{
		Name:    "diff",
		Short:   "Show the diff between two commits",
		Long:    "Stream the raw diff between two refs (SHAs, branches, or tags). Without --full the tail is shown; --full writes the complete diff to a temp file and prints its path.",
		Flags:   flags,
		MinArgs: 2, MaxArgs: 2,
		Examples: []app.Example{
			{Cmd: "bkt-axi commit diff feature/x main", What: "diff between two branches"},
			{Cmd: "bkt-axi commit diff abc1234 def5678 --full", What: "write the complete diff to a temp file"},
		},
		Run: runCommitDiff,
	}
}

func newCommitStatusCmd() *app.Command {
	return &app.Command{
		Name:    "status",
		Short:   "Show build statuses for a commit",
		Long:    "List the CI build statuses reported against a commit. Works on both Cloud and Data Center.",
		Flags:   selectorFlags(),
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{
			{Cmd: "bkt-axi commit status abc1234", What: "build statuses for the commit"},
		},
		Run: runCommitStatus,
	}
}

// commitViewExtraFields maps a --fields token to its schema column.
var commitViewExtraFields = map[string]axi.Field{
	"parents":      {Key: "parents", Extractor: axi.JoinArray(axi.Pluck("parents"), " ")},
	"author_email": {Key: "author_email", Extractor: axi.Pluck("author_email")},
	"committed":    {Key: "committed_at", Extractor: axi.RelativeTime(axi.Pluck("committed_at"))},
}

func runCommitView(ctx *app.Context) error {
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

	sha := strings.TrimSpace(ctx.Args[0])
	commit, err := client.GetCommit(context.Background(), scope, sha)
	if err != nil {
		return err
	}

	full := ctx.Flags.Bool("full")
	message := commit.Message
	if !full {
		message = axi.TruncateBody(message, axi.DefaultBodyBudget)
	}

	schema := []axi.Field{
		{Key: "sha", Extractor: axi.Pluck("sha")},
		{Key: "message", Extractor: axi.Const(message)},
		{Key: "author", Extractor: axi.Pluck("author")},
		{Key: "authored", Extractor: axi.RelativeTime(axi.Pluck("authored_at"))},
		{Key: "url", Extractor: axi.Pluck("url")},
	}
	// Extend the base schema with any requested extras (changes field order so
	// extras always appear after the core fields).
	extended, err := extendSchema(schema, commitViewExtraFields, parseFields(ctx.Flags.String("fields")), "commit view")
	if err != nil {
		return err
	}

	help := []string(nil)
	if !full && commitWasTruncated(commit.Message) {
		help = append(help, fmt.Sprintf("Run `bkt-axi commit view %s --full` to see the complete message", shortSHA(sha)))
	}

	fields := []axi.KV{{Key: "commit", Value: axi.NewObject(axi.Ordered(*commit, extended)...)}}
	// Render the truncated message in place of the full one.
	if len(help) > 0 {
		fields = append(fields, axi.KV{Key: "help", Value: axi.HelpRows(help)})
	}

	// The detail payload for JSON/YAML uses the truncated message and raw
	// machine timestamps (TOON shows humanized relative time).
	detail := detailExtracted(commit, extended)
	detail["message"] = message
	detail["authored"] = rfc3339(commit.AuthoredAt)
	detail["committed"] = rfc3339(commit.CommittedAt)
	payload := map[string]any{"commit": detail}
	if len(help) > 0 {
		payload["help"] = help
	}
	emit(ctx, payload, axi.Marshal(axi.NewObject(fields...)))
	return nil
}

func runCommitDiff(ctx *app.Context) error {
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

	from := strings.TrimSpace(ctx.Args[0])
	to := strings.TrimSpace(ctx.Args[1])
	diff, err := client.CommitDiff(context.Background(), scope, from, to)
	if err != nil {
		return err
	}

	full := ctx.Flags.Bool("full")
	help := []string(nil)
	var diffValue, fullPath string

	if full {
		path, werr := writeTempOutput("bkt-axi-diff-*.diff", diff)
		if werr != nil {
			return axi.Errorf("failed to write diff to temp file: %s", werr)
		}
		fullPath = path
		diffValue = diff
		help = append(help, "Read the complete output: `cat "+fullPath+"`")
	} else {
		diffValue = axi.TruncateTail(diff, commitDiffBudget)
		if axi.ExceedsBudget(diff, commitDiffBudget) {
			help = append(help, fmt.Sprintf("Run `bkt-axi commit diff %s %s --full` to write the complete diff to a file", from, to))
		}
	}

	docFields := []axi.KV{
		{Key: "from", Value: from},
		{Key: "to", Value: to},
		{Key: "diff", Value: diffValue},
	}
	if fullPath != "" {
		docFields = append(docFields, axi.KV{Key: "full_path", Value: fullPath})
	}
	if len(help) > 0 {
		docFields = append(docFields, axi.KV{Key: "help", Value: axi.HelpRows(help)})
	}

	payload := map[string]any{
		"from": from,
		"to":   to,
		"diff": diffValue,
	}
	if fullPath != "" {
		payload["full_path"] = fullPath
	}
	if len(help) > 0 {
		payload["help"] = help
	}
	emit(ctx, payload, axi.Marshal(axi.NewObject(docFields...)))
	return nil
}

func runCommitStatus(ctx *app.Context) error {
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

	sha := strings.TrimSpace(ctx.Args[0])
	statuses, err := client.CommitStatuses(context.Background(), scope, sha)
	if err != nil {
		return err
	}

	if len(statuses) == 0 {
		msg := fmt.Sprintf("0 build statuses for commit %s", shortSHA(sha))
		doc := axi.NewObject(
			axi.KV{Key: "statuses", Value: msg},
			axi.KV{Key: "help", Value: axi.HelpRows(axi.EmptyCommitStatus())},
		)
		emit(ctx, map[string]any{"statuses": msg}, axi.Marshal(doc))
		return nil
	}

	schema := []axi.Field{
		{Key: "state", Extractor: axi.Pluck("state")},
		{Key: "key", Extractor: axi.Pluck("key")},
		{Key: "name", Extractor: axi.Pluck("name")},
		{Key: "url", Extractor: axi.Pluck("url")},
		{Key: "description", Extractor: axi.Pluck("description")},
	}
	items := toAny(statuses)
	count := len(statuses)
	doc := axi.NewObject(
		axi.KV{Key: "count", Value: count},
		axi.KV{Key: "statuses", Value: axi.Rows(items, schema)},
	)
	emit(ctx, listPayloadRows("statuses", count, items, schema, nil), axi.Marshal(doc))
	return nil
}

func commitWasTruncated(s string) bool {
	return len(strings.TrimSpace(s)) > axi.DefaultBodyBudget
}

// shortSHA keeps at most 12 characters of a SHA for compact hint display.
func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
