package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ruttybob/bkt-axi/internal/axi"
)

// buildTestApp constructs a tiny app with one noun and two verbs to exercise
// dispatch, flag parsing, and help rendering without the full command tree.
func buildTestApp() *App {
	saw := ""
	listRun := func(ctx *Context) error {
		saw = ctx.Flags.String("state")
		if ctx.Flags.Bool("mine") {
			saw += ":mine"
		}
		ctx.Print("state=" + saw)
		return nil
	}
	viewRun := func(ctx *Context) error {
		ctx.Print("view " + ctx.Args[0])
		return nil
	}
	noop := func(s string) {}
	noop(saw) // keep saw referenced without leaking state across tests
	a := &App{
		Name:        "bkt-axi",
		Description: "test binary",
		Commands: []*Command{
			{
				Name:  "pr",
				Short: "pull requests",
				Children: []*Command{
					{
						Name:    "list",
						Short:   "List pull requests",
						MinArgs: 0, MaxArgs: 0,
						Flags: FlagSet{
							{Name: "state", Type: FlagString, Default: "open", Desc: "filter state"},
							{Name: "mine", Type: FlagBool, Default: false, Desc: "only mine"},
							{Name: "limit", Type: FlagInt, Default: 50, Desc: "page size"},
							{Name: "fields", Type: FlagString, Default: "", Desc: "extra fields"},
							{Name: "context", Type: FlagString, Default: "", Desc: "named context"},
						},
						Run: listRun,
					},
					{
						Name:    "view",
						Short:   "View a pull request",
						MinArgs: 1, MaxArgs: 1,
						Run: viewRun,
					},
				},
			},
		},
	}
	return a
}

func TestDispatch_UnknownFlagExit2(t *testing.T) {
	a := buildTestApp()
	var out bytes.Buffer
	a.Stdout = &out
	code := a.Run([]string{"pr", "list", "--stat", "open"})
	if code != ExitUsage {
		t.Fatalf("expected exit %d for unknown flag, got %d", ExitUsage, code)
	}
	got := out.String()
	if !strings.Contains(got, "error: unknown flag --stat for `pr list`") {
		t.Fatalf("missing error line:\n%s", got)
	}
	if !strings.Contains(got, "valid flags for `list`: --state, --mine, --limit, --fields, --context (--help always allowed)") {
		t.Fatalf("missing inline valid-flag list:\n%s", got)
	}
}

func TestDispatch_KnownFlagsParse(t *testing.T) {
	a := buildTestApp()
	var out bytes.Buffer
	a.Stdout = &out
	code := a.Run([]string{"pr", "list", "--state", "merged", "--mine", "--limit", "10"})
	if code != ExitSuccess {
		t.Fatalf("expected exit 0, got %d (%s)", code, out.String())
	}
	if !strings.Contains(out.String(), "state=merged:mine") {
		t.Fatalf("flags not parsed correctly: %s", out.String())
	}
}

func TestDispatch_EqualsFormAndBoolFalse(t *testing.T) {
	a := buildTestApp()
	var out bytes.Buffer
	a.Stdout = &out
	code := a.Run([]string{"pr", "list", "--state=all", "--mine=false"})
	if code != ExitSuccess {
		t.Fatalf("expected exit 0, got %d (%s)", code, out.String())
	}
	// mine=false → should NOT append :mine
	if !strings.Contains(out.String(), "state=all") {
		t.Fatalf("equals-form not parsed: %q", out.String())
	}
	if strings.Contains(out.String(), ":mine") {
		t.Fatalf("bool=false should not set the flag: %q", out.String())
	}
}

func TestDispatch_HelpBlock(t *testing.T) {
	a := buildTestApp()
	var out bytes.Buffer
	a.Stdout = &out
	code := a.Run([]string{"pr", "list", "--help"})
	if code != ExitSuccess {
		t.Fatalf("--help should exit 0, got %d", code)
	}
	got := out.String()
	if !strings.Contains(got, "command: pr list") {
		t.Fatalf("help missing command line:\n%s", got)
	}
	if !strings.Contains(got, "flags[5]{flag,description,default}:") {
		t.Fatalf("help missing flags block:\n%s", got)
	}
}

func TestDispatch_UnknownCommandExit2(t *testing.T) {
	a := buildTestApp()
	var out bytes.Buffer
	a.Stdout = &out
	code := a.Run([]string{"repo", "list"})
	if code != ExitUsage {
		t.Fatalf("unknown noun should exit 2, got %d", code)
	}
	if !strings.Contains(out.String(), "unknown command `repo`") {
		t.Fatalf("missing unknown-command error:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "available commands: pr") {
		t.Fatalf("missing available-commands hint:\n%s", out.String())
	}
}

func TestDispatch_HomeFallback(t *testing.T) {
	a := buildTestApp()
	var out bytes.Buffer
	a.Stdout = &out
	code := a.Run([]string{})
	if code != ExitSuccess {
		t.Fatalf("no-args home should exit 0, got %d", code)
	}
	if !strings.Contains(out.String(), "bin: bkt-axi") {
		t.Fatalf("home fallback missing bin:\n%s", out.String())
	}
}

func TestDispatch_MissingPositional(t *testing.T) {
	a := buildTestApp()
	var out bytes.Buffer
	a.Stdout = &out
	code := a.Run([]string{"pr", "view"})
	if code != ExitUsage {
		t.Fatalf("missing positional should exit 2, got %d", code)
	}
	if !strings.Contains(out.String(), "requires at least 1") {
		t.Fatalf("missing positional error wrong:\n%s", out.String())
	}
}

func TestExitCodeMapping(t *testing.T) {
	if axi.ExitCode(nil) != ExitSuccess {
		t.Fatal("nil must map to 0")
	}
	if axi.ExitCode(axi.Errorf("x")) != ExitError {
		t.Fatal("runtime error must map to 1")
	}
	if axi.ExitCode(axi.UsageError("x")) != ExitUsage {
		t.Fatal("usage error must map to 2")
	}
	if axi.ExitCode(axi.NoOp("done")) != ExitSuccess {
		t.Fatal("no-op must map to 0")
	}
}

// --- Phase 3 dispatcher features: short flags, string slices, nested/leaf dispatch ---

func buildNestedApp() *App {
	// pr reviewer list <id>: three-level nesting (noun → noun → verb).
	reviewerList := &Command{
		Name: "list", Short: "List reviewers", MinArgs: 1, MaxArgs: 1,
		Run: func(ctx *Context) error {
			ctx.Print("reviewers for " + ctx.Args[0])
			return nil
		},
	}
	reviewer := &Command{Name: "reviewer", Short: "Reviewers", Children: []*Command{reviewerList}}
	pr := &Command{Name: "pr", Short: "prs", Children: []*Command{reviewer}}
	// leaf noun with a repeatable short-flag field.
	apiRun := func(ctx *Context) error {
		ctx.Print("fields=" + strings.Join(ctx.Flags.StringSlice("field"), ",") + " method=" + ctx.Flags.String("method"))
		return nil
	}
	api := &Command{
		Name: "api", Short: "raw", MinArgs: 1, MaxArgs: 1, Run: apiRun,
		Flags: FlagSet{
			{Name: "method", Type: FlagString, Default: "GET", Desc: "method"},
			{Name: "field", Short: "F", Type: FlagStringSlice, Default: []string{}, Desc: "repeatable"},
		},
	}
	return &App{Name: "bkt-axi", Description: "test", Commands: []*Command{pr, api}}
}

func TestDispatch_NestedNounThreeLevels(t *testing.T) {
	a := buildNestedApp()
	var out bytes.Buffer
	a.Stdout = &out
	code := a.Run([]string{"pr", "reviewer", "list", "42"})
	if code != ExitSuccess {
		t.Fatalf("nested dispatch exit %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "reviewers for 42") {
		t.Fatalf("nested verb did not run: %q", out.String())
	}
}

func TestDispatch_LeafNounWithShortFlagAndSlice(t *testing.T) {
	a := buildNestedApp()
	var out bytes.Buffer
	a.Stdout = &out
	code := a.Run([]string{"api", "/user", "-F", "a=1", "--field", "b=2", "-Fc=3"})
	if code != ExitSuccess {
		t.Fatalf("leaf dispatch exit %d: %s", code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "fields=a=1,b=2,c=3") {
		t.Fatalf("slice + short flag not accumulated: %q", got)
	}
	if !strings.Contains(got, "method=GET") {
		t.Fatalf("default method wrong: %q", got)
	}
}

func TestDispatch_LeafNounHelp(t *testing.T) {
	a := buildNestedApp()
	var out bytes.Buffer
	a.Stdout = &out
	code := a.Run([]string{"api", "--help"})
	if code != ExitSuccess {
		t.Fatalf("--help exit %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "command: api") || !strings.Contains(out.String(), "--field, -F") {
		t.Fatalf("leaf --help wrong:\n%s", out.String())
	}
}

func TestDispatch_NestedUnknownVerbClearHint(t *testing.T) {
	a := buildNestedApp()
	var out bytes.Buffer
	a.Stdout = &out
	code := a.Run([]string{"pr", "reviewer", "bogus"})
	if code != ExitUsage {
		t.Fatalf("unknown nested verb should exit 2, got %d", code)
	}
	if !strings.Contains(out.String(), "unknown command `bogus`") || !strings.Contains(out.String(), "available commands: list") {
		t.Fatalf("missing nested hint:\n%s", out.String())
	}
}
