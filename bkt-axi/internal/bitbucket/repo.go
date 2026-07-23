package bitbucket

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruttybob/bkt-axi/internal/bitbucket/cloud"
	"github.com/ruttybob/bkt-axi/internal/bitbucket/dc"
)

// repo.go adapts the salvaged line-specific clients into the normalized Repo
// model. This file is the SINGLE place that switches on host.Kind for
// repositories; the command layer never does.

// RepoListOptions configures a repository listing. Repositories are workspace-
// scoped (Cloud) or project-scoped (DC); RepoSlug is not required here.
type RepoListOptions struct {
	Filter string // optional case-insensitive name/slug substring filter
	Limit  int    // page size cap; <=0 uses 100
}

const defaultRepoLimit = 100

func clampRepoLimit(n int) int {
	if n <= 0 {
		return defaultRepoLimit
	}
	if n > 100 {
		return 100
	}
	return n
}

// ListRepos fetches one bounded page of repositories for the resolved scope
// (Cloud workspace or DC project) and maps them to the normalized model.
func (c *Client) ListRepos(ctx context.Context, scope Scope, opts RepoListOptions) (*RepoListResult, error) {
	limit := clampRepoLimit(opts.Limit)
	switch c.Kind {
	case KindCloud:
		return c.listReposCloud(ctx, scope, opts, limit)
	case KindDC:
		return c.listReposDC(ctx, scope, opts, limit)
	}
	return nil, fmt.Errorf("unsupported host kind %q", c.Kind)
}

func (c *Client) listReposCloud(ctx context.Context, scope Scope, opts RepoListOptions, limit int) (*RepoListResult, error) {
	if scope.Workspace == "" {
		return nil, fmt.Errorf("workspace is required; use --workspace or set a context")
	}
	page, err := c.cloud.ListRepositoriesPage(ctx, scope.Workspace, limit, "")
	if err != nil {
		return nil, mapHTTPError(err, "repositories")
	}
	repos := make([]Repo, 0, len(page.Values))
	for i := range page.Values {
		r := mapCloudRepo(&page.Values[i])
		if repoMatches(r, opts.Filter) {
			repos = append(repos, r)
		}
	}
	return &RepoListResult{Repos: repos, Shown: len(repos), MoreAvailable: page.Next != ""}, nil
}

func (c *Client) listReposDC(ctx context.Context, scope Scope, opts RepoListOptions, limit int) (*RepoListResult, error) {
	if scope.ProjectKey == "" {
		return nil, fmt.Errorf("project is required; use --project or set a context")
	}
	page, err := c.dc.ListRepositoriesPage(ctx, scope.ProjectKey, limit, 0)
	if err != nil {
		return nil, mapHTTPError(err, "repositories")
	}
	repos := make([]Repo, 0, len(page.Values))
	for i := range page.Values {
		r := mapDCRepo(&page.Values[i])
		if repoMatches(r, opts.Filter) {
			repos = append(repos, r)
		}
	}
	// DC exposes an authoritative total only at the page level when paging to
	// the end; the first page's IsLast tells us whether more exist.
	return &RepoListResult{Repos: repos, Shown: len(repos), MoreAvailable: !page.IsLast}, nil
}

// GetRepo fetches a single repository. slug falls back to scope.RepoSlug when
// empty, so `repo view` with no argument shows the resolved repo.
func (c *Client) GetRepo(ctx context.Context, scope Scope, slug string) (*Repo, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		slug = scope.RepoSlug
	}
	if slug == "" {
		return nil, fmt.Errorf("repository slug is required; use --repo or set a context")
	}
	switch c.Kind {
	case KindCloud:
		if scope.Workspace == "" {
			return nil, fmt.Errorf("workspace is required; use --workspace or set a context")
		}
		repo, err := c.cloud.GetRepository(ctx, scope.Workspace, slug)
		if err != nil {
			return nil, mapHTTPError(err, fmt.Sprintf("repository %s/%s", scope.Workspace, slug))
		}
		r := mapCloudRepo(repo)
		return &r, nil
	case KindDC:
		if scope.ProjectKey == "" {
			return nil, fmt.Errorf("project is required; use --project or set a context")
		}
		repo, err := c.dc.GetRepository(ctx, scope.ProjectKey, slug)
		if err != nil {
			return nil, mapHTTPError(err, fmt.Sprintf("repository %s/%s", scope.ProjectKey, slug))
		}
		r := mapDCRepo(repo)
		return &r, nil
	}
	return nil, fmt.Errorf("unsupported host kind %q", c.Kind)
}

// --- mappers -------------------------------------------------------------

func mapCloudRepo(r *cloud.Repository) Repo {
	out := Repo{
		Slug:          r.Slug,
		Name:          r.Name,
		SCM:           normalizeSCM(r.SCM),
		Workspace:     r.Workspace.Slug,
		Project:       r.Project.Key,
		Visibility:    cloudVisibility(r.IsPrivate),
		DefaultBranch: r.MainBranch.Name,
		URL:           r.Links.HTML.Href,
		Updated:       parseTime(r.UpdatedOn),
	}
	out.CloneHTTPS, out.CloneSSH = cloudCloneURLs(r.Links.Clone)
	return out
}

func mapDCRepo(r *dc.Repository) Repo {
	out := Repo{
		Slug:          r.Slug,
		Name:          r.Name,
		SCM:           "git",
		Project:       dcProjectKey(r.Project),
		DefaultBranch: r.DefaultBranch,
		URL:           firstDCRepoLink(r.Links.Web, r.Links.Self),
	}
	out.CloneHTTPS, out.CloneSSH = dcCloneURLs(r.Links.Clone)
	return out
}

func normalizeSCM(s string) string {
	if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
		return s
	}
	return "git"
}

// cloudVisibility derives a private/public label from the Cloud is_private flag.
func cloudVisibility(isPrivate bool) string {
	if isPrivate {
		return "private"
	}
	return "public"
}

func cloudCloneURLs(clones []struct {
	Href string `json:"href"`
	Name string `json:"name"`
}) (https, ssh string) {
	for _, c := range clones {
		switch strings.ToLower(c.Name) {
		case "https":
			https = c.Href
		case "ssh":
			ssh = c.Href
		}
	}
	return
}

func dcCloneURLs(clones []struct {
	Href string `json:"href"`
	Name string `json:"name"`
}) (https, ssh string) {
	for _, c := range clones {
		switch strings.ToLower(c.Name) {
		case "http", "https":
			https = c.Href
		case "ssh":
			ssh = c.Href
		}
	}
	return
}

func dcProjectKey(p *dc.Project) string {
	if p == nil {
		return ""
	}
	return p.Key
}

func firstDCRepoLink(web, self []struct {
	Href string `json:"href"`
}) string {
	if len(web) > 0 && web[0].Href != "" {
		return web[0].Href
	}
	if len(self) > 0 {
		return self[0].Href
	}
	return ""
}

// repoMatches reports whether a repo's slug or name contains filter
// (case-insensitive). An empty filter matches everything.
func repoMatches(r Repo, filter string) bool {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return true
	}
	needle := strings.ToLower(filter)
	return strings.Contains(strings.ToLower(r.Slug), needle) ||
		strings.Contains(strings.ToLower(r.Name), needle)
}

// RepoScopeWord renders the workspace/project for count lines ("in acme" /
// "in project KEY"). It is line-aware because repos are not repo-scoped.
func RepoScopeWord(scope Scope) string {
	if scope.Workspace != "" {
		return scope.Workspace
	}
	if scope.ProjectKey != "" {
		return "project " + scope.ProjectKey
	}
	return "the resolved scope"
}
