package session

// claude.go installs the Claude Code SessionStart hook.
//
// Claude Code reads hooks from ~/.claude/settings.json (user-global) or a
// project's .claude/settings.json. A SessionStart hook's stdout is injected as
// additional context at the start of every matching session, so running
// `bkt-axi` (no args) prints the content-first home view directly into the
// conversation. The matcher is limited to startup|resume|clear so the hook
// does not re-run (and re-fetch live state) on every context compaction.

import (
	"path/filepath"
)

// claudeSessionMatcher limits the hook to fresh starts, resumes, and clears —
// the SessionStart sources where ambient context is useful. It skips
// "compact" and "fork" so bkt-axi is not re-invoked on every compaction.
const claudeSessionMatcher = "startup|resume|clear"

// installClaude installs/repairs the Claude Code SessionStart hook in
// ~/.claude/settings.json.
func installClaude(binPath string) (Result, error) {
	home, err := userHome()
	if err != nil {
		return Result{App: AppClaude}, err
	}
	settings := filepath.Join(home, ".claude", "settings.json")
	t := hookTarget{
		app:     AppClaude,
		file:    settings,
		event:   "SessionStart",
		matcher: claudeSessionMatcher,
		command: ResolveCommand(binPath),
		binPath: binPath,
	}
	return installHookedApp(t)
}
