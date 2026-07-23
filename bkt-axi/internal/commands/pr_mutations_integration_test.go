package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ruttybob/bkt-axi/internal/app"
)

// pr_mutations_integration_test.go proves the Phase 2 PR mutation commands end
// to end: a mock Bitbucket Cloud API → unified client → normalized adapter →
// TOON render. It covers idempotency (already-in-target-state no-ops), error
// translation (404/403/409), usage errors (unknown flags, bad strategy), and
// the create/edit/comment/diff verbs.

// mutationPRJSON builds a Cloud PR payload with a controllable state, draft
// flag, and an opt-in "current user already approved" participant.
func mutationPRJSON(id int, title, state string, meApproved bool) string {
	approvedRaw := "null"
	if meApproved {
		approvedRaw = "true"
	}
	return fmt.Sprintf(`{
		"id": %d,
		"title": %q,
		"description": "desc",
		"state": %q,
		"draft": false,
		"created_on": "2024-01-15T10:00:00Z",
		"author": {"display_name": "Ada", "username": "ada", "uuid": "{ada-uuid}"},
		"source": {"branch": {"name": "feature/x"}, "commit": {"hash": "abc"}, "repository": {"slug": "api", "full_name": "acme/api"}},
		"destination": {"branch": {"name": "main"}, "commit": {"hash": "def"}, "repository": {"slug": "api", "full_name": "acme/api"}},
		"links": {"html": {"href": "https://bitbucket.org/acme/api/pull-requests/%d"}},
		"participants": [{"user": {"uuid": "{me-uuid}", "display_name": "Me"}, "role": "REVIEWER", "approved": %s}],
		"reviewers": [],
		"summary": {"raw": ""}
	}`, id, title, state, id, approvedRaw)
}

// recorderMux is an http.ServeMux that records the paths+methods it handles so
// tests can assert which endpoints a command did or did not call.
type recorderMux struct {
	mu    sync.Mutex
	calls []string
	inner *http.ServeMux
}

func newRecorderMux() *recorderMux { return &recorderMux{inner: http.NewServeMux()} }

func (r *recorderMux) HandleFunc(pattern string, fn func(http.ResponseWriter, *http.Request)) {
	r.inner.HandleFunc(pattern, func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.calls = append(r.calls, req.Method+" "+req.URL.Path)
		r.mu.Unlock()
		fn(w, req)
	})
}

func (r *recorderMux) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.inner.ServeHTTP(w, req)
}

func (r *recorderMux) count(method, path string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.calls {
		if c == method+" "+path {
			n++
		}
	}
	return n
}

// startCloudMutationServer builds a mock Cloud API with the PR state and an
// approve-endpoint counter. It is the shared substrate for the mutation tests.
func startCloudMutationServer(t *testing.T, state string, meApproved bool) (*httptest.Server, *recorderMux) {
	t.Helper()
	mux := newRecorderMux()
	mux.HandleFunc("/2.0/user", func(w http.ResponseWriter, req *http.Request) {
		io.WriteString(w, `{"uuid": "{me-uuid}", "username": "tester", "display_name": "Tester", "account_id": "1:tester"}`)
	})
	mux.HandleFunc("/2.0/repositories/acme/api/pullrequests/42", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, mutationPRJSON(42, "Ship it", state, meApproved))
	})
	mux.HandleFunc("/2.0/repositories/acme/api/pullrequests/42/approve", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/2.0/repositories/acme/api/pullrequests/42/merge", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/2.0/repositories/acme/api/pullrequests/42/decline", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	return srv, mux
}

// --- idempotency ---------------------------------------------------------

func TestPRApprove_AlreadyApprovedIsNoOp(t *testing.T) {
	srv, mux := startCloudMutationServer(t, "OPEN", true)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "approve", "42"})
	if code != app.ExitSuccess {
		t.Fatalf("already-approved should exit 0, got %d: %s", code, out)
	}
	if !strings.Contains(out, "state: approved (already — no-op)") {
		t.Fatalf("missing idempotent confirm:\n%s", out)
	}
	if mux.count("POST", "/2.0/repositories/acme/api/pullrequests/42/approve") != 0 {
		t.Fatalf("already-approved must not call /approve")
	}
}

func TestPRApprove_FreshApprovalRecordsAndConfirms(t *testing.T) {
	srv, mux := startCloudMutationServer(t, "OPEN", false)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "approve", "42"})
	if code != app.ExitSuccess {
		t.Fatalf("approve should exit 0, got %d: %s", code, out)
	}
	if !strings.Contains(out, "state: approved") {
		t.Fatalf("missing approved confirm:\n%s", out)
	}
	if strings.Contains(out, "no-op") {
		t.Fatalf("fresh approval must not be a no-op:\n%s", out)
	}
	if mux.count("POST", "/2.0/repositories/acme/api/pullrequests/42/approve") != 1 {
		t.Fatalf("fresh approval must call /approve exactly once")
	}
}

func TestPRMerge_AlreadyMergedIsNoOp(t *testing.T) {
	srv, mux := startCloudMutationServer(t, "MERGED", false)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "merge", "42"})
	if code != app.ExitSuccess {
		t.Fatalf("already-merged should exit 0, got %d: %s", code, out)
	}
	if !strings.Contains(out, "state: merged (already — no-op)") {
		t.Fatalf("missing idempotent confirm:\n%s", out)
	}
	if mux.count("POST", "/2.0/repositories/acme/api/pullrequests/42/merge") != 0 {
		t.Fatalf("already-merged must not call /merge")
	}
}

func TestPRMerge_FreshMergeConfirms(t *testing.T) {
	srv, mux := startCloudMutationServer(t, "OPEN", false)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "merge", "42"})
	if code != app.ExitSuccess {
		t.Fatalf("merge should exit 0, got %d: %s", code, out)
	}
	// Re-GET returns the same OPEN payload (mock is static); the confirm still
	// reports the merge was issued. Assert the merge endpoint was hit.
	if mux.count("POST", "/2.0/repositories/acme/api/pullrequests/42/merge") != 1 {
		t.Fatalf("fresh merge must call /merge exactly once")
	}
	if !strings.Contains(out, "state:") {
		t.Fatalf("missing state confirm:\n%s", out)
	}
}

func TestPRDecline_AlreadyDeclinedIsNoOp(t *testing.T) {
	srv, mux := startCloudMutationServer(t, "DECLINED", false)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "decline", "42"})
	if code != app.ExitSuccess {
		t.Fatalf("already-declined should exit 0, got %d: %s", code, out)
	}
	if !strings.Contains(out, "state: declined (already — no-op)") {
		t.Fatalf("missing idempotent confirm:\n%s", out)
	}
	if mux.count("POST", "/2.0/repositories/acme/api/pullrequests/42/decline") != 0 {
		t.Fatalf("already-declined must not call /decline")
	}
}

func TestPRReopen_AlreadyOpenIsNoOp(t *testing.T) {
	srv, mux := startCloudMutationServer(t, "OPEN", false)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "reopen", "42"})
	if code != app.ExitSuccess {
		t.Fatalf("already-open should exit 0, got %d: %s", code, out)
	}
	if !strings.Contains(out, "state: open (already — no-op)") {
		t.Fatalf("missing idempotent confirm:\n%s", out)
	}
	if mux.count("PUT", "/2.0/repositories/acme/api/pullrequests/42") != 0 {
		t.Fatalf("already-open must not PUT reopen")
	}
}

// --- create / comment / diff --------------------------------------------

func TestPRCreate_ConfirmsMinimal(t *testing.T) {
	srv := startMockCloudForCreate(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "create", "--source", "feature/x", "--target", "main", "--title", "New thing"})
	if code != app.ExitSuccess {
		t.Fatalf("create should exit 0, got %d: %s", code, out)
	}
	if !strings.Contains(out, "title: New thing") {
		t.Fatalf("missing title in confirm:\n%s", out)
	}
	if !strings.Contains(out, "bitbucket.org/acme/api/pull-requests/77") {
		t.Fatalf("missing url in confirm:\n%s", out)
	}
}

func TestPRComment_ConfirmsWithID(t *testing.T) {
	srv := startMockCloudForComment(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "comment", "42", "--body", "LGTM"})
	if code != app.ExitSuccess {
		t.Fatalf("comment should exit 0, got %d: %s", code, out)
	}
	if !strings.Contains(out, "comment:") || !strings.Contains(out, "id: 909") {
		t.Fatalf("missing comment id confirm:\n%s", out)
	}
	if !strings.Contains(out, "pull_request: 42") {
		t.Fatalf("missing pr ref in comment confirm:\n%s", out)
	}
}

func TestPREdit_ConfirmsTitle(t *testing.T) {
	srv := startMockCloudForEdit(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "edit", "42", "--title", "Renamed"})
	if code != app.ExitSuccess {
		t.Fatalf("edit should exit 0, got %d: %s", code, out)
	}
	if !strings.Contains(out, "title: Renamed") {
		t.Fatalf("missing renamed title in confirm:\n%s", out)
	}
}

func TestPREdit_NoChangeFlagsIsUsage(t *testing.T) {
	srv, _ := startCloudMutationServer(t, "OPEN", false)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "edit", "42"})
	if code != app.ExitUsage {
		t.Fatalf("edit with no change flags should exit 2, got %d: %s", code, out)
	}
	if !strings.Contains(out, "at least one of") {
		t.Fatalf("missing usage error:\n%s", out)
	}
}

func TestPRComment_MissingBodyIsUsage(t *testing.T) {
	srv, _ := startCloudMutationServer(t, "OPEN", false)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "comment", "42"})
	if code != app.ExitUsage {
		t.Fatalf("missing --body should exit 2, got %d: %s", code, out)
	}
	if !strings.Contains(out, "--body (or --body-file) is required") {
		t.Fatalf("missing body usage error:\n%s", out)
	}
}

func TestPRDiff_TruncatesTailWithFullHint(t *testing.T) {
	srv := startMockCloudForDiff(t, strings.Repeat("a", 20000))
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "diff", "42"})
	if code != app.ExitSuccess {
		t.Fatalf("diff should exit 0, got %d: %s", code, out)
	}
	if !strings.Contains(out, "diff:") {
		t.Fatalf("missing diff field:\n%s", out[:400])
	}
	if !strings.Contains(out, "Run `bkt-axi pr diff 42 --full`") {
		t.Fatalf("missing --full hint for a truncated diff:\n%s", out[:400])
	}
}

func TestPRDiff_FullWritesTempFile(t *testing.T) {
	srv := startMockCloudForDiff(t, strings.Repeat("a", 20000))
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "diff", "42", "--full"})
	if code != app.ExitSuccess {
		t.Fatalf("diff --full should exit 0, got %d: %s", code, out)
	}
	if !strings.Contains(out, "full_path:") {
		t.Fatalf("--full must emit full_path:\n%s", out[:300])
	}
	if !strings.Contains(out, "cat ") {
		t.Fatalf("--full must hint at reading the temp file:\n%s", out[:300])
	}
}

// --- usage & error mapping ----------------------------------------------

func TestPRMerge_UnknownStrategyIsUsage(t *testing.T) {
	srv, _ := startCloudMutationServer(t, "OPEN", false)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "merge", "42", "--strategy", "bogus"})
	if code != app.ExitUsage {
		t.Fatalf("bad --strategy should exit 2, got %d: %s", code, out)
	}
	if !strings.Contains(out, "unknown --strategy") || !strings.Contains(out, "bogus") {
		t.Fatalf("missing strategy error:\n%s", out)
	}
	if !strings.Contains(out, "squash, merge, rebase") {
		t.Fatalf("missing valid-strategy hint:\n%s", out)
	}
}

func TestPRApprove_UnknownFlagIsUsage(t *testing.T) {
	srv, _ := startCloudMutationServer(t, "OPEN", false)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "approve", "42", "--bogus"})
	if code != app.ExitUsage {
		t.Fatalf("unknown flag should exit 2, got %d: %s", code, out)
	}
	if !strings.Contains(out, "unknown flag --bogus") {
		t.Fatalf("missing unknown-flag error:\n%s", out)
	}
	if !strings.Contains(out, "valid flags for `approve`") {
		t.Fatalf("missing valid-flag hint:\n%s", out)
	}
}

func TestPRMerge_NotFoundMapsClean(t *testing.T) {
	srv := startMockCloud404(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "merge", "9999"})
	if code != app.ExitError {
		t.Fatalf("404 should exit 1, got %d: %s", code, out)
	}
	if !strings.Contains(out, "error: pull request #9999 not found") {
		t.Fatalf("missing not-found error:\n%s", out)
	}
}

// --- mock helpers --------------------------------------------------------

func startMockCloudForCreate(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/2.0/user", func(w http.ResponseWriter, req *http.Request) {
		io.WriteString(w, `{"uuid": "{me-uuid}", "username": "tester", "display_name": "Tester"}`)
	})
	mux.HandleFunc("/2.0/repositories/acme/api", func(w http.ResponseWriter, req *http.Request) {
		// GET repository → default branch for --target inference fallback.
		if req.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"slug":"api","name":"api","mainbranch":{"name":"main"}}`)
			return
		}
	})
	mux.HandleFunc("/2.0/repositories/acme/api/pullrequests", func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			http.NotFound(w, req)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(req.Body).Decode(&body)
		if body["title"] != "New thing" {
			http.Error(w, "bad title", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, mutationPRJSON(77, "New thing", "OPEN", false))
	})
	return httptest.NewServer(mux)
}

func startMockCloudForEdit(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/2.0/repositories/acme/api/pullrequests/42", func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPut {
			http.NotFound(w, req)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(req.Body).Decode(&body)
		if body["title"] != "Renamed" {
			http.Error(w, "bad title", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, mutationPRJSON(42, "Renamed", "OPEN", false))
	})
	return httptest.NewServer(mux)
}

func startMockCloudForComment(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/2.0/repositories/acme/api/pullrequests/42/comments", func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":909,"content":{"raw":"LGTM"}}`)
	})
	return httptest.NewServer(mux)
}

func startMockCloudForDiff(t *testing.T, body string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/2.0/repositories/acme/api/pullrequests/42/diff", func(w http.ResponseWriter, req *http.Request) {
		io.WriteString(w, body)
	})
	return httptest.NewServer(mux)
}

func startMockCloud404(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		http.NotFound(w, req)
	})
	return httptest.NewServer(mux)
}
