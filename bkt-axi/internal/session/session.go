// Package session installs and repairs AXI §7 session integrations for the
// supported agent harnesses (Claude Code, Codex, OpenCode) and generates the
// companion SKILL.md. Installers are idempotent, path-repairing, and
// non-destructive: they touch only the hook entry that runs this binary and
// preserve every other key the user has in their settings files.
package session

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// App is a supported target harness.
type App string

const (
	AppClaude   App = "claude"
	AppCodex    App = "codex"
	AppOpenCode App = "opencode"
	AppAll      App = "all"
)

// allApps is the canonical install order for `--app all`.
var allApps = []App{AppClaude, AppCodex, AppOpenCode}

// Action reports what an installer did for one app.
type Action string

const (
	ActionInstalled Action = "installed" // created the hook or added a missing entry
	ActionRepaired  Action = "repaired"  // updated a stale binary path
	ActionNoOp      Action = "noop"      // already correct; file untouched
)

// Result reports the outcome of installing one app.
type Result struct {
	App    App
	Action Action
	Path   string // settings/plugin file touched (home-collapsed for display)
	Note   string // optional extra detail (e.g. an enabled feature flag)
}

// Install installs or repairs the session hook for the named app. AppAll runs
// every supported harness in order. Each install is idempotent; an error from
// one app does not block the others when AppAll fans out — partial results are
// returned alongside the first error.
func Install(app App, binPath string) ([]Result, error) {
	switch app {
	case AppClaude, AppCodex, AppOpenCode:
		r, err := installOne(app, binPath)
		return []Result{r}, err
	case AppAll:
		var results []Result
		var firstErr error
		for _, a := range allApps {
			r, err := installOne(a, binPath)
			if err != nil {
				if r.Note == "" {
					r.Note = err.Error()
				}
				if firstErr == nil {
					firstErr = err
				}
			}
			results = append(results, r)
		}
		return results, firstErr
	default:
		return nil, fmt.Errorf("unknown app %q (valid: claude, codex, opencode, all)", app)
	}
}

func installOne(app App, binPath string) (Result, error) {
	switch app {
	case AppClaude:
		return installClaude(binPath)
	case AppCodex:
		return installCodex(binPath)
	case AppOpenCode:
		return installOpenCode(binPath)
	default:
		return Result{App: app}, fmt.Errorf("unknown app %q", app)
	}
}

// ValidApp reports whether s names a supported --app value.
func ValidApp(s string) bool {
	switch App(s) {
	case AppClaude, AppCodex, AppOpenCode, AppAll:
		return true
	}
	return false
}

// AllApps returns the canonical install order used by `--app all`.
func AllApps() []App {
	return append([]App(nil), allApps...)
}

// ResolveCommand returns the hook command string for the given executable:
// the bare PATH name "bkt-axi" when it resolves to this binary, else the
// absolute path. This keeps global installs portable while ensuring the hook
// never accidentally runs a different binary (AXI §7 "Portable commands").
// An empty binPath resolves to the bare name.
func ResolveCommand(binPath string) string {
	if binPath != "" {
		if found, err := exec.LookPath("bkt-axi"); err == nil && sameExecutable(found, binPath) {
			return "bkt-axi"
		}
	}
	if binPath == "" {
		return "bkt-axi"
	}
	return binPath
}

// sameExecutable reports whether a and b refer to the same file (after symlink
// resolution), so a PATH-resolved binary and an absolute path compare equal.
func sameExecutable(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil && ra != "" {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil && rb != "" {
		b = rb
	}
	if a == b {
		return true
	}
	sa, err := os.Stat(a)
	if err != nil {
		return false
	}
	sb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(sa, sb)
}

// hookProgram returns the first token of a hook command (the program path or
// name), so an installer can decide whether a handler is "ours".
func hookProgram(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	// Shell words: split on whitespace. A SessionStart command is always a
	// simple "prog args..." invocation; quotes are not needed for our binary
	// name, so a whitespace split is sufficient and robust.
	for _, r := range strings.Fields(command) {
		return r
	}
	return ""
}

// isOurHandler reports whether a hook command runs this bkt-axi binary. It
// matches by program basename (so the bare PATH name `bkt-axi` and any
// `/path/bkt-axi` are recognised) and, when binPath is set, by file identity
// (so a renamed build or a relocated binary is still recognised across
// reinstalls). This is how an installer identifies the entry it owns (AXI §7
// "Path repair").
func isOurHandler(command, binPath string) bool {
	prog := hookProgram(command)
	if prog == "" {
		return false
	}
	if filepath.Base(prog) == "bkt-axi" {
		return true
	}
	return binPath != "" && sameExecutable(prog, binPath)
}

// writeIfChanged writes data to path only when it differs from the current
// content, returning Installed when it wrote and NoOp when the file already
// matched. The parent directory is created when needed.
func writeIfChanged(path string, data []byte) (Action, error) {
	if existing, err := os.ReadFile(path); err == nil {
		if bytesEqual(existing, data) {
			return ActionNoOp, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return ActionNoOp, err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return ActionNoOp, err
	}
	return ActionInstalled, nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
