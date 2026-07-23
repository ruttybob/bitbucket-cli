package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruttybob/bkt-axi/internal/app"
	"github.com/ruttybob/bkt-axi/internal/axi"
)

// status.go rolls up CI/build status and rate-limit telemetry. Per the Phase 3
// spec: commit and PR build statuses target Data Center, pipelines target
// Cloud, and rate-limit telemetry is reported on both.

// NewStatusCmd builds the `status` noun.
func NewStatusCmd() *app.Command {
	return &app.Command{
		Name:  "status",
		Short: "CI/build status and rate-limit rollup",
		Long:  "Inspect commit and pull-request build statuses (Data Center), pipeline runs (Cloud), and rate-limit telemetry (both).",
		Children: []*app.Command{
			newStatusCommitCmd(),
			newStatusPRCmd(),
			newStatusPipelineCmd(),
			newStatusRateLimitCmd(),
		},
	}
}

var buildStatusSchema = []axi.Field{
	{Key: "state", Extractor: axi.Pluck("state")},
	{Key: "key", Extractor: axi.Pluck("key")},
	{Key: "name", Extractor: axi.Pluck("name")},
	{Key: "url", Extractor: axi.Pluck("url")},
}

func newStatusCommitCmd() *app.Command {
	return &app.Command{
		Name:    "commit",
		Short:   "Build statuses for a commit (Data Center)",
		Long:    "Show build statuses for a commit on Bitbucket Data Center.",
		Flags:   selectorFlags(),
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{{Cmd: "bkt-axi status commit abc123", What: "statuses for commit abc123"}},
		Run:      runStatusCommit,
	}
}

func runStatusCommit(ctx *app.Context) error {
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
	if sha == "" {
		return axi.UsageError("`status commit` requires a commit sha")
	}
	statuses, err := client.CommitStatuses(context.Background(), scope, sha)
	if err != nil {
		return err
	}
	return emitStatuses(ctx, toAny(statuses), fmt.Sprintf("commit %s", sha))
}

func newStatusPRCmd() *app.Command {
	return &app.Command{
		Name:    "pr",
		Short:   "Build statuses for a pull request's head commit (Data Center)",
		Long:    "Show build statuses for a pull request's head commit on Bitbucket Data Center.",
		Flags:   selectorFlags(),
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{{Cmd: "bkt-axi status pr 42", What: "statuses for PR #42's head commit"}},
		Run:      runStatusPR,
	}
}

func runStatusPR(ctx *app.Context) error {
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
	statuses, err := client.PRHeadStatuses(context.Background(), scope, id)
	if err != nil {
		return err
	}
	return emitStatuses(ctx, toAny(statuses), fmt.Sprintf("pull request #%d", id))
}

func emitStatuses(ctx *app.Context, statuses []any, noun string) error {
	if len(statuses) == 0 {
		emitEmpty(ctx, "statuses", fmt.Sprintf("0 build statuses for %s", noun), []string{
			"Builds may not have reported yet; retry shortly",
		})
		return nil
	}
	emitList(ctx, "statuses", statuses, buildStatusSchema, len(statuses), nil)
	return nil
}

func newStatusPipelineCmd() *app.Command {
	return &app.Command{
		Name:    "pipeline",
		Short:   "Show a Cloud pipeline run",
		Long:    "Show the state of a Bitbucket Cloud pipeline run by build number.",
		Flags:   selectorFlags(),
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{{Cmd: "bkt-axi status pipeline 42", What: "state of pipeline build #42"}},
		Run:      runStatusPipeline,
	}
}

func runStatusPipeline(ctx *app.Context) error {
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
	uuid := strings.TrimSpace(ctx.Args[0])
	if uuid == "" {
		return axi.UsageError("`status pipeline` requires a pipeline build number")
	}
	build, bErr := parseID(uuid)
	if bErr != nil {
		return axi.UsageError(fmt.Sprintf("invalid pipeline build number %q: %s", ctx.Args[0], bErr))
	}
	run, err := client.GetPipeline(context.Background(), scope, build)
	if err != nil {
		return err
	}
	schema := []axi.Field{
		{Key: "build", Extractor: axi.Pluck("build")},
		{Key: "uuid", Extractor: axi.Pluck("uuid")},
		{Key: "state", Extractor: axi.Pluck("state")},
		{Key: "result", Extractor: axi.Pluck("result")},
		{Key: "ref", Extractor: axi.Pluck("ref")},
		{Key: "duration", Extractor: axi.Pluck("duration")},
		{Key: "created_at", Extractor: axi.RelativeTime(axi.Pluck("created_at"))},
	}
	emitDetail(ctx, "pipeline", *run, schema, nil)
	return nil
}

func newStatusRateLimitCmd() *app.Command {
	return &app.Command{
		Name:    "rate-limit",
		Aliases: []string{"ratelimit"},
		Short:   "Show rate-limit telemetry",
		Long:    "Show the last observed rate-limit telemetry for the active host (both platforms). Cloud derives this from response headers; there is no dedicated endpoint.",
		Flags:   selectorFlags(),
		MinArgs: 0, MaxArgs: 0,
		Examples: []app.Example{{Cmd: "bkt-axi status rate-limit", What: "current rate-limit headroom"}},
		Run:      runStatusRateLimit,
	}
}

func runStatusRateLimit(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	rl, err := client.RateLimit(context.Background())
	if err != nil {
		return err
	}
	if rl.Limit == 0 && rl.Remaining == 0 {
		emitEmpty(ctx, "rate_limit", "no rate-limit headers observed for this host", []string{
			"Rate-limit telemetry is populated from response headers; run a data command first",
		})
		return nil
	}
	schema := []axi.Field{
		{Key: "limit", Extractor: axi.Pluck("limit")},
		{Key: "remaining", Extractor: axi.Pluck("remaining")},
		{Key: "reset", Extractor: axi.Pluck("reset")},
		{Key: "source", Extractor: axi.Pluck("source")},
	}
	emitDetail(ctx, "rate_limit", *rl, schema, nil)
	return nil
}
