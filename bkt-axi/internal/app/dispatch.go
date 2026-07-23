package app

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/ruttybob/bkt-axi/internal/axi"
)

// ParsedFlags holds typed flag values merged from a command's declared
// defaults and the values supplied on the command line.
type ParsedFlags struct {
	vals map[string]any // name → typed value (string|int|bool)
	set  map[string]bool
}

// String returns the flag's string value (default when unset).
func (p ParsedFlags) String(name string) string {
	if v, ok := p.vals[name]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// Int returns the flag's int value (default when unset).
func (p ParsedFlags) Int(name string) int {
	if v, ok := p.vals[name]; ok {
		switch n := v.(type) {
		case int:
			return n
		case int64:
			return int(n)
		}
	}
	return 0
}

// Bool returns the flag's bool value (default when unset).
func (p ParsedFlags) Bool(name string) bool {
	if v, ok := p.vals[name]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// Changed reports whether name was explicitly set on the command line.
func (p ParsedFlags) Changed(name string) bool { return p.set[name] }

// StringSlice returns the accumulated []string for a FlagStringSlice flag. The
// slice is non-nil only when the flag was declared as a slice; otherwise nil.
func (p ParsedFlags) StringSlice(name string) []string {
	if v, ok := p.vals[name]; ok {
		if s, ok := v.([]string); ok {
			return s
		}
	}
	return nil
}

// errUnknownFlag carries the offending flag name so the dispatcher can build a
// self-correcting usage error with the command's valid-flag list.
type errUnknownFlag struct{ name string }

func (e *errUnknownFlag) Error() string { return "unknown flag --" + e.name }

// errFlagNeedsArg is returned when a value flag has no argument.
type errFlagNeedsArg struct{ name string }

func (e *errFlagNeedsArg) Error() string { return "flag --" + e.name + " requires an argument" }

// errBadValue is returned when a flag value cannot be coerced.
type errBadValue struct{ name, raw string }

func (e *errBadValue) Error() string { return "invalid value for --" + e.name + ": " + e.raw }

// parseFlags walks args, splitting them into typed flag values and positionals.
// A flag is --name, --name=value, or --name value (value flags). Bool flags take
// an optional =value (--draft=false). Any flag not declared on fs and not global
// is rejected by name (AXI §6). "--" terminates flag parsing.
func parseFlags(args []string, fs FlagSet) (ParsedFlags, []string, error) {
	parsed := ParsedFlags{
		vals: make(map[string]any, len(fs)+len(globalFlags)),
		set:  make(map[string]bool),
	}
	// seed declared defaults
	for _, f := range fs {
		parsed.vals[f.Name] = seedDefault(f)
	}
	for name, ft := range globalFlags {
		switch ft {
		case FlagBool:
			parsed.vals[name] = false
		default:
			parsed.vals[name] = ""
		}
	}

	var positionals []string
	i := 0
	for i < len(args) {
		tok := args[i]
		i++
		if tok == "--" {
			positionals = append(positionals, args[i:]...)
			break
		}
		// Short flag: a single leading dash followed by a non-dash char that
		// matches a declared Short. -F, -Fvalue, and -F value are all accepted.
		if isShortFlag(tok) {
			if name, inline, hasInline, ok, err := resolveShort(tok, fs); ok {
				if err != nil {
					return parsed, nil, err
				}
				if perr := parsed.consume(fs, name, inline, args, &i, withInline(hasInline)); perr != nil {
					return parsed, nil, perr
				}
				continue
			}
		}
		if !strings.HasPrefix(tok, "--") || tok == "--" {
			positionals = append(positionals, tok)
			continue
		}
		// strip leading --
		body := tok[2:]
		name := body
		var inline string
		hasInline := false
		if eq := strings.IndexByte(body, '='); eq >= 0 {
			name = body[:eq]
			inline = body[eq+1:]
			hasInline = true
		}
		name = strings.ToLower(name)

		if perr := parsed.consume(fs, name, inlineIf(hasInline, inline), args, &i, withInline(hasInline)); perr != nil {
			return parsed, nil, perr
		}
	}
	return parsed, positionals, nil
}

// seedDefault returns the zero value for a flag's type, used when no Default is
// declared. Slice flags seed to an empty (non-nil) slice so accessors are safe.
func seedDefault(f Flag) any {
	switch f.Type {
	case FlagStringSlice:
		if d, ok := f.Default.([]string); ok {
			return append([]string(nil), d...)
		}
		return []string{}
	case FlagBool:
		if f.Default != nil {
			return f.Default
		}
		return false
	default:
		if f.Default != nil {
			return f.Default
		}
		return ""
	}
}

// isShortFlag reports whether tok looks like a short flag: "-", then a letter,
// and not "--". Negative numbers ("-3") are excluded because the second char is
// not a letter.
func isShortFlag(tok string) bool {
	if len(tok) < 2 || tok[0] != '-' || tok[1] == '-' {
		return false
	}
	c := tok[1]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// resolveShort maps a -X[/value] token to its declared long name when a Flag
// with that Short exists. Returns ok=false when no short matches (the token is
// then treated as a positional). hasInline reports whether a value was attached
// to the token (-Fvalue / -F=value); an inline value attached to a bool short
// is a usage error.
func resolveShort(tok string, fs FlagSet) (name, inline string, hasInline, ok bool, err error) {
	body := tok[1:]
	short := string(body[0])
	for _, f := range fs {
		if f.Short == short {
			name = f.Name
			if len(body) > 1 {
				rest := body[1:]
				if rest[0] == '=' {
					rest = rest[1:]
				}
				if f.Type == FlagBool {
					return "", "", false, false, &errBadValue{name: name, raw: tok}
				}
				inline = rest
				hasInline = true
			}
			return name, inline, hasInline, true, nil
		}
	}
	return "", "", false, false, nil
}

// consume applies one flag occurrence (already name-resolved) to parsed. It
// handles type coercion, inline vs. next-arg value, and slice accumulation.
func (p ParsedFlags) consume(fs FlagSet, name, inline string, args []string, i *int, opts ...consumeOpt) error {
	ft, known := flagType(name, fs)
	if !known {
		return &errUnknownFlag{name: name}
	}
	hasInline := false
	for _, o := range opts {
		if o.inlineSet {
			hasInline = true
		}
	}

	switch ft {
	case FlagBool:
		val := true
		if hasInline {
			b, err := strconv.ParseBool(inline)
			if err != nil {
				return &errBadValue{name: name, raw: inline}
			}
			val = b
		}
		p.vals[name] = val
		p.set[name] = true
	case FlagInt:
		raw, ok := takeValue(args, i, inline, hasInline)
		if !ok {
			return &errFlagNeedsArg{name: name}
		}
		n, err := strconv.Atoi(raw)
		if err != nil {
			return &errBadValue{name: name, raw: raw}
		}
		p.vals[name] = n
		p.set[name] = true
	case FlagStringSlice:
		raw, ok := takeValue(args, i, inline, hasInline)
		if !ok {
			return &errFlagNeedsArg{name: name}
		}
		cur, _ := p.vals[name].([]string)
		p.vals[name] = append(cur, raw)
		p.set[name] = true
	default: // FlagString
		raw, ok := takeValue(args, i, inline, hasInline)
		if !ok {
			return &errFlagNeedsArg{name: name}
		}
		p.vals[name] = raw
		p.set[name] = true
	}
	return nil
}

// consumeOpt tunes consume; only the inline marker is used today.
type consumeOpt struct{ inlineSet bool }

func withInline(b bool) consumeOpt { return consumeOpt{inlineSet: b} }
func inlineIf(b bool, s string) string {
	if b {
		return s
	}
	return ""
}

// flagType reports the declared type of name across the command's set and the
// globals, and whether it is known at all.
func flagType(name string, fs FlagSet) (FlagType, bool) {
	if ft, ok := globalFlags[name]; ok {
		return ft, true
	}
	for _, f := range fs {
		if f.Name == name {
			return f.Type, true
		}
	}
	return 0, false
}

// takeValue returns the inline value when present, else consumes the next arg.
func takeValue(args []string, i *int, inline string, hasInline bool) (string, bool) {
	if hasInline {
		return inline, true
	}
	if *i >= len(args) {
		return "", false
	}
	v := args[*i]
	*i++
	return v, true
}

// validateArgs enforces the command's positional-arg bounds.
func validateArgs(cmd *Command, args []string) error {
	if cmd.MinArgs > 0 && len(args) < cmd.MinArgs {
		return axi.UsageError(fmt.Sprintf(
			"`%s` requires at least %d positional argument(s), got %d",
			cmd.path(), cmd.MinArgs, len(args)))
	}
	if cmd.MaxArgs >= 0 && len(args) > cmd.MaxArgs {
		return axi.UsageError(fmt.Sprintf(
			"`%s` accepts at most %d positional argument(s), got %d",
			cmd.path(), cmd.MaxArgs, len(args)))
	}
	return nil
}

// Run is the entry point: it dispatches argv and renders any error as TOON on
// stdout. Returns the AXI exit code (0 success, 1 error, 2 usage).
func (a *App) Run(argv []string) int {
	a.wireParents()
	err := a.dispatch(argv)
	if err == nil {
		return axi.ExitSuccess
	}
	code := axi.ExitCode(err)
	a.renderError(err)
	return code
}

// renderError prints a structured error to stdout. Non-AxiError values are
// wrapped so dependency noise never leaks; the agent always sees a clean
// `error:` line plus any help hints.
func (a *App) renderError(err error) {
	var ae *axi.AxiError
	if errors.As(err, &ae) {
		io.WriteString(a.out(), axi.RenderError(ae)+"\n")
		return
	}
	io.WriteString(a.out(), axi.RenderError(axi.Errorf("%s", err.Error()))+"\n")
}

// dispatch resolves the noun/verb path, parses flags, and invokes the command.
// It returns nil on success or an *axi.AxiError (usage or runtime).
func (a *App) dispatch(argv []string) error {
	if len(argv) == 0 {
		return a.runHome()
	}

	// Root-level universal flags: --help/-h/help route to the content-first
	// home view (§8); --version prints the version line.
	switch argv[0] {
	case "--help", "-h", "help":
		return a.runHome()
	case "--version", "-V":
		a.Println(a.Name + " " + a.Version)
		return nil
	}

	noun := a.findNoun(argv[0])
	if noun == nil {
		return unknownCommandErr(argv[0], a.nounNames())
	}

	// Walk the command tree to arbitrary depth: descend through named children
	// (noun → noun → verb) until a leaf command (Run != nil) is reached, then
	// parse the remaining tokens as its flags and positionals. This supports
	// both shallow commands (`pr list`, `api <path>`) and grouped nouns
	// (`pr reviewer list <id>`, `perms project grant <key> <user> <perm>`).
	cmd := noun
	rest := argv[1:]
	for cmd.Run == nil {
		if len(rest) == 0 {
			return a.printNounHelp(cmd)
		}
		child := cmd.findChild(rest[0])
		if child == nil {
			return unknownCommandErr(rest[0], cmd.childNames())
		}
		cmd = child
		rest = rest[1:]
	}

	parsed, positionals, err := parseFlags(rest, cmd.Flags)
	if err != nil {
		return flagParseError(cmd, err)
	}

	if parsed.Bool("help") {
		return a.printCommandHelp(cmd)
	}

	if err := validateArgs(cmd, positionals); err != nil {
		return err
	}

	ctx := &Context{App: a, Cmd: cmd, Args: positionals, Flags: parsed}
	return cmd.Run(ctx)
}

// flagParseError converts a parser error into a self-correcting usage error.
// Unknown flags list the command's valid flags inline.
func flagParseError(verb *Command, err error) error {
	var uf *errUnknownFlag
	if errors.As(err, &uf) {
		e := axi.UsageError(fmt.Sprintf("unknown flag --%s for `%s`", uf.name, verb.path()))
		e.Suggestions = validFlagList(verb)
		return e
	}
	return axi.UsageError(err.Error())
}

// validFlagList builds the "valid flags for `<cmd>`: …" hint shown after an
// unknown-flag error, including only the command's declared flags plus the
// universal --help (§6: --help is the one flag always allowed).
func validFlagList(cmd *Command) []string {
	names := cmd.Flags.Names()
	prefixed := make([]string, 0, len(names))
	for _, n := range names {
		prefixed = append(prefixed, "--"+n)
	}
	return []string{"valid flags for `" + leafName(cmd.path()) + "`: " + strings.Join(prefixed, ", ") + " (--help always allowed)"}
}

func unknownCommandErr(name string, valid []string) *axi.AxiError {
	e := axi.UsageError(fmt.Sprintf("unknown command `%s`", name))
	if len(valid) > 0 {
		e.Suggestions = []string{"available commands: " + strings.Join(valid, ", ")}
	} else {
		e.Suggestions = []string{"run `bkt-axi` with no arguments for the home view"}
	}
	return e
}

// leafName returns the last segment of a command path ("pr list" → "list").
func leafName(path string) string {
	if i := strings.LastIndex(path, " "); i >= 0 {
		return path[i+1:]
	}
	return path
}

// wireParents sets each command's parent pointer so path() renders correctly,
// recursing through the whole tree so grouped nouns (`pr reviewer list`) build
// their full path.
func (a *App) wireParents() {
	for _, n := range a.Commands {
		wireTree(n, &Command{Name: ""}) // noun's parent is the (nameless) root
	}
}

// wireTree sets cmd's parent and recurses into its children.
func wireTree(cmd, parent *Command) {
	cmd.parent = parent
	for _, c := range cmd.Children {
		wireTree(c, cmd)
	}
}

func (a *App) nounNames() []string {
	out := make([]string, 0, len(a.Commands))
	for _, n := range a.Commands {
		out = append(out, n.Name)
	}
	return out
}

func (c *Command) childNames() []string {
	out := make([]string, 0, len(c.Children))
	for _, ch := range c.Children {
		out = append(out, ch.Name)
	}
	return out
}
