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
