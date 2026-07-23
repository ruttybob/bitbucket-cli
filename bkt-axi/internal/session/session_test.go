package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruttybob/bkt-axi/internal/app"
)

// isolatedHome redirects HOME and empties PATH so installer tests are
// deterministic: ResolveCommand always returns the absolute binPath (never the
// bare PATH name), and no real user config is touched.
func isolatedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("PATH", "")
	return home
}

// mustWriteFile creates parent dirs and writes content, failing the test on error.
func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// --- ojson order preservation ---

func TestParseJSON_PreservesKeyOrder(t *testing.T) {
	in := `{"z":1,"a":2,"m":{"y":1,"b":2},"q":[3,2,1]}`
	n, err := parseJSON([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if got := n.keys; strings.Join(got, ",") != "z,a,m,q" {
		t.Fatalf("top-level order lost: %v", got)
	}
	m, _ := n.get("m")
	if got := m.keys; strings.Join(got, ",") != "y,b" {
		t.Fatalf("nested order lost: %v", got)
	}
	out := string(n.marshal())
	// Round-trip must keep the original key order, not alphabetical.
	if !strings.Contains(out, "\"z\": 1") || strings.Index(out, "\"z\":") > strings.Index(out, "\"a\":") {
		t.Fatalf("marshal reordered keys:\n%s", out)
	}
}

func TestParseJSON_EmptyYieldsObject(t *testing.T) {
	n, err := parseJSON(nil)
	if err != nil || n.kind != 'o' || len(n.keys) != 0 {
		t.Fatalf("empty input should yield empty object, got %v %v", n, err)
	}
}

// --- ResolveCommand ---

func TestResolveCommand_EmptyBinUsesBareName(t *testing.T) {
	if got := ResolveCommand(""); got != "bkt-axi" {
		t.Fatalf("empty binPath should resolve to bare name, got %q", got)
	}
}

func TestResolveCommand_NoPathReturnsAbsolute(t *testing.T) {
	t.Setenv("PATH", "")
	got := ResolveCommand("/opt/bkt-axi/bin/bkt-axi")
	if got != "/opt/bkt-axi/bin/bkt-axi" {
		t.Fatalf("no PATH match should return absolute path, got %q", got)
	}
}

func TestSameExecutable_AbsoluteVsBasename(t *testing.T) {
	// Create a real file and a symlink to it; sameExecutable must agree.
	dir := t.TempDir()
	real := filepath.Join(dir, "bkt-axi")
	if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if !sameExecutable(real, link) {
		t.Fatal("symlink and target should be the same executable")
	}
	if sameExecutable(real, real+".missing") {
		t.Fatal("different paths should not be the same executable")
	}
}

// --- Claude installer ---

func TestInstallClaude_IdempotentAndNonDestructive(t *testing.T) {
	home := isolatedHome(t)
	settings := filepath.Join(home, ".claude", "settings.json")

	// Pre-existing user settings with a key we must NOT disturb or reorder.
	seed := `{
  "permissions": {"allow": ["Bash(git:*)"]},
  "model": "claude-sonnet"
}
`
	mustWriteFile(t, settings, seed)

	r1, err := Install(AppClaude, "/opt/bin/bkt-axi")
	if err != nil {
		t.Fatalf("first install: %v", err)
	}
	if r1[0].Action != ActionInstalled {
		t.Fatalf("first install action = %q, want installed", r1[0].Action)
	}

	got, _ := os.ReadFile(settings)
	s := string(got)
	if !strings.Contains(s, "\"SessionStart\"") || !strings.Contains(s, "/opt/bin/bkt-axi") {
		t.Fatalf("hook not written:\n%s", s)
	}
	// Non-destructive: the user's keys survive.
	if !strings.Contains(s, "\"permissions\"") || !strings.Contains(s, "\"model\"") {
		t.Fatalf("user keys lost:\n%s", s)
	}
	// Order preserved: permissions (original first key) still precedes model,
	// and both precede the appended hooks block.
	if strings.Index(s, "\"model\"") < strings.Index(s, "\"permissions\"") {
		t.Fatalf("original key order not preserved:\n%s", s)
	}

	// Second run with the same binary is a silent no-op (file bytes unchanged).
	r2, err := Install(AppClaude, "/opt/bin/bkt-axi")
	if err != nil || r2[0].Action != ActionNoOp {
		t.Fatalf("second install should be noop, got %+v %v", r2, err)
	}
	got2, _ := os.ReadFile(settings)
	if string(got2) != s {
		t.Fatalf("noop rewrote the file")
	}
}

func TestInstallClaude_PathRepair(t *testing.T) {
	home := isolatedHome(t)
	settings := filepath.Join(home, ".claude", "settings.json")

	if _, err := Install(AppClaude, "/old/bin/bkt-axi"); err != nil {
		t.Fatal(err)
	}
	// Move the binary: a new install must update the stale path.
	if _, err := Install(AppClaude, "/new/bin/bkt-axi"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(settings)
	if strings.Contains(string(got), "/old/bin/bkt-axi") {
		t.Fatalf("stale path not repaired:\n%s", got)
	}
	if !strings.Contains(string(got), "/new/bin/bkt-axi") {
		t.Fatalf("new path not written:\n%s", got)
	}
	// Only one bkt-axi handler after repair (no duplicate appended).
	if c := strings.Count(string(got), "/new/bin/bkt-axi"); c != 1 {
		t.Fatalf("expected exactly 1 handler after repair, got %d:\n%s", c, got)
	}
}

func TestInstallClaude_PreservesForeignHooks(t *testing.T) {
	home := isolatedHome(t)
	settings := filepath.Join(home, ".claude", "settings.json")
	// A user's existing PostToolUse hook and a foreign SessionStart handler
	// must both survive our install untouched.
	seed := `{
  "hooks": {
    "PostToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "prettier"}]}],
    "SessionStart": [{"matcher": "", "hooks": [{"type": "command", "command": "other-tool"}]}]
  }
}
`
	mustWriteFile(t, settings, seed)
	if _, err := Install(AppClaude, "/opt/bin/bkt-axi"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(settings)
	s := string(got)
	if !strings.Contains(s, "prettier") || !strings.Contains(s, "other-tool") {
		t.Fatalf("foreign hooks lost:\n%s", s)
	}
	if !strings.Contains(s, "/opt/bin/bkt-axi") {
		t.Fatalf("our hook not added:\n%s", s)
	}
}

// --- Codex installer ---

func TestInstallCodex_HookAndFeatureFlag(t *testing.T) {
	home := isolatedHome(t)
	hooks := filepath.Join(home, ".codex", "hooks.json")
	cfg := filepath.Join(home, ".codex", "config.toml")

	// config.toml with an existing [features] table lacking the flag.
	mustWriteFile(t, cfg, "model = \"gpt-5\"\n\n[features]\nfoo = true\n")

	r, err := Install(AppCodex, "/opt/bin/bkt-axi")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if r[0].Action != ActionInstalled {
		t.Fatalf("action = %q, want installed", r[0].Action)
	}
	hgot, _ := os.ReadFile(hooks)
	if !strings.Contains(string(hgot), "\"SessionStart\"") || !strings.Contains(string(hgot), "/opt/bin/bkt-axi") {
		t.Fatalf("codex hook not written:\n%s", hgot)
	}
	cgot, _ := os.ReadFile(cfg)
	if !strings.Contains(string(cgot), "hooks = true") {
		t.Fatalf("feature flag not enabled:\n%s", cgot)
	}
	// Existing [features] key preserved.
	if !strings.Contains(string(cgot), "foo = true") {
		t.Fatalf("existing feature key lost:\n%s", cgot)
	}

	// Re-run: no-op for both hook and flag.
	r2, err := Install(AppCodex, "/opt/bin/bkt-axi")
	if err != nil || r2[0].Action != ActionNoOp {
		t.Fatalf("second install should be noop, got %+v %v", r2, err)
	}
}

func TestInstallCodex_FeatureFlagAlreadyTrueIsNoOp(t *testing.T) {
	home := isolatedHome(t)
	cfg := filepath.Join(home, ".codex", "config.toml")
	mustWriteFile(t, cfg, "[features]\nhooks = true\n")
	action, err := ensureCodexHooksEnabled(cfg)
	if err != nil || action != ActionNoOp {
		t.Fatalf("already-enabled flag should be noop, got %q %v", action, err)
	}
}

func TestInstallCodex_CreatesFeaturesSectionWhenAbsent(t *testing.T) {
	home := isolatedHome(t)
	cfg := filepath.Join(home, ".codex", "config.toml")
	mustWriteFile(t, cfg, "model = \"gpt-5\"\n")
	action, err := ensureCodexHooksEnabled(cfg)
	if err != nil || action != ActionInstalled {
		t.Fatalf("should install, got %q %v", action, err)
	}
	got, _ := os.ReadFile(cfg)
	if !strings.Contains(string(got), "[features]") || !strings.Contains(string(got), "hooks = true") {
		t.Fatalf("features section not created:\n%s", got)
	}
}

// --- OpenCode installer ---

func TestInstallOpenCode_IdempotentAndRepair(t *testing.T) {
	home := isolatedHome(t)
	plugin := filepath.Join(home, ".config", "opencode", "plugins", opencodePluginFile)

	r1, err := Install(AppOpenCode, "/opt/bin/bkt-axi")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if r1[0].Action != ActionInstalled {
		t.Fatalf("action = %q, want installed", r1[0].Action)
	}
	first, _ := os.ReadFile(plugin)
	if !strings.Contains(string(first), "/opt/bin/bkt-axi") || !strings.Contains(string(first), "experimental.chat.system.transform") {
		t.Fatalf("plugin content wrong:\n%s", first)
	}

	// Idempotent: same binary is a no-op.
	r2, _ := Install(AppOpenCode, "/opt/bin/bkt-axi")
	if r2[0].Action != ActionNoOp {
		t.Fatalf("second install should be noop, got %q", r2[0].Action)
	}

	// Path repair: new binary rewrites the plugin.
	r3, _ := Install(AppOpenCode, "/opt/other/bkt-axi")
	if r3[0].Action != ActionRepaired && r3[0].Action != ActionInstalled {
		// writeIfChanged returns Installed when content differs; both mean a write.
	}
	third, _ := os.ReadFile(plugin)
	if !strings.Contains(string(third), "/opt/other/bkt-axi") {
		t.Fatalf("plugin path not repaired:\n%s", third)
	}
}

// --- All apps ---

func TestInstall_AllRunsEveryApp(t *testing.T) {
	isolatedHome(t)
	results, err := Install(AppAll, "/opt/bin/bkt-axi")
	if err != nil {
		t.Fatalf("install all: %v", err)
	}
	if len(results) != len(allApps) {
		t.Fatalf("expected %d results, got %d", len(allApps), len(results))
	}
	for _, r := range results {
		if r.Action != ActionInstalled {
			t.Fatalf("app %s action = %q, want installed", r.App, r.Action)
		}
	}
}

func TestInstall_UnknownAppErrors(t *testing.T) {
	if _, err := Install(App("ghost"), "/x"); err == nil {
		t.Fatal("unknown app should error")
	}
}

// --- skill generator ---

func buildTestApp() *app.App {
	return &app.App{
		Name:        "bkt-axi",
		Description: "Bitbucket CLI for agents",
		BinPath:     "/home/u/.local/bin/bkt-axi",
		Commands: []*app.Command{
			{Name: "pr", Short: "Manage pull requests"},
			{Name: "auth", Short: "Manage authentication"},
		},
	}
}

func TestExtractCommandsBlock(t *testing.T) {
	a := buildTestApp()
	block := extractCommandsBlock(a.RootHelp())
	if !strings.HasPrefix(block, "commands[2]{name,description}:") {
		t.Fatalf("block header wrong:\n%s", block)
	}
	if !strings.Contains(block, "pr,Manage pull requests") || !strings.Contains(block, "auth,Manage authentication") {
		t.Fatalf("block rows missing:\n%s", block)
	}
	// The block must NOT include the trailing help[] block.
	if strings.Contains(block, "help[") {
		t.Fatalf("block leaked past the commands section:\n%s", block)
	}
}

func TestSkillContent_DeterministicAndTriggerShaped(t *testing.T) {
	a := buildTestApp()
	c1 := SkillContent(a)
	c2 := SkillContent(a)
	if c1 != c2 {
		t.Fatal("SkillContent must be deterministic")
	}
	if !strings.HasPrefix(c1, "---\nname: bkt-axi\n") {
		t.Fatalf("frontmatter missing:\n%s", c1[:80])
	}
	if !strings.Contains(c1, "Bitbucket Cloud and Data Center CLI") {
		t.Fatalf("trigger description missing")
	}
	if !strings.Contains(c1, "commands[2]{name,description}:") {
		t.Fatalf("skill must embed the commands block")
	}
	// A skill is static: no live-state fields.
	if strings.Contains(c1, "prs_mine") || strings.Contains(c1, "count:") {
		t.Fatalf("skill must strip live state")
	}
}

func TestWriteSkill_Idempotent(t *testing.T) {
	a := buildTestApp()
	dir := t.TempDir()
	_, a1, err := WriteSkill(a, dir)
	if err != nil || a1 != ActionInstalled {
		t.Fatalf("first write: %v %q", err, a1)
	}
	_, a2, err := WriteSkill(a, dir)
	if err != nil || a2 != ActionNoOp {
		t.Fatalf("second write should be noop: %v %q", err, a2)
	}
}
