package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruttybob/bkt-axi/internal/app"
)

// branch_integration_test.go proves the `branch list` Phase 1 command end to end,
// including the opt-in commit-derived extras and the --text pipe-friendly mode.

func startMockCloudBranches(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/2.0/repositories/acme/api/refs/branches", func(w http.ResponseWriter, r *http.Request) {
		page := map[string]any{
			"values": []any{
				json.RawMessage(`{"name": "main", "default": true, "target": {"hash": "aaa111", "type": "commit"}, "links": {"html": {"href": "https://bitbucket.org/acme/api/src/main"}}}`),
				json.RawMessage(`{"name": "feature/x", "default": false, "target": {"hash": "bbb222", "type": "commit"}, "links": {"html": {"href": "https://bitbucket.org/acme/api/src/feature/x"}}}`),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(page)
	})
	// Per-branch head commit lookup (opt-in via message/author/updated fields).
	mux.HandleFunc("/2.0/repositories/acme/api/commit/aaa111", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"hash":"aaa111","message":"Initial commit","date":"2024-01-10T10:00:00Z","author":{"raw":"Ada Lovelace <ada@example.com>"}}`)
	})
	mux.HandleFunc("/2.0/repositories/acme/api/commit/bbb222", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"hash":"bbb222","message":"Add feature x","date":"2024-01-14T10:00:00Z","author":{"raw":"Grace Hopper <grace@example.com>"}}`)
	})
	return httptest.NewServer(mux)
}

func TestBranchList_RendersTOON(t *testing.T) {
	srv := startMockCloudBranches(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"branch", "list"})
	if code != 0 {
		t.Fatalf("branch list exit %d: %s", code, out)
	}
	if !strings.Contains(out, "branches[2]{name,default,latest_commit}:") {
		t.Fatalf("missing list header:\n%s", out)
	}
	if !strings.Contains(out, "  main,yes,aaa111") {
		t.Fatalf("default branch row wrong:\n%s", out)
	}
	if !strings.Contains(out, "  feature/x,no,bbb222") {
		t.Fatalf("non-default branch row wrong:\n%s", out)
	}
	if !strings.Contains(out, "count: 2") {
		t.Fatalf("missing count:\n%s", out)
	}
	if !strings.Contains(out, "help[1]{step}:") {
		t.Fatalf("missing help block:\n%s", out)
	}
}

func TestBranchList_FieldsCommitDetail(t *testing.T) {
	srv := startMockCloudBranches(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"branch", "list", "--fields", "message,author"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "branches[2]{name,default,latest_commit,message,author}:") {
		t.Fatalf("--fields did not extend schema:\n%s", out)
	}
	if !strings.Contains(out, "Add feature x") || !strings.Contains(out, "Grace Hopper") {
		t.Fatalf("commit-derived columns missing:\n%s", out)
	}
}

func TestBranchList_TextMode(t *testing.T) {
	srv := startMockCloudBranches(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"branch", "list", "--text"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("--text should print one branch name per line, got %d lines:\n%s", len(lines), out)
	}
	if lines[0] != "main" || lines[1] != "feature/x" {
		t.Fatalf("--text branch names wrong: %v", lines)
	}
}

func TestBranchList_NoRepoResolved(t *testing.T) {
	srv := startMockCloudBranches(t)
	defer srv.Close()
	dir := t.TempDir()
	// Config with no default_repo and no workspace, so scope resolves empty.
	cfg := fmt.Sprintf(`version: 1
active_context: test
contexts:
  test:
    host: testcloud
hosts:
  testcloud:
    kind: cloud
    base_url: %s/2.0
    username: tester
    token: test-token
    auth_method: basic
`, strings.TrimRight(srv.URL, "/"))
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("BKT_CONFIG_DIR", dir)

	out, code := runAppCapture(t, []string{"branch", "list"})
	if code != app.ExitError {
		t.Fatalf("no repo should exit 1, got %d: %s", code, out)
	}
	if !strings.Contains(out, "no repository resolved") {
		t.Fatalf("missing no-repo error:\n%s", out)
	}
}
