package bitbucket

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/ruttybob/bkt-axi/internal/bitbucket/cloud"
)

// issue.go adapts the salvaged Cloud issue client into the normalized Issue
// model. This is the SINGLE place that switches on host.Kind for issues;
// Data Center's issue tracker was removed in modern releases, so any DC host
// gets a clear Cloud-only error here.

// IssueListOptions mirrors the cross-command issue filter set.
type IssueListOptions struct {
	State string // "", or a Cloud state token (open, resolved, …); "all" disables filtering
	Limit int    // page size cap; <=0 uses 50
}

const defaultIssueLimit = 50

// ListIssues fetches one bounded page of issues for scope.
func (c *Client) ListIssues(ctx context.Context, scope Scope, opts IssueListOptions) (*IssueListResult, error) {
	if c.Kind != KindCloud {
		return nil, CloudOnly("issues", c.hostKindLabel())
	}
	if scope.Workspace == "" || scope.RepoSlug == "" {
		return nil, fmt.Errorf("workspace and repo are required; use --workspace/--repo or set a context")
	}
	limit := clampIssueLimit(opts.Limit)
	issues, err := c.cloud.ListIssues(ctx, scope.Workspace, scope.RepoSlug, cloud.IssueListOptions{
		State: opts.State,
		Limit: limit,
	})
	if err != nil {
		return nil, mapHTTPError(err, "issues")
	}
	out := make([]Issue, 0, len(issues))
	for i := range issues {
		out = append(out, mapCloudIssue(&issues[i]))
	}
	// The salvaged client flattens pages up to limit; when it returned exactly
	// `limit` items a further page may exist.
	more := len(issues) == limit && limit > 0
	return &IssueListResult{Issues: out, Shown: len(out), MoreAvailable: more}, nil
}

func clampIssueLimit(n int) int {
	if n <= 0 {
		return defaultIssueLimit
	}
	if n > 100 {
		return 100
	}
	return n
}

// GetIssue fetches a single issue and maps it to the normalized model.
func (c *Client) GetIssue(ctx context.Context, scope Scope, id int) (*Issue, error) {
	if c.Kind != KindCloud {
		return nil, CloudOnly("issues", c.hostKindLabel())
	}
	if scope.Workspace == "" || scope.RepoSlug == "" {
		return nil, fmt.Errorf("workspace and repo are required; use --workspace/--repo or set a context")
	}
	issue, err := c.cloud.GetIssue(ctx, scope.Workspace, scope.RepoSlug, id)
	if err != nil {
		return nil, mapHTTPError(err, fmtIssue(id))
	}
	m := mapCloudIssue(issue)
	return &m, nil
}

// IssueCreateInput configures issue creation.
type IssueCreateInput struct {
	Title    string
	Content  string
	Kind     string
	Priority string
	Assignee string
}

// CreateIssue creates a new issue and returns the normalized result.
func (c *Client) CreateIssue(ctx context.Context, scope Scope, in IssueCreateInput) (*Issue, error) {
	if c.Kind != KindCloud {
		return nil, CloudOnly("issues", c.hostKindLabel())
	}
	if scope.Workspace == "" || scope.RepoSlug == "" {
		return nil, fmt.Errorf("workspace and repo are required; use --workspace/--repo or set a context")
	}
	issue, err := c.cloud.CreateIssue(ctx, scope.Workspace, scope.RepoSlug, cloud.CreateIssueInput{
		Title:    in.Title,
		Content:  in.Content,
		Kind:     in.Kind,
		Priority: in.Priority,
		Assignee: in.Assignee,
	})
	if err != nil {
		return nil, mapHTTPError(err, "issue")
	}
	m := mapCloudIssue(issue)
	return &m, nil
}

// IssueEditInput configures issue edits. Pointer fields distinguish "unset"
// from "clear". Assignee=="" clears the assignee.
type IssueEditInput struct {
	Title    *string
	Content  *string
	Kind     *string
	Priority *string
	Assignee *string
}

// UpdateIssue edits an issue and returns the normalized result.
func (c *Client) UpdateIssue(ctx context.Context, scope Scope, id int, in IssueEditInput) (*Issue, error) {
	if c.Kind != KindCloud {
		return nil, CloudOnly("issues", c.hostKindLabel())
	}
	if scope.Workspace == "" || scope.RepoSlug == "" {
		return nil, fmt.Errorf("workspace and repo are required; use --workspace/--repo or set a context")
	}
	issue, err := c.cloud.UpdateIssue(ctx, scope.Workspace, scope.RepoSlug, id, cloud.UpdateIssueInput{
		Title:    in.Title,
		Content:  in.Content,
		Kind:     in.Kind,
		Priority: in.Priority,
		Assignee: in.Assignee,
	})
	if err != nil {
		return nil, mapHTTPError(err, fmtIssue(id))
	}
	m := mapCloudIssue(issue)
	return &m, nil
}

// CloseIssue resolves an issue. changed is false (idempotent no-op) when the
// issue is already in a terminal state.
func (c *Client) CloseIssue(ctx context.Context, scope Scope, id int) (*Issue, bool, error) {
	if c.Kind != KindCloud {
		return nil, false, CloudOnly("issues", c.hostKindLabel())
	}
	current, err := c.GetIssue(ctx, scope, id)
	if err != nil {
		return nil, false, err
	}
	if isTerminalIssueState(current.State) {
		return current, false, nil
	}
	state := "resolved"
	issue, err := c.cloud.UpdateIssue(ctx, scope.Workspace, scope.RepoSlug, id, cloud.UpdateIssueInput{State: &state})
	if err != nil {
		return nil, false, mapHTTPError(err, fmtIssue(id))
	}
	m := mapCloudIssue(issue)
	return &m, true, nil
}

// ReopenIssue reopens an issue. changed is false (idempotent no-op) when the
// issue is already in an active state.
func (c *Client) ReopenIssue(ctx context.Context, scope Scope, id int) (*Issue, bool, error) {
	if c.Kind != KindCloud {
		return nil, false, CloudOnly("issues", c.hostKindLabel())
	}
	current, err := c.GetIssue(ctx, scope, id)
	if err != nil {
		return nil, false, err
	}
	if isActiveIssueState(current.State) {
		return current, false, nil
	}
	state := "open"
	issue, err := c.cloud.UpdateIssue(ctx, scope.Workspace, scope.RepoSlug, id, cloud.UpdateIssueInput{State: &state})
	if err != nil {
		return nil, false, mapHTTPError(err, fmtIssue(id))
	}
	m := mapCloudIssue(issue)
	return &m, true, nil
}

// CreateIssueComment adds a comment to an issue.
func (c *Client) CreateIssueComment(ctx context.Context, scope Scope, id int, body string) error {
	if c.Kind != KindCloud {
		return CloudOnly("issues", c.hostKindLabel())
	}
	if scope.Workspace == "" || scope.RepoSlug == "" {
		return fmt.Errorf("workspace and repo are required")
	}
	if _, err := c.cloud.CreateIssueComment(ctx, scope.Workspace, scope.RepoSlug, id, body); err != nil {
		return mapHTTPError(err, fmtIssue(id))
	}
	return nil
}

// ListIssueComments fetches the normalized comment thread for an issue.
func (c *Client) ListIssueComments(ctx context.Context, scope Scope, id int) ([]Comment, error) {
	if c.Kind != KindCloud {
		return nil, CloudOnly("issues", c.hostKindLabel())
	}
	if scope.Workspace == "" || scope.RepoSlug == "" {
		return nil, fmt.Errorf("workspace and repo are required")
	}
	comments, err := c.cloud.ListIssueComments(ctx, scope.Workspace, scope.RepoSlug, id, 100)
	if err != nil {
		return nil, mapHTTPError(err, fmtIssue(id))
	}
	out := make([]Comment, 0, len(comments))
	for i := range comments {
		out = append(out, mapCloudIssueComment(&comments[i]))
	}
	return out, nil
}

// ListIssueAttachments fetches attachments for an issue.
func (c *Client) ListIssueAttachments(ctx context.Context, scope Scope, id int) ([]IssueAttachment, error) {
	if c.Kind != KindCloud {
		return nil, CloudOnly("issues", c.hostKindLabel())
	}
	if scope.Workspace == "" || scope.RepoSlug == "" {
		return nil, fmt.Errorf("workspace and repo are required")
	}
	atts, err := c.cloud.ListIssueAttachments(ctx, scope.Workspace, scope.RepoSlug, id)
	if err != nil {
		return nil, mapHTTPError(err, fmtIssue(id))
	}
	out := make([]IssueAttachment, 0, len(atts))
	for i := range atts {
		out = append(out, IssueAttachment{Name: atts[i].Name, URL: atts[i].Links.Self.Href})
	}
	return out, nil
}

// UploadIssueAttachment uploads a file as an issue attachment.
func (c *Client) UploadIssueAttachment(ctx context.Context, scope Scope, id int, filename string, r io.Reader) (*IssueAttachment, error) {
	if c.Kind != KindCloud {
		return nil, CloudOnly("issues", c.hostKindLabel())
	}
	if scope.Workspace == "" || scope.RepoSlug == "" {
		return nil, fmt.Errorf("workspace and repo are required")
	}
	att, err := c.cloud.UploadIssueAttachment(ctx, scope.Workspace, scope.RepoSlug, id, filename, r)
	if err != nil {
		return nil, mapHTTPError(err, fmtIssue(id))
	}
	return &IssueAttachment{Name: att.Name, URL: att.Links.Self.Href}, nil
}

// DownloadIssueAttachment streams an issue attachment to w.
func (c *Client) DownloadIssueAttachment(ctx context.Context, scope Scope, id int, filename string, w io.Writer) error {
	if c.Kind != KindCloud {
		return CloudOnly("issues", c.hostKindLabel())
	}
	if scope.Workspace == "" || scope.RepoSlug == "" {
		return fmt.Errorf("workspace and repo are required")
	}
	if err := c.cloud.DownloadIssueAttachment(ctx, scope.Workspace, scope.RepoSlug, id, filename, w); err != nil {
		return mapHTTPError(err, fmt.Sprintf("attachment %q on issue #%d", filename, id))
	}
	return nil
}

// DeleteIssueAttachment deletes an attachment from an issue.
func (c *Client) DeleteIssueAttachment(ctx context.Context, scope Scope, id int, filename string) error {
	if c.Kind != KindCloud {
		return CloudOnly("issues", c.hostKindLabel())
	}
	if scope.Workspace == "" || scope.RepoSlug == "" {
		return fmt.Errorf("workspace and repo are required")
	}
	if err := c.cloud.DeleteIssueAttachment(ctx, scope.Workspace, scope.RepoSlug, id, filename); err != nil {
		return mapHTTPError(err, fmt.Sprintf("attachment %q on issue #%d", filename, id))
	}
	return nil
}

// --- issue state classification -----------------------------------------

// terminal issue states: the issue is considered closed/done.
var terminalIssueStates = map[string]bool{
	"resolved": true, "closed": true, "duplicate": true,
	"invalid": true, "wontfix": true,
}

// active issue states: the issue is considered open/workable.
var activeIssueStates = map[string]bool{
	"new": true, "open": true,
}

func isTerminalIssueState(state string) bool {
	return terminalIssueStates[strings.ToLower(strings.TrimSpace(state))]
}

func isActiveIssueState(state string) bool {
	return activeIssueStates[strings.ToLower(strings.TrimSpace(state))]
}

// --- mappers -------------------------------------------------------------

func mapCloudIssue(i *cloud.Issue) Issue {
	return Issue{
		ID:        i.ID,
		Title:     i.Title,
		State:     strings.ToLower(strings.TrimSpace(i.State)),
		Priority:  strings.TrimSpace(i.Priority),
		Kind:      strings.TrimSpace(i.Kind),
		Assignee:  cloudAccountName(i.Assignee),
		Reporter:  cloudAccountName(i.Reporter),
		Content:   strings.TrimSpace(i.Content.Raw),
		URL:       i.Links.HTML.Href,
		CreatedAt: parseTime(i.CreatedOn),
		UpdatedAt: parseTime(i.UpdatedOn),
	}
}

func mapCloudIssueComment(c *cloud.IssueComment) Comment {
	out := Comment{
		ID:        c.ID,
		Text:      strings.TrimSpace(c.Content.Raw),
		CreatedAt: parseTime(c.CreatedOn),
		State:     "open",
	}
	if c.User != nil {
		out.Author = cloudAccountName(c.User)
	}
	return out
}

// cloudAccountName returns the best display name for a Cloud account pointer.
func cloudAccountName(a *cloud.Account) string {
	if a == nil {
		return ""
	}
	if a.DisplayName != "" {
		return a.DisplayName
	}
	if a.Nickname != "" {
		return a.Nickname
	}
	return a.UUID
}

// cloudUserName returns the best display name for a Cloud participant User
// (distinct from the Account type used by issue reporters/assignees).
func cloudUserName(u cloud.User) string {
	if u.Display != "" {
		return u.Display
	}
	if u.Nickname != "" {
		return u.Nickname
	}
	if u.Username != "" {
		return u.Username
	}
	return u.UUID
}

func fmtIssue(id int) string {
	return "issue #" + strconv.Itoa(id)
}
