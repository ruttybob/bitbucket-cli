package commands

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruttybob/bkt-axi/internal/app"
)

// repo_branch_integration_test.go covers the Phase 2 repo create verb (Cloud)
// and the DC branch create/delete verbs, plus the DC-only guard that rejects
// branch mutations on Cloud.

// --- repo create (Cloud) -------------------------------------------------

func TestRepoCreate_ConfirmsDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/2.0/repositories/acme/newrepo" && req.Method == http.MethodPost {
			var body map[string]any
			_ = json.NewDecoder(req.Body).Decode(&body)
			if body["is_private"] != true {
				t.Errorf("expected private repo by default, got %v", body["is_private"])
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"slug":"newrepo","name":"newrepo","scm":"git","is_private":true,"mainbranch":{"name":"main"},"links":{"html":{"href":"https://bitbucket.org/acme/newrepo"},"clone":[{"href":"https://bitbucket.org/acme/newrepo.git","name":"https"},{"href":"git@bitbucket.org:acme/newrepo.git","name":"ssh"}]},"project":{"key":"ENG"}}`)
			return
		}
		http.NotFound(w, req)
	}))
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"repo", "create", "newrepo", "--cloud-project", "ENG"})
	if code != app.ExitSuccess {
		t.Fatalf("repo create should exit 0, got %d: %s", code, out)
	}
	if !strings.Contains(out, "repository:") {
		t.Fatalf("missing repository detail:\n%s", out)
	}
	if !strings.Contains(out, "slug: newrepo") || !strings.Contains(out, "project: ENG") {
		t.Fatalf("missing repo fields:\n%s", out)
	}
	if !strings.Contains(out, "bitbucket.org/acme/newrepo") {
		t.Fatalf("missing url:\n%s", out)
	}
}

func TestRepoCreate_PublicFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/2.0/repositories/acme/pub" && req.Method == http.MethodPost {
			var body map[string]any
			_ = json.NewDecoder(req.Body).Decode(&body)
			if body["is_private"] != false {
				t.Errorf("--public should set is_private=false, got %v", body["is_private"])
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"slug":"pub","name":"pub","scm":"git","mainbranch":{"name":"main"},"links":{"html":{"href":"https://bitbucket.org/acme/pub"}}}`)
			return
		}
		http.NotFound(w, req)
	}))
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"repo", "create", "pub", "--public"})
	if code != app.ExitSuccess {
		t.Fatalf("repo create --public should exit 0, got %d: %s", code, out)
	}
}

// --- branch create/delete (DC) -------------------------------------------

func TestBranchCreate_ConfirmsName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.URL.Path == "/rest/api/1.0/projects/PROJ/repos/api" && req.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"slug":"api","name":"api","defaultBranch":"main","project":{"key":"PROJ"},"links":{}}`)
		case req.URL.Path == "/rest/branch-utils/1.0/projects/PROJ/repos/api/branches" && req.Method == http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(req.Body).Decode(&body)
			if body["name"] != "refs/heads/feature/y" {
				t.Errorf("unexpected branch name %v", body["name"])
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"id":"refs/heads/feature/y","displayId":"feature/y","type":"BRANCH"}`)
		default:
			http.NotFound(w, req)
		}
	}))
	defer srv.Close()
	writeTestDCConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"branch", "create", "feature/y"})
	if code != app.ExitSuccess {
		t.Fatalf("branch create should exit 0, got %d: %s", code, out)
	}
	if !strings.Contains(out, "branch:") || !strings.Contains(out, "name: feature/y") {
		t.Fatalf("missing branch confirm:\n%s", out)
	}
}

func TestBranchCreate_FromStartPoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.URL.Path == "/rest/branch-utils/1.0/projects/PROJ/repos/api/branches" && req.Method == http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(req.Body).Decode(&body)
			if body["startPoint"] != "refs/heads/release-1.2" {
				t.Errorf("expected startPoint refs/heads/release-1.2, got %v", body["startPoint"])
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"displayId":"hotfix/z"}`)
		default:
			http.NotFound(w, req)
		}
	}))
	defer srv.Close()
	writeTestDCConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"branch", "create", "hotfix/z", "--from", "release-1.2"})
	if code != app.ExitSuccess {
		t.Fatalf("branch create --from should exit 0, got %d: %s", code, out)
	}
	if !strings.Contains(out, "name: hotfix/z") {
		t.Fatalf("missing branch confirm:\n%s", out)
	}
}

func TestBranchDelete_DryRunConfirms(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/rest/branch-utils/1.0/projects/PROJ/repos/api/branches" && req.Method == http.MethodDelete {
			var body map[string]any
			_ = json.NewDecoder(req.Body).Decode(&body)
			if body["dryRun"] != true {
				t.Errorf("expected dryRun=true, got %v", body["dryRun"])
			}
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, `{"deletable":true}`)
			return
		}
		http.NotFound(w, req)
	}))
	defer srv.Close()
	writeTestDCConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"branch", "delete", "feature/old", "--dry-run"})
	if code != app.ExitSuccess {
		t.Fatalf("branch delete --dry-run should exit 0, got %d: %s", code, out)
	}
	if !strings.Contains(out, "state: deletable (dry-run)") {
		t.Fatalf("missing dry-run confirm:\n%s", out)
	}
}

// --- DC-only guard -------------------------------------------------------

func TestBranchCreate_RejectedOnCloud(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.NotFound(w, req)
	}))
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"branch", "create", "x"})
	if code != app.ExitError {
		t.Fatalf("branch create on Cloud should exit 1, got %d: %s", code, out)
	}
	if !strings.Contains(out, "Data Center") {
		t.Fatalf("missing DC-only error:\n%s", out)
	}
}

// --- repo clone (URL resolution + clean git-error surfacing) -------------

func TestRepoClone_ResolvesURLThenSurfacesGitError(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// GET repository → advertise a clone link pointing at the mock (which is
		// not a real git server, so the subsequent `git clone` fails cleanly).
		if req.URL.Path == "/2.0/repositories/acme/api" && req.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"slug":"api","name":"api","links":{"clone":[{"href":"`+srv.URL+`/git/api.git","name":"https"}],"html":{"href":"`+srv.URL+`/api"}}}`)
			return
		}
		http.NotFound(w, req)
	}))
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	dest := filepath.Join(t.TempDir(), "api-clone")
	out, code := runAppCapture(t, []string{"repo", "clone", "api", "--dest", dest})
	if code != app.ExitError {
		t.Fatalf("clone of a non-git URL should exit 1, got %d: %s", code, out)
	}
	if !strings.Contains(out, "clone") {
		t.Fatalf("missing clone error:\n%s", out)
	}
}
