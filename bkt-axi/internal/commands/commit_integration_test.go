package commands

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// commit_integration_test.go proves the `commit` Phase 1 commands end to end:
// view (with truncation), diff (tail-truncated + --full temp file), and status.

func longCommitMessage() string {
	return strings.Repeat("This is a long commit message body. ", 60)
}

func startMockCloudCommits(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/2.0/repositories/acme/api/commit/abc1234", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, fmt.Sprintf(`{"hash":"abc1234","message":%q,"date":"2024-01-15T10:00:00Z","author":{"raw":"Ada Lovelace <ada@example.com>","user":{"display_name":"Ada Lovelace"}},"parents":[{"hash":"def5678","type":"commit"}],"links":{"html":{"href":"https://bitbucket.org/acme/api/commits/abc1234"}}}`, longCommitMessage()))
	})
	mux.HandleFunc("/2.0/repositories/acme/api/diff/abc1234..def5678", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		// A diff larger than the 8000-char tail budget.
		io.WriteString(w, strings.Repeat("context line\n", 900))
		io.WriteString(w, "+final meaningful change at the tail\n")
	})
	mux.HandleFunc("/2.0/repositories/acme/api/commit/abc1234/statuses", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"values":[{"state":"SUCCESSFUL","key":"ci","name":"CI Build","url":"https://example.com/build/1","description":"Passed"}]}`)
	})
	return httptest.NewServer(mux)
}

func TestCommitView_RendersDetail(t *testing.T) {
	srv := startMockCloudCommits(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"commit", "view", "abc1234"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "commit:") {
		t.Fatalf("missing detail object:\n%s", out)
	}
	if !strings.Contains(out, "sha: abc1234") || !strings.Contains(out, "author: Ada Lovelace") {
		t.Fatalf("missing identity fields:\n%s", out)
	}
	// The full message exceeds the 500-char body budget, so it must be truncated.
	if !strings.Contains(out, "...(truncated") {
		t.Fatalf("expected truncated message preview:\n%s", out)
	}
	if !strings.Contains(out, "help[1]{step}:") {
		t.Fatalf("missing --full help hint:\n%s", out)
	}
	if !strings.Contains(out, "--full") {
		t.Fatalf("help hint should mention --full:\n%s", out)
	}
}

func TestCommitView_FullShowsCompleteMessage(t *testing.T) {
	srv := startMockCloudCommits(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"commit", "view", "abc1234", "--full"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if strings.Contains(out, "...(truncated") {
		t.Fatalf("--full should not truncate:\n%s", out)
	}
}

func TestCommitView_FieldsParents(t *testing.T) {
	srv := startMockCloudCommits(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"commit", "view", "abc1234", "--fields", "parents"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "parents: def5678") {
		t.Fatalf("missing parents field:\n%s", out)
	}
}

func TestCommitDiff_TruncatesTailAndHintsFull(t *testing.T) {
	srv := startMockCloudCommits(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"commit", "diff", "abc1234", "def5678"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	// Tail truncation must keep the meaningful tail and drop the leading bulk.
	if !strings.Contains(out, "final meaningful change at the tail") {
		t.Fatalf("tail preview missing the last line:\n%s", out)
	}
	if !strings.Contains(out, "...(truncated") {
		t.Fatalf("expected truncation marker:\n%s", out)
	}
	if !strings.Contains(out, "--full") {
		t.Fatalf("missing --full help hint:\n%s", out)
	}
	if strings.Contains(out, "full_path") {
		t.Fatalf("default diff should not write a temp file:\n%s", out)
	}
}

func TestCommitDiff_FullWritesTempFile(t *testing.T) {
	srv := startMockCloudCommits(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"commit", "diff", "abc1234", "def5678", "--full"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "full_path:") {
		t.Fatalf("--full should emit a full_path field:\n%s", out)
	}
	if strings.Contains(out, "...(truncated") {
		t.Fatalf("--full should not truncate:\n%s", out)
	}
}

func TestCommitStatus_RendersStatuses(t *testing.T) {
	srv := startMockCloudCommits(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"commit", "status", "abc1234"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "statuses[1]{state,key,name,url,description}:") {
		t.Fatalf("missing status list header:\n%s", out)
	}
	if !strings.Contains(out, "successful") {
		t.Fatalf("state not normalized to lowercase:\n%s", out)
	}
	if !strings.Contains(out, "CI Build") {
		t.Fatalf("missing status name:\n%s", out)
	}
}

func TestCommitStatus_Empty(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/2.0/repositories/acme/api/commit/abc1234/statuses", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"values":[]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"commit", "status", "abc1234"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "0 build statuses") {
		t.Fatalf("missing empty-state message:\n%s", out)
	}
	if !strings.Contains(out, "help[2]{step}:") {
		t.Fatalf("missing empty-state help:\n%s", out)
	}
}
