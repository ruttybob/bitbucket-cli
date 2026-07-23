package commands

import (
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

// phase3_dc_test.go proves the Data Center-only nouns and the cross-platform
// webhook path end to end, plus the platform-restriction errors.

// writeTestDCConfigPhase3 creates an isolated config pointing at a DC host.
func writeTestDCConfigPhase3(t *testing.T, srv string) string {
	t.Helper()
	dir := t.TempDir()
	cfg := fmt.Sprintf(`version: 1
active_context: dc
contexts:
  dc:
    host: testdc
    project_key: ENG
    default_repo: api
hosts:
  testdc:
    kind: dc
    base_url: %s
    username: tester
    token: test-token
    auth_method: bearer
`, strings.TrimRight(srv, "/"))
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("BKT_CONFIG_DIR", dir)
	return dir
}

// startMockDC answers the Data Center endpoints used by the Phase 3 commands.
func startMockDC(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	// projects
	mux.HandleFunc("/rest/api/1.0/projects", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"values":[{"key":"ENG","name":"Engineering","description":"Core platform"}],"isLastPage":true,"size":1}`)
	})

	// repository webhooks (list/create) + by-id (delete/test)
	mux.HandleFunc("/rest/api/1.0/projects/ENG/repos/api/webhooks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"values":[{"id":1,"name":"CI","url":"https://hook.example/cb","active":true,"events":["repo:refs_changed"]}]}`)
	})
	mux.HandleFunc("/rest/api/1.0/projects/ENG/repos/api/webhooks/1/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// project permissions
	mux.HandleFunc("/rest/api/1.0/projects/ENG/permissions/users", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"values":[{"user":{"name":"ada","displayName":"Ada Lovelace"},"permission":"PROJECT_ADMIN"}],"isLastPage":true}`)
	})

	// admin logging
	mux.HandleFunc("/rest/api/1.0/admin/logs/settings", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"level":"INFO","async":true}`)
	})

	// pull request (for task list / checks head commit)
	mux.HandleFunc("/rest/api/1.0/projects/ENG/repos/api/pull-requests/42", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":42,"title":"PR","state":"OPEN","version":0,"fromRef":{"id":"refs/heads/feature","displayId":"feature","latestCommit":"abc123","repository":{"slug":"api","project":{"key":"ENG"}}},"toRef":{"id":"refs/heads/main","displayId":"main","latestCommit":"def","repository":{"slug":"api","project":{"key":"ENG"}}}}`)
	})
	mux.HandleFunc("/rest/api/1.0/projects/ENG/repos/api/pull-requests/42/blocker-comments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"values":[{"id":1,"state":"OPEN","text":"fix tests","author":{"name":"ada","displayName":"Ada"},"createdDate":1,"updatedDate":1}],"isLastPage":true}`)
	})

	// commit build statuses
	mux.HandleFunc("/rest/build-status/1.0/commits/abc123", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"values":[{"state":"SUCCESSFUL","key":"ci","name":"build","url":"https://ci.example/1","description":"ok"}]}`)
	})

	return httptest.NewServer(mux)
}

func TestDC_ProjectList_RendersTOON(t *testing.T) {
	srv := startMockDC(t)
	defer srv.Close()
	writeTestDCConfigPhase3(t, srv.URL)

	out, code := runAppCapture(t, []string{"project", "list"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "projects[1]{key,name,description}:") {
		t.Fatalf("missing list header:\n%s", out)
	}
	if !strings.Contains(out, "ENG,Engineering") {
		t.Fatalf("row wrong:\n%s", out)
	}
}

func TestDC_WebhookList_RendersTOON(t *testing.T) {
	srv := startMockDC(t)
	defer srv.Close()
	writeTestDCConfigPhase3(t, srv.URL)

	out, code := runAppCapture(t, []string{"webhook", "list"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "webhooks[1]{id,url,active,events}:") {
		t.Fatalf("missing list header:\n%s", out)
	}
	if !strings.Contains(out, "repo:refs_changed") || !strings.Contains(out, "hook.example/cb") {
		t.Fatalf("row missing:\n%s", out)
	}
}

func TestDC_WebhookTest_Succeeds(t *testing.T) {
	srv := startMockDC(t)
	defer srv.Close()
	writeTestDCConfigPhase3(t, srv.URL)

	out, code := runAppCapture(t, []string{"webhook", "test", "1"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "test delivery triggered for webhook 1") {
		t.Fatalf("missing confirmation:\n%s", out)
	}
}

func TestDC_PermsProjectList_RendersTOON(t *testing.T) {
	srv := startMockDC(t)
	defer srv.Close()
	writeTestDCConfigPhase3(t, srv.URL)

	out, code := runAppCapture(t, []string{"perms", "project", "list", "ENG"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "permissions[1]{user,permission}:") {
		t.Fatalf("missing list header:\n%s", out)
	}
	if !strings.Contains(out, "PROJECT_ADMIN") {
		t.Fatalf("row wrong:\n%s", out)
	}
}

func TestDC_AdminLoggingGet_RendersDetail(t *testing.T) {
	srv := startMockDC(t)
	defer srv.Close()
	writeTestDCConfigPhase3(t, srv.URL)

	out, code := runAppCapture(t, []string{"admin", "logging", "get"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "logging:") || !strings.Contains(out, "level: INFO") {
		t.Fatalf("missing detail:\n%s", out)
	}
}

func TestDC_PRTaskList_RendersTOON(t *testing.T) {
	srv := startMockDC(t)
	defer srv.Close()
	writeTestDCConfigPhase3(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "task", "list", "42"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "tasks[1]{id,state,text,author}:") {
		t.Fatalf("missing list header:\n%s", out)
	}
}

func TestDC_StatusCommit_RendersStatuses(t *testing.T) {
	srv := startMockDC(t)
	defer srv.Close()
	writeTestDCConfigPhase3(t, srv.URL)

	out, code := runAppCapture(t, []string{"status", "commit", "abc123"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "statuses[1]{state,key,name,url}:") {
		t.Fatalf("missing statuses header:\n%s", out)
	}
}

// --- platform restrictions ------------------------------------------------

func TestPlatform_IssueOnDC_ClearError(t *testing.T) {
	srv := startMockDC(t)
	defer srv.Close()
	writeTestDCConfigPhase3(t, srv.URL)

	out, code := runAppCapture(t, []string{"issue", "list"})
	if code != app.ExitError {
		t.Fatalf("Cloud-only op on DC should exit 1, got %d: %s", code, out)
	}
	if !strings.Contains(out, "issues is Bitbucket Cloud only") {
		t.Fatalf("missing platform error:\n%s", out)
	}
	if !strings.Contains(out, "the active host is Bitbucket Data Center") {
		t.Fatalf("missing active-host context:\n%s", out)
	}
}

func TestPlatform_PipelineOnDC_ClearError(t *testing.T) {
	srv := startMockDC(t)
	defer srv.Close()
	writeTestDCConfigPhase3(t, srv.URL)

	out, code := runAppCapture(t, []string{"status", "pipeline", "42"})
	if code != app.ExitError {
		t.Fatalf("Cloud-only op on DC should exit 1, got %d: %s", code, out)
	}
	if !strings.Contains(out, "pipelines is Bitbucket Cloud only") {
		t.Fatalf("missing platform error:\n%s", out)
	}
}

func TestPlatform_ProjectOnCloud_ClearError(t *testing.T) {
	srv := startMockCloudPhase3(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"project", "list"})
	if code != app.ExitError {
		t.Fatalf("DC-only op on Cloud should exit 1, got %d: %s", code, out)
	}
	if !strings.Contains(out, "projects is Bitbucket Data Center only") {
		t.Fatalf("missing platform error:\n%s", out)
	}
}
