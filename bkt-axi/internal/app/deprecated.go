package app

import (
	"strings"

	"github.com/ruttybob/bkt-axi/internal/axi"
)

// deprecated.go is the migration shim that maps old `bkt` / pre-consolidation
// command forms to their `bkt-axi` equivalents (spec §4.1 consolidations). It
// is for discoverability and migration, NOT full backwards compatibility: a
// deprecated form prints a one-line notice naming the new command and exits 2
// (usage) so an agent self-corrects in one turn. The old verb's behavior is
// never silently emulated.

// Deprecation describes one renamed/consolidated/dropped command form.
type Deprecation struct {
	// OldPath is the pre-consolidation path exactly as typed ("pr suggestion").
	OldPath string
	// NewForm is the new command to use, with placeholders for runtime values
	// ("<id>"). Shown verbatim in the redirect hint.
	NewForm string
	// Note explains the change in one short clause ("renamed", "folded into
	// comment flags", "now a flag on pr merge"). Empty for a plain rename.
	Note string
	// Dropped marks forms with no direct replacement; NewForm then names the
	// closest alternative and the hint is advisory ("was dropped; …").
	Dropped bool
}

// deprecatedForms is the canonical consolidation table (spec §4.1). Keep it
// ordered for stable output. Messages avoid parentheses and semicolons so the
// TOON basic-string renderer keeps them unquoted and token-light.
var deprecatedForms = []Deprecation{
	{
		OldPath: "pr reviewer-group",
		NewForm: "pr reviewer",
		Note:    "reviewer groups and plain reviewers were consolidated into one command",
	},
	{
		OldPath: "pr reaction",
		NewForm: "pr comment <id>",
		Note:    "reactions were folded into the comment surface",
	},
	{
		OldPath: "pr suggestion",
		NewForm: "pr suggestions <id>",
		Note:    "renamed to the plural noun",
	},
	{
		OldPath: "pr auto-merge",
		NewForm: "pr merge <id> --auto",
		Note:    "auto-merge is now a flag on pr merge and pr create, not a command",
	},
	{
		OldPath: "pr publish",
		NewForm: "pr edit <id> --publish",
		Note:    "publishing is now a flag on pr edit",
	},
	{
		OldPath: "repo browse",
		NewForm: "bkt-axi repo view [<slug>]",
		Note:    "agents copy URLs. repo view prints a url field instead of opening a browser",
		Dropped: true,
	},
}

// resolveDeprecated returns the Deprecation for an attempted old command path
// (case-insensitive on the final token), or nil when the path is not a known
// deprecated form. parentPath is the resolved command path so far (e.g. "pr")
// and token is the next token that failed to resolve to a child (e.g.
// "suggestion"); together they form the attempted old path.
func resolveDeprecated(parentPath, token string) *Deprecation {
	if token == "" {
		return nil
	}
	attempted := strings.TrimSpace(parentPath + " " + strings.ToLower(token))
	for i := range deprecatedForms {
		if deprecatedForms[i].OldPath == attempted {
			d := deprecatedForms[i]
			return &d
		}
	}
	return nil
}

// deprecationError renders a Deprecation as a structured AxiError: a clear
// one-line notice plus a self-correcting hint naming the new command. Exit 2
// (usage): the form as typed is not valid in bkt-axi and the agent's next move
// is to use the new form.
func deprecationError(d *Deprecation) *axi.AxiError {
	var msg string
	if d.Dropped {
		msg = "`" + d.OldPath + "` was dropped; " + d.Note
	} else {
		msg = "`" + d.OldPath + "` has been replaced; " + d.Note
	}
	e := &axi.AxiError{
		Message: msg,
		Code:    axi.CodeDeprecated,
		Exit:    ExitUsage,
	}
	if d.Dropped {
		e.Suggestions = []string{"Use `" + d.NewForm + "` instead"}
	} else {
		e.Suggestions = []string{"Run `bkt-axi " + d.NewForm + "` instead"}
	}
	return e
}
