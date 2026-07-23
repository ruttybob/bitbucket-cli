package session

// hooks.go implements the shared installer for the Claude Code / Codex hook
// shape:
//
//	{
//	  "hooks": {
//	    "SessionStart": [
//	      { "matcher": "...", "hooks": [ { "type": "command", "command": "..." } ] }
//	    ]
//	  }
//	}
//
// Both harnesses parse this exact schema. The installer preserves every other
// top-level key and every other matcher group/handler the user has, touches
// only the SessionStart entry that runs this binary, and is idempotent.

import (
	"os"
	"path/filepath"
)

// hookTarget describes one Claude/Codex-style hook to install.
type hookTarget struct {
	app     App
	file    string // settings/hooks JSON path
	event   string // e.g. "SessionStart"
	matcher string // matcher value ("" matches all sources)
	command string // desired hook command (PATH-verified or absolute)
	binPath string // absolute path of this binary, for robust "ours" detection
}

// installHookedApp installs/repairs a SessionStart hook described by t. It
// returns Installed when it added the entry, Repaired when it updated a stale
// path, and NoOp when the entry already ran the desired command.
func installHookedApp(t hookTarget) (Result, error) {
	res := Result{App: t.app, Path: displayPath(t.file), Action: ActionNoOp}

	data, err := os.ReadFile(t.file)
	if err != nil && !os.IsNotExist(err) {
		return res, err
	}
	root, err := parseJSON(data)
	if err != nil {
		return res, err
	}

	hooks, ok := root.get("hooks")
	if !ok || hooks.kind != 'o' {
		hooks = jObj()
		root.set("hooks", hooks)
	}
	groups, ok := hooks.get(t.event)
	if !ok || groups.kind != 'a' {
		groups = jArr()
		hooks.set(t.event, groups)
	}

	// Find (or create) the matcher group we coalesce into. We key on the
	// matcher string so a user's existing matcher groups are left intact.
	grp := findGroup(groups, t.matcher)
	if grp == nil {
		grp = jObj()
		grp.set("matcher", jStr(t.matcher))
		grp.set("hooks", jArr())
		groups.items = append(groups.items, grp)
	}
	handlers, ok := grp.get("hooks")
	if !ok || handlers.kind != 'a' {
		handlers = jArr()
		grp.set("hooks", handlers)
	}

	// Find our handler (command runs this binary). Update its command if stale;
	// append it when absent. Never touch another tool's handler.
	action := ActionNoOp
	idx, existing := findOurHandler(handlers, t.binPath)
	switch {
	case existing == nil:
		handlers.items = append(handlers.items, newHandler(t.command))
		action = ActionInstalled
	case handlerCommand(existing) != t.command:
		handlers.items[idx] = newHandler(t.command)
		action = ActionRepaired
	}

	if action == ActionNoOp {
		return res, nil
	}
	if err := os.MkdirAll(filepath.Dir(t.file), 0o755); err != nil {
		return res, err
	}
	if err := os.WriteFile(t.file, root.marshal(), 0o600); err != nil {
		return res, err
	}
	res.Action = action
	return res, nil
}

// findGroup returns the matcher-group object whose "matcher" equals m, or nil.
func findGroup(groups *jnode, matcher string) *jnode {
	for _, g := range groups.items {
		if g.kind != 'o' {
			continue
		}
		if m, ok := g.strVal("matcher"); ok && m == matcher {
			return g
		}
	}
	return nil
}

// findOurHandler returns the index and node of the first handler in arr that
// runs this binary, or (-1, nil) when none is ours. binPath is the absolute
// path of the running executable (may be ""), used so a renamed build or a
// relocated binary is still recognised, not just a path ending in "bkt-axi".
func findOurHandler(arr *jnode, binPath string) (int, *jnode) {
	for i, h := range arr.items {
		if h.kind != 'o' {
			continue
		}
		if cmd, ok := h.strVal("command"); ok && isOurHandler(cmd, binPath) {
			return i, h
		}
	}
	return -1, nil
}

func handlerCommand(h *jnode) string {
	cmd, _ := h.strVal("command")
	return cmd
}

// newHandler builds a {type:"command", command:cmd} handler object.
func newHandler(command string) *jnode {
	h := jObj()
	h.set("type", jStr("command"))
	h.set("command", jStr(command))
	return h
}

// displayPath collapses the user home directory to ~ for friendly reporting.
func displayPath(p string) string {
	return collapseHome(p)
}
