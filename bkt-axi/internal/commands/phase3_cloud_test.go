package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ruttybob/bkt-axi/internal/app"
)

// phase3_cloud_test.go proves the Phase 3 Cloud nouns end to end: mock API →
// unified client → normalized adapter → TOON. It reuses writeTestConfig and
// runAppCapture from pr_integration_test.go.

// startMockCloudPhase3 answers the Cloud issue, webhook, variable, pipeline,
// and raw-passthrough endpoints used by the Phase 3 commands.
func startMockCloudPhase3(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	// --- issues ---
	mux.HandleFunc("/2.0/repositories/acme/api/issues", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			page := map[string]any{"values": []any{
				json.RawMessage(cloudIssueJSON(5, "Fix bug", "open", "major")),
				json.RawMessage(cloudIssueJSON(6, "Add docs", "resolved", "trivial")),
			}}
			json.NewEncoder(w).Encode(page)
		case http.MethodPost:
			io.WriteString(w, cloudIssueJSON(7, r.URL.Query().Get("peek"), "open", "major"))
		}
	})
	mux.HandleFunc("/2.0/repositories/acme/api/issues/5", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			io.WriteString(w, cloudIssueJSON(5, "Fix bug", "open", "major"))
		case http.MethodPut:
			// Reflect the requested state back so close/reopen tests see it.
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			state, _ := body["state"].(string)
			if state == "" {
				state = "open"
			}
			io.WriteString(w, cloudIssueJSON(5, "Fix bug", state, "major"))
		}
	})
	mux.HandleFunc("/2.0/repositories/acme/api/issues/9999", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/2.0/repositories/acme/api/issues/5/comments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"values":[{"id":1,"content":{"raw":"Note"},"user":{"display_name":"Bob"},"created_on":"2024-01-15T11:00:00Z"}]}`)
	})

	// --- webhooks ---
	mux.HandleFunc("/2.0/repositories/acme/api/hooks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			io.WriteString(w, `{"values":[{"uuid":"{abc-1}","description":"CI","url":"https://hook.example/cb","active":true,"events":["repo:push"]}]}`)
		case http.MethodPost:
			io.WriteString(w, `{"uuid":"{new-1}","description":"CI","url":"https://hook.example/new","active":true,"events":["repo:push"]}`)
		}
	})
	mux.HandleFunc("/2.0/repositories/acme/api/hooks/missing", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	// --- variables ---
	mux.HandleFunc("/2.0/repositories/acme/api/pipelines_config/variables", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			io.WriteString(w, `{"values":[{"uuid":"{11111111-1111-1111-1111-111111111111}","key":"TOKEN","value":"","secured":true},{"uuid":"{22222222-2222-2222-2222-222222222222}","key":"DEBUG","value":"true","secured":false}]}`)
		case http.MethodPost:
			io.WriteString(w, `{"uuid":"{33333333-3333-3333-3333-333333333333}","key":"NEW","value":"x","secured":false}`)
		}
	})
	// Any variable-by-uuid sub-path: update (PUT) / delete (DELETE).
	mux.HandleFunc("/2.0/repositories/acme/api/pipelines_config/variables/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"uuid":"{11111111-1111-1111-1111-111111111111}","key":"TOKEN","value":"y","secured":true}`)
	})

	// --- pipelines ---
	mux.HandleFunc("/2.0/repositories/acme/api/pipelines/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/pipelines/") {
			// pipeline list (used by api paginate)
			io.WriteString(w, `{"values":[{"uuid":"{p1}","build_number":1}],"next":""}`)
			return
		}
		io.WriteString(w, `{"uuid":"{p1}","build_number":42,"state":{"name":"COMPLETED","result":{"name":"SUCCESSFUL"}},"target":{"ref":{"name":"main"}},"created_on":"2024-01-15T10:00:00Z"}`)
	})

	// --- generic raw passthrough targets ---
	mux.HandleFunc("/2.0/user", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"uuid":"{me}","username":"tester","display_name":"Tester"}`)
	})
	mux.HandleFunc("/2.0/repositories/acme/api", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			io.WriteString(w, `{"created":true,"method":"POST"}`)
			return
		}
		io.WriteString(w, `{"slug":"api","name":"api"}`)
	})

	// paginated collection with a next link to a second page.
	mux.HandleFunc("/2.0/paginated", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "2" {
			io.WriteString(w, `{"values":[{"n":3},{"n":4}]}`)
			return
		}
		base := "http://" + r.Host
		io.WriteString(w, fmt.Sprintf(`{"values":[{"n":1},{"n":2}],"next":"%s/2.0/paginated?page=2"}`, base))
	})

	return httptest.NewServer(mux)
}

func cloudIssueJSON(id int, title, state, priority string) string {
	return fmt.Sprintf(`{
		"id": %d, "title": %q, "state": %q, "priority": %q, "kind": "bug",
		"content": {"raw": "Body for issue %d"},
		"assignee": {"display_name": "Ada"}, "reporter": {"display_name": "Bob"},
		"links": {"html": {"href": "https://bitbucket.org/acme/api/issues/%d"}},
		"created_on": "2024-01-15T10:00:00Z", "updated_on": "2024-01-16T10:00:00Z"
	}`, id, title, state, priority, id, id)
}

func TestIssueList_RendersTOON(t *testing.T) {
	srv := startMockCloudPhase3(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"issue", "list"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "issues[2]{id,title,state,priority}:") {
		t.Fatalf("missing list header:\n%s", out)
	}
	if !strings.Contains(out, "  5,Fix bug,open,major") {
		t.Fatalf("row wrong:\n%s", out)
	}
	if !strings.Contains(out, "count: 2") {
		t.Fatalf("missing count:\n%s", out)
	}
}

func TestIssueList_FieldsExtendsSchema(t *testing.T) {
	srv := startMockCloudPhase3(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"issue", "list", "--fields", "assignee,kind"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "issues[2]{id,title,state,priority,assignee,kind}:") {
		t.Fatalf("--fields did not extend schema:\n%s", out)
	}
}

func TestIssueList_FieldsUnknownValue(t *testing.T) {
	srv := startMockCloudPhase3(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"issue", "list", "--fields", "nope"})
	if code != app.ExitUsage {
		t.Fatalf("unknown --fields should exit 2, got %d: %s", code, out)
	}
	if !strings.Contains(out, "unknown --fields value `nope`") {
		t.Fatalf("missing fields error:\n%s", out)
	}
}

func TestIssueView_RendersDetail(t *testing.T) {
	srv := startMockCloudPhase3(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"issue", "view", "5"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "issue:") || !strings.Contains(out, "title: Fix bug") {
		t.Fatalf("missing detail:\n%s", out)
	}
}

func TestIssueCreate_RequiresTitle(t *testing.T) {
	srv := startMockCloudPhase3(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"issue", "create"})
	if code != app.ExitUsage {
		t.Fatalf("missing --title should exit 2, got %d: %s", code, out)
	}
	if !strings.Contains(out, "requires --title") {
		t.Fatalf("missing title error:\n%s", out)
	}
}

func TestIssueClose_IdempotentNoOp(t *testing.T) {
	srv := startMockCloudPhase3(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	// Issue #6 is already resolved in the mock list, but close fetches #6 by id.
	// Override the per-id handler to report a terminal state.
	srv.Config.Handler = idResolvedHandler(srv.Config.Handler, 5)
	out, code := runAppCapture(t, []string{"issue", "close", "5"})
	if code != 0 {
		t.Fatalf("idempotent close should exit 0, got %d: %s", code, out)
	}
	if !strings.Contains(out, "already resolved (no-op)") {
		t.Fatalf("missing no-op message:\n%s", out)
	}
}

func TestIssueClose_ChangesState(t *testing.T) {
	srv := startMockCloudPhase3(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"issue", "close", "5"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "issue:") || !strings.Contains(out, "state: resolved") {
		t.Fatalf("close should show resolved issue:\n%s", out)
	}
}

func TestWebhookList_RendersTOON(t *testing.T) {
	srv := startMockCloudPhase3(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"webhook", "list"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "webhooks[1]{id,url,active,events}:") {
		t.Fatalf("missing list header:\n%s", out)
	}
	if !strings.Contains(out, "abc-1,") || !strings.Contains(out, "repo:push") {
		t.Fatalf("row wrong:\n%s", out)
	}
}

func TestWebhookDelete_Idempotent404(t *testing.T) {
	srv := startMockCloudPhase3(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"webhook", "delete", "missing"})
	if code != 0 {
		t.Fatalf("404 delete should exit 0, got %d: %s", code, out)
	}
	if !strings.Contains(out, "already absent (no-op)") {
		t.Fatalf("missing no-op message:\n%s", out)
	}
}

func TestVariableList_RendersTOON(t *testing.T) {
	srv := startMockCloudPhase3(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"variable", "list"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "variables[2]{key,scope,secured}:") {
		t.Fatalf("missing list header:\n%s", out)
	}
}

func TestVariableSet_CreateThenUpdate(t *testing.T) {
	srv := startMockCloudPhase3(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	// NEW key is absent in the mock list → create path.
	out, code := runAppCapture(t, []string{"variable", "set", "NEW", "--value", "x"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "created repo variable NEW") {
		t.Fatalf("missing created message:\n%s", out)
	}
	// TOKEN key is present → update path.
	out, code = runAppCapture(t, []string{"variable", "set", "TOKEN", "--value", "y", "--secured"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "updated repo variable TOKEN") {
		t.Fatalf("missing updated message:\n%s", out)
	}
}

func TestVariableDelete_IdempotentAbsent(t *testing.T) {
	srv := startMockCloudPhase3(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"variable", "delete", "NOPE"})
	if code != 0 {
		t.Fatalf("absent delete should exit 0, got %d: %s", code, out)
	}
	if !strings.Contains(out, "already absent (no-op)") {
		t.Fatalf("missing no-op message:\n%s", out)
	}
}

func TestAPI_GETPassthrough(t *testing.T) {
	srv := startMockCloudPhase3(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"api", "/user"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "username: tester") {
		t.Fatalf("raw passthrough missing field:\n%s", out)
	}
}

func TestAPI_POSTWithShortFlag(t *testing.T) {
	srv := startMockCloudPhase3(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"api", "/repositories/acme/api", "--method", "POST", "-F", "name=newrepo"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "created: true") || !strings.Contains(out, "method: POST") {
		t.Fatalf("POST passthrough wrong:\n%s", out)
	}
}

func TestAPI_PaginateAccumulates(t *testing.T) {
	srv := startMockCloudPhase3(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"api", "/paginated", "--paginate"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "values[4]") {
		t.Fatalf("paginate did not accumulate 4 values:\n%s", out)
	}
}

func TestPRReviewerList_RendersTOON(t *testing.T) {
	srv := startMockCloud(t) // Phase 0 mock has the PR with a reviewer participant
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "reviewer", "list", "42"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "reviewers[1]{name,state,approved}:") {
		t.Fatalf("missing reviewer header:\n%s", out)
	}
}

func TestPRChecks_RendersStatuses(t *testing.T) {
	srv := startMockCloudChecks(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pr", "checks", "42"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "statuses[1]{state,key,name,url}:") {
		t.Fatalf("missing checks header:\n%s", out)
	}
}

// idResolvedHandler wraps h so that GET /2.0/repositories/acme/api/issues/5
// reports state "resolved" (for the idempotent-close test).
func idResolvedHandler(h http.Handler, issueID int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == fmt.Sprintf("/2.0/repositories/acme/api/issues/%d", issueID) {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, cloudIssueJSON(issueID, "Fix bug", "resolved", "major"))
			return
		}
		h.ServeHTTP(w, r)
	})
}

// startMockCloudChecks adds commit-status endpoints to the Phase 0 PR mock so
// `pr checks` resolves a head commit and its statuses.
func startMockCloudChecks(t *testing.T) *httptest.Server {
	t.Helper()
	base := startMockCloud(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// commit statuses for the head commit hash "abc"
		if r.URL.Path == "/2.0/repositories/acme/api/commit/abc/statuses" {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"values":[{"state":"SUCCESSFUL","key":"ci","name":"build","url":"https://ci.example/1","description":"ok"}]}`)
			return
		}
		// delegate everything else (PR view) to the Phase 0 server's transport
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/2.0/repositories/acme/api/pullrequests/42" {
			io.WriteString(w, cloudPRJSON(42, "Single PR view", "OPEN", false))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	// Re-point the existing server at the richer mux.
	base.Config.Handler = mux
	return base
}
