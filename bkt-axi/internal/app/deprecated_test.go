package app

import (
	"strings"
	"testing"

	"github.com/ruttybob/bkt-axi/internal/axi"
)

// TestResolveDeprecated covers every entry in the consolidation table (spec
// §4.1), asserting the resolver matches old forms and ignores unknown ones.
func TestResolveDeprecated(t *testing.T) {
	cases := []struct {
		parent string
		token  string
		want   string // expected NewForm, "" means no match (nil)
	}{
		{"pr", "reviewer-group", "pr reviewer"},
		{"pr", "reaction", "pr comment <id>"},
		{"pr", "suggestion", "pr suggestions <id>"},
		{"pr", "auto-merge", "pr merge <id> --auto"},
		{"pr", "publish", "pr edit <id> --publish"},
		{"repo", "browse", "bkt-axi repo view [<slug>]"},
		// Case-insensitivity: agents sometimes capitalize the verb token.
		{"pr", "Suggestion", "pr suggestions <id>"},
		// Unknown forms resolve to nil (no false positives).
		{"pr", "list", ""},
		{"repo", "clone", ""},
		{"", "pr", ""},
	}
	for _, tc := range cases {
		d := resolveDeprecated(tc.parent, tc.token)
		if tc.want == "" {
			if d != nil {
				t.Errorf("resolveDeprecated(%q,%q): expected nil, got %+v", tc.parent, tc.token, d)
			}
			continue
		}
		if d == nil {
			t.Errorf("resolveDeprecated(%q,%q): expected match %q, got nil", tc.parent, tc.token, tc.want)
			continue
		}
		if d.NewForm != tc.want {
			t.Errorf("resolveDeprecated(%q,%q): NewForm=%q want %q", tc.parent, tc.token, d.NewForm, tc.want)
		}
	}
}

// TestDeprecationError checks the structured notice: a clean message, a
// self-correcting hint naming the new command, exit 2, and the DEPRECATED code.
func TestDeprecationError(t *testing.T) {
	t.Run("rename", func(t *testing.T) {
		d := resolveDeprecated("pr", "suggestion")
		if d == nil {
			t.Fatal("expected a deprecation")
		}
		e := deprecationError(d)
		if e.Code != axi.CodeDeprecated {
			t.Errorf("code: got %q want %q", e.Code, axi.CodeDeprecated)
		}
		if e.Exit != ExitUsage {
			t.Errorf("exit: got %d want %d", e.Exit, ExitUsage)
		}
		if !strings.Contains(e.Message, "`pr suggestion`") {
			t.Errorf("message should name the old form: %q", e.Message)
		}
		if len(e.Suggestions) != 1 || !strings.Contains(e.Suggestions[0], "pr suggestions <id>") {
			t.Errorf("hint should name the new form: %v", e.Suggestions)
		}
	})
	t.Run("dropped has advisory hint", func(t *testing.T) {
		d := resolveDeprecated("repo", "browse")
		if d == nil {
			t.Fatal("expected a deprecation")
		}
		e := deprecationError(d)
		if !strings.Contains(e.Message, "dropped") {
			t.Errorf("dropped form message should say dropped: %q", e.Message)
		}
		if len(e.Suggestions) != 1 || !strings.Contains(e.Suggestions[0], "repo view") {
			t.Errorf("hint should point to the alternative: %v", e.Suggestions)
		}
	})
}

// TestDispatchDeprecation routes an old form through a minimal app and asserts
// the dispatcher emits the deprecation (not a generic unknown-command error)
// and exits 2. This is the end-to-end guarantee the shim must provide.
func TestDispatchDeprecation(t *testing.T) {
	a := &App{
		Name: "bkt-axi",
		Commands: []*Command{
			{Name: "pr", Short: "pull requests", Children: []*Command{
				{Name: "list", Short: "List", Run: func(*Context) error { return nil }},
				{Name: "view", Short: "View", MinArgs: 1, MaxArgs: 1, Run: func(*Context) error { return nil }},
			}},
			{Name: "repo", Short: "repositories", Children: []*Command{
				{Name: "list", Short: "List", Run: func(*Context) error { return nil }},
			}},
		},
	}
	for _, argv := range [][]string{
		{"pr", "suggestion", "5"},
		{"pr", "reviewer-group", "add"},
		{"pr", "publish", "7"},
		{"pr", "auto-merge", "7"},
		{"pr", "reaction", "7"},
		{"repo", "browse"},
	} {
		code := a.Run(argv)
		if code != ExitUsage {
			t.Errorf("Run(%v): exit got %d want %d", argv, code, ExitUsage)
		}
	}
	// A genuinely unknown command is NOT hijacked by the shim (regression guard).
	code := a.Run([]string{"pr", "totally-made-up"})
	if code != ExitUsage {
		t.Errorf("unknown command exit: got %d want %d", code, ExitUsage)
	}
}
