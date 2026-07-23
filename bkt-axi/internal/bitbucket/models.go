package bitbucket

import "time"

// This package holds the NORMALIZED domain layer: one set of models that the
// AXI command layer renders, independent of whether the active host is
// Bitbucket Cloud or Data Center. Each adapter (pr.go) calls a salvaged client
// (internal/bitbucket/cloud or /dc) as-is and maps its response into these
// structs. The per-command `switch host.Kind` this replaces used to live in
// every pkg/cmd/<x>/*.go file; here it lives once, in the adapter.

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

// Repo is the minimal normalized repository, used by the home view.
type Repo struct {
	Slug          string `toon:"slug"`
	Name          string `toon:"name"`
	Project       string `toon:"project"`   // DC project key
	Workspace     string `toon:"workspace"` // Cloud workspace
	DefaultBranch string `toon:"default_branch"`
	URL           string `toon:"url"`
}
