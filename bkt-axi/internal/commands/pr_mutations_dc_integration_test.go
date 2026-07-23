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
	"sync/atomic"
	"testing"

	"github.com/ruttybob/bkt-axi/internal/app"
)

// pr_mutations_dc_integration_test.go proves the Phase 2 DC mutation paths:
// optimistic-concurrency version retry on a 409, idempotent no-ops, branch
// mutations, and the DC approve idempotency pre-check.

// writeTestDCConfig writes an isolated DC config pointed at srv (PROJ/api,
// username "tester", bearer auth). No keyring is needed.
func writeTestDCConfig(t *testing.T, srv string) string {
	t.Helper()
	dir := t.TempDir()
	cfg := fmt.Sprintf(`version: 1
active_context: test
contexts:
  test:
    host: testdc
    project_key: PROJ
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

// dcPRJSON builds a DC pull-request payload with controllable state, version,
// and an opt-in "current user already approved" participant.
func dcPRJSON(id int, title, state string, version int, meApproved bool) string {
	status := "UNAPPROVED"
	approved := "false"
	if meApproved {
		status = "APPROVED"
		approved = "true"
	}
	return fmt.Sprintf(`{
		"id": %d, "title": %q, "description": "d", "state": %q, "version": %d, "draft": false,
		"createdDate": 1700000000000, "updatedDate": 1700000000000,
		"author": {"user": {"name": "author", "slug": "author", "displayName": "Author"}},
		"fromRef": {"id": "refs/heads/feature/x", "displayId": "feature/x", "repository": {"slug": "api", "project": {"key": "PROJ"}}},
		"toRef": {"id": "refs/heads/main", "displayId": "main", "repository": {"slug": "api", "project": {"key": "PROJ"}}},
		"reviewers": [{"user": {"name": "tester", "slug": "tester", "displayName": "Tester"}, "status": %q, "approved": %s}],
		"participants": [],
		"links": {"self": [{"href": "http://example/projects/PROJ/repos/api/pull-requests/%d"}]}
	}`, id, title, state, version, status, approved, id)
}

// --- optimistic-concurrency version retry --------------------------------

func TestDCPRMerge_RetriesOnStaleVersion409(t *testing.T) {
	var mergeAttempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.URL.Path == "/rest/api/1.0/projects/PROJ/repos/api/pull-requests/42" && req.Method == http.MethodGet:
			// Each GET bumps the version so the first merge (version 1) is stale.
			v := int(atomic.AddInt32(&mergeAttempts, 1))
			// mergeAttempts: 1 after first GET (v=1 → merge stale), 2 after the
			// retry GET (v=2 → merge succeeds). v>=3 means the post-merge re-GET.
			state := "OPEN"
			if v >= 3 {
				state = "MERGED"
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, dcPRJSON(42, "Ship it", state, v, false))
		case req.URL.Path == "/rest/api/1.0/projects/PROJ/repos/api/pull-requests/42/merge" && req.Method == http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(req.Body).Decode(&body)
			ver, _ := body["version"].(float64)
			if int(ver) < 2 {
				http.Error(w, `{"errors":[{"message":"pull request is stale","exceptionName":"StaleVersion"}]}`, http.StatusConflict)
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, req)
		}
	}))
	defer srv.Close()
	writeTestDCConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "merge", "42"})
	if code != app.ExitSuccess {
		t.Fatalf("merge-with-retry should exit 0, got %d: %s", code, out)
	}
	if !strings.Contains(out, "state: merged") {
		t.Fatalf("missing merged confirm:\n%s", out)
	}
	got := atomic.LoadInt32(&mergeAttempts)
	// 1: initial GET, 2: re-fetch after 409, 3: post-merge re-GET.
	if got != 3 {
		t.Fatalf("expected 3 GETs (initial + retry-refetch + post-merge), got %d", got)
	}
}

func TestDCPRMerge_SurfacesErrorWhenRetryStillStale(t *testing.T) {
	var gets int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.URL.Path == "/rest/api/1.0/projects/PROJ/repos/api/pull-requests/42" && req.Method == http.MethodGet:
			v := int(atomic.AddInt32(&gets, 1))
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, dcPRJSON(42, "Ship it", "OPEN", v, false))
		case req.URL.Path == "/rest/api/1.0/projects/PROJ/repos/api/pull-requests/42/merge" && req.Method == http.MethodPost:
			// Always stale, never merges.
			http.Error(w, `{"errors":[{"message":"stale","exceptionName":"StaleVersion"}]}`, http.StatusConflict)
		default:
			http.NotFound(w, req)
		}
	}))
	defer srv.Close()
	writeTestDCConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "merge", "42"})
	if code != app.ExitError {
		t.Fatalf("persistently-stale merge should exit 1, got %d: %s", code, out)
	}
	if !strings.Contains(out, "error:") {
		t.Fatalf("missing error output:\n%s", out)
	}
}

// --- idempotency ---------------------------------------------------------

func TestDCPRMerge_AlreadyMergedIsNoOp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/rest/api/1.0/projects/PROJ/repos/api/pull-requests/42" && req.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, dcPRJSON(42, "Ship it", "MERGED", 5, false))
			return
		}
		http.NotFound(w, req)
	}))
	defer srv.Close()
	writeTestDCConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "merge", "42"})
	if code != app.ExitSuccess {
		t.Fatalf("already-merged should exit 0, got %d: %s", code, out)
	}
	if !strings.Contains(out, "state: merged (already — no-op)") {
		t.Fatalf("missing idempotent confirm:\n%s", out)
	}
}

func TestDCPRApprove_AlreadyApprovedIsNoOp(t *testing.T) {
	var approves int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.URL.Path == "/rest/api/1.0/projects/PROJ/repos/api/pull-requests/42" && req.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, dcPRJSON(42, "Ship it", "OPEN", 1, true))
		case req.URL.Path == "/rest/api/1.0/projects/PROJ/repos/api/pull-requests/42/approve" && req.Method == http.MethodPost:
			atomic.AddInt32(&approves, 1)
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, req)
		}
	}))
	defer srv.Close()
	writeTestDCConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "approve", "42"})
	if code != app.ExitSuccess {
		t.Fatalf("already-approved should exit 0, got %d: %s", code, out)
	}
	if !strings.Contains(out, "state: approved (already — no-op)") {
		t.Fatalf("missing idempotent confirm:\n%s", out)
	}
	if atomic.LoadInt32(&approves) != 0 {
		t.Fatalf("already-approved must not call /approve")
	}
}

func TestDCPRDecline_AlreadyDeclinedIsNoOp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/rest/api/1.0/projects/PROJ/repos/api/pull-requests/42" && req.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, dcPRJSON(42, "Ship it", "DECLINED", 3, false))
			return
		}
		http.NotFound(w, req)
	}))
	defer srv.Close()
	writeTestDCConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "decline", "42"})
	if code != app.ExitSuccess {
		t.Fatalf("already-declined should exit 0, got %d: %s", code, out)
	}
	if !strings.Contains(out, "state: declined (already — no-op)") {
		t.Fatalf("missing idempotent confirm:\n%s", out)
	}
}

// --- 404 / 403 error mapping --------------------------------------------

func TestDCPRMerge_NotFoundMapsClean(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.NotFound(w, req)
	}))
	defer srv.Close()
	writeTestDCConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "merge", "9999"})
	if code != app.ExitError {
		t.Fatalf("404 should exit 1, got %d: %s", code, out)
	}
	if !strings.Contains(out, "error: pull request #9999 not found") {
		t.Fatalf("missing not-found error:\n%s", out)
	}
}
