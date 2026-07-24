package bitbucket

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ruttybob/bkt-axi/internal/bitbucket/cloud"
	"github.com/ruttybob/bkt-axi/internal/bitbucket/dc"
)

// pr.go adapts the salvaged line-specific clients into the normalized PR
// model. This file is the SINGLE place that switches on host.Kind for pull
// requests; the command layer never does.

// PRListOptions mirrors the cross-line filter set. Mine/Reviewer carry a user
// identity (Cloud UUID/nickname/account id, or a DC slug).
type PRListOptions struct {
	State    string // "", OPEN, MERGED, DECLINED, ALL
	Mine     string // author identity
	Reviewer string // reviewer identity
	Limit    int    // page size cap; <=0 uses 50
}

const defaultPRLimit = 50

func clampLimit(n int) int {
	if n <= 0 {
		return defaultPRLimit
	}
	if n > 100 {
		return 100
	}
	return n
}

// ListPRs fetches one bounded page of pull requests for scope and maps them to
// the normalized model. MoreAvailable is true when the upstream reported a next
// page the call did not follow.
func (c *Client) ListPRs(ctx context.Context, scope Scope, opts PRListOptions) (*PRListResult, error) {
	limit := clampLimit(opts.Limit)
	switch c.Kind {
	case KindCloud:
		return c.listPRsCloud(ctx, scope, opts, limit)
	case KindDC:
		return c.listPRsDC(ctx, scope, opts, limit)
	}
	return nil, fmt.Errorf("unsupported host kind %q", c.Kind)
}

func (c *Client) listPRsCloud(ctx context.Context, scope Scope, opts PRListOptions, limit int) (*PRListResult, error) {
	if scope.Workspace == "" || scope.RepoSlug == "" {
		return nil, fmt.Errorf("workspace and repo are required; use --workspace/--repo or set a context")
	}
	page, err := c.cloud.ListRepoPullRequestsPage(ctx, scope.Workspace, scope.RepoSlug, cloud.PullRequestListOptions{
		State:    opts.State,
		Limit:    limit,
		Mine:     opts.Mine,
		Reviewer: opts.Reviewer,
	}, "")
	if err != nil {
		return nil, c.mapErr(err, "pull requests")
	}
	prs := make([]PR, 0, len(page.Values))
	for i := range page.Values {
		prs = append(prs, mapCloudPR(&page.Values[i]))
	}
	return &PRListResult{PRs: prs, Shown: len(prs), MoreAvailable: page.Next != ""}, nil
}

func (c *Client) listPRsDC(ctx context.Context, scope Scope, opts PRListOptions, limit int) (*PRListResult, error) {
	if scope.ProjectKey == "" || scope.RepoSlug == "" {
		return nil, fmt.Errorf("project and repo are required; use --project/--repo or set a context")
	}
	do := dc.RepoPullRequestsOptions{
		State: opts.State,
		Limit: limit,
	}
	// DC participant filtering is role+username; only one of mine/reviewer maps.
	switch {
	case opts.Mine != "":
		do.Role, do.Username = "AUTHOR", opts.Mine
	case opts.Reviewer != "":
		do.Role, do.Username = "REVIEWER", opts.Reviewer
	}
	page, err := c.dc.ListRepoPullRequestsPage(ctx, scope.ProjectKey, scope.RepoSlug, do)
	if err != nil {
		return nil, c.mapErr(err, "pull requests")
	}
	prs := make([]PR, 0, len(page.Values))
	for i := range page.Values {
		prs = append(prs, mapDCPR(&page.Values[i]))
	}
	return &PRListResult{PRs: prs, Shown: len(prs), MoreAvailable: !page.IsLast}, nil
}

// GetPR fetches a single pull request and maps it to the normalized model.
func (c *Client) GetPR(ctx context.Context, scope Scope, id int) (*PR, error) {
	switch c.Kind {
	case KindCloud:
		if scope.Workspace == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("workspace and repo are required; use --workspace/--repo or set a context")
		}
		pr, err := c.cloud.GetPullRequest(ctx, scope.Workspace, scope.RepoSlug, id)
		if err != nil {
			return nil, c.mapErr(err, fmt.Sprintf("pull request #%d", id))
		}
		m := mapCloudPR(pr)
		return &m, nil
	case KindDC:
		if scope.ProjectKey == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("project and repo are required; use --project/--repo or set a context")
		}
		pr, err := c.dc.GetPullRequest(ctx, scope.ProjectKey, scope.RepoSlug, id)
		if err != nil {
			return nil, c.mapErr(err, fmt.Sprintf("pull request #%d", id))
		}
		m := mapDCPR(pr)
		return &m, nil
	}
	return nil, fmt.Errorf("unsupported host kind %q", c.Kind)
}

// ListComments fetches the normalized comment thread for a pull request.
func (c *Client) ListComments(ctx context.Context, scope Scope, id int) ([]Comment, error) {
	switch c.Kind {
	case KindCloud:
		if scope.Workspace == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("workspace and repo are required")
		}
		comments, err := c.cloud.ListPullRequestComments(ctx, scope.Workspace, scope.RepoSlug, id, 100)
		if err != nil {
			return nil, c.mapErr(err, "comments")
		}
		out := make([]Comment, 0, len(comments))
		for i := range comments {
			out = append(out, mapCloudComment(&comments[i]))
		}
		return out, nil
	case KindDC:
		if scope.ProjectKey == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("project and repo are required")
		}
		comments, err := c.dc.ListPullRequestComments(ctx, scope.ProjectKey, scope.RepoSlug, id)
		if err != nil {
			return nil, c.mapErr(err, "comments")
		}
		out := make([]Comment, 0, len(comments))
		for i := range comments {
			out = append(out, mapDCComment(&comments[i]))
		}
		return out, nil
	}
	return nil, fmt.Errorf("unsupported host kind %q", c.Kind)
}

// --- mappers -------------------------------------------------------------

func mapCloudPR(pr *cloud.PullRequest) PR {
	desc := strings.TrimSpace(pr.Description)
	if desc == "" {
		desc = strings.TrimSpace(pr.Summary.Raw)
	}
	return PR{
		ID:          pr.ID,
		Title:       pr.Title,
		State:       strings.ToLower(pr.State),
		Draft:       pr.Draft,
		Author:      cloudAuthor(pr),
		From:        pr.Source.Branch.Name,
		To:          pr.Destination.Branch.Name,
		Review:      string(cloudReview(pr)),
		Description: desc,
		URL:         pr.Links.HTML.Href,
		CreatedAt:   parseTime(pr.CreatedOn),
		Reviewers:   cloudReviewerNames(pr),
	}
}

func mapDCPR(pr *dc.PullRequest) PR {
	return PR{
		ID:          pr.ID,
		Title:       pr.Title,
		State:       strings.ToLower(pr.State),
		Draft:       pr.Draft,
		Author:      dcDisplayName(pr.Author.User),
		From:        pr.FromRef.DisplayID,
		To:          pr.ToRef.DisplayID,
		Review:      string(dcReview(pr.Reviewers)),
		Description: strings.TrimSpace(pr.Description),
		URL:         firstDCSelfLink(pr.Links.Self),
		CreatedAt:   parseEpochMillis(pr.CreatedDate),
		Reviewers:   dcReviewerNames(pr.Reviewers),
	}
}

func mapCloudComment(c *cloud.PullRequestComment) Comment {
	out := Comment{
		ID:        c.ID,
		Text:      strings.TrimSpace(c.Content.Raw),
		CreatedAt: parseTime(c.CreatedOn),
		State:     "open",
	}
	if c.User != nil {
		out.Author = c.User.DisplayName
	}
	if c.Resolution != nil {
		out.State = "resolved"
	}
	return out
}

func mapDCComment(c *dc.PullRequestComment) Comment {
	state := "open"
	if c.State == "RESOLVED" || c.ThreadResolved {
		state = "resolved"
	}
	return Comment{
		ID:        c.ID,
		Author:    dcDisplayName(c.Author),
		Text:      strings.TrimSpace(c.Text),
		CreatedAt: ptrEpochMillis(c.CreatedDate),
		State:     state,
	}
}

// --- review derivation ---------------------------------------------------

func cloudReview(pr *cloud.PullRequest) ReviewSummary {
	anyApproved := false
	anyChanges := false
	for _, p := range pr.Participants {
		if strings.EqualFold(p.Role, "REVIEWER") {
			if p.Approved != nil && *p.Approved {
				anyApproved = true
			}
			if strings.EqualFold(p.State, "changes_requested") {
				anyChanges = true
			}
		}
	}
	return summarizeReview(anyApproved, anyChanges)
}

func dcReview(reviewers []dc.PullRequestReviewer) ReviewSummary {
	anyApproved := false
	anyChanges := false
	for _, r := range reviewers {
		if r.Approved != nil && *r.Approved {
			anyApproved = true
		}
		switch strings.ToUpper(r.Status) {
		case "APPROVED":
			anyApproved = true
		case "NEEDS_WORK":
			anyChanges = true
		}
	}
	return summarizeReview(anyApproved, anyChanges)
}

func summarizeReview(anyApproved, anyChanges bool) ReviewSummary {
	switch {
	case anyChanges:
		return ReviewChanges
	case anyApproved:
		return ReviewApproved
	default:
		return ReviewRequired
	}
}

// --- helpers -------------------------------------------------------------

func cloudAuthor(pr *cloud.PullRequest) string {
	if pr.Author.DisplayName != "" {
		return pr.Author.DisplayName
	}
	if pr.AuthorNickname != "" {
		return pr.AuthorNickname
	}
	return pr.Author.Username
}

func cloudReviewerNames(pr *cloud.PullRequest) []string {
	if len(pr.Reviewers) == 0 {
		return nil
	}
	out := make([]string, 0, len(pr.Reviewers))
	for _, r := range pr.Reviewers {
		if r.Display != "" {
			out = append(out, r.Display)
		} else if r.Nickname != "" {
			out = append(out, r.Nickname)
		} else {
			out = append(out, r.Username)
		}
	}
	return out
}

func dcDisplayName(u dc.User) string {
	if u.FullName != "" {
		return u.FullName
	}
	return u.Name
}

func dcReviewerNames(reviewers []dc.PullRequestReviewer) []string {
	if len(reviewers) == 0 {
		return nil
	}
	out := make([]string, 0, len(reviewers))
	for _, r := range reviewers {
		out = append(out, dcDisplayName(r.User))
	}
	return out
}

func firstDCSelfLink(links []struct {
	Href string `json:"href"`
}) string {
	if len(links) == 0 {
		return ""
	}
	return links[0].Href
}

// Adapter error translation is centralized in httperr.go's Client.mapErr (it
// threads host kind and Retry-After); call sites use c.mapErr(err, noun).

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

func parseEpochMillis(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

func ptrEpochMillis(ms *int64) time.Time {
	if ms == nil {
		return time.Time{}
	}
	return parseEpochMillis(*ms)
}
