package commands

// skill_freshness_test.go is the CI freshness check (AXI §7 "single source of
// truth"): it regenerates the SKILL.md from the current command tree and fails
// if the committed skills/bkt-axi/SKILL.md is stale. To regenerate after
// changing the command surface, run:
//
//	BKT_AXI_GEN_SKILL=1 go test ./internal/commands/ -run TestRegenerateSkillArtifact
//
// then commit the updated file.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ruttybob/bkt-axi/internal/session"
)

// findCommittedSkill locates skills/bkt-axi/SKILL.md by walking up from this
// test file's directory to the repository root. It returns the target path
// whether or not the file exists yet (the caller checks existence), and avoids
// shelling out to git so it works in any working directory. The repository
// root is the first ancestor that contains both a bkt-axi/ module dir and a
// skills/ dir.
func findCommittedSkill(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)
	for i := 0; i < 10; i++ {
		if isRepoRoot(dir) {
			return filepath.Join(dir, "skills", "bkt-axi", "SKILL.md")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("repository root (containing both bkt-axi/ and skills/) not found walking up from " + filepath.Dir(thisFile))
	return ""
}

// isRepoRoot reports whether dir looks like the repo root: it has both the
// bkt-axi/ Go module and a skills/ directory.
func isRepoRoot(dir string) bool {
	if dir == "" || dir == "/" {
		return false
	}
	bktAxi, err1 := os.Stat(filepath.Join(dir, "bkt-axi"))
	skills, err2 := os.Stat(filepath.Join(dir, "skills"))
	return err1 == nil && err2 == nil && bktAxi.IsDir() && skills.IsDir()
}

// TestSkillNotStale fails when the committed skill does not match what the
// generator produces from the current command tree. This is the gate that
// keeps the skill from drifting from the CLI's own help output.
func TestSkillNotStale(t *testing.T) {
	committed := findCommittedSkill(t)
	existing, err := os.ReadFile(committed)
	if err != nil {
		t.Fatalf("cannot read committed skill %s: %v (run BKT_AXI_GEN_SKILL=1 go test ./internal/commands/ -run TestRegenerateSkillArtifact to create it)", committed, err)
	}
	want := session.SkillContent(NewApp(""))
	if string(existing) != want {
		// Show a compact, actionable diff so the failure is obvious in CI.
		firstDiff := firstDifferingLine(string(existing), want)
		t.Errorf("committed skills/bkt-axi/SKILL.md is stale (first diff: %s)\n"+
			"regenerate with: BKT_AXI_GEN_SKILL=1 go test ./internal/commands/ -run TestRegenerateSkillArtifact", firstDiff)
	}
}

// TestRegenerateSkillArtifact (re)writes the committed skill from the current
// command tree. Skipped unless BKT_AXI_GEN_SKILL=1 so it never runs in CI as a
// normal test; it is a developer affordance invoked explicitly.
func TestRegenerateSkillArtifact(t *testing.T) {
	if os.Getenv("BKT_AXI_GEN_SKILL") != "1" {
		t.Skip("set BKT_AXI_GEN_SKILL=1 to regenerate the committed skill")
	}
	committed := findCommittedSkill(t)
	content := session.SkillContent(NewApp(""))
	if err := os.MkdirAll(filepath.Dir(committed), 0o755); err != nil {
		t.Fatalf("mkdir for committed skill: %v", err)
	}
	if err := os.WriteFile(committed, []byte(content), 0o644); err != nil {
		t.Fatalf("write committed skill: %v", err)
	}
	t.Logf("regenerated %s (%d bytes)", committed, len(content))
}

// firstDifferingLine returns a one-line summary of the first line where a and b
// differ, for compact CI output.
func firstDifferingLine(a, b string) string {
	la := strings.Split(a, "\n")
	lb := strings.Split(b, "\n")
	n := len(la)
	if len(lb) < n {
		n = len(lb)
	}
	for i := 0; i < n; i++ {
		if la[i] != lb[i] {
			return "line " + itoa(i+1) + ": committed=" + trunc(la[i]) + " vs generated=" + trunc(lb[i])
		}
	}
	if len(la) != len(lb) {
		return "line count differs: committed=" + itoa(len(la)) + " vs generated=" + itoa(len(lb))
	}
	return "files differ in a way no single line captures"
}

func trunc(s string) string {
	if len(s) > 60 {
		return s[:60] + "…"
	}
	return s
}

func itoa(n int) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%10]
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
