package commands

import (
	"context"
	"fmt"
	"io"

	"github.com/ruttybob/bkt-axi/internal/app"
	"github.com/ruttybob/bkt-axi/internal/axi"
	"github.com/ruttybob/bkt-axi/internal/bitbucket"
)

// branch.go implements the Phase 1 branch command: `branch list`. Branch
// create/delete are Data Center only and ship in Phase 2 (mutations).

// NewBranchCmd builds the `branch` noun and its Phase 1 verbs.
func NewBranchCmd() *app.Command {
	return &app.Command{
		Name:  "branch",
		Short: "Work with branches",
		Long:  "List and inspect branches across Bitbucket Cloud and Data Center.",
		Children: []*app.Command{
			newBranchListCmd(),
		},
	}
}

func newBranchListCmd() *app.Command {
	flags := append(app.FlagSet{
		{Name: "filter", Type: app.FlagString, Default: "", Desc: "Only branches whose name contains this text"},
		{Name: "limit", Type: app.FlagInt, Default: 100, Desc: "Maximum number of branches to show (1-100)"},
		{Name: "fields", Type: app.FlagString, Default: "", Desc: "Extra columns (comma-sep): metadata,message,author,updated"},
		{Name: "text", Type: app.FlagBool, Default: false, Desc: "Plain-text output: one branch name per line (pipe-friendly)"},
	}, selectorFlags()...)
	return &app.Command{
		Name:    "list",
		Aliases: []string{"ls"},
		Short:   "List branches in the resolved repository",
		Long:    "List branches for the resolved repository. The default schema is {name,default,latest_commit}; use --fields to add columns. `metadata`, `message`, `author`, and `updated` are commit-derived and cost one extra request per branch.",
		Flags:   flags,
		MinArgs: 0, MaxArgs: 0,
		Examples: []app.Example{
			{Cmd: "bkt-axi branch list", What: "branches in the resolved repository"},
			{Cmd: "bkt-axi branch list --text", What: "branch names only, one per line (pipe-friendly)"},
			{Cmd: "bkt-axi branch list --fields message,author", What: "add the head commit's message and author"},
		},
		Run: runBranchList,
	}
}

// branchExtraFields maps a --fields token to its schema column.
var branchExtraFields = map[string]axi.Field{
	"metadata": {Key: "metadata", Extractor: axi.Custom(branchMetadata)},
	"message":  {Key: "message", Extractor: axi.Pluck("message")},
	"author":   {Key: "author", Extractor: axi.Pluck("author")},
	"updated":  {Key: "updated", Extractor: axi.RelativeTime(axi.Pluck("updated"))},
}

// branchMetadata renders a compact line-native descriptor for a branch: the
// ref type (e.g. "BRANCH" on DC, "commit" on Cloud), so the column is honest
// without pretending line-native fields exist where they do not.
func branchMetadata(item any) any {
	b, ok := item.(bitbucket.Branch)
	if !ok {
		return ""
	}
	return b.Type
}

func branchListSchema(extra []string) ([]axi.Field, error) {
	schema := []axi.Field{
		{Key: "name", Extractor: axi.Pluck("name")},
		{Key: "default", Extractor: axi.BoolYesNo(axi.Pluck("default"))},
		{Key: "latest_commit", Extractor: axi.Pluck("latest_commit")},
	}
	return extendSchema(schema, branchExtraFields, extra, "branch list")
}

func runBranchList(ctx *app.Context) error {
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

	extras := parseFields(ctx.Flags.String("fields"))
	// message/author/updated are commit-derived; opt into the per-branch fetch.
	withCommitDetail := containsAny(extras, "message", "author", "updated")

	schema, err := branchListSchema(extras)
	if err != nil {
		return err
	}

	result, err := client.ListBranches(context.Background(), scope, bitbucket.BranchListOptions{
		Filter:           ctx.Flags.String("filter"),
		Limit:            ctx.Flags.Int("limit"),
		WithCommitDetail: withCommitDetail,
	})
	if err != nil {
		return err
	}

	// --text: one branch name per line, no schema/count/help. Plain and pipe-friendly.
	if ctx.Flags.Bool("text") {
		emitBranchText(ctx, result.Branches)
		return nil
	}

	if len(result.Branches) == 0 {
		msg := fmt.Sprintf("0 branches in %s", scope.String())
		doc := axi.NewObject(
			axi.KV{Key: "branches", Value: msg},
			axi.KV{Key: "help", Value: axi.HelpRows(axi.EmptyBranchList())},
		)
		emit(ctx, map[string]any{"branches": msg}, axi.Marshal(doc))
		return nil
	}

	items := toAny(result.Branches)
	count := countLine(result.Shown, 0, result.MoreAvailable)
	doc := axi.NewObject(
		axi.KV{Key: "count", Value: count},
		axi.KV{Key: "branches", Value: axi.Rows(items, schema)},
		axi.KV{Key: "help", Value: axi.HelpRows(axi.AfterBranchList(result.MoreAvailable))},
	)
	payload := listPayloadRows("branches", count, items, schema, axi.AfterBranchList(result.MoreAvailable))
	emit(ctx, payload, axi.Marshal(doc))
	return nil
}

func emitBranchText(ctx *app.Context, branches []bitbucket.Branch) {
	w := ctx.Out()
	for i := range branches {
		io.WriteString(w, branches[i].Name+"\n")
	}
}

// containsAny reports whether haystack contains any of needles.
func containsAny(haystack []string, needles ...string) bool {
	set := make(map[string]bool, len(haystack))
	for _, h := range haystack {
		set[h] = true
	}
	for _, n := range needles {
		if set[n] {
			return true
		}
	}
	return false
}
