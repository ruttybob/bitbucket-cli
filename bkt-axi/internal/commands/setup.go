package commands

// setup.go implements `bkt-axi setup` (AXI §7): a user-invoked command that
// installs or repairs session hooks for Claude Code, Codex, and OpenCode, and
// optionally refreshes the generated SKILL.md into each harness's skill dir.
// It is idempotent and path-repairing: re-running with the same binary is a
// silent no-op, and a moved/reinstalled binary is updated in place.

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ruttybob/bkt-axi/internal/app"
	"github.com/ruttybob/bkt-axi/internal/axi"
	"github.com/ruttybob/bkt-axi/internal/session"
)

// NewSetupCmd builds the `setup` noun.
func NewSetupCmd() *app.Command {
	return &app.Command{
		Name:  "setup",
		Short: "Install or repair session hooks and skill",
		Long: "Install (or repair) the AXI §7 session integration for one or more " +
			"agent harnesses. A SessionStart hook injects the live home view at the " +
			"start of every session. --skill also writes the generated SKILL.md into " +
			"each harness's skill directory. Idempotent: re-running with the same " +
			"binary is a silent no-op.",
		Flags: app.FlagSet{
			{Name: "app", Type: app.FlagString, Default: "all",
				Desc: "target harness: claude, codex, opencode, or all"},
			{Name: "skill", Type: app.FlagBool, Default: false,
				Desc: "also install/refresh the generated SKILL.md into each harness skill dir"},
		},
		MinArgs: 0, MaxArgs: 0,
		Examples: []app.Example{
			{Cmd: "bkt-axi setup --app all", What: "install hooks for every supported harness"},
			{Cmd: "bkt-axi setup --app claude --skill", What: "install the Claude hook and skill"},
		},
		Run: runSetup,
	}
}

// setupRow is one line of the setup result summary.
type setupRow struct {
	App    string `toon:"app"`
	Action string `toon:"action"`
	Target string `toon:"target"`
}

var setupSchema = []axi.Field{
	{Key: "app", Extractor: axi.Pluck("app")},
	{Key: "action", Extractor: axi.Pluck("action")},
	{Key: "target", Extractor: axi.Pluck("target")},
}

func runSetup(ctx *app.Context) error {
	appFlag := strings.ToLower(strings.TrimSpace(ctx.Flags.String("app")))
	if appFlag == "" {
		appFlag = "all"
	}
	if !session.ValidApp(appFlag) {
		return axi.UsageError("`setup` received invalid --app " + appFlag).
			With("valid --app values: claude, codex, opencode, all")
	}

	binPath := realExecutablePath()
	results, err := session.Install(session.App(appFlag), binPath)
	// A failed install for one app is reported in the result summary; only a
	// total failure (unknown app) short-circuits here.

	rows := make([]setupRow, 0, len(results)*2)
	notes := make([]string, 0)
	for _, r := range results {
		rows = append(rows, setupRow{
			App:    string(r.App),
			Action: string(r.Action),
			Target: r.Path,
		})
		if r.Note != "" {
			notes = append(notes, string(r.App)+": "+r.Note)
		}
	}

	// Optionally refresh the skill into each installed harness's skill dir.
	if ctx.Flags.Bool("skill") {
		apps := session.AllApps()
		if session.App(appFlag) != session.AppAll {
			apps = []session.App{session.App(appFlag)}
		}
		for _, a := range apps {
			dir, derr := session.SkillDirFor(a)
			if derr != nil {
				notes = append(notes, string(a)+": skill dir unavailable: "+derr.Error())
				continue
			}
			path, action, werr := session.WriteSkill(ctx.App, dir)
			if werr != nil {
				notes = append(notes, string(a)+": skill write failed: "+werr.Error())
				continue
			}
			rows = append(rows, setupRow{
				App:    string(a) + ":skill",
				Action: string(action),
				Target: path,
			})
		}
	}

	help := []string{
		"Re-running `bkt-axi setup` with the same binary is a no-op",
		"Run `bkt-axi setup` again after moving or reinstalling bkt-axi to repair hook paths",
	}
	if len(notes) > 0 {
		// Surface non-fatal detail (e.g. an enabled feature flag) as help hints.
		help = append(help, notes...)
	}

	emitList(ctx, "result", toAny(rows), setupSchema, len(rows), help)
	if err != nil {
		// An install error for a concrete app is surfaced in the summary above;
		// return it so the exit code reflects the partial failure.
		return axi.Errorf("setup completed with errors: %s", err)
	}
	return nil
}

// realExecutablePath returns the absolute path of the running binary, used to
// decide whether a hook can use the bare PATH name or needs the absolute path.
// It mirrors main.binPath but does NOT collapse the home directory, since the
// hook command needs a real, shell-resolvable path. Falls back to "" (which
// ResolveCommand turns into the bare name) when the executable cannot be
// resolved (e.g. `go run`).
func realExecutablePath() string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return ""
	}
	if abs, err := filepath.Abs(exe); err == nil {
		return abs
	}
	return exe
}
