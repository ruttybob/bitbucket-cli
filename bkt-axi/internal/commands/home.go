package commands

import (
	"context"
	"time"

	"github.com/ruttybob/bkt-axi/internal/app"
	"github.com/ruttybob/bkt-axi/internal/axi"
	"github.com/ruttybob/bkt-axi/internal/bitbucket"
	"github.com/ruttybob/bkt-axi/internal/config"
)

// home.go renders the content-first no-args dashboard (AXI §8). It resolves the
// CWD git remote → context → host → repo, and — when auth and a repo resolve —
// shows the agent's open PRs and PRs awaiting their review. When nothing
// resolves it shows a setup nudge instead of help text.

const homeLimit = 5

// RunHome is wired as App.Home. It never returns a hard error: a failed
// resolution degrades to the setup nudge, since "no auth yet" is a valid first
// run state, not a crash.
func RunHome(a *app.App) error {
	cfg, err := config.Load()
	if err != nil {
		printSetupNudge(a, "could not read config: "+err.Error())
		return nil
	}

	r, err := bitbucket.ResolveHost(cfg, "", "")
	if err != nil {
		printSetupNudge(a, "")
		return nil
	}

	scope := bitbucket.ResolveScope(r, bitbucket.ScopeOverrides{})

	header := []axi.KV{
		{Key: "bin", Value: a.BinPath},
		{Key: "description", Value: a.Description},
		{Key: "host", Value: bitbucket.HostDisplay(r)},
		{Key: "kind", Value: r.Host.Kind},
	}
	if r.Host.Kind == "cloud" {
		header = append(header, axi.KV{Key: "workspace", Value: scope.Workspace})
	} else {
		header = append(header, axi.KV{Key: "project", Value: scope.ProjectKey})
	}
	header = append(header, axi.KV{Key: "repo", Value: scope.String()})

	// Best-effort live PR fetch. Any failure degrades silently to a header-only
	// dashboard so the agent always gets the orienting context.
	if !scope.Empty() {
		if client, cerr := bitbucket.NewClient(r.Host, r.HostKey); cerr == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			if id, _, _ := client.CurrentUser(ctx); id != "" {
				if mine, merr := client.ListPRs(ctx, scope, bitbucket.PRListOptions{Mine: id, State: "open", Limit: homeLimit}); merr == nil && len(mine.PRs) > 0 {
					header = append(header, axi.KV{Key: "prs_mine", Value: axi.Rows(toAny(mine.PRs), prsMineSchema())})
				}
				if review, rerr := client.ListPRs(ctx, scope, bitbucket.PRListOptions{Reviewer: id, State: "open", Limit: homeLimit}); rerr == nil && len(review.PRs) > 0 {
					header = append(header, axi.KV{Key: "prs_review", Value: axi.Rows(toAny(review.PRs), prsReviewSchema())})
				}
			}
		}
	}

	header = append(header, axi.KV{Key: "help", Value: axi.HelpRows(homeHelp(scope))})
	a.Println(axi.Marshal(axi.NewObject(header...)))
	return nil
}

func prsMineSchema() []axi.Field {
	return []axi.Field{
		{Key: "id", Extractor: axi.Pluck("id")},
		{Key: "title", Extractor: axi.Pluck("title")},
		{Key: "state", Extractor: axi.Pluck("state")},
		{Key: "review", Extractor: axi.Pluck("review")},
	}
}

func prsReviewSchema() []axi.Field {
	return []axi.Field{
		{Key: "id", Extractor: axi.Pluck("id")},
		{Key: "title", Extractor: axi.Pluck("title")},
		{Key: "author", Extractor: axi.Pluck("author")},
		{Key: "state", Extractor: axi.Pluck("state")},
	}
}

func homeHelp(scope bitbucket.Scope) []string {
	hints := []string{
		"Run `bkt-axi pr list` for all pull requests in this repository",
		"Run `bkt-axi pr view <id>` for a pull request's details",
	}
	if scope.Empty() {
		hints = append(hints, "No repository resolved from the current directory; run `bkt-axi` inside a Bitbucket checkout or set --repo")
	}
	return hints
}

// printSetupNudge renders the first-run / no-auth state.
func printSetupNudge(a *app.App, detail string) {
	fields := []axi.KV{
		{Key: "bin", Value: a.BinPath},
		{Key: "description", Value: a.Description},
	}
	hints := []string{
		"Run `bkt-axi auth login` to authenticate a Bitbucket host (Cloud or Data Center)",
		"Set BKT_HOST and BKT_TOKEN for headless use",
		"Run `bkt-axi auth status` to see configured hosts",
	}
	if detail != "" {
		fields = append(fields, axi.KV{Key: "note", Value: detail})
	}
	fields = append(fields, axi.KV{Key: "help", Value: axi.HelpRows(hints)})
	a.Println(axi.Marshal(axi.NewObject(fields...)))
}
