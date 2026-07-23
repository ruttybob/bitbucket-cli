package bitbucket

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruttybob/bkt-axi/internal/bitbucket/httpx"
)

// status.go rolls up CI/build status and rate-limit telemetry. It reuses the
// cross-platform CommitStatuses (commit.go) and Pipeline (pipeline.go) adapters
// from Phase 1 rather than re-deriving them, keeping one normalized path per
// concept. Per the Phase 3 spec: commit build statuses and pipeline runs target
// their native line, PR head-commit statuses target Data Center, and rate-limit
// telemetry is reported on both.

// PRHeadStatuses returns build statuses for a pull request's head commit
// (Data Center). It resolves the head SHA from the PR, then delegates to the
// shared CommitStatuses adapter.
func (c *Client) PRHeadStatuses(ctx context.Context, scope Scope, id int) ([]BuildStatus, error) {
	if c.Kind != KindDC {
		return nil, DCOnly("pull request build statuses", c.hostKindLabel())
	}
	if scope.ProjectKey == "" || scope.RepoSlug == "" {
		return nil, fmt.Errorf("project and repo are required; use --project/--repo or set a context")
	}
	pr, err := c.dc.GetPullRequest(ctx, scope.ProjectKey, scope.RepoSlug, id)
	if err != nil {
		return nil, mapHTTPError(err, fmt.Sprintf("pull request #%d", id))
	}
	sha := strings.TrimSpace(pr.FromRef.LatestCommit)
	if sha == "" {
		return nil, fmt.Errorf("pull request #%d has no head commit to inspect", id)
	}
	return c.CommitStatuses(ctx, scope, sha)
}

// RateLimit pings the host to refresh telemetry, then returns the last
// observed rate-limit headers. When the host advertises no rate-limit headers
// (common for Cloud), Limit and Remaining are both zero.
func (c *Client) RateLimit(ctx context.Context) (*RateLimitInfo, error) {
	var rl httpx.RateLimit
	switch c.Kind {
	case KindCloud:
		_ = c.cloud.Ping(ctx)
		rl = c.cloud.RateLimit()
	case KindDC:
		_ = c.dc.Ping(ctx)
		rl = c.dc.RateLimit()
	default:
		return nil, fmt.Errorf("unsupported host kind %q", c.Kind)
	}
	return &RateLimitInfo{
		Limit:     rl.Limit,
		Remaining: rl.Remaining,
		Reset:     rl.Reset,
		Source:    rl.Source,
	}, nil
}
