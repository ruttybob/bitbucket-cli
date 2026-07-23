package app

import (
	"io"
)

// app holds the bespoke command dispatcher (~300 lines, no cobra). It parses
// `noun verb [positionals] [flags]`, declares a per-command known-flag set,
// rejects unknown flags by name with an inline valid-flag list (exit 2), routes
// --help to a per-command TOON help block, and routes no-args to the
// content-first home view.

// FlagType restricts a flag's accepted value kind so the parser can coerce and
// the usage error can stay precise.
type FlagType int

const (
	FlagString FlagType = iota
	FlagInt
	FlagBool
)

// Flag declares one known flag on a command. The declared set is authoritative:
// any flag not present here (and not global) is rejected by name with exit 2.
type Flag struct {
	Name    string
	Type    FlagType
	Default any
	Desc    string
}

// FlagSet is the ordered, declared flag set for a command.
type FlagSet []Flag

// Known reports whether name is a declared flag.
func (fs FlagSet) Known(name string) bool {
	for _, f := range fs {
		if f.Name == name {
			return true
		}
	}
	return false
}

// Names returns the declared flag names in declaration order.
func (fs FlagSet) Names() []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Name)
	}
	return out
}

// globalFlags pass on every command and are never reported as unknown. Per AXI
// §6 a CLI may standardize its own always-allowed set; --help is universal, and
// --json/--yaml are the structured-output escape hatches.
var globalFlags = map[string]FlagType{
	"help": FlagBool,
	"json": FlagBool,
	"yaml": FlagBool,
}

// isGlobal reports whether name is an always-allowed global flag.
func isGlobal(name string) bool {
	_, ok := globalFlags[name]
	return ok
}

// Example is one usage example in a command's --help block.
type Example struct {
	Cmd  string // the runnable command (with placeholders for runtime values)
	What string // what it does
}

// Command is one node in the noun/verb tree. Nouns (pr, auth) carry Children
// and no Run; verbs (list, view) carry a Run and a declared FlagSet.
type Command struct {
	Name     string
	Aliases  []string
	Short    string // one-line summary, shown in noun help and the home view
	Long     string // longer description, shown in the command's --help block
	Flags    FlagSet
	MinArgs  int // minimum positional count (inclusive)
	MaxArgs  int // maximum positional count; -1 means unlimited
	Examples []Example
	Run      func(ctx *Context) error
	Children []*Command

	parent *Command
}

// findChild resolves a name or alias to a child command.
func (c *Command) findChild(name string) *Command {
	for _, ch := range c.Children {
		if ch.Name == name {
			return ch
		}
		for _, a := range ch.Aliases {
			if a == name {
				return ch
			}
		}
	}
	return nil
}

// path renders the command path for messages, e.g. "pr list".
func (c *Command) path() string {
	if c.parent == nil || c.parent.Name == "" {
		return c.Name
	}
	return c.parent.path() + " " + c.Name
}

// App is the root dispatcher. It owns the command tree and the IO streams.
type App struct {
	Name        string
	Description string
	Version     string // binary version, shown by `--version`
	BinPath     string // absolute path of the running executable (home view)
	Commands    []*Command
	Stdout      io.Writer
	Stderr      io.Writer

	// Home renders the no-args content-first dashboard. The commands package
	// assigns this when wiring the App so the dispatcher stays free of domain
	// logic; a nil Home falls back to a minimal identity view.
	Home func(a *App) error
}

// findNoun resolves argv[0] to a top-level command.
func (a *App) findNoun(name string) *Command {
	for _, n := range a.Commands {
		if n.Name == name {
			return n
		}
		for _, al := range n.Aliases {
			if al == name {
				return n
			}
		}
	}
	return nil
}

// stdout/stderr accessors default to os.Stdout/os.Stderr when unset so tests
// can inject buffers without wiring every field.
func (a *App) out() io.Writer {
	if a.Stdout != nil {
		return a.Stdout
	}
	return defaultStdout
}

// Println writes s plus a trailing newline to the app's stdout.
func (a *App) Println(s string) {
	io.WriteString(a.out(), s+"\n")
}
