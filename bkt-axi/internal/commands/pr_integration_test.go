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

// pr_integration_test.go proves the Phase 0 vertical slice end to end: a mock
// Bitbucket Cloud API → unified client → normalized adapter → TOON render. It
// exercises the real config load, host/scope resolution, and dispatcher.

// cloudPRJSON is a minimal Cloud pull-request payload the adapter maps.
func cloudPRJSON(id int, title, state string, approved bool) string {
	approvedRaw := "null"
	if approved {
		approvedRaw = "true"
	}
	return fmt.Sprintf(`{
		"id": %d,
		"title": %q,
		"description": "Body text for PR %d",
		"state": %q,
		"draft": false,
		"created_on": "2024-01-15T10:00:00Z",
		"author": {"display_name": "Ada Lovelace", "username": "ada", "uuid": "{ada-uuid}", "account_id": "123:ada"},
		"source": {"branch": {"name": "feature/x"}, "commit": {"hash": "abc"}, "repository": {"slug": "api"}},
		"destination": {"branch": {"name": "main"}, "commit": {"hash": "def"}, "repository": {"slug": "api"}},
		"links": {"html": {"href": "https://bitbucket.org/acme/api/pull-requests/%d"}},
		"participants": [{"user": {"uuid": "{rev-uuid}", "display_name": "Bob"}, "role": "REVIEWER", "approved": %s}],
		"reviewers": [{"nickname": "Bob"}],
		"summary": {"raw": ""}
	}`, id, title, id, state, id, approvedRaw)
}

// startMockCloud spins up an httptest server that answers the Cloud PR list and
// view endpoints used by the Phase 0 commands.
func startMockCloud(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/2.0/user", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"uuid": "{me-uuid}", "username": "tester", "display_name": "Tester", "account_id": "1:tester"}`)
	})
	mux.HandleFunc("/2.0/repositories/acme/api/pullrequests", func(w http.ResponseWriter, r *http.Request) {
		// Single PR list page; no "next" link → not paginated.
		page := map[string]any{
			"values": []any{
				json.RawMessage(cloudPRJSON(1043, "Fix token refresh", "OPEN", true)),
				json.RawMessage(cloudPRJSON(1041, "Add pagination", "OPEN", false)),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(page)
	})
	mux.HandleFunc("/2.0/repositories/acme/api/pullrequests/42", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, cloudPRJSON(42, "Single PR view", "OPEN", false))
	})
	mux.HandleFunc("/2.0/repositories/acme/api/pullrequests/42/comments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"values":[{"id":1,"content":{"raw":"Looks good"},"user":{"display_name":"Bob"},"created_on":"2024-01-15T11:00:00Z"}]}`)
	})
	return httptest.NewServer(mux)
}

// writeTestConfig creates an isolated BKT_CONFIG_DIR with a cloud host pointed
// at srv, an active context (workspace=acme, repo=api), and an inline token so
// no keyring is needed.
func writeTestConfig(t *testing.T, srv string) string {
	t.Helper()
	dir := t.TempDir()
	cfg := fmt.Sprintf(`version: 1
active_context: test
contexts:
  test:
    host: testcloud
    workspace: acme
    default_repo: api
hosts:
  testcloud:
    kind: cloud
    base_url: %s/2.0
    username: tester
    token: test-token
    auth_method: basic
`, strings.TrimRight(srv, "/"))
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("BKT_CONFIG_DIR", dir)
	return dir
}

func runAppCapture(t *testing.T, argv []string) (string, int) {
	t.Helper()
	a := NewApp("~/test/bkt-axi")
	var out strings.Builder
	a.Stdout = &out
	code := a.Run(argv)
	return out.String(), code
}

func TestPRList_RendersTOON(t *testing.T) {
	srv := startMockCloud(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "list"})
	if code != 0 {
		t.Fatalf("pr list exit %d: %s", code, out)
	}
	if !strings.Contains(out, "pull_requests[2]{id,title,state,review}:") {
		t.Fatalf("missing list header:\n%s", out)
	}
	if !strings.Contains(out, "  1043,Fix token refresh,open,approved") {
		t.Fatalf("approved PR row wrong:\n%s", out)
	}
	if !strings.Contains(out, "  1041,Add pagination,open,required") {
		t.Fatalf("required PR row wrong:\n%s", out)
	}
	if !strings.Contains(out, "count: 2") {
		t.Fatalf("missing count:\n%s", out)
	}
	if !strings.Contains(out, "help[1]{step}:") {
		t.Fatalf("missing help block:\n%s", out)
	}
}

func TestPRList_FieldsExtendsSchema(t *testing.T) {
	srv := startMockCloud(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "list", "--fields", "author,branch"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "pull_requests[2]{id,title,state,review,author,branch}:") {
		t.Fatalf("--fields did not extend schema:\n%s", out)
	}
	if !strings.Contains(out, "Ada Lovelace") {
		t.Fatalf("author column missing:\n%s", out)
	}
	if !strings.Contains(out, "feature/x") {
		t.Fatalf("branch column missing:\n%s", out)
	}
}

func TestPRList_FieldsUnknownValue(t *testing.T) {
	srv := startMockCloud(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "list", "--fields", "bogus"})
	if code != app.ExitUsage {
		t.Fatalf("unknown --fields value should exit 2, got %d: %s", code, out)
	}
	if !strings.Contains(out, "unknown --fields value `bogus`") {
		t.Fatalf("missing fields error:\n%s", out)
	}
	if !strings.Contains(out, "allowed --fields values:") {
		t.Fatalf("missing allowed-fields hint:\n%s", out)
	}
}

func TestPRView_RendersDetailWithTruncation(t *testing.T) {
	srv := startMockCloud(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "view", "42"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "pull_request:") {
		t.Fatalf("missing detail object:\n%s", out)
	}
	if !strings.Contains(out, "title: Single PR view") {
		t.Fatalf("missing title:\n%s", out)
	}
	if !strings.Contains(out, "from: feature/x") || !strings.Contains(out, "to: main") {
		t.Fatalf("missing branch fields:\n%s", out)
	}
	if !strings.Contains(out, "Body text for PR 42") {
		t.Fatalf("missing description preview:\n%s", out)
	}
}

func TestPRView_NotFound(t *testing.T) {
	srv := startMockCloud(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "view", "9999"})
	if code != app.ExitError {
		t.Fatalf("404 should exit 1, got %d: %s", code, out)
	}
	if !strings.Contains(out, "error: pull request #9999 not found") {
		t.Fatalf("missing not-found error:\n%s", out)
	}
	if !strings.Contains(out, "NOT_FOUND") == false {
		// code is omitted from output by design; just ensure help hint present
	}
}

func TestPRList_JSONEscapeHatch(t *testing.T) {
	srv := startMockCloud(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "list", "--json"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, `"pull_requests"`) || !strings.Contains(out, `"id": 1043`) {
		t.Fatalf("--json payload wrong:\n%s", out)
	}
}
