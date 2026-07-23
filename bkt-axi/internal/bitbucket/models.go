package bitbucket

import "time"

// This package holds the NORMALIZED domain layer: one set of models that the
// AXI command layer renders, independent of whether the active host is
// Bitbucket Cloud or Data Center. Each adapter (pr.go, repo.go, branch.go,
// commit.go, pipeline.go) calls a salvaged client (internal/bitbucket/cloud or
// /dc) as-is and maps its response into these structs. The per-command
// `switch host.Kind` this replaces used to live in every pkg/cmd/<x>/*.go file;
// here it lives once, in the adapter.

// PR is the normalized pull request. Field order and toon tags match the
// canonical detail schema; list schemas project a subset via the axi field DSL.
type PR struct {
	ID          int       `toon:"id"`
	Title       string    `toon:"title"`
	State       string    `toon:"state"` // open|merged|declined (lowercased)
	Draft       bool      `toon:"draft"`
	Author      string    `toon:"author"`
	From        string    `toon:"from"` // source branch display id
	To          string    `toon:"to"`   // target branch display id
	Review      string    `toon:"review"`
	Checks      string    `toon:"checks"`
	Comments    int       `toon:"comments"`
	Description string    `toon:"description"`
	URL         string    `toon:"url"`
	CreatedAt   time.Time `toon:"created_at"`
	Reviewers   []string  `toon:"reviewers"`
}

// ReviewSummary is the derived review state surfaced as the `review` column.
// It is computed once per PR by the adapter so commands never touch raw
// participant/reviewer payloads.
type ReviewSummary string

const (
	ReviewApproved ReviewSummary = "approved"
	ReviewChanges  ReviewSummary = "changes_requested"
	ReviewRequired ReviewSummary = "required"
)

// PRListResult bundles a page of normalized PRs with pagination metadata so a
// command can render "count: N of M total" or "count: N shown (more available)".
type PRListResult struct {
	PRs           []PR
	Shown         int  // len(PRs)
	MoreAvailable bool // upstream reported a next page we did not follow
	Total         int  // authoritative total when the API exposes one; else 0
}

// Comment is a normalized pull request comment used by `pr view --comments`.
type Comment struct {
	ID        int       `toon:"id"`
	Author    string    `toon:"author"`
	CreatedAt time.Time `toon:"created_at"`
	State     string    `toon:"state"` // open|resolved (DC) / presence-based (cloud)
	Text      string    `toon:"text"`
}

// PRMutation is the result of a normalized PR mutation: the updated pull
// request plus a flag reporting whether the target state already held (an
// idempotent no-op, AXI §6). Commands render the "(already — no-op)" suffix on
// the state field when Already is true.
type PRMutation struct {
	PR      *PR
	Already bool
}

// Repo is the normalized repository. The home view and `repo` commands render
// it; the `repo list` schema projects a subset via the axi field DSL.
type Repo struct {
	Slug          string    `toon:"slug"`
	Name          string    `toon:"name"`
	SCM           string    `toon:"scm"`       // git (default) | mercurial
	Project       string    `toon:"project"`   // DC project key (Cloud project key when present)
	Workspace     string    `toon:"workspace"` // Cloud workspace slug
	Visibility    string    `toon:"visibility"`
	DefaultBranch string    `toon:"default_branch"`
	URL           string    `toon:"url"`
	CloneHTTPS    string    `toon:"clone_https"`
	CloneSSH      string    `toon:"clone_ssh"`
	Updated       time.Time `toon:"updated"`
}

// RepoListResult bundles a page of normalized repos with pagination metadata.
type RepoListResult struct {
	Repos         []Repo
	Shown         int
	MoreAvailable bool
	Total         int // authoritative total when the API exposes one (DC); else 0
}

// Branch is the normalized branch. Message/Author/UpdatedAt are commit-derived
// and only populated when the caller asks for commit detail (opt-in, since it
// costs one commit fetch per branch).
type Branch struct {
	Name         string    `toon:"name"`
	IsDefault    bool      `toon:"default"`
	LatestCommit string    `toon:"latest_commit"`
	Message      string    `toon:"message"`
	Author       string    `toon:"author"`
	UpdatedAt    time.Time `toon:"updated"`
	DisplayID    string    `toon:"display_id"`
	Type         string    `toon:"type"`
}

// BranchListResult bundles a page of normalized branches with pagination metadata.
type BranchListResult struct {
	Branches      []Branch
	Shown         int
	MoreAvailable bool
}

// BranchMutation is the result of a normalized branch mutation. DC branch
// creation echoes the new branch; the Already flag covers a future "already
// exists" no-op. Name is the human-friendly branch name (e.g. "feature/x").
type BranchMutation struct {
	Name    string
	Already bool
}

// Commit is the normalized commit used by `commit view`.
type Commit struct {
	SHA         string    `toon:"sha"`
	Message     string    `toon:"message"`
	Author      string    `toon:"author"`
	AuthorEmail string    `toon:"author_email"`
	AuthoredAt  time.Time `toon:"authored_at"`
	CommittedAt time.Time `toon:"committed_at"`
	Parents     []string  `toon:"parents"`
	URL         string    `toon:"url"`
}

// Pipeline is the normalized Cloud pipeline run used by `pipeline list/view`.
// Data Center has no pipelines concept, so this is Cloud-shaped.
type Pipeline struct {
	BuildNumber int       `toon:"build"`
	UUID        string    `toon:"uuid"`
	State       string    `toon:"state"`  // IN_PROGRESS|COMPLETED|PENDING|HALTED
	Result      string    `toon:"result"` // SUCCESSFUL|FAILED|ERROR (when completed)
	Ref         string    `toon:"ref"`
	Trigger     string    `toon:"trigger"`
	CreatedAt   time.Time `toon:"created_at"`
	CompletedAt time.Time `toon:"completed_at"`
	Duration    string    `toon:"duration"` // humanized, derived when completed
}

// PipelineStep is one step execution within a pipeline run.
type PipelineStep struct {
	Name   string `toon:"name"`
	UUID   string `toon:"uuid"`
	State  string `toon:"state"`
	Result string `toon:"result"`
}

// PipelineListResult bundles a page of normalized pipelines with pagination
// metadata.
type PipelineListResult struct {
	Pipelines     []Pipeline
	Shown         int
	MoreAvailable bool
}

// BuildStatus is the normalized build/CI status attached to a commit, used by
// `commit status`. State is normalized to lowercase (e.g. successful|failed|in
// progress|stopped).
type BuildStatus struct {
	State       string `toon:"state"`
	Key         string `toon:"key"`
	Name        string `toon:"name"`
	URL         string `toon:"url"`
	Description string `toon:"description"`
}

// --- Phase 3 normalized models ------------------------------------------
//
// Each noun below mirrors the §one-switch principle: the command layer reads
// one struct regardless of host kind; the per-line mapping lives once in the
// matching adapter file (issue.go, webhook.go, …).

// Issue is the normalized Bitbucket Cloud issue. DC's issue tracker was
// removed in modern releases, so issues are Cloud-only.
type Issue struct {
	ID        int       `toon:"id"`
	Title     string    `toon:"title"`
	State     string    `toon:"state"` // new|open|resolved|closed|duplicate|invalid|wontfix|on hold (lowercased)
	Priority  string    `toon:"priority"`
	Kind      string    `toon:"kind"`
	Assignee  string    `toon:"assignee"`
	Reporter  string    `toon:"reporter"`
	Content   string    `toon:"content"`
	URL       string    `toon:"url"`
	CreatedAt time.Time `toon:"created_at"`
	UpdatedAt time.Time `toon:"updated_at"`
}

// IssueListResult bundles a bounded issue page with pagination metadata.
type IssueListResult struct {
	Issues        []Issue
	Shown         int
	MoreAvailable bool
}

// Webhook is the normalized repository webhook. ID is the API's opaque
// identifier rendered as a string (Cloud UUID or DC numeric id-as-string).
type Webhook struct {
	ID      string   `toon:"id"`
	Name    string   `toon:"name"` // DC name / Cloud description
	URL     string   `toon:"url"`
	Active  bool     `toon:"active"`
	Events  []string `toon:"events"`
	Created string   `toon:"created"` // best-effort; empty when the API omits it
}

// WebhookListResult bundles a bounded webhook page with pagination metadata.
type WebhookListResult struct {
	Webhooks      []Webhook
	Shown         int
	MoreAvailable bool
}

// Variable is the normalized Bitbucket Cloud pipeline variable, scoped to
// repo, workspace, or a deployment environment.
type Variable struct {
	Key     string `toon:"key"`
	Value   string `toon:"value"`
	Secured bool   `toon:"secured"`
	Scope   string `toon:"scope"` // repo|workspace|deployment
	UUID    string `toon:"-"`     // internal: API identity for update/delete
}

// Project is the normalized Bitbucket Data Center project.
type Project struct {
	Key         string `toon:"key"`
	Name        string `toon:"name"`
	Description string `toon:"description"`
}

// Permission is the normalized Data Center user permission assignment.
type Permission struct {
	User       string `toon:"user"`
	Permission string `toon:"permission"`
}

// PullRequestTask is the normalized pull request task (Data Center blocker
// comment surfaced as a checkable task).
type PullRequestTask struct {
	ID     int    `toon:"id"`
	State  string `toon:"state"` // open|resolved (lowercased)
	Text   string `toon:"text"`
	Author string `toon:"author"`
}

// RateLimitInfo is the normalized rate-limit telemetry derived from response
// headers (Cloud has no clean rate-limit endpoint; DC surfaces headers too).
type RateLimitInfo struct {
	Limit     int       `toon:"limit"`
	Remaining int       `toon:"remaining"`
	Reset     time.Time `toon:"reset"`
	Source    string    `toon:"source"`
}

// Reviewer is the normalized pull request reviewer (used by `pr reviewer list`).
type Reviewer struct {
	Name     string `toon:"name"`
	State    string `toon:"state"` // approved|changes_requested|unreviewed
	Approved bool   `toon:"approved"`
}

// Suggestion is the normalized inline code suggestion on a pull request.
type Suggestion struct {
	ID        int    `toon:"id"`
	CommentID int    `toon:"comment_id"`
	Text      string `toon:"text"`
	Applied   bool   `toon:"applied"`
}

// IssueAttachment is the normalized Bitbucket Cloud issue attachment.
type IssueAttachment struct {
	Name string `toon:"name"`
	URL  string `toon:"url"`
}
