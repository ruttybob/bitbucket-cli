package commands

import (
	"fmt"
	"sort"

	"github.com/ruttybob/bkt-axi/internal/app"
	"github.com/ruttybob/bkt-axi/internal/axi"
	"github.com/ruttybob/bkt-axi/internal/config"
)

// context.go implements local configuration-context management. Contexts are
// pure local state (no API calls): named bundles of host + scope defaults that
// the resolver picks up via --context or the active context.

// NewContextCmd builds the `context` noun.
func NewContextCmd() *app.Command {
	return &app.Command{
		Name:  "context",
		Short: "Manage configuration contexts",
		Long:  "Create, list, switch, and delete named configuration contexts (host + scope defaults).",
		Children: []*app.Command{
			newContextListCmd(),
			newContextCreateCmd(),
			newContextUseCmd(),
			newContextDeleteCmd(),
		},
	}
}

// contextRow is the normalized view of a context for `context list`.
type contextRow struct {
	Name    string `toon:"name"`
	Host    string `toon:"host"`
	Kind    string `toon:"kind"`
	Project string `toon:"project"`
	Repo    string `toon:"repo"`
	Active  bool   `toon:"-"`
}

var contextListSchema = []axi.Field{
	{Key: "name", Extractor: axi.Pluck("name")},
	{Key: "host", Extractor: axi.Pluck("host")},
	{Key: "kind", Extractor: axi.Pluck("kind")},
	{Key: "project", Extractor: axi.Pluck("project")},
	{Key: "repo", Extractor: axi.Pluck("repo")},
}

func newContextListCmd() *app.Command {
	return &app.Command{
		Name:    "list",
		Aliases: []string{"ls"},
		Short:   "List configuration contexts",
		Long:    "List named configuration contexts with their host, kind, and scope defaults. The default schema is {name,host,kind,project,repo}.",
		MinArgs: 0, MaxArgs: 0,
		Examples: []app.Example{{Cmd: "bkt-axi context list", What: "show configured contexts"}},
		Run:      runContextList,
	}
}

func runContextList(ctx *app.Context) error {
	cfg, err := ctx.Config()
	if err != nil {
		return err
	}
	names := make([]string, 0, len(cfg.Contexts))
	for n := range cfg.Contexts {
		names = append(names, n)
	}
	sort.Strings(names)

	rows := make([]contextRow, 0, len(names))
	for _, n := range names {
		c := cfg.Contexts[n]
		if c == nil {
			continue
		}
		kind := ""
		if h, ok := cfg.Hosts[c.Host]; ok && h != nil {
			kind = h.Kind
		}
		rows = append(rows, contextRow{
			Name: n, Host: c.Host, Kind: kind,
			Project: c.ProjectKey, Repo: c.DefaultRepo, Active: n == cfg.ActiveContext,
		})
	}
	if len(rows) == 0 {
		emitEmpty(ctx, "contexts", "0 contexts configured", []string{
			"Run `bkt-axi context create <name> --host <host>` to add a context",
		})
		return nil
	}
	help := []string{"Run `bkt-axi context use <name>` to switch the active context"}
	emitList(ctx, "contexts", toAny(rows), contextListSchema, len(rows), help)
	return nil
}

func newContextCreateCmd() *app.Command {
	return &app.Command{
		Name:  "create",
		Short: "Create or update a context",
		Long:  "Create (or update) a named context. --host is required and must reference a configured host.",
		Flags: app.FlagSet{
			{Name: "host", Type: app.FlagString, Default: "", Desc: "Host key this context uses (required)"},
			{Name: "project", Type: app.FlagString, Default: "", Desc: "Default Data Center project key"},
			{Name: "workspace", Type: app.FlagString, Default: "", Desc: "Default Cloud workspace"},
			{Name: "repo", Type: app.FlagString, Default: "", Desc: "Default repository slug"},
		},
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{{Cmd: "bkt-axi context create prod --host mydc --project ENG --repo api", What: "add a DC context"}},
		Run:      runContextCreate,
	}
}

func runContextCreate(ctx *app.Context) error {
	cfg, err := ctx.Config()
	if err != nil {
		return err
	}
	name := ctx.Args[0]
	host := ctx.Flags.String("host")
	if host == "" {
		return axi.UsageError("`context create` requires --host")
	}
	if _, ok := cfg.Hosts[host]; !ok {
		return axi.Errorf("host %q is not configured; run `bkt-axi auth login` first", host)
	}
	cfg.SetContext(name, &config.Context{
		Host:        host,
		ProjectKey:  ctx.Flags.String("project"),
		Workspace:   ctx.Flags.String("workspace"),
		DefaultRepo: ctx.Flags.String("repo"),
	})
	if err := cfg.Save(); err != nil {
		return axi.Errorf("saving config: %s", err)
	}
	emitConfirmation(ctx, fmt.Sprintf("context %s now uses host %s", name, host))
	return nil
}

func newContextUseCmd() *app.Command {
	return &app.Command{
		Name:    "use",
		Aliases: []string{"switch"},
		Short:   "Switch the active context",
		Long:    "Set the active context by name.",
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{{Cmd: "bkt-axi context use prod", What: "activate the prod context"}},
		Run:      runContextUse,
	}
}

func runContextUse(ctx *app.Context) error {
	cfg, err := ctx.Config()
	if err != nil {
		return err
	}
	name := ctx.Args[0]
	if err := cfg.SetActiveContext(name); err != nil {
		return axi.Errorf("context %q not found", name)
	}
	if err := cfg.Save(); err != nil {
		return axi.Errorf("saving config: %s", err)
	}
	emitConfirmation(ctx, "active context is now "+name)
	return nil
}

func newContextDeleteCmd() *app.Command {
	return &app.Command{
		Name:    "delete",
		Aliases: []string{"rm"},
		Short:   "Delete a context",
		Long:    "Remove a named context. Idempotent: a no-op when the context does not exist.",
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{{Cmd: "bkt-axi context delete old", What: "remove the old context"}},
		Run:      runContextDelete,
	}
}

func runContextDelete(ctx *app.Context) error {
	cfg, err := ctx.Config()
	if err != nil {
		return err
	}
	name := ctx.Args[0]
	if _, ok := cfg.Contexts[name]; !ok {
		emitConfirmation(ctx, "context "+name+" already absent (no-op)")
		return nil
	}
	cfg.DeleteContext(name)
	if err := cfg.Save(); err != nil {
		return axi.Errorf("saving config: %s", err)
	}
	emitConfirmation(ctx, "deleted context "+name)
	return nil
}
