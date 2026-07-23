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

// repo_integration_test.go proves the `repo` Phase 1 commands end to end: a
// mock Bitbucket Cloud API → unified client → normalized adapter → TOON render.

func cloudRepoJSON(slug, name, scm string, private bool) string {
	return fmt.Sprintf(`{
		"uuid": "{%s-uuid}",
		"name": %q,
		"slug": %q,
		"scm": %q,
		"is_private": %t,
		"updated_on": "2024-01-15T10:00:00Z",
		"links": {"html": {"href": "https://bitbucket.org/acme/%s"}, "clone": [{"name": "https", "href": "https://bitbucket.org/acme/%s.git"}, {"name": "ssh", "href": "git@bitbucket.org:acme/%s.git"}]},
		"workspace": {"slug": "acme"},
		"project": {"key": "ENG"},
		"mainbranch": {"name": "main"}
	}`, slug, name, slug, scm, private, slug, slug, slug)
}

func startMockCloudRepos(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/2.0/repositories/acme", func(w http.ResponseWriter, r *http.Request) {
		page := map[string]any{
			"values": []any{
				json.RawMessage(cloudRepoJSON("api", "API Service", "git", true)),
				json.RawMessage(cloudRepoJSON("web", "Web Frontend", "git", false)),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(page)
	})
	mux.HandleFunc("/2.0/repositories/acme/api", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, cloudRepoJSON("api", "API Service", "git", true))
	})
	return httptest.NewServer(mux)
}

func TestRepoList_RendersTOON(t *testing.T) {
	srv := startMockCloudRepos(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"repo", "list"})
	if code != 0 {
		t.Fatalf("repo list exit %d: %s", code, out)
	}
	if !strings.Contains(out, "repositories[2]{slug,name,scm}:") {
		t.Fatalf("missing list header:\n%s", out)
	}
	if !strings.Contains(out, "  api,API Service,git") {
		t.Fatalf("repo row wrong:\n%s", out)
	}
	if !strings.Contains(out, "count: 2") {
		t.Fatalf("missing count:\n%s", out)
	}
	if !strings.Contains(out, "help[1]{step}:") {
		t.Fatalf("missing help block:\n%s", out)
	}
}

func TestRepoList_FieldsExtendsSchema(t *testing.T) {
	srv := startMockCloudRepos(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"repo", "list", "--fields", "visibility,default_branch"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "repositories[2]{slug,name,scm,visibility,default_branch}:") {
		t.Fatalf("--fields did not extend schema:\n%s", out)
	}
	if !strings.Contains(out, "private") || !strings.Contains(out, "main") {
		t.Fatalf("extra columns missing:\n%s", out)
	}
}

func TestRepoList_FieldsUnknownValue(t *testing.T) {
	srv := startMockCloudRepos(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"repo", "list", "--fields", "bogus"})
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

func TestRepoView_RendersDetail(t *testing.T) {
	srv := startMockCloudRepos(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"repo", "view", "api"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "repository:") {
		t.Fatalf("missing detail object:\n%s", out)
	}
	if !strings.Contains(out, "slug: api") || !strings.Contains(out, "name: API Service") {
		t.Fatalf("missing identity fields:\n%s", out)
	}
	if !strings.Contains(out, "visibility: private") {
		t.Fatalf("missing visibility:\n%s", out)
	}
	if !strings.Contains(out, "https://bitbucket.org/acme/api.git") {
		t.Fatalf("missing clone https:\n%s", out)
	}
	if !strings.Contains(out, "git@bitbucket.org:acme/api.git") {
		t.Fatalf("missing clone ssh:\n%s", out)
	}
}

func TestRepoView_WebPrintsURL(t *testing.T) {
	srv := startMockCloudRepos(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"repo", "view", "api", "--web"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "url: ") || !strings.Contains(out, "https://bitbucket.org/acme/api") {
		t.Fatalf("--web should print the URL:\n%s", out)
	}
}
