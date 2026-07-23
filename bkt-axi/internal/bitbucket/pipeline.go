package bitbucket

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ruttybob/bkt-axi/internal/bitbucket/cloud"
)

// pipeline.go adapts the salvaged Cloud client into the normalized Pipeline
// model. Pipelines are a Bitbucket Cloud concept; Data Center has no
// equivalent, so every method here returns a clear Cloud-only error when the
// active host is Data Center (see CloudOnly in guards.go).

// hostKindLabel renders the active host's kind as a human product name.
func (c *Client) hostKindLabel() string {
	switch c.Kind {
	case KindCloud:
		return "Bitbucket Cloud"
	case KindDC:
		return "Bitbucket Data Center"
	}
	return string(c.Kind)
}

// PipelineListOptions configures a pipeline listing.
type PipelineListOptions struct {
	Limit int // page size cap; <=0 uses 25
}

const defaultPipelineLimit = 25

func clampPipelineLimit(n int) int {
	if n <= 0 {
		return defaultPipelineLimit
	}
	if n > 100 {
		return 100
	}
	return n
}

// ListPipelines fetches one bounded page of Cloud pipelines. It is Cloud-only.
func (c *Client) ListPipelines(ctx context.Context, scope Scope, opts PipelineListOptions) (*PipelineListResult, error) {
	if c.Kind != KindCloud {
		return nil, CloudOnly("pipelines", c.hostKindLabel())
	}
	if scope.Workspace == "" || scope.RepoSlug == "" {
		return nil, fmt.Errorf("workspace and repo are required; use --workspace/--repo or set a context")
	}
	limit := clampPipelineLimit(opts.Limit)
	pipelines, err := c.cloud.ListPipelines(ctx, scope.Workspace, scope.RepoSlug, limit)
	if err != nil {
		return nil, mapHTTPError(err, "pipelines")
	}
	out := make([]Pipeline, 0, len(pipelines))
	for i := range pipelines {
		out = append(out, mapCloudPipeline(&pipelines[i]))
	}
	return &PipelineListResult{Pipelines: out, Shown: len(out), MoreAvailable: len(pipelines) >= limit}, nil
}

// GetPipeline fetches a single pipeline by build number. It is Cloud-only.
func (c *Client) GetPipeline(ctx context.Context, scope Scope, buildNumber int) (*Pipeline, error) {
	if c.Kind != KindCloud {
		return nil, CloudOnly("pipelines", c.hostKindLabel())
	}
	if scope.Workspace == "" || scope.RepoSlug == "" {
		return nil, fmt.Errorf("workspace and repo are required; use --workspace/--repo or set a context")
	}
	p, err := c.cloud.GetPipelineByBuildNumber(ctx, scope.Workspace, scope.RepoSlug, buildNumber)
	if err != nil {
		return nil, mapHTTPError(err, fmt.Sprintf("pipeline #%d", buildNumber))
	}
	m := mapCloudPipeline(p)
	return &m, nil
}

// ListPipelineSteps enumerates step executions for a pipeline run. It is
// Cloud-only. pipelineUUID may be the bare UUID or wrapped in braces.
func (c *Client) ListPipelineSteps(ctx context.Context, scope Scope, pipelineUUID string) ([]PipelineStep, error) {
	if c.Kind != KindCloud {
		return nil, CloudOnly("pipelines", c.hostKindLabel())
	}
	if scope.Workspace == "" || scope.RepoSlug == "" {
		return nil, fmt.Errorf("workspace and repo are required; use --workspace/--repo or set a context")
	}
	steps, err := c.cloud.ListPipelineSteps(ctx, scope.Workspace, scope.RepoSlug, pipelineUUID)
	if err != nil {
		return nil, mapHTTPError(err, "pipeline steps")
	}
	out := make([]PipelineStep, 0, len(steps))
	for i := range steps {
		out = append(out, mapCloudPipelineStep(&steps[i]))
	}
	return out, nil
}

// GetPipelineLogs fetches the log output for a single pipeline step. It is
// Cloud-only.
func (c *Client) GetPipelineLogs(ctx context.Context, scope Scope, pipelineUUID, stepUUID string) ([]byte, error) {
	if c.Kind != KindCloud {
		return nil, CloudOnly("pipelines", c.hostKindLabel())
	}
	if scope.Workspace == "" || scope.RepoSlug == "" {
		return nil, fmt.Errorf("workspace and repo are required; use --workspace/--repo or set a context")
	}
	return c.cloud.GetPipelineLogs(ctx, scope.Workspace, scope.RepoSlug, pipelineUUID, stepUUID)
}

// --- mappers -------------------------------------------------------------

func mapCloudPipeline(p *cloud.Pipeline) Pipeline {
	out := Pipeline{
		BuildNumber: p.BuildNumber,
		UUID:        strings.Trim(p.UUID, "{}"),
		State:       strings.ToUpper(strings.TrimSpace(p.State.Name)),
		Result:      strings.ToUpper(strings.TrimSpace(p.State.Result.Name)),
		Ref:         p.Target.Ref.Name,
		Trigger:     pipelineTriggerLabel(p),
		CreatedAt:   parseTime(p.CreatedOn),
		CompletedAt: parseTime(p.CompletedOn),
	}
	out.Duration = humanizeDuration(out.CreatedAt, out.CompletedAt)
	return out
}

func mapCloudPipelineStep(s *cloud.PipelineStep) PipelineStep {
	result := s.State.Result.Name
	if result == "" {
		result = s.Result.Name
	}
	return PipelineStep{
		Name:   s.Name,
		UUID:   strings.Trim(s.UUID, "{}"),
		State:  strings.ToUpper(strings.TrimSpace(s.State.Name)),
		Result: strings.ToUpper(strings.TrimSpace(result)),
	}
}

// pipelineTriggerLabel renders a compact trigger descriptor: the upstream
// trigger type (e.g. "deployment"), the selector pattern when present, or the
// target type as a fallback so the column is never empty.
func pipelineTriggerLabel(p *cloud.Pipeline) string {
	if t := strings.TrimSpace(p.Trigger.Type); t != "" {
		return t
	}
	if p.Trigger.Selector != nil && strings.TrimSpace(p.Trigger.Selector.Pattern) != "" {
		return p.Trigger.Selector.Pattern
	}
	if tt := strings.TrimSpace(p.Target.Type); tt != "" {
		return tt
	}
	return ""
}

// humanizeDuration renders the span between start and end as "1m30s" / "12s".
// Returns "" when end is missing or not after start.
func humanizeDuration(start, end time.Time) string {
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return ""
	}
	d := end.Sub(start)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) - m*60
	if s == 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dm%ds", m, s)
}
