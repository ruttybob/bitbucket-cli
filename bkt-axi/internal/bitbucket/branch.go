package bitbucket

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruttybob/bkt-axi/internal/bitbucket/cloud"
	"github.com/ruttybob/bkt-axi/internal/bitbucket/dc"
)

// branch.go adapts the salvaged line-specific clients into the normalized
// Branch model. This file is the SINGLE place that switches on host.Kind for
// branches; the command layer never does.

// BranchListOptions configures a branch listing.
type BranchListOptions struct {
	Filter           string // optional case-insensitive name substring filter
	Limit            int    // page size cap; <=0 uses 100
	WithCommitDetail bool   // populate Message/Author/UpdatedAt from each branch's head commit (one extra request per branch)
}

// ListBranches fetches one bounded page of branches for the resolved repository
// and maps them to the normalized model.
func (c *Client) ListBranches(ctx context.Context, scope Scope, opts BranchListOptions) (*BranchListResult, error) {
	limit := clampRepoLimit(opts.Limit)
	switch c.Kind {
	case KindCloud:
		return c.listBranchesCloud(ctx, scope, opts, limit)
	case KindDC:
		return c.listBranchesDC(ctx, scope, opts, limit)
	}
	return nil, fmt.Errorf("unsupported host kind %q", c.Kind)
}

func (c *Client) listBranchesCloud(ctx context.Context, scope Scope, opts BranchListOptions, limit int) (*BranchListResult, error) {
	if scope.Workspace == "" || scope.RepoSlug == "" {
		return nil, fmt.Errorf("workspace and repo are required; use --workspace/--repo or set a context")
	}
	branches, err := c.cloud.ListBranches(ctx, scope.Workspace, scope.RepoSlug, cloud.BranchListOptions{
		Filter: opts.Filter,
		Limit:  limit,
	})
	if err != nil {
		return nil, mapHTTPError(err, "branches")
	}
	out := make([]Branch, 0, len(branches))
	for i := range branches {
		b := mapCloudBranch(&branches[i])
		if opts.WithCommitDetail && b.LatestCommit != "" {
			if commit, cerr := c.cloud.GetCommit(ctx, scope.Workspace, scope.RepoSlug, b.LatestCommit); cerr == nil {
				enrichBranchFromCloud(&b, commit)
			}
		}
		out = append(out, b)
	}
	return &BranchListResult{Branches: out, Shown: len(out), MoreAvailable: len(branches) >= limit}, nil
}

func (c *Client) listBranchesDC(ctx context.Context, scope Scope, opts BranchListOptions, limit int) (*BranchListResult, error) {
	if scope.ProjectKey == "" || scope.RepoSlug == "" {
		return nil, fmt.Errorf("project and repo are required; use --project/--repo or set a context")
	}
	branches, err := c.dc.ListBranches(ctx, scope.ProjectKey, scope.RepoSlug, dc.BranchListOptions{
		Filter: opts.Filter,
		Limit:  limit,
	})
	if err != nil {
		return nil, mapHTTPError(err, "branches")
	}
	out := make([]Branch, 0, len(branches))
	for i := range branches {
		b := mapDCBranch(&branches[i])
		if opts.WithCommitDetail && b.LatestCommit != "" {
			if commit, cerr := c.dc.GetCommit(ctx, scope.ProjectKey, scope.RepoSlug, b.LatestCommit); cerr == nil {
				enrichBranchFromDC(&b, commit)
			}
		}
		out = append(out, b)
	}
	return &BranchListResult{Branches: out, Shown: len(out), MoreAvailable: len(branches) >= limit}, nil
}

// --- mappers -------------------------------------------------------------

func mapCloudBranch(b *cloud.Branch) Branch {
	return Branch{
		Name:         b.Name,
		IsDefault:    b.IsDefault,
		LatestCommit: b.Target.Hash,
		DisplayID:    b.Name,
		Type:         b.Target.Type,
	}
}

func mapDCBranch(b *dc.Branch) Branch {
	return Branch{
		Name:         b.DisplayID,
		IsDefault:    b.IsDefault,
		LatestCommit: b.LatestCommit,
		DisplayID:    b.DisplayID,
		Type:         b.Type,
	}
}

func enrichBranchFromCloud(b *Branch, c *cloud.Commit) {
	b.Message = firstLine(c.Message)
	b.UpdatedAt = parseTime(c.Date)
	if c.Author.User.DisplayName != "" {
		b.Author = c.Author.User.DisplayName
	} else {
		b.Author = firstLine(c.Author.Raw)
	}
}

func enrichBranchFromDC(b *Branch, c *dc.Commit) {
	b.Message = firstLine(c.Message)
	b.UpdatedAt = parseEpochMillis(c.AuthorTimestamp)
	if c.Author.DisplayName != "" {
		b.Author = c.Author.DisplayName
	} else {
		b.Author = c.Author.Name
	}
}

// firstLine returns the message up to the first newline, trimmed.
func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' {
			return strings.TrimSpace(s[:i])
		}
	}
	return strings.TrimSpace(s)
}
