package commands

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ruttybob/bkt-axi/internal/app"
	"github.com/ruttybob/bkt-axi/internal/axi"
	"github.com/ruttybob/bkt-axi/internal/bitbucket"
	"github.com/ruttybob/bkt-axi/internal/git"
)

// repo.go implements the repository commands: the Phase 1 read verbs `repo
// list` and `repo view`, and the Phase 2 mutation verbs `repo create` and
// `repo clone`.

// NewRepoCmd builds the `repo` noun and its verbs.
func NewRepoCmd() *app.Command {
	return &app.Command{
		Name:  "repo",
		Short: "Work with repositories",
		Long:  "List, inspect, create, and clone repositories across Bitbucket Cloud and Data Center.",
		Children: []*app.Command{
			newRepoListCmd(),
			newRepoViewCmd(),
			newRepoCreateCmd(),
			newRepoCloneCmd(),
		},
	}
}

func newRepoListCmd() *app.Command {
	flags := append(app.FlagSet{
		{Name: "limit", Type: app.FlagInt, Default: 100, Desc: "Maximum number of repositories to show (1-100)"},
		{Name: "fields", Type: app.FlagString, Default: "", Desc: "Extra columns (comma-sep): project,visibility,default_branch,url,updated"},
	}, selectorFlags()...)
	return &app.Command{
		Name:    "list",
		Aliases: []string{"ls"},
		Short:   "List repositories in the resolved workspace or project",
		Long:    "List repositories for the resolved workspace (Cloud) or project (Data Center). The default schema is {slug,name,scm}; use --fields to add columns.",
		Flags:   flags,
		MinArgs: 0, MaxArgs: 0,
		Examples: []app.Example{
			{Cmd: "bkt-axi repo list", What: "repositories in the resolved workspace/project"},
			{Cmd: "bkt-axi repo list --fields visibility,default_branch", What: "add visibility and default branch columns"},
		},
		Run: runRepoList,
	}
}

func newRepoViewCmd() *app.Command {
	flags := append(app.FlagSet{
		{Name: "web", Type: app.FlagBool, Default: false, Desc: "Print the repository's web URL"},
	}, selectorFlags()...)
	return &app.Command{
		Name:    "view",
		Short:   "Show details for a repository",
		Long:    "Display a repository's full state: slug, name, project, default branch, visibility, web URL, and clone URLs. With no argument, shows the resolved repository.",
		Flags:   flags,
		MinArgs: 0, MaxArgs: 1,
		Examples: []app.Example{
			{Cmd: "bkt-axi repo view", What: "details for the resolved repository"},
			{Cmd: "bkt-axi repo view api", What: "details for the `api` repository"},
			{Cmd: "bkt-axi repo view api --web", What: "print the repository's web URL"},
		},
		Run: runRepoView,
	}
}

// repoExtraFields maps a --fields token to its schema column.
var repoExtraFields = map[string]axi.Field{
	"project":        {Key: "project", Extractor: axi.Pluck("project")},
	"visibility":     {Key: "visibility", Extractor: axi.Pluck("visibility")},
	"default_branch": {Key: "default_branch", Extractor: axi.Pluck("default_branch")},
	"url":            {Key: "url", Extractor: axi.Pluck("url")},
	"updated":        {Key: "updated", Extractor: axi.RelativeTime(axi.Pluck("updated"))},
}

func repoListSchema(extra []string) ([]axi.Field, error) {
	schema := []axi.Field{
		{Key: "slug", Extractor: axi.Pluck("slug")},
		{Key: "name", Extractor: axi.Pluck("name")},
		{Key: "scm", Extractor: axi.Pluck("scm")},
	}
	return extendSchema(schema, repoExtraFields, extra, "repo list")
}

// extendSchema appends --fields extras to a base schema, rejecting unknown
// tokens with exit 2 and the allowed list. It is shared by every list command
// so the --fields contract is identical across nouns.
func extendSchema(base []axi.Field, extras map[string]axi.Field, requested []string, cmd string) ([]axi.Field, error) {
	seen := make(map[string]bool, len(base))
	for _, f := range base {
		seen[f.Key] = true
	}
	for _, name := range requested {
		if seen[name] {
			continue
		}
		f, ok := extras[name]
		if !ok {
			allowed := sortedKeys(extras)
			e := axi.UsageError(fmt.Sprintf("unknown --fields value `%s` for `%s`", name, cmd))
			e.Suggestions = []string{"allowed --fields values: " + strings.Join(allowed, ", ")}
			return nil, e
		}
		base = append(base, f)
		seen[name] = true
	}
	return base, nil
}

func sortedKeys(m map[string]axi.Field) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// stable, human-friendly ordering: sort alphabetically.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}

func runRepoList(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Workspace == "" && scope.ProjectKey == "" {
		return axi.Errorf("no workspace or project resolved; use --workspace (Cloud) or --project (DC), or set a context")
	}

	schema, err := repoListSchema(parseFields(ctx.Flags.String("fields")))
	if err != nil {
		return err
	}

	result, err := client.ListRepos(context.Background(), scope, bitbucket.RepoListOptions{
		Limit: ctx.Flags.Int("limit"),
	})
	if err != nil {
		return err
	}

	scopeWord := bitbucket.RepoScopeWord(scope)

	if len(result.Repos) == 0 {
		msg := fmt.Sprintf("0 repositories in %s", scopeWord)
		doc := axi.NewObject(
			axi.KV{Key: "repositories", Value: msg},
			axi.KV{Key: "help", Value: axi.HelpRows(axi.EmptyRepoList(scopeWord))},
		)
		emit(ctx, map[string]any{"repositories": msg}, axi.Marshal(doc))
		return nil
	}

	items := toAny(result.Repos)
	count := countLine(result.Shown, result.Total, result.MoreAvailable)
	doc := axi.NewObject(
		axi.KV{Key: "count", Value: count},
		axi.KV{Key: "repositories", Value: axi.Rows(items, schema)},
		axi.KV{Key: "help", Value: axi.HelpRows(axi.AfterRepoList(result.MoreAvailable))},
	)
	payload := listPayloadRows("repositories", count, items, schema, axi.AfterRepoList(result.MoreAvailable))
	emit(ctx, payload, axi.Marshal(doc))
	return nil
}

func runRepoView(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}

	var slug string
	if len(ctx.Args) > 0 {
		slug = strings.TrimSpace(ctx.Args[0])
	}
	repo, err := client.GetRepo(context.Background(), scope, slug)
	if err != nil {
		return err
	}

	// --web short-circuits: just print the web URL (pipe-friendly, like `gh repo view --web`).
	if ctx.Flags.Bool("web") {
		emit(ctx, map[string]any{"url": repo.URL}, axi.Marshal(axi.NewObject(axi.KV{Key: "url", Value: repo.URL})))
		return nil
	}

	schema := []axi.Field{
		{Key: "slug", Extractor: axi.Pluck("slug")},
		{Key: "name", Extractor: axi.Pluck("name")},
		{Key: "scm", Extractor: axi.Pluck("scm")},
		{Key: "project", Extractor: axi.Pluck("project")},
		{Key: "visibility", Extractor: axi.Pluck("visibility")},
		{Key: "default_branch", Extractor: axi.Pluck("default_branch")},
		{Key: "url", Extractor: axi.Pluck("url")},
		{Key: "clone_https", Extractor: axi.Pluck("clone_https")},
		{Key: "clone_ssh", Extractor: axi.Pluck("clone_ssh")},
		{Key: "updated", Extractor: axi.RelativeTime(axi.Pluck("updated"))},
	}
	doc := axi.NewObject(axi.KV{Key: "repository", Value: axi.NewObject(axi.Ordered(*repo, schema)...)})
	// JSON/YAML carries the raw timestamp (TOON shows humanized relative time).
	detail := detailExtracted(repo, schema)
	detail["updated"] = rfc3339(repo.Updated)
	payload := map[string]any{"repository": detail}
	emit(ctx, payload, axi.Marshal(doc))
	return nil
}

// listPayloadRows builds the JSON/YAML payload mirroring a generic TOON list
// doc. label is the collection key (e.g. "repositories").
func listPayloadRows(label string, count any, items []any, schema []axi.Field, help []string) map[string]any {
	rows := make([]map[string]any, 0, len(items))
	for _, it := range items {
		rows = append(rows, axi.Extract(it, schema))
	}
	out := map[string]any{"count": count, label: rows}
	if len(help) > 0 {
		out["help"] = help
	}
	return out
}

// detailExtracted projects one item through schema for the JSON/YAML payload.
func detailExtracted(item any, schema []axi.Field) map[string]any {
	return axi.Extract(item, schema)
}

// --- mutations (Phase 2) -------------------------------------------------

// repoDetailSchema is the ordered field set for the repo detail confirm.
func repoDetailSchema() []axi.Field {
	return []axi.Field{
		{Key: "slug", Extractor: axi.Pluck("slug")},
		{Key: "name", Extractor: axi.Pluck("name")},
		{Key: "scm", Extractor: axi.Pluck("scm")},
		{Key: "project", Extractor: axi.Pluck("project")},
		{Key: "workspace", Extractor: axi.Pluck("workspace")},
		{Key: "default_branch", Extractor: axi.Pluck("default_branch")},
		{Key: "clone_https", Extractor: axi.Pluck("clone_https")},
		{Key: "clone_ssh", Extractor: axi.Pluck("clone_ssh")},
		{Key: "url", Extractor: axi.Pluck("url")},
	}
}

// emitRepoDetail renders the normalized repository as a detail document.
func emitRepoDetail(ctx *app.Context, repo *bitbucket.Repo) {
	doc := axi.NewObject(axi.KV{Key: "repository", Value: axi.NewObject(axi.Ordered(*repo, repoDetailSchema())...)})
	emit(ctx, detailRepoPayload(repo), axi.Marshal(doc))
}

func detailRepoPayload(repo *bitbucket.Repo) map[string]any {
	return map[string]any{
		"repository": map[string]any{
			"slug":           repo.Slug,
			"name":           repo.Name,
			"scm":            repo.SCM,
			"project":        repo.Project,
			"workspace":      repo.Workspace,
			"default_branch": repo.DefaultBranch,
			"clone_https":    repo.CloneHTTPS,
			"clone_ssh":      repo.CloneSSH,
			"url":            repo.URL,
		},
	}
}

// --- repo create ---------------------------------------------------------

func newRepoCreateCmd() *app.Command {
	flags := append(app.FlagSet{
		{Name: "public", Type: app.FlagBool, Default: false, Desc: "Make the repository public (private by default)"},
		{Name: "description", Type: app.FlagString, Default: "", Desc: "Repository description"},
		{Name: "scm", Type: app.FlagString, Default: "git", Desc: "Source control management (default git)"},
		{Name: "default-branch", Type: app.FlagString, Default: "", Desc: "Default branch name"},
		{Name: "forkable", Type: app.FlagBool, Default: false, Desc: "Data Center: allow forks"},
		{Name: "cloud-project", Type: app.FlagString, Default: "", Desc: "Cloud: project key to create the repository under"},
	}, selectorFlags()...)
	return &app.Command{
		Name:    "create",
		Short:   "Create a repository",
		Long:    "Create a repository. The positional argument is the repository slug on Cloud, or the repository name on Data Center (which derives the slug). --workspace (Cloud) or --project (Data Center) is required.",
		Flags:   flags,
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{
			{Cmd: "bkt-axi repo create api", What: "create a private repository named api"},
			{Cmd: "bkt-axi repo create api --public --description \"API service\"", What: "create a public repository with a description"},
		},
		Run: runRepoCreate,
	}
}

func runRepoCreate(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if client.Kind == bitbucket.KindCloud && scope.Workspace == "" {
		return axi.Errorf("a workspace is required; use --workspace or set a context")
	}
	if client.Kind == bitbucket.KindDC && scope.ProjectKey == "" {
		return axi.Errorf("a project key is required; use --project or set a context")
	}

	slug := strings.TrimSpace(ctx.Args[0])
	if slug == "" {
		return axi.UsageError("repository slug is required as the positional argument")
	}

	repo, err := client.CreateRepo(context.Background(), scope, bitbucket.CreateRepoInput{
		Slug:          slug,
		Description:   ctx.Flags.String("description"),
		SCM:           ctx.Flags.String("scm"),
		DefaultBranch: ctx.Flags.String("default-branch"),
		Public:        ctx.Flags.Bool("public"),
		Forkable:      ctx.Flags.Bool("forkable"),
		CloudProject:  ctx.Flags.String("cloud-project"),
	})
	if err != nil {
		return err
	}
	emitRepoDetail(ctx, repo)
	return nil
}

// --- repo clone ----------------------------------------------------------

func newRepoCloneCmd() *app.Command {
	flags := append(app.FlagSet{
		{Name: "ssh", Type: app.FlagBool, Default: false, Desc: "Clone over SSH instead of HTTPS"},
		{Name: "dest", Type: app.FlagString, Default: "", Desc: "Destination directory (defaults to the repository slug)"},
	}, selectorFlags()...)
	return &app.Command{
		Name:    "clone",
		Short:   "Clone a repository",
		Long:    "Clone a repository into a new directory. Resolves the clone URL from the repository's advertised links; --ssh selects the SSH link.",
		Flags:   flags,
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{
			{Cmd: "bkt-axi repo clone api", What: "clone the api repository over HTTPS"},
			{Cmd: "bkt-axi repo clone api --ssh --dest api-src", What: "clone over SSH into api-src"},
		},
		Run: runRepoClone,
	}
}

func runRepoClone(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	slug := strings.TrimSpace(ctx.Args[0])
	if slug == "" {
		return axi.UsageError("repository slug is required as the positional argument")
	}

	// Resolve scope with the slug as the repo override so the adapter fetches
	// the right repository's clone links.
	overrides := ctx.ScopeOverrides()
	overrides.RepoSlug = slug
	resolved, err := ctx.Resolve()
	if err != nil {
		return err
	}
	scope := bitbucket.ResolveScope(resolved, overrides)
	if client.Kind == bitbucket.KindCloud && scope.Workspace == "" {
		return axi.Errorf("a workspace is required; use --workspace or set a context")
	}
	if client.Kind == bitbucket.KindDC && scope.ProjectKey == "" {
		return axi.Errorf("a project key is required; use --project or set a context")
	}

	cloneURL, err := client.RepoCloneURL(context.Background(), scope, ctx.Flags.Bool("ssh"))
	if err != nil {
		return err
	}
	if cloneURL == "" {
		return axi.Errorf("repository %s has no clone URL available", scope.String())
	}

	dest := strings.TrimSpace(ctx.Flags.String("dest"))
	if dest == "" {
		dest = slug
	}
	if !filepath.IsAbs(dest) {
		if abs, derr := filepath.Abs(dest); derr == nil {
			dest = abs
		}
	}

	if _, e := git.Run("", "clone", cloneURL, dest); e != nil {
		return axi.Errorf("clone %s: %s", cloneURL, e)
	}

	doc := axi.NewObject(
		axi.KV{Key: "clone", Value: axi.NewObject(
			axi.KV{Key: "repository", Value: scope.String()},
			axi.KV{Key: "url", Value: cloneURL},
			axi.KV{Key: "dest", Value: dest},
		)},
	)
	payload := map[string]any{
		"clone": map[string]any{
			"repository": scope.String(),
			"url":        cloneURL,
			"dest":       dest,
		},
	}
	emit(ctx, payload, axi.Marshal(doc))
	return nil
}
