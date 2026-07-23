package bitbucket

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruttybob/bkt-axi/internal/bitbucket/cloud"
	"github.com/ruttybob/bkt-axi/internal/bitbucket/dc"
)

// commit.go adapts the salvaged line-specific clients into the normalized
// Commit model and the BuildStatus model. This file is the SINGLE place that
// switches on host.Kind for commits; the command layer never does.

// GetCommit fetches a single commit by SHA (or any resolvable ref) and maps it
// to the normalized model.
func (c *Client) GetCommit(ctx context.Context, scope Scope, sha string) (*Commit, error) {
	if strings.TrimSpace(sha) == "" {
		return nil, fmt.Errorf("commit SHA is required")
	}
	switch c.Kind {
	case KindCloud:
		if scope.Workspace == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("workspace and repo are required; use --workspace/--repo or set a context")
		}
		commit, err := c.cloud.GetCommit(ctx, scope.Workspace, scope.RepoSlug, sha)
		if err != nil {
			return nil, c.mapErr(err, fmt.Sprintf("commit %s", sha))
		}
		m := mapCloudCommit(commit)
		return &m, nil
	case KindDC:
		if scope.ProjectKey == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("project and repo are required; use --project/--repo or set a context")
		}
		commit, err := c.dc.GetCommit(ctx, scope.ProjectKey, scope.RepoSlug, sha)
		if err != nil {
			return nil, c.mapErr(err, fmt.Sprintf("commit %s", sha))
		}
		m := mapDCCommit(commit)
		return &m, nil
	}
	return nil, fmt.Errorf("unsupported host kind %q", c.Kind)
}

// CommitDiff streams the raw unified diff between two refs into a string. On
// Cloud the refs are joined into a "from..to" spec; on DC they are passed as
// separate from/to query params.
func (c *Client) CommitDiff(ctx context.Context, scope Scope, from, to string) (string, error) {
	if strings.TrimSpace(from) == "" || strings.TrimSpace(to) == "" {
		return "", fmt.Errorf("from and to refs are required")
	}
	var b strings.Builder
	switch c.Kind {
	case KindCloud:
		if scope.Workspace == "" || scope.RepoSlug == "" {
			return "", fmt.Errorf("workspace and repo are required; use --workspace/--repo or set a context")
		}
		spec := from + ".." + to
		if err := c.cloud.CommitDiff(ctx, scope.Workspace, scope.RepoSlug, spec, &b); err != nil {
			return "", c.mapErr(err, "diff")
		}
	case KindDC:
		if scope.ProjectKey == "" || scope.RepoSlug == "" {
			return "", fmt.Errorf("project and repo are required; use --project/--repo or set a context")
		}
		if err := c.dc.CommitDiff(ctx, scope.ProjectKey, scope.RepoSlug, from, to, &b); err != nil {
			return "", c.mapErr(err, "diff")
		}
	default:
		return "", fmt.Errorf("unsupported host kind %q", c.Kind)
	}
	return b.String(), nil
}

// CommitStatuses fetches build statuses for a commit on either line.
func (c *Client) CommitStatuses(ctx context.Context, scope Scope, sha string) ([]BuildStatus, error) {
	if strings.TrimSpace(sha) == "" {
		return nil, fmt.Errorf("commit SHA is required")
	}
	switch c.Kind {
	case KindCloud:
		if scope.Workspace == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("workspace and repo are required; use --workspace/--repo or set a context")
		}
		statuses, err := c.cloud.CommitStatuses(ctx, scope.Workspace, scope.RepoSlug, sha)
		if err != nil {
			return nil, c.mapErr(err, fmt.Sprintf("statuses for commit %s", sha))
		}
		out := make([]BuildStatus, 0, len(statuses))
		for i := range statuses {
			out = append(out, mapCommitStatus(&statuses[i]))
		}
		return out, nil
	case KindDC:
		statuses, err := c.dc.CommitStatuses(ctx, sha)
		if err != nil {
			return nil, c.mapErr(err, fmt.Sprintf("statuses for commit %s", sha))
		}
		out := make([]BuildStatus, 0, len(statuses))
		for i := range statuses {
			out = append(out, mapCommitStatus(&statuses[i]))
		}
		return out, nil
	}
	return nil, fmt.Errorf("unsupported host kind %q", c.Kind)
}

// --- mappers -------------------------------------------------------------

func mapCloudCommit(c *cloud.Commit) Commit {
	out := Commit{
		SHA:        c.Hash,
		Message:    c.Message,
		AuthoredAt: parseTime(c.Date),
		URL:        c.Links.HTML.Href,
	}
	if c.Author.User.DisplayName != "" {
		out.Author = c.Author.User.DisplayName
	} else {
		out.Author = firstLine(c.Author.Raw)
	}
	out.AuthorEmail = emailFromRaw(c.Author.Raw)
	out.Parents = make([]string, 0, len(c.Parents))
	for _, p := range c.Parents {
		out.Parents = append(out.Parents, p.Hash)
	}
	return out
}

func mapDCCommit(c *dc.Commit) Commit {
	out := Commit{
		SHA:         c.ID,
		Message:     c.Message,
		Author:      dcDisplayName(commitAuthorUser(c)),
		AuthorEmail: c.Author.EmailAddress,
		AuthoredAt:  parseEpochMillis(c.AuthorTimestamp),
		CommittedAt: parseEpochMillis(c.CommitterTimestamp),
	}
	out.Parents = make([]string, 0, len(c.Parents))
	for _, p := range c.Parents {
		out.Parents = append(out.Parents, p.DisplayID)
	}
	return out
}

// commitAuthorUser projects the DC commit author into the dc.User shape that
// dcDisplayName reads, so author display falls back through display name → name.
func commitAuthorUser(c *dc.Commit) dc.User {
	return dc.User{
		Name:     c.Author.Name,
		FullName: c.Author.DisplayName,
	}
}

func mapCommitStatus(s *cloud.CommitStatus) BuildStatus {
	return BuildStatus{
		State:       normalizeStatusState(s.State),
		Key:         s.Key,
		Name:        s.Name,
		URL:         s.URL,
		Description: s.Description,
	}
}

// normalizeStatusState lowercases and trims build-status states so Cloud
// ("SUCCESSFUL") and DC ("SUCCESSFUL") render identically. Unknown values pass
// through lowercased.
func normalizeStatusState(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// emailFromRaw extracts the address from a Cloud author.raw value shaped
// "Name <email>". Returns "" when no angle brackets are present.
func emailFromRaw(raw string) string {
	raw = strings.TrimSpace(raw)
	start := strings.LastIndexByte(raw, '<')
	end := strings.LastIndexByte(raw, '>')
	if start >= 0 && end > start {
		return strings.TrimSpace(raw[start+1 : end])
	}
	return ""
}
