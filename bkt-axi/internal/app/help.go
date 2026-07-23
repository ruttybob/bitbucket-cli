package app

import (
	"strings"

	"github.com/ruttybob/bkt-axi/internal/axi"
)

// help.go renders the AXI help blocks as TOON: the no-args home fallback, the
// noun-subtree help (`pr` with no verb), and the per-command --help block. All
// are structured documents the agent can parse, not prose manuals.

// runHome renders the no-args content-first dashboard. When the commands
// package has registered a Home builder, it does the live-data work; otherwise
// a minimal identity view (bin + description + available commands) is shown.
func (a *App) runHome() error {
	if a.Home != nil {
		return a.Home(a)
	}
	rows := make([]axi.Object, 0, len(a.Commands))
	for _, n := range a.Commands {
		rows = append(rows, axi.NewObject(
			axi.KV{Key: "name", Value: n.Name},
			axi.KV{Key: "description", Value: n.Short},
		))
	}
	doc := axi.NewObject(
		axi.KV{Key: "bin", Value: a.displayBin()},
		axi.KV{Key: "description", Value: a.Description},
		axi.KV{Key: "commands", Value: rows},
		axi.KV{Key: "help", Value: axi.HelpRows([]string{"Run `bkt-axi <command> --help` for details"})},
	)
	a.Println(axi.Marshal(doc))
	return nil
}

// displayBin returns the executable path with the home directory collapsed to ~.
func (a *App) displayBin() string {
	if a.BinPath == "" {
		return a.Name
	}
	return collapseHome(a.BinPath)
}

// printNounHelp renders the subcommand list for a noun (e.g. `pr` with no verb,
// or a grouped noun like `pr reviewer`). It uses the full command path so
// nested nouns identify themselves ("issue attachment", not just "attachment").
func (a *App) printNounHelp(noun *Command) error {
	rows := make([]axi.Object, 0, len(noun.Children))
	for _, v := range noun.Children {
		rows = append(rows, axi.NewObject(
			axi.KV{Key: "name", Value: v.Name},
			axi.KV{Key: "description", Value: v.Short},
		))
	}
	doc := axi.NewObject(
		axi.KV{Key: "command", Value: noun.path()},
		axi.KV{Key: "description", Value: noun.Long},
		axi.KV{Key: "subcommands", Value: rows},
		axi.KV{Key: "help", Value: axi.HelpRows([]string{
			"Run `bkt-axi " + noun.path() + " <subcommand> --help` for details",
		})},
	)
	a.Println(axi.Marshal(doc))
	return nil
}

// printCommandHelp renders a verb's concise --help block: description, usage,
// declared flags with defaults, and examples (AXI §10).
func (a *App) printCommandHelp(cmd *Command) error {
	fields := []axi.KV{
		{Key: "command", Value: cmd.path()},
		{Key: "description", Value: firstNonEmpty(cmd.Long, cmd.Short)},
		{Key: "usage", Value: a.Name + " " + cmd.path() + positionalHint(cmd) + " [flags]"},
	}
	if len(cmd.Flags) > 0 {
		flagRows := make([]axi.Object, 0, len(cmd.Flags))
		for _, f := range cmd.Flags {
			flagRows = append(flagRows, axi.NewObject(
				axi.KV{Key: "flag", Value: flagLabel(f)},
				axi.KV{Key: "description", Value: f.Desc},
				axi.KV{Key: "default", Value: flagDefaultDisplay(f.Default)},
			))
		}
		fields = append(fields, axi.KV{Key: "flags", Value: flagRows})
	}
	if len(cmd.Examples) > 0 {
		exRows := make([]axi.Object, 0, len(cmd.Examples))
		for _, ex := range cmd.Examples {
			exRows = append(exRows, axi.NewObject(
				axi.KV{Key: "cmd", Value: ex.Cmd},
				axi.KV{Key: "what", Value: ex.What},
			))
		}
		fields = append(fields, axi.KV{Key: "examples", Value: exRows})
	}
	a.Println(axi.Marshal(axi.NewObject(fields...)))
	return nil
}

func positionalHint(cmd *Command) string {
	switch {
	case cmd.MinArgs == 0 && cmd.MaxArgs == 0:
		return ""
	case cmd.MinArgs > 0 && cmd.MaxArgs == cmd.MinArgs:
		return " <args>"
	case cmd.MinArgs == 1 && cmd.MaxArgs < 0:
		return " <id>..."
	default:
		return " [args]"
	}
}

func flagDefaultDisplay(v any) string {
	switch d := v.(type) {
	case nil:
		return ""
	case string:
		if d == "" {
			return ""
		}
		return d
	case int:
		return strings.TrimSpace(toa(d))
	case bool:
		if d {
			return "true"
		}
		return "false"
	}
	return strings.TrimSpace(formatAny(v))
}

// flagLabel renders a flag's display name with its short alias when declared.
func flagLabel(f Flag) string {
	if f.Short != "" {
		return "--" + f.Name + ", -" + f.Short
	}
	return "--" + f.Name
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// collapseHome rewrites a leading home directory to ~ for compact display.
func collapseHome(p string) string {
	home := userHomeDir()
	if home != "" && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
