package axi

import (
	"strings"
	"testing"
)

func TestRenderList_Default(t *testing.T) {
	items := []any{
		map[string]any{"id": 1043, "title": "Fix token refresh on expiry", "state": "OPEN", "approved": true},
		map[string]any{"id": 1041, "title": "Add pagination", "state": "OPEN", "approved": false},
	}
	schema := []Field{
		{Key: "id", Extractor: Pluck("id")},
		{Key: "title", Extractor: Pluck("title")},
		{Key: "state", Extractor: Lower(Pluck("state"))},
		{Key: "review", Extractor: MapEnum(BoolYesNo(Pluck("approved")), map[string]string{"yes": "approved", "no": "required"})},
	}
	got := RenderList("pull_requests", items, schema)
	want := `pull_requests[2]{id,title,state,review}:
  1043,Fix token refresh on expiry,open,approved
  1041,Add pagination,open,required`
	if got != want {
		t.Fatalf("RenderList mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestMarshal_FullDoc_CountListHelp(t *testing.T) {
	items := []any{map[string]any{"id": 1, "state": "open"}}
	schema := []Field{{Key: "id", Extractor: Pluck("id")}, {Key: "state", Extractor: Lower(Pluck("state"))}}
	doc := NewObject(
		KV{Key: "count", Value: "1 of 47 total"},
		KV{Key: "pull_requests", Value: Rows(items, schema)},
		KV{Key: "help", Value: HelpRows([]string{"Run `bkt-axi pr view <id>` for details"})},
	)
	got := Marshal(doc)
	if !strings.Contains(got, "count: 1 of 47 total") {
		t.Fatalf("missing count line:\n%s", got)
	}
	if !strings.Contains(got, "pull_requests[1]{id,state}:") {
		t.Fatalf("missing array header:\n%s", got)
	}
	if !strings.Contains(got, "help[1]{step}:") {
		t.Fatalf("missing help header:\n%s", got)
	}
	if !strings.Contains(got, "  Run `bkt-axi pr view <id>` for details") {
		t.Fatalf("help hint not on its own line:\n%s", got)
	}
}

func TestRenderHelp_NoLengthMarkerPrefix(t *testing.T) {
	got := RenderHelp([]string{"a", "b", "c"})
	if !strings.HasPrefix(got, "help[3]{step}:") {
		t.Fatalf("expected bare [3] length marker, got:\n%s", got)
	}
}

func TestRenderError_WithSuggestions(t *testing.T) {
	e := UsageError("unknown flag --stat for `pr list`", "--state", "--mine", "--reviewer")
	got := RenderError(e)
	if !strings.HasPrefix(got, "error: unknown flag --stat") {
		t.Fatalf("missing error line:\n%s", got)
	}
	if !strings.Contains(got, "help[1]{step}:") {
		t.Fatalf("missing help block:\n%s", got)
	}
}

func TestTruncateBody(t *testing.T) {
	long := strings.Repeat("x", 600)
	got := TruncateBody(long, DefaultBodyBudget)
	if !strings.Contains(got, "...(truncated, 600 chars total)") {
		t.Fatalf("expected truncation note, got: %q", got)
	}
	// preview budget enforced
	preview := strings.TrimSpace(strings.Split(got, "\n")[0])
	if len(preview) > DefaultBodyBudget+5 {
		t.Fatalf("preview exceeded budget: %d", len(preview))
	}

	short := "hello"
	if got := TruncateBody(short, DefaultBodyBudget); got != short {
		t.Fatalf("short body should be unchanged, got %q", got)
	}
	if got := TruncateBody("", DefaultBodyBudget); got != "" {
		t.Fatalf("empty body should stay empty, got %q", got)
	}
}

func TestExitCodes(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{nil, ExitSuccess},
		{Errorf("boom"), ExitError},
		{UsageError("bad flag"), ExitUsage},
		{NoOp("already done"), ExitSuccess},
	}
	for _, c := range cases {
		if got := ExitCode(c.err); got != c.want {
			t.Fatalf("ExitCode(%v) = %d, want %d", c.err, got, c.want)
		}
	}
}

func TestMapHTTPError_ConflictIdempotent(t *testing.T) {
	e := MapHTTPError(409, "409 Conflict: already approved", "pull request #42", true)
	if e.Exit != ExitSuccess {
		t.Fatalf("idempotent 409 should be exit 0 (no-op), got %d", e.Exit)
	}
	if e.Code != "NOOP" {
		t.Fatalf("expected NOOP code, got %s", e.Code)
	}

	e2 := MapHTTPError(404, "404 Not Found", "pull request #99", false)
	if e2.Code != CodeNotFound || e2.Exit != ExitError {
		t.Fatalf("404 should map to NOT_FOUND/exit1: %+v", e2)
	}

	e3 := MapHTTPError(401, "401 Unauthorized", "host", false)
	if e3.Code != CodeAuthRequired {
		t.Fatalf("401 should map to AUTH_REQUIRED: %+v", e3)
	}
}
