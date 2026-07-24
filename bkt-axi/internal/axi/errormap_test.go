package axi

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// TestMapError walks every status the translation table must cover for both
// Cloud and Data Center, asserting the machine code, exit code, and that the
// message is a clean actionable sentence (no raw upstream status prefix leaks).
func TestMapError(t *testing.T) {
	cases := []struct {
		name      string
		ctx       ErrorContext
		wantCode  string
		wantExit  int
		wantInMsg string // substring expected in Message (case-insensitive)
	}{
		{
			name: "404 pr", ctx: ErrorContext{Status: 404, Noun: "pull request #9999", Upstream: "404 Not Found: PR missing"},
			wantCode: CodeNotFound, wantExit: ExitError, wantInMsg: "pull request #9999 not found",
		},
		{
			name: "404 generic resource", ctx: ErrorContext{Status: 404, Noun: "repository acme/api-gateway"},
			wantCode: CodeNotFound, wantExit: ExitError, wantInMsg: "repository acme/api-gateway not found",
		},
		{
			name: "404 blank noun defaults to resource", ctx: ErrorContext{Status: 404, Noun: ""},
			wantCode: CodeNotFound, wantExit: ExitError, wantInMsg: "resource not found",
		},
		{
			name: "401 cloud", ctx: ErrorContext{Status: 401, Noun: "pull request #42", HostKind: "cloud"},
			wantCode: CodeAuthRequired, wantExit: ExitError, wantInMsg: "authentication required",
		},
		{
			name: "401 dc", ctx: ErrorContext{Status: 401, Noun: "branch", HostKind: "dc"},
			wantCode: CodeAuthRequired, wantExit: ExitError, wantInMsg: "authentication required",
		},
		{
			name: "403 cloud scope hint", ctx: ErrorContext{Status: 403, Noun: "pull request #42", HostKind: "cloud"},
			wantCode: CodeForbidden, wantExit: ExitError, wantInMsg: "permission denied",
		},
		{
			name: "403 generic", ctx: ErrorContext{Status: 403, Noun: "webhook"},
			wantCode: CodeForbidden, wantExit: ExitError, wantInMsg: "permission denied",
		},
		{
			name: "409 idempotent is no-op exit0", ctx: ErrorContext{Status: 409, Noun: "pull request #7 approval", Idempotent: true},
			wantCode: CodeIdempotentNoop, wantExit: ExitSuccess, wantInMsg: "already in the requested state",
		},
		{
			name: "409 stale-version is conflict-with-suggestion", ctx: ErrorContext{Status: 409, Noun: "pull request #7", StaleVersion: true},
			wantCode: CodeConflictWithSuggestion, wantExit: ExitError, wantInMsg: "changed since you last read it",
		},
		{
			name: "409 plain conflict", ctx: ErrorContext{Status: 409, Noun: "branch main", Upstream: "409 Conflict: already exists"},
			wantCode: CodeConflict, wantExit: ExitError, wantInMsg: "already exists",
		},
		{
			name: "422 validation strips status prefix", ctx: ErrorContext{Status: 422, Noun: "pull request", Upstream: "422 Unprocessable Entity: title required"},
			wantCode: CodeValidation, wantExit: ExitError, wantInMsg: "title required",
		},
		{
			name: "429 with retry-after", ctx: ErrorContext{Status: 429, Noun: "pipelines", RetryAfter: 42 * time.Second},
			wantCode: CodeRateLimited, wantExit: ExitError, wantInMsg: "retry in 42s",
		},
		{
			name: "429 without retry-after", ctx: ErrorContext{Status: 429, Noun: "pipelines"},
			wantCode: CodeRateLimited, wantExit: ExitError, wantInMsg: "wait and retry",
		},
		{
			name: "500 unavailable", ctx: ErrorContext{Status: 500, Noun: "repositories"},
			wantCode: CodeUnavailable, wantExit: ExitError, wantInMsg: "unavailable",
		},
		{
			name: "502 unavailable", ctx: ErrorContext{Status: 502, Noun: "repositories"},
			wantCode: CodeUnavailable, wantExit: ExitError, wantInMsg: "unavailable",
		},
		{
			name: "503 unavailable", ctx: ErrorContext{Status: 503, Noun: "repositories"},
			wantCode: CodeUnavailable, wantExit: ExitError, wantInMsg: "unavailable",
		},
		{
			name: "unknown status falls through", ctx: ErrorContext{Status: 418, Noun: "repo", Upstream: "418 I'm a teapot"},
			wantCode: CodeUnknown, wantExit: ExitError, wantInMsg: "failed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := MapError(tc.ctx)
			if e.Code != tc.wantCode {
				t.Errorf("code: got %q want %q", e.Code, tc.wantCode)
			}
			if e.Exit != tc.wantExit {
				t.Errorf("exit: got %d want %d", e.Exit, tc.wantExit)
			}
			if !strings.Contains(strings.ToLower(e.Message), strings.ToLower(tc.wantInMsg)) {
				t.Errorf("message %q missing %q", e.Message, tc.wantInMsg)
			}
			// No upstream status prefix may leak into the agent-facing message.
			if strings.Contains(e.Message, "404 Not Found:") || strings.Contains(e.Message, "422 Unprocessable Entity:") || strings.Contains(e.Message, "418 I'm a teapot:") {
				t.Errorf("message leaked upstream status prefix: %q", e.Message)
			}
		})
	}
}

// TestMapErrorHints asserts the self-correcting hints are present where the spec
// requires them (login instructions, scope hints, retry countdown, stale-version
// retry), and absent on bare validation/unknown.
func TestMapErrorHints(t *testing.T) {
	t.Run("401 cloud login instructions", func(t *testing.T) {
		e := MapError(ErrorContext{Status: 401, Noun: "pull request #1", HostKind: "cloud"})
		if !hintContains(e, "auth login --kind cloud --web") {
			t.Errorf("missing cloud login hint: %v", e.Suggestions)
		}
	})
	t.Run("401 dc login instructions", func(t *testing.T) {
		e := MapError(ErrorContext{Status: 401, Noun: "branch", HostKind: "dc"})
		if !hintContains(e, "auth login <") {
			t.Errorf("missing dc login hint: %v", e.Suggestions)
		}
	})
	t.Run("403 cloud scope hint", func(t *testing.T) {
		e := MapError(ErrorContext{Status: 403, Noun: "repo", HostKind: "cloud"})
		if !hintContains(e, "scope") {
			t.Errorf("missing scope hint: %v", e.Suggestions)
		}
	})
	t.Run("429 retry-after hint", func(t *testing.T) {
		e := MapError(ErrorContext{Status: 429, Noun: "pipelines", RetryAfter: 90 * time.Second})
		if !hintContains(e, "1m30s") {
			t.Errorf("missing retry-after countdown: %v", e.Suggestions)
		}
	})
	t.Run("409 stale-version retry hint", func(t *testing.T) {
		e := MapError(ErrorContext{Status: 409, Noun: "pull request #3", StaleVersion: true})
		if !hintContains(e, "fresh version") && !hintContains(e, "Re-run") {
			t.Errorf("missing retry hint: %v", e.Suggestions)
		}
	})
	t.Run("404 pr discovery hint", func(t *testing.T) {
		e := MapError(ErrorContext{Status: 404, Noun: "pull request #9999"})
		if !hintContains(e, "bkt-axi pr list") {
			t.Errorf("missing pr list hint: %v", e.Suggestions)
		}
	})
	t.Run("404 non-pr noun uses generic discovery", func(t *testing.T) {
		e := MapError(ErrorContext{Status: 404, Noun: "repository acme/api-gateway"})
		// Must NOT resolve to the pr-only hint; should offer repo discovery.
		if hintContains(e, "to see available pull requests") {
			t.Errorf("non-pr noun got pr-only hint: %v", e.Suggestions)
		}
		if !hintContains(e, "repo list") {
			t.Errorf("missing repo discovery hint: %v", e.Suggestions)
		}
	})
	t.Run("validation has no hints", func(t *testing.T) {
		e := MapError(ErrorContext{Status: 422, Noun: "pull request", Upstream: "bad"})
		if len(e.Suggestions) != 0 {
			t.Errorf("validation should have no hints: %v", e.Suggestions)
		}
	})
}

func hintContains(e *AxiError, want string) bool {
	for _, s := range e.Suggestions {
		if strings.Contains(s, want) {
			return true
		}
	}
	return false
}

// TestMapTransportError covers the network-error translation: timeouts and
// unreachable hosts both map to NETWORK_ERROR with a retry/connectivity hint.
func TestMapTransportError(t *testing.T) {
	t.Run("nil is nil", func(t *testing.T) {
		if e := MapTransportError(nil, "pull request #1"); e != nil {
			t.Errorf("nil err should yield nil, got %+v", e)
		}
	})
	t.Run("context deadline is timeout", func(t *testing.T) {
		e := MapTransportError(context.DeadlineExceeded, "pull request #1")
		if e.Code != CodeNetworkError {
			t.Errorf("code: got %q want %q", e.Code, CodeNetworkError)
		}
		if !strings.Contains(e.Message, "timed out") {
			t.Errorf("message should mention timeout: %q", e.Message)
		}
		if !hintContains(e, "timeout") {
			t.Errorf("missing timeout hint: %v", e.Suggestions)
		}
	})
	t.Run("net timeout error", func(t *testing.T) {
		// A synthetic net.Error with Timeout()==true.
		err := &timeoutErr{msg: "dial tcp: i/o timeout"}
		e := MapTransportError(err, "repo")
		if e.Code != CodeNetworkError || !strings.Contains(e.Message, "timed out") {
			t.Errorf("expected timeout mapping, got code=%q msg=%q", e.Code, e.Message)
		}
	})
	t.Run("connection refused is unreachable", func(t *testing.T) {
		err := errors.New("dial tcp 1.2.3.4:443: connect: connection refused")
		e := MapTransportError(err, "repo")
		if e.Code != CodeNetworkError {
			t.Errorf("code: got %q want %q", e.Code, CodeNetworkError)
		}
		if !strings.Contains(e.Message, "could not reach") {
			t.Errorf("message should mention unreachable: %q", e.Message)
		}
		if !hintContains(e, "connectivity") {
			t.Errorf("missing connectivity hint: %v", e.Suggestions)
		}
	})
}

// timeoutErr is a minimal net.Error stub for testing the timeout branch.
type timeoutErr struct{ msg string }

func (t *timeoutErr) Error() string   { return t.msg }
func (t *timeoutErr) Timeout() bool   { return true }
func (t *timeoutErr) Temporary() bool { return false }

// TestCodeForStatus keeps the classifier consistent with MapError for the
// status codes callers branch on without a full message.
func TestCodeForStatus(t *testing.T) {
	want := map[int]string{
		404: CodeNotFound, 401: CodeAuthRequired, 403: CodeForbidden,
		409: CodeConflict, 422: CodeValidation, 429: CodeRateLimited,
		500: CodeUnavailable, 503: CodeUnavailable, 418: CodeUnknown,
	}
	for status, code := range want {
		if got := CodeForStatus(status); got != code {
			t.Errorf("CodeForStatus(%d): got %q want %q", status, got, code)
		}
	}
}

// TestMapHTTPErrorLegacy confirms the legacy entry point still routes through
// the comprehensive table (api passthrough depends on it).
func TestMapHTTPErrorLegacy(t *testing.T) {
	e := MapHTTPError(404, "404 Not Found: nope", "api request", false)
	if e.Code != CodeNotFound {
		t.Fatalf("code: got %q want %q", e.Code, CodeNotFound)
	}
	e = MapHTTPError(409, "conflict", "pull request #9", true)
	if e.Code != CodeIdempotentNoop || e.Exit != ExitSuccess {
		t.Fatalf("idempotent 409: got code=%q exit=%d", e.Code, e.Exit)
	}
}

// TestStripStatus guards the upstream-noise scrubbing for messages that fold in
// upstream text (plain conflict, validation).
func TestStripStatus(t *testing.T) {
	cases := map[string]string{
		"404 Not Found: pull request missing":      "pull request missing",
		"422 Unprocessable Entity: title required": "title required",
		"plain message": "plain message",
		"":              "",
	}
	for in, want := range cases {
		if got := stripStatus(in); got != want {
			t.Errorf("stripStatus(%q): got %q want %q", in, got, want)
		}
	}
}

// ensure net is referenced (the timeoutErr above needs the interface match).
var _ net.Error = (*timeoutErr)(nil)
