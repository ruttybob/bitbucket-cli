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

// pipeline_integration_test.go proves the `pipeline` Phase 1 commands end to end
// (Cloud only): list, view (with steps/logs), log truncation + --full temp file,
// and the clear Cloud-only error when run against a Data Center host.

// Valid UUIDs so the Cloud client's UUID normalization (which rejects
// non-canonical UUIDs) accepts them. Braces are URL-encoded (%7B/%7D) in paths.
const (
	pipe42UUID    = "11111111-1111-1111-1111-111111111111"
	pipe41UUID    = "22222222-2222-2222-2222-222222222222"
	stepBuildUUID = "33333333-3333-3333-3333-333333333333"
	stepTestUUID  = "44444444-4444-4444-4444-444444444444"
)

func pipelineJSON(build int, uuid, stateName, result, ref string) string {
	return fmt.Sprintf(`{
		"uuid": "{%s}",
		"build_number": %d,
		"state": {"name": %q, "result": {"name": %q}},
		"target": {"type": "pipeline_ref_target", "ref": {"name": %q}},
		"trigger": {"type": "manual"},
		"created_on": "2024-01-15T10:00:00Z",
		"completed_on": "2024-01-15T10:01:30Z"
	}`, uuid, build, stateName, result, ref)
}

func startMockCloudPipelines(t *testing.T) *httptest.Server {
	t.Helper()
	stepJSON := fmt.Sprintf(`{"values":[{"uuid":"{%s}","name":"build","state":{"name":"COMPLETED","result":{"name":"SUCCESSFUL"}}},{"uuid":"{%s}","name":"test","state":{"name":"COMPLETED","result":{"name":"FAILED"}}}]}`, stepBuildUUID, stepTestUUID)
	// Brace-wrapped UUIDs as they appear in the decoded request path.
	pipeStepsPath := "/pipelines/{" + pipe42UUID + "}/steps"
	buildLogPath := "/pipelines/{" + pipe42UUID + "}/steps/{" + stepBuildUUID + "}/log"
	testLogPath := "/pipelines/{" + pipe42UUID + "}/steps/{" + stepTestUUID + "}/log"

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// The Cloud client appends trailing slashes to pipeline/steps paths
		// (/pipelines/?…, /…/steps/?…); normalize them away for matching.
		path := strings.TrimRight(r.URL.Path, "/")
		switch {
		case path == "/2.0/repositories/acme/api/pipelines":
			page := map[string]any{
				"values": []any{
					json.RawMessage(pipelineJSON(42, pipe42UUID, "COMPLETED", "SUCCESSFUL", "main")),
					json.RawMessage(pipelineJSON(41, pipe41UUID, "COMPLETED", "FAILED", "feature/x")),
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(page)

		case path == "/2.0/repositories/acme/api/pipelines/42":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, pipelineJSON(42, pipe42UUID, "COMPLETED", "SUCCESSFUL", "main"))

		case strings.HasSuffix(path, pipeStepsPath):
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, stepJSON)

		case strings.HasSuffix(path, buildLogPath):
			w.Header().Set("Content-Type", "application/octet-stream")
			io.WriteString(w, "build log line one\nbuild log line two\n")

		case strings.HasSuffix(path, testLogPath):
			w.Header().Set("Content-Type", "application/octet-stream")
			io.WriteString(w, "test log: failure occurred\n")

		default:
			http.NotFound(w, r)
		}
	})
	return httptest.NewServer(mux)
}

func TestPipelineList_RendersTOON(t *testing.T) {
	srv := startMockCloudPipelines(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pipeline", "list"})
	if code != 0 {
		t.Fatalf("pipeline list exit %d: %s", code, out)
	}
	if !strings.Contains(out, "pipelines[2]{build,state,ref,created}:") {
		t.Fatalf("missing list header:\n%s", out)
	}
	if !strings.Contains(out, "  42,COMPLETED,main,") {
		t.Fatalf("pipeline row wrong:\n%s", out)
	}
	if !strings.Contains(out, "count: 2") {
		t.Fatalf("missing count:\n%s", out)
	}
	if !strings.Contains(out, "help[1]{step}:") {
		t.Fatalf("missing help block:\n%s", out)
	}
}

func TestPipelineList_FieldsDuration(t *testing.T) {
	srv := startMockCloudPipelines(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pipeline", "list", "--fields", "trigger,duration"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "pipelines[2]{build,state,ref,created,trigger,duration}:") {
		t.Fatalf("--fields did not extend schema:\n%s", out)
	}
	if !strings.Contains(out, "manual") || !strings.Contains(out, "1m30s") {
		t.Fatalf("trigger/duration columns missing:\n%s", out)
	}
}

func TestPipelineList_StepsSummary(t *testing.T) {
	srv := startMockCloudPipelines(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pipeline", "list", "--fields", "steps"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "steps}:") || !strings.Contains(out, "2 (1 failed)") {
		t.Fatalf("steps summary column missing:\n%s", out)
	}
}

func TestPipelineView_DetailWithSteps(t *testing.T) {
	srv := startMockCloudPipelines(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pipeline", "view", "42", "--steps"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "pipeline:") || !strings.Contains(out, "build: 42") {
		t.Fatalf("missing detail object:\n%s", out)
	}
	if !strings.Contains(out, "state: COMPLETED") || !strings.Contains(out, "result: SUCCESSFUL") {
		t.Fatalf("missing state/result:\n%s", out)
	}
	if !strings.Contains(out, "steps[2]{name,state,result,uuid}:") {
		t.Fatalf("missing steps block:\n%s", out)
	}
}

func TestPipelineView_LogsConcatenated(t *testing.T) {
	srv := startMockCloudPipelines(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pipeline", "view", "42", "--logs"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "build log line one") || !strings.Contains(out, "test log: failure occurred") {
		t.Fatalf("concatenated logs missing:\n%s", out)
	}
}

func TestPipelineView_LogFailedOnly(t *testing.T) {
	srv := startMockCloudPipelines(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"pipeline", "view", "42", "--log-failed"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "test log: failure occurred") {
		t.Fatalf("failed step log missing:\n%s", out)
	}
	if strings.Contains(out, "build log line one") {
		t.Fatalf("--log-failed should exclude the successful step:\n%s", out)
	}
}

// writeTestConfigDC builds an isolated config pointing at a Data Center host, so
// Cloud-only commands can be tested against a DC host for the platform-error path.
func writeTestConfigDC(t *testing.T, srv string) {
	t.Helper()
	dir := t.TempDir()
	cfg := fmt.Sprintf(`version: 1
active_context: test
contexts:
  test:
    host: testdc
    project_key: ACME
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
}

func TestPipelineList_CloudOnlyAgainstDC(t *testing.T) {
	// A DC host needs a base URL but no endpoints are hit: the Cloud-only guard
	// fires before any request.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	writeTestConfigDC(t, srv.URL)

	out, code := runAppCapture(t, []string{"pipeline", "list"})
	if code != app.ExitError {
		t.Fatalf("Cloud-only command against DC should exit 1, got %d: %s", code, out)
	}
	if !strings.Contains(out, "pipelines is Bitbucket Cloud only") {
		t.Fatalf("missing Cloud-only error:\n%s", out)
	}
	if !strings.Contains(out, "Bitbucket Data Center") {
		t.Fatalf("error should name the active host kind:\n%s", out)
	}
}

func TestPipelineView_CloudOnlyAgainstDC(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	writeTestConfigDC(t, srv.URL)

	out, code := runAppCapture(t, []string{"pipeline", "view", "42"})
	if code != app.ExitError {
		t.Fatalf("Cloud-only command against DC should exit 1, got %d: %s", code, out)
	}
	if !strings.Contains(out, "pipelines is Bitbucket Cloud only") {
		t.Fatalf("missing Cloud-only error:\n%s", out)
	}
}

// TestPipelineView_LogsTruncatedAndFull exercises the large-content truncation
// contract for pipeline logs: tail-truncated preview by default, and a temp
// file path + untruncated output with --full.
func TestPipelineView_LogsTruncatedAndFull(t *testing.T) {
	// A single failed step whose log exceeds the 20000-char tail budget.
	bigLog := strings.Repeat("log line of moderate length\n", 1000) // ~27k chars
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimRight(r.URL.Path, "/")
		switch {
		case path == "/2.0/repositories/acme/api/pipelines/42":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, pipelineJSON(42, pipe42UUID, "COMPLETED", "FAILED", "main"))
		case strings.HasSuffix(path, "/pipelines/{"+pipe42UUID+"}/steps"):
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, fmt.Sprintf(`{"values":[{"uuid":"{%s}","name":"test","state":{"name":"COMPLETED","result":{"name":"FAILED"}}}]}`, stepTestUUID))
		case strings.HasSuffix(path, "/pipelines/{"+pipe42UUID+"}/steps/{"+stepTestUUID+"}/log"):
			w.Header().Set("Content-Type", "application/octet-stream")
			io.WriteString(w, bigLog)
		default:
			http.NotFound(w, r)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	// Default --logs: tail-truncated, no temp file, --full hint present.
	out, code := runAppCapture(t, []string{"pipeline", "view", "42", "--logs"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "...(truncated") {
		t.Fatalf("expected log tail truncation:\n%s", out)
	}
	if !strings.Contains(out, "--full") {
		t.Fatalf("missing --full hint:\n%s", out)
	}
	if strings.Contains(out, "full_path") {
		t.Fatalf("default --logs should not write a temp file:\n%s", out)
	}

	// --full: untruncated, temp file path emitted.
	out, code = runAppCapture(t, []string{"pipeline", "view", "42", "--logs", "--full"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "full_path:") {
		t.Fatalf("--full should emit full_path:\n%s", out)
	}
	if strings.Contains(out, "...(truncated") {
		t.Fatalf("--full should not truncate:\n%s", out)
	}
}
