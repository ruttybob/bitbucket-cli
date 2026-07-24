package commands

// setup_test.go exercises `bkt-axi setup` end to end through the dispatcher:
// it installs hooks into an isolated HOME, re-runs for idempotency, repairs a
// moved binary, and refreshes the skill with --skill.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupIsolated redirects HOME and stages a fake `bkt-axi` on PATH that
// resolves to the running test binary. This exercises the real PATH-verified
// command resolution (ResolveCommand returns the bare `bkt-axi`) and lets
// isOurHandler recognise our handler by basename, exactly as production does.
func setupIsolated(t *testing.T) (home string, run func(argv []string) (string, int)) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	// Stage bkt-axi on PATH as a symlink to this test binary so LookPath finds
	// it and sameExecutable agrees it IS the current executable.
	binDir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	link := filepath.Join(binDir, "bkt-axi")
	if err := os.Symlink(exe, link); err != nil {
		t.Fatalf("symlink fake bkt-axi: %v", err)
	}
	t.Setenv("PATH", binDir)

	run = func(argv []string) (string, int) {
		a := NewApp("")
		var out strings.Builder
		a.Stdout = &out
		code := a.Run(argv) // run first; out.String() must be read after
		return out.String(), code
	}
	return home, run
}

func TestSetup_InstallsAllApps(t *testing.T) {
	home, run := setupIsolated(t)
	out, code := run([]string{"setup", "--app", "all"})
	if code != 0 {
		t.Fatalf("setup exit %d: %s", code, out)
	}
	if !strings.Contains(out, "result[3]{app,action,target}:") {
		t.Fatalf("missing result summary:\n%s", out)
	}
	for _, app := range []string{"claude", "codex", "opencode"} {
		if !strings.Contains(out, app) {
			t.Fatalf("result missing app %q:\n%s", app, out)
		}
	}
	// Files actually written.
	for _, p := range []string{
		filepath.Join(home, ".claude", "settings.json"),
		filepath.Join(home, ".codex", "hooks.json"),
		filepath.Join(home, ".config", "opencode", "plugins", "bkt_axi.ts"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected file %s: %v", p, err)
		}
	}
}

func TestSetup_IdempotentReRun(t *testing.T) {
	_, run := setupIsolated(t)
	if _, code := run([]string{"setup", "--app", "claude"}); code != 0 {
		t.Fatal("first setup failed")
	}
	out, code := run([]string{"setup", "--app", "claude"})
	if code != 0 {
		t.Fatalf("second setup exit %d: %s", code, out)
	}
	// Every reported action is noop.
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "claude,") {
			if !strings.Contains(line, ",noop,") {
				t.Fatalf("second setup should report noop, got line: %s\n%s", line, out)
			}
		}
	}
}

func TestSetup_PathRepair(t *testing.T) {
	home, run := setupIsolated(t)
	// First install writes the test-binary path (whatever os.Executable gives).
	if _, code := run([]string{"setup", "--app", "opencode"}); code != 0 {
		t.Fatal("first setup failed")
	}
	plugin := filepath.Join(home, ".config", "opencode", "plugins", "bkt_axi.ts")
	first, _ := os.ReadFile(plugin)

	// Simulate a reinstall that resolves to a different absolute path by
	// forcing the app's BinPath; setup resolves os.Executable itself, so we
	// instead verify the repair mechanic directly: overwrite the plugin with a
	// stale path, re-run, and confirm it is corrected back to the live path.
	stale := strings.ReplaceAll(string(first), "bkt-axi", "/stale/path/bkt-axi")
	if err := os.WriteFile(plugin, []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, code := run([]string{"setup", "--app", "opencode"}); code != 0 {
		t.Fatal("repair setup failed")
	}
	repaired, _ := os.ReadFile(plugin)
	if strings.Contains(string(repaired), "/stale/path/bkt-axi") {
		t.Fatalf("stale path not repaired:\n%s", repaired)
	}
	if string(repaired) != string(first) {
		t.Fatalf("repair did not restore the canonical content")
	}
}

func TestSetup_SkillFlag(t *testing.T) {
	home, run := setupIsolated(t)
	out, code := run([]string{"setup", "--app", "claude", "--skill"})
	if code != 0 {
		t.Fatalf("setup --skill exit %d: %s", code, out)
	}
	// Skill written into the Claude skill dir.
	skill := filepath.Join(home, ".claude", "skills", "bkt-axi", "SKILL.md")
	data, err := os.ReadFile(skill)
	if err != nil {
		t.Fatalf("skill not installed at %s: %v", skill, err)
	}
	if !strings.HasPrefix(string(data), "---\nname: bkt-axi\n") {
		t.Fatalf("skill frontmatter wrong:\n%s", data)
	}
}

func TestSetup_InvalidAppExits2(t *testing.T) {
	_, run := setupIsolated(t)
	out, code := run([]string{"setup", "--app", "ghost"})
	if code != 2 {
		t.Fatalf("invalid app should exit 2, got %d: %s", code, out)
	}
	if !strings.Contains(out, "invalid --app ghost") || !strings.Contains(out, "claude, codex, opencode, all") {
		t.Fatalf("missing self-correcting error:\n%s", out)
	}
}

func TestSetup_HelpBlock(t *testing.T) {
	_, run := setupIsolated(t)
	out, code := run([]string{"setup", "--help"})
	if code != 0 {
		t.Fatalf("--help exit %d", code)
	}
	if !strings.Contains(out, "command: setup") || !strings.Contains(out, "--app") || !strings.Contains(out, "--skill") {
		t.Fatalf("setup --help wrong:\n%s", out)
	}
}
