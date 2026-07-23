package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruttybob/bkt-axi/internal/app"
	"github.com/ruttybob/bkt-axi/internal/axi"
	"github.com/ruttybob/bkt-axi/internal/bitbucket"
)

// pipeline.go implements the Phase 1 pipeline commands (Cloud only): `pipeline
// list` and `pipeline view`. Pipelines are a Bitbucket Cloud concept; running
// either verb against a Data Center host returns a clear Cloud-only error
// emitted by the adapter.

// pipelineLogBudget is the tail-preview size for pipeline logs without --full.
const pipelineLogBudget = 20000

// NewPipelineCmd builds the `pipeline` noun and its Phase 1 verbs.
func NewPipelineCmd() *app.Command {
	return &app.Command{
		Name:  "pipeline",
		Short: "Work with pipelines (Bitbucket Cloud)",
		Long:  "List and inspect Bitbucket Cloud pipelines. Pipelines are a Cloud-only feature.",
		Children: []*app.Command{
			newPipelineListCmd(),
			newPipelineViewCmd(),
		},
	}
}

func newPipelineListCmd() *app.Command {
	flags := append(app.FlagSet{
		{Name: "limit", Type: app.FlagInt, Default: 25, Desc: "Maximum number of pipelines to show (1-100)"},
		{Name: "fields", Type: app.FlagString, Default: "", Desc: "Extra columns (comma-sep): trigger,duration,steps"},
	}, selectorFlags()...)
	return &app.Command{
		Name:    "list",
		Aliases: []string{"ls"},
		Short:   "List recent pipelines (Bitbucket Cloud)",
		Long:    "List recent pipeline runs for the resolved repository. The default schema is {build,state,ref,created}; use --fields to add columns. `steps` fetches a per-run summary (one extra request per pipeline).",
		Flags:   flags,
		MinArgs: 0, MaxArgs: 0,
		Examples: []app.Example{
			{Cmd: "bkt-axi pipeline list", What: "recent pipelines in the resolved repository"},
			{Cmd: "bkt-axi pipeline list --fields trigger,duration", What: "add trigger and duration columns"},
		},
		Run: runPipelineList,
	}
}

func newPipelineViewCmd() *app.Command {
	flags := append(app.FlagSet{
		{Name: "steps", Type: app.FlagBool, Default: false, Desc: "Include the step list"},
		{Name: "logs", Type: app.FlagBool, Default: false, Desc: "Include concatenated step logs (tail-truncated)"},
		{Name: "log-failed", Type: app.FlagBool, Default: false, Desc: "Only include logs from failed steps"},
		{Name: "full", Type: app.FlagBool, Default: false, Desc: "Do not truncate logs; write them to a temp file and print its path"},
	}, selectorFlags()...)
	return &app.Command{
		Name:    "view",
		Short:   "Show details for a pipeline (Bitbucket Cloud)",
		Long:    "Display a pipeline's full state. --steps lists its steps; --logs/--log-failed append concatenated step logs (tail-truncated unless --full writes them to a temp file).",
		Flags:   flags,
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{
			{Cmd: "bkt-axi pipeline view 42", What: "details for pipeline #42"},
			{Cmd: "bkt-axi pipeline view 42 --steps", What: "include the step list"},
			{Cmd: "bkt-axi pipeline view 42 --log-failed --full", What: "write failed-step logs to a temp file"},
		},
		Run: runPipelineView,
	}
}

// pipelineExtraFields maps a --fields token to its schema column. The `steps`
// column carries a per-run summary and its extractor is injected at render time
// (it closes over a locally-computed summary map), so the placeholder here is
// replaced before Rows() runs.
var pipelineExtraFields = map[string]axi.Field{
	"trigger":  {Key: "trigger", Extractor: axi.Pluck("trigger")},
	"duration": {Key: "duration", Extractor: axi.Pluck("duration")},
	"steps":    {Key: "steps", Extractor: axi.Const("")},
}

func pipelineListSchema(extra []string) ([]axi.Field, error) {
	schema := []axi.Field{
		{Key: "build", Extractor: axi.Pluck("build")},
		{Key: "state", Extractor: axi.Pluck("state")},
		{Key: "ref", Extractor: axi.Pluck("ref")},
		{Key: "created", Extractor: axi.RelativeTime(axi.Pluck("created_at"))},
	}
	return extendSchema(schema, pipelineExtraFields, extra, "pipeline list")
}

func runPipelineList(ctx *app.Context) error {
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
	wantSteps := containsAny(extras, "steps")

	schema, err := pipelineListSchema(extras)
	if err != nil {
		return err
	}

	result, err := client.ListPipelines(context.Background(), scope, bitbucket.PipelineListOptions{
		Limit: ctx.Flags.Int("limit"),
	})
	if err != nil {
		return err
	}

	if len(result.Pipelines) == 0 {
		msg := fmt.Sprintf("0 pipelines in %s", scope.String())
		doc := axi.NewObject(
			axi.KV{Key: "pipelines", Value: msg},
			axi.KV{Key: "help", Value: axi.HelpRows(axi.EmptyPipelineList())},
		)
		emit(ctx, map[string]any{"pipelines": msg}, axi.Marshal(doc))
		return nil
	}

	// The `steps` extra needs a per-run summary (one extra request per pipeline).
	// Compute it into a local map and inject a closure-based extractor so the
	// generic Rows() renderer still drives the column.
	if wantSteps {
		summaries := make(map[string]string, len(result.Pipelines))
		for i := range result.Pipelines {
			summaries[result.Pipelines[i].UUID] = pipelineStepsSummary(client, scope, result.Pipelines[i].UUID)
		}
		for i := range schema {
			if schema[i].Key == "steps" {
				schema[i].Extractor = axi.Custom(func(item any) any {
					if p, ok := item.(bitbucket.Pipeline); ok {
						return summaries[p.UUID]
					}
					return ""
				})
			}
		}
	}

	items := toAny(result.Pipelines)
	count := countLine(result.Shown, 0, result.MoreAvailable)
	doc := axi.NewObject(
		axi.KV{Key: "count", Value: count},
		axi.KV{Key: "pipelines", Value: axi.Rows(items, schema)},
		axi.KV{Key: "help", Value: axi.HelpRows(axi.AfterPipelineList(result.MoreAvailable))},
	)
	payload := listPayloadRows("pipelines", count, items, schema, axi.AfterPipelineList(result.MoreAvailable))
	emit(ctx, payload, axi.Marshal(doc))
	return nil
}

// pipelineStepsSummary fetches a run's steps and renders "N (X failed)". Errors
// degrade to an empty string so a single failed fetch never breaks the listing.
func pipelineStepsSummary(client *bitbucket.Client, scope bitbucket.Scope, pipelineUUID string) string {
	if pipelineUUID == "" {
		return ""
	}
	steps, err := client.ListPipelineSteps(context.Background(), scope, pipelineUUID)
	if err != nil {
		return ""
	}
	if len(steps) == 0 {
		return "0"
	}
	failed := 0
	for _, s := range steps {
		if s.Result == "FAILED" || s.Result == "ERROR" {
			failed++
		}
	}
	if failed == 0 {
		return fmt.Sprintf("%d", len(steps))
	}
	return fmt.Sprintf("%d (%d failed)", len(steps), failed)
}

func runPipelineView(ctx *app.Context) error {
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
		return axi.UsageError(fmt.Sprintf("invalid pipeline build number %q: %s", ctx.Args[0], idErr))
	}

	pipeline, err := client.GetPipeline(context.Background(), scope, id)
	if err != nil {
		return err
	}

	detailSchema := []axi.Field{
		{Key: "build", Extractor: axi.Pluck("build")},
		{Key: "state", Extractor: axi.Pluck("state")},
		{Key: "result", Extractor: axi.Pluck("result")},
		{Key: "ref", Extractor: axi.Pluck("ref")},
		{Key: "trigger", Extractor: axi.Pluck("trigger")},
		{Key: "duration", Extractor: axi.Pluck("duration")},
		{Key: "created", Extractor: axi.RelativeTime(axi.Pluck("created_at"))},
		{Key: "completed", Extractor: axi.RelativeTime(axi.Pluck("completed_at"))},
		{Key: "uuid", Extractor: axi.Pluck("uuid")},
	}

	fields := []axi.KV{{Key: "pipeline", Value: axi.NewObject(axi.Ordered(*pipeline, detailSchema)...)}}
	// Raw machine timestamps for the JSON/YAML escape hatch (TOON humanizes).
	pipelineDetail := detailExtracted(pipeline, detailSchema)
	pipelineDetail["created"] = rfc3339(pipeline.CreatedAt)
	pipelineDetail["completed"] = rfc3339(pipeline.CompletedAt)
	payload := map[string]any{"pipeline": pipelineDetail}

	wantSteps := ctx.Flags.Bool("steps")
	wantLogs := ctx.Flags.Bool("logs")
	wantFailed := ctx.Flags.Bool("log-failed")
	full := ctx.Flags.Bool("full")

	var steps []bitbucket.PipelineStep
	if wantSteps || wantLogs || wantFailed {
		steps, err = client.ListPipelineSteps(context.Background(), scope, pipeline.UUID)
		if err != nil {
			return err
		}
	}
	if wantSteps {
		fields = append(fields, axi.KV{Key: "steps", Value: pipelineStepRows(steps)})
		payload["steps"] = pipelineStepPayload(steps)
	}
	if wantLogs || wantFailed {
		logsField, logPayload, logErr := assemblePipelineLogs(client, scope, pipeline.UUID, pipeline.BuildNumber, steps, wantFailed, full)
		if logErr != nil {
			return logErr
		}
		fields = append(fields, logsField...)
		for k, v := range logPayload {
			payload[k] = v
		}
	}

	emit(ctx, payload, axi.Marshal(axi.NewObject(fields...)))
	return nil
}

func pipelineStepRows(steps []bitbucket.PipelineStep) []axi.Object {
	schema := []axi.Field{
		{Key: "name", Extractor: axi.Pluck("name")},
		{Key: "state", Extractor: axi.Pluck("state")},
		{Key: "result", Extractor: axi.Pluck("result")},
		{Key: "uuid", Extractor: axi.Pluck("uuid")},
	}
	rows := make([]axi.Object, 0, len(steps))
	for i := range steps {
		rows = append(rows, axi.NewObject(axi.Ordered(steps[i], schema)...))
	}
	return rows
}

func pipelineStepPayload(steps []bitbucket.PipelineStep) []map[string]any {
	schema := []axi.Field{
		{Key: "name", Extractor: axi.Pluck("name")},
		{Key: "state", Extractor: axi.Pluck("state")},
		{Key: "result", Extractor: axi.Pluck("result")},
		{Key: "uuid", Extractor: axi.Pluck("uuid")},
	}
	out := make([]map[string]any, 0, len(steps))
	for i := range steps {
		out = append(out, axi.Extract(steps[i], schema))
	}
	return out
}

// assemblePipelineLogs concatenates the requested steps' logs, applies the
// truncation/temp-file policy, and returns the TOON fields and JSON payload
// fragment to merge into the view document.
func assemblePipelineLogs(client *bitbucket.Client, scope bitbucket.Scope, pipelineUUID string, buildNumber int, steps []bitbucket.PipelineStep, failedOnly, full bool) ([]axi.KV, map[string]any, error) {
	selected := selectLogSteps(steps, failedOnly)
	var b strings.Builder
	for _, s := range selected {
		log, lerr := client.GetPipelineLogs(context.Background(), scope, pipelineUUID, s.UUID)
		if lerr != nil {
			// A missing log for one step degrades to a placeholder; the whole
			// view should not fail because a step log expired.
			fmt.Fprintf(&b, "--- %s [%s] (log unavailable) ---\n", s.Name, s.UUID)
			continue
		}
		fmt.Fprintf(&b, "--- %s [%s] ---\n", s.Name, s.UUID)
		b.Write(log)
		if len(log) > 0 && log[len(log)-1] != '\n' {
			b.WriteByte('\n')
		}
	}
	combined := b.String()

	var logValue, fullPath string
	help := []string(nil)
	if full {
		path, werr := writeTempOutput("bkt-axi-pipeline-*.log", combined)
		if werr != nil {
			return nil, nil, axi.Errorf("failed to write logs to temp file: %s", werr)
		}
		fullPath = path
		logValue = combined
		help = append(help, "Read the complete output: `cat "+fullPath+"`")
	} else {
		logValue = axi.TruncateTail(combined, pipelineLogBudget)
		if axi.ExceedsBudget(combined, pipelineLogBudget) {
			hint := fmt.Sprintf("Run `bkt-axi pipeline view %d --logs", buildNumber)
			if failedOnly {
				hint += " --log-failed"
			}
			hint += " --full` to write the complete logs to a file"
			help = append(help, hint)
		}
	}

	docFields := []axi.KV{{Key: "logs", Value: logValue}}
	payload := map[string]any{"logs": logValue}
	if fullPath != "" {
		docFields = append(docFields, axi.KV{Key: "full_path", Value: fullPath})
		payload["full_path"] = fullPath
	}
	if len(help) > 0 {
		docFields = append(docFields, axi.KV{Key: "help", Value: axi.HelpRows(help)})
		payload["help"] = help
	}
	return docFields, payload, nil
}

// selectLogSteps returns the steps to fetch logs for: all steps by default, or
// only failed/errored steps when failedOnly is set.
func selectLogSteps(steps []bitbucket.PipelineStep, failedOnly bool) []bitbucket.PipelineStep {
	if !failedOnly {
		return steps
	}
	out := make([]bitbucket.PipelineStep, 0, len(steps))
	for _, s := range steps {
		if s.Result == "FAILED" || s.Result == "ERROR" {
			out = append(out, s)
		}
	}
	return out
}
