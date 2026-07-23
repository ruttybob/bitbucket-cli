package bitbucket

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruttybob/bkt-axi/internal/bitbucket/cloud"
	"github.com/ruttybob/bkt-axi/internal/bitbucket/dc"
)

// pr_ext.go adds the Phase 3 pull-request subcommands to the unified client:
// reviewers (add/remove/list), tasks (DC), suggestions (DC), and checks
// (cross-platform head-commit status). The one-switch principle still holds:
// each method switches on host.Kind once.

// --- reviewers ----------------------------------------------------------

// ListPRReviewers enumerates the pull request's reviewers with their approval
// state.
func (c *Client) ListPRReviewers(ctx context.Context, scope Scope, id int) ([]Reviewer, error) {
	switch c.Kind {
	case KindCloud:
		if scope.Workspace == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("workspace and repo are required")
		}
		pr, err := c.cloud.GetPullRequest(ctx, scope.Workspace, scope.RepoSlug, id)
		if err != nil {
			return nil, mapHTTPError(err, fmt.Sprintf("pull request #%d", id))
		}
		return mapCloudReviewers(pr), nil
	case KindDC:
		if scope.ProjectKey == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("project and repo are required")
		}
		pr, err := c.dc.GetPullRequest(ctx, scope.ProjectKey, scope.RepoSlug, id)
		if err != nil {
			return nil, mapHTTPError(err, fmt.Sprintf("pull request #%d", id))
		}
		return mapDCReviewers(pr.Reviewers), nil
	}
	return nil, fmt.Errorf("unsupported host kind %q", c.Kind)
}

// AddPRReviewer adds a reviewer. Idempotent: a no-op when the user is already a
// reviewer (changed=false).
func (c *Client) AddPRReviewer(ctx context.Context, scope Scope, id int, user string) (bool, error) {
	user = strings.TrimSpace(user)
	if user == "" {
		return false, fmt.Errorf("reviewer is required")
	}
	switch c.Kind {
	case KindCloud:
		return c.cloudAddReviewer(ctx, scope, id, user)
	case KindDC:
		return c.dcAddReviewer(ctx, scope, id, user)
	}
	return false, fmt.Errorf("unsupported host kind %q", c.Kind)
}

// RemovePRReviewer removes a reviewer. Idempotent: a no-op when the user is not
// a reviewer (changed=false).
func (c *Client) RemovePRReviewer(ctx context.Context, scope Scope, id int, user string) (bool, error) {
	user = strings.TrimSpace(user)
	if user == "" {
		return false, fmt.Errorf("reviewer is required")
	}
	switch c.Kind {
	case KindCloud:
		return c.cloudRemoveReviewer(ctx, scope, id, user)
	case KindDC:
		return c.dcRemoveReviewer(ctx, scope, id, user)
	}
	return false, fmt.Errorf("unsupported host kind %q", c.Kind)
}

func (c *Client) cloudAddReviewer(ctx context.Context, scope Scope, id int, user string) (bool, error) {
	pr, err := c.cloud.GetPullRequest(ctx, scope.Workspace, scope.RepoSlug, id)
	if err != nil {
		return false, mapHTTPError(err, fmt.Sprintf("pull request #%d", id))
	}
	if cloudReviewerPresent(user, pr.Reviewers) {
		return false, nil
	}
	list := append(cloudReviewerIdentities(pr.Reviewers), user)
	if _, err := c.cloud.UpdatePullRequest(ctx, scope.Workspace, scope.RepoSlug, id, cloud.UpdatePullRequestInput{
		Reviewers: list,
	}); err != nil {
		return false, mapHTTPError(err, "reviewer "+user)
	}
	return true, nil
}

func (c *Client) cloudRemoveReviewer(ctx context.Context, scope Scope, id int, user string) (bool, error) {
	pr, err := c.cloud.GetPullRequest(ctx, scope.Workspace, scope.RepoSlug, id)
	if err != nil {
		return false, mapHTTPError(err, fmt.Sprintf("pull request #%d", id))
	}
	list := cloudReviewerIdentities(pr.Reviewers)
	next := make([]string, 0, len(list))
	removed := false
	for _, r := range list {
		if strings.EqualFold(r, user) {
			removed = true
			continue
		}
		next = append(next, r)
	}
	if !removed {
		return false, nil
	}
	if _, err := c.cloud.UpdatePullRequest(ctx, scope.Workspace, scope.RepoSlug, id, cloud.UpdatePullRequestInput{
		Reviewers: next,
	}); err != nil {
		return false, mapHTTPError(err, "reviewer "+user)
	}
	return true, nil
}

func (c *Client) dcAddReviewer(ctx context.Context, scope Scope, id int, user string) (bool, error) {
	pr, err := c.dc.GetPullRequest(ctx, scope.ProjectKey, scope.RepoSlug, id)
	if err != nil {
		return false, mapHTTPError(err, fmt.Sprintf("pull request #%d", id))
	}
	for _, r := range pr.Reviewers {
		if dcUserMatches(user, r.User) {
			return false, nil
		}
	}
	reviewers := append(pr.Reviewers, dc.PullRequestReviewer{User: dc.User{Name: user, Slug: user}})
	if err := c.dcUpdateReviewers(ctx, scope, id, pr, reviewers); err != nil {
		return false, err
	}
	return true, nil
}

func (c *Client) dcRemoveReviewer(ctx context.Context, scope Scope, id int, user string) (bool, error) {
	pr, err := c.dc.GetPullRequest(ctx, scope.ProjectKey, scope.RepoSlug, id)
	if err != nil {
		return false, mapHTTPError(err, fmt.Sprintf("pull request #%d", id))
	}
	next := make([]dc.PullRequestReviewer, 0, len(pr.Reviewers))
	removed := false
	for _, r := range pr.Reviewers {
		if dcUserMatches(user, r.User) {
			removed = true
			continue
		}
		next = append(next, r)
	}
	if !removed {
		return false, nil
	}
	if err := c.dcUpdateReviewers(ctx, scope, id, pr, next); err != nil {
		return false, err
	}
	return true, nil
}

// dcUpdateReviewers re-PUTs the PR preserving title/description/refs while
// replacing the reviewer list. DC's PUT replaces the whole PR, so all fields
// must be echoed back.
func (c *Client) dcUpdateReviewers(ctx context.Context, scope Scope, id int, pr *dc.PullRequest, reviewers []dc.PullRequestReviewer) error {
	fromRef := &pr.FromRef
	toRef := &pr.ToRef
	if _, err := c.dc.UpdatePullRequest(ctx, scope.ProjectKey, scope.RepoSlug, id, pr.Version, dc.UpdatePROptions{
		Title:       pr.Title,
		Description: pr.Description,
		Reviewers:   reviewers,
		FromRef:     fromRef,
		ToRef:       toRef,
	}); err != nil {
		return mapHTTPError(err, fmt.Sprintf("pull request #%d", id))
	}
	return nil
}

// cloudReviewerPresent reports whether user matches any of the Cloud PR's
// reviewer identities (by uuid, username, nickname, account_id, or display).
func cloudReviewerPresent(user string, reviewers []cloud.User) bool {
	for i := range reviewers {
		if cloudUserMatches(user, reviewers[i]) {
			return true
		}
	}
	return false
}

func cloudUserMatches(user string, u cloud.User) bool {
	user = strings.ToLower(strings.TrimSpace(user))
	candidates := []string{u.UUID, u.AccountID, u.Username, u.Nickname, u.Display}
	for _, c := range candidates {
		if strings.EqualFold(strings.TrimSpace(c), user) {
			return true
		}
	}
	return false
}

func dcUserMatches(user string, u dc.User) bool {
	user = strings.ToLower(strings.TrimSpace(user))
	candidates := []string{u.Slug, u.Name, u.FullName, u.Email}
	for _, c := range candidates {
		if strings.EqualFold(strings.TrimSpace(c), user) {
			return true
		}
	}
	return false
}

func mapCloudReviewers(pr *cloud.PullRequest) []Reviewer {
	out := make([]Reviewer, 0, len(pr.Participants))
	for i := range pr.Participants {
		p := pr.Participants[i]
		if !strings.EqualFold(p.Role, "REVIEWER") {
			continue
		}
		approved := p.Approved != nil && *p.Approved
		state := "unreviewed"
		if approved {
			state = "approved"
		} else if strings.EqualFold(p.State, "changes_requested") {
			state = "changes_requested"
		}
		out = append(out, Reviewer{Name: cloudUserName(p.User), State: state, Approved: approved})
	}
	return out
}

func mapDCReviewers(reviewers []dc.PullRequestReviewer) []Reviewer {
	out := make([]Reviewer, 0, len(reviewers))
	for _, r := range reviewers {
		state := "unreviewed"
		approved := false
		if r.Approved != nil && *r.Approved {
			approved = true
			state = "approved"
		}
		switch strings.ToUpper(r.Status) {
		case "APPROVED":
			approved = true
			state = "approved"
		case "NEEDS_WORK":
			state = "changes_requested"
		}
		out = append(out, Reviewer{Name: dcDisplayName(r.User), State: state, Approved: approved})
	}
	return out
}

// --- tasks (DC) ---------------------------------------------------------

// ListPRTasks lists pull request tasks (DC blocker comments).
func (c *Client) ListPRTasks(ctx context.Context, scope Scope, id int) ([]PullRequestTask, error) {
	if c.Kind != KindDC {
		return nil, DCOnly("pull request tasks", c.hostKindLabel())
	}
	if scope.ProjectKey == "" || scope.RepoSlug == "" {
		return nil, fmt.Errorf("project and repo are required")
	}
	tasks, err := c.dc.ListPullRequestTasks(ctx, scope.ProjectKey, scope.RepoSlug, id)
	if err != nil {
		return nil, mapHTTPError(err, fmt.Sprintf("tasks for pull request #%d", id))
	}
	out := make([]PullRequestTask, 0, len(tasks))
	for i := range tasks {
		out = append(out, mapDCTask(&tasks[i]))
	}
	return out, nil
}

// CreatePRTask creates a pull request task (DC).
func (c *Client) CreatePRTask(ctx context.Context, scope Scope, id int, text string) (*PullRequestTask, error) {
	if c.Kind != KindDC {
		return nil, DCOnly("pull request tasks", c.hostKindLabel())
	}
	if scope.ProjectKey == "" || scope.RepoSlug == "" {
		return nil, fmt.Errorf("project and repo are required")
	}
	task, err := c.dc.CreatePullRequestTask(ctx, scope.ProjectKey, scope.RepoSlug, id, text)
	if err != nil {
		return nil, mapHTTPError(err, fmt.Sprintf("tasks for pull request #%d", id))
	}
	m := mapDCTask(task)
	return &m, nil
}

// CompletePRTask resolves a task. Idempotent: a no-op when already resolved
// (changed=false).
func (c *Client) CompletePRTask(ctx context.Context, scope Scope, id, taskID int) (*PullRequestTask, bool, error) {
	return c.setPRTask(ctx, scope, id, taskID, true)
}

// ReopenPRTask reopens a task. Idempotent: a no-op when already open
// (changed=false).
func (c *Client) ReopenPRTask(ctx context.Context, scope Scope, id, taskID int) (*PullRequestTask, bool, error) {
	return c.setPRTask(ctx, scope, id, taskID, false)
}

func (c *Client) setPRTask(ctx context.Context, scope Scope, id, taskID int, resolve bool) (*PullRequestTask, bool, error) {
	if c.Kind != KindDC {
		return nil, false, DCOnly("pull request tasks", c.hostKindLabel())
	}
	if scope.ProjectKey == "" || scope.RepoSlug == "" {
		return nil, false, fmt.Errorf("project and repo are required")
	}
	tasks, err := c.dc.ListPullRequestTasks(ctx, scope.ProjectKey, scope.RepoSlug, id)
	if err != nil {
		return nil, false, mapHTTPError(err, fmt.Sprintf("tasks for pull request #%d", id))
	}
	want := dc.TaskStateResolved
	if !resolve {
		want = dc.TaskStateOpen
	}
	for i := range tasks {
		if tasks[i].ID == taskID && tasks[i].State == want {
			m := mapDCTask(&tasks[i])
			return &m, false, nil
		}
	}
	task, err := c.dc.SetPullRequestTaskState(ctx, scope.ProjectKey, scope.RepoSlug, id, taskID, resolve)
	if err != nil {
		return nil, false, mapHTTPError(err, fmt.Sprintf("task #%d", taskID))
	}
	m := mapDCTask(task)
	return &m, true, nil
}

func mapDCTask(t *dc.PullRequestTask) PullRequestTask {
	return PullRequestTask{
		ID:     t.ID,
		State:  strings.ToLower(strings.TrimSpace(t.State)),
		Text:   t.Text,
		Author: dcDisplayName(t.Author),
	}
}

// --- suggestions (DC) ---------------------------------------------------

// ListPRSuggestions scans a pull request's comments for inline code
// suggestions (Data Center). Best-effort: Bitbucket DC surfaces suggestions in
// comment properties, so a comment counts as a suggestion carrier when its
// properties carry a suggestion indicator. Comments without that indicator are
// skipped; an empty result is definitive for this scan.
func (c *Client) ListPRSuggestions(ctx context.Context, scope Scope, id int) ([]Suggestion, error) {
	if c.Kind != KindDC {
		return nil, DCOnly("pull request suggestions", c.hostKindLabel())
	}
	if scope.ProjectKey == "" || scope.RepoSlug == "" {
		return nil, fmt.Errorf("project and repo are required")
	}
	comments, err := c.dc.ListPullRequestComments(ctx, scope.ProjectKey, scope.RepoSlug, id)
	if err != nil {
		return nil, mapHTTPError(err, fmt.Sprintf("suggestions for pull request #%d", id))
	}
	out := make([]Suggestion, 0)
	for i := range comments {
		if sug := extractSuggestion(&comments[i]); sug != nil {
			out = append(out, *sug)
		}
	}
	return out, nil
}

// ApplyPRSuggestion applies a code suggestion (DC). Idempotent: a no-op when
// the suggestion is already applied (the API reports it).
func (c *Client) ApplyPRSuggestion(ctx context.Context, scope Scope, prID, commentID, suggestionID int) (bool, error) {
	if c.Kind != KindDC {
		return false, DCOnly("pull request suggestions", c.hostKindLabel())
	}
	if scope.ProjectKey == "" || scope.RepoSlug == "" {
		return false, fmt.Errorf("project and repo are required")
	}
	// Preview first to honor idempotency: an already-applied suggestion is a no-op.
	if sug, err := c.dc.SuggestionPreview(ctx, scope.ProjectKey, scope.RepoSlug, prID, commentID, suggestionID); err == nil && sug != nil && sug.Applied {
		return false, nil
	} else if err != nil && !isNotFound(err) {
		return false, mapHTTPError(err, fmt.Sprintf("suggestion #%d", suggestionID))
	}
	if err := c.dc.ApplySuggestion(ctx, scope.ProjectKey, scope.RepoSlug, prID, commentID, suggestionID); err != nil {
		return false, mapHTTPError(err, fmt.Sprintf("suggestion #%d", suggestionID))
	}
	return true, nil
}

// extractSuggestion reads a suggestion out of a DC comment's properties when
// present. Bitbucket DC keys suggestion metadata under a "suggestion"-named
// property; this matches case-insensitively and falls back to nil.
func extractSuggestion(c *dc.PullRequestComment) *Suggestion {
	if c == nil || len(c.Properties) == 0 {
		return nil
	}
	for k, v := range c.Properties {
		if !strings.Contains(strings.ToLower(k), "suggest") {
			continue
		}
		text := ""
		applied := false
		if m, ok := v.(map[string]any); ok {
			if t, ok := m["text"].(string); ok {
				text = t
			}
			if t, ok := m["applied"].(bool); ok {
				applied = t
			}
		}
		if text == "" {
			text = strings.TrimSpace(c.Text)
		}
		return &Suggestion{ID: c.ID, CommentID: c.ID, Text: text, Applied: applied}
	}
	return nil
}

// --- checks (cross-platform) -------------------------------------------

// PRChecks returns build/CI statuses for a pull request's head commit. It
// resolves the head SHA per platform, then delegates to the shared
// CommitStatuses adapter so both lines share one mapping path.
func (c *Client) PRChecks(ctx context.Context, scope Scope, id int) ([]BuildStatus, error) {
	var sha string
	switch c.Kind {
	case KindCloud:
		if scope.Workspace == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("workspace and repo are required")
		}
		pr, err := c.cloud.GetPullRequest(ctx, scope.Workspace, scope.RepoSlug, id)
		if err != nil {
			return nil, mapHTTPError(err, fmt.Sprintf("pull request #%d", id))
		}
		sha = strings.TrimSpace(pr.Source.Commit.Hash)
	case KindDC:
		if scope.ProjectKey == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("project and repo are required")
		}
		pr, err := c.dc.GetPullRequest(ctx, scope.ProjectKey, scope.RepoSlug, id)
		if err != nil {
			return nil, mapHTTPError(err, fmt.Sprintf("pull request #%d", id))
		}
		sha = strings.TrimSpace(pr.FromRef.LatestCommit)
	default:
		return nil, fmt.Errorf("unsupported host kind %q", c.Kind)
	}
	if sha == "" {
		return nil, fmt.Errorf("pull request #%d has no head commit to inspect", id)
	}
	return c.CommitStatuses(ctx, scope, sha)
}
