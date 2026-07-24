package session

// codex.go installs the Codex SessionStart hook.
//
// OpenAI's Codex CLI reads hooks from ~/.codex/hooks.json (user-global) or a
// project's .codex/hooks.json, using the same {hooks:{<Event>:[{matcher,hooks:
// [{type,command}]}]}} schema as Claude Code. Hooks are experimental and off
// by default: the [features] table in ~/.codex/config.toml must set
// `hooks = true` (the current canonical flag; `codex_hooks` is a legacy alias)
// for any hook to fire. A SessionStart hook's stdout is injected as context,
// so running `bkt-axi` prints the home view into the session.

import (
	"os"
	"path/filepath"
	"strings"
)

// codexSessionMatcher limits the hook to startup|resume, the SessionStart
// sources Codex emits (per its hook reference).
const codexSessionMatcher = "startup|resume"

// installCodex installs/repairs the Codex SessionStart hook in
// ~/.codex/hooks.json and ensures hooks are enabled in ~/.codex/config.toml.
func installCodex(binPath string) (Result, error) {
	home, err := userHome()
	if err != nil {
		return Result{App: AppCodex}, err
	}
	hooksFile := filepath.Join(home, ".codex", "hooks.json")
	t := hookTarget{
		app:     AppCodex,
		file:    hooksFile,
		event:   "SessionStart",
		matcher: codexSessionMatcher,
		command: ResolveCommand(binPath),
		binPath: binPath,
	}
	res, err := installHookedApp(t)
	if err != nil {
		return res, err
	}

	// Ensure the feature flag is on. Report it in Note so the user knows the
	// config.toml was touched even when the hook itself was already current.
	cfgPath := filepath.Join(home, ".codex", "config.toml")
	flagAction, ferr := ensureCodexHooksEnabled(cfgPath)
	if ferr != nil {
		res.Note = "hook " + string(res.Action) + "; config.toml update failed: " + ferr.Error()
		return res, ferr
	}
	if flagAction == ActionInstalled && res.Action == ActionNoOp {
		// Hook was already correct but we enabled the feature flag.
		res.Action = ActionRepaired
		res.Note = "enabled hooks feature in config.toml"
	}
	return res, nil
}

// ensureCodexHooksEnabled guarantees [features].hooks = true exists in the
// Codex config.toml at path, preserving every other line. It does a narrow,
// line-based edit rather than pulling in a TOML library, since only this one
// boolean under one known table is ever at stake.
func ensureCodexHooksEnabled(path string) (Action, error) {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return ActionNoOp, err
	}
	lines := strings.Split(string(data), "\n")

	// Locate the [features] table header (exact, ignoring surrounding spaces).
	secStart := -1
	for i, ln := range lines {
		if strings.TrimSpace(ln) == "[features]" {
			secStart = i
			break
		}
	}

	if secStart == -1 {
		// No [features] table: append one (with a separating blank line when
		// the file is non-empty and does not already end with one).
		out := string(data)
		if len(out) > 0 && !strings.HasSuffix(out, "\n") {
			lines = append(lines, "")
		}
		lines = append(lines, "[features]", "hooks = true")
		return writeIfChanged(path, []byte(strings.Join(lines, "\n")+"\n"))
	}

	// Bound the table: from secStart+1 up to the next header or EOF.
	end := len(lines)
	for i := secStart + 1; i < len(lines); i++ {
		if isTomlHeader(lines[i]) {
			end = i
			break
		}
	}

	// Look for an existing `hooks` key inside [features].
	for i := secStart + 1; i < end; i++ {
		if k, v, ok := parseTomlKV(lines[i]); ok && k == "hooks" {
			if strings.TrimSpace(v) == "true" {
				return ActionNoOp, nil // already enabled
			}
			lines[i] = "hooks = true"
			return writeIfChanged(path, []byte(strings.Join(lines, "\n")+"\n"))
		}
	}

	// hooks key absent: insert it directly under the [features] header.
	updated := append([]string{}, lines[:secStart+1]...)
	updated = append(updated, "hooks = true")
	updated = append(updated, lines[secStart+1:]...)
	return writeIfChanged(path, []byte(strings.Join(updated, "\n")+"\n"))
}

// isTomlHeader reports whether ln is a TOML table header like "[features]".
func isTomlHeader(ln string) bool {
	t := strings.TrimSpace(ln)
	return strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") && !strings.HasPrefix(t, "[[")
}

// parseTomlKV splits a "key = value" TOML line, returning the bare key and the
// raw (unquoted) value. Comment-only or blank lines return ok=false.
func parseTomlKV(ln string) (key, value string, ok bool) {
	// Strip a trailing inline comment that is not inside a quoted value. For
	// the narrow boolean we edit this is robust; we do not need full TOML.
	s := stripTomlComment(ln)
	eq := strings.IndexByte(s, '=')
	if eq < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(s[:eq])
	value = strings.TrimSpace(s[eq+1:])
	value = strings.Trim(value, `"'`)
	if key == "" {
		return "", "", false
	}
	return key, value, true
}

// stripTomlComment removes an unquoted '#' comment from a TOML line.
func stripTomlComment(ln string) string {
	inSingle, inDouble := false, false
	for i := 0; i < len(ln); i++ {
		c := ln[i]
		switch c {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return ln[:i]
			}
		}
	}
	return ln
}
