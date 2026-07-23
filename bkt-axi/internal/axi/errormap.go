package axi

import (
	"fmt"
	"strings"
)

// Stable machine codes for translated errors. Callers branch on these instead
// of scraping status text.
const (
	CodeNotFound       = "NOT_FOUND"
	CodeAuthRequired   = "AUTH_REQUIRED"
	CodeForbidden      = "FORBIDDEN"
	CodeConflict       = "CONFLICT"
	CodeRateLimited    = "RATE_LIMITED"
	CodeUnavailable    = "UNAVAILABLE"
	CodeUnknown        = "UNKNOWN"
	CodeValidation     = "VALIDATION"
	CodeIdempotentNoop = "NOOP"
)

// MapHTTPError translates an upstream HTTP status code into a structured
// AxiError. axi stays substrate-agnostic: the caller extracts the status from
// whatever transport type it uses (e.g. an *httpx.HTTPError) and passes it in.
//
// Mapping table (§6 + errormap spec):
//   - 404 → NOT_FOUND      (exit 1)
//   - 401 → AUTH_REQUIRED  (exit 1)
//   - 403 → FORBIDDEN      (exit 1)
//   - 409 → CONFLICT; when idempotent is true (approve/merge) it becomes a
//     NoOp (exit 0): the desired state already holds
//   - 422 → VALIDATION     (exit 1)
//   - 429 → RATE_LIMITED   (exit 1)
//   - 5xx → UNAVAILABLE    (exit 1)
//
// noun is the acted-on thing ("pull request #42") folded into the message;
// upstreamStatus keeps the raw text for diagnostics without leaking dependency
// names into the agent-facing suggestion.
func MapHTTPError(status int, upstreamStatus, noun string, idempotent bool) *AxiError {
	if strings.TrimSpace(noun) == "" {
		noun = "resource"
	}
	switch {
	case status == 404:
		return NewError(CodeNotFound, fmt.Sprintf("%s not found", noun)).
			With("Run `bkt-axi pr list` to see available pull requests")
	case status == 401:
		return NewError(CodeAuthRequired, fmt.Sprintf("authentication required for %s", noun)).
			With("Run `bkt-axi auth login` to authenticate")
	case status == 403:
		return NewError(CodeForbidden, fmt.Sprintf("permission denied for %s", noun)).
			With("Run `bkt-axi auth status` to check your configured token")
	case status == 409 && idempotent:
		return NoOp(fmt.Sprintf("%s already in the requested state (no-op)", noun))
	case status == 409:
		return NewError(CodeConflict, fmt.Sprintf("%s conflict: %s", noun, stripStatus(upstreamStatus))).
			With("The resource changed since you last read it; retry after refreshing")
	case status == 422:
		return NewError(CodeValidation, fmt.Sprintf("invalid request for %s: %s", noun, stripStatus(upstreamStatus)))
	case status == 429:
		return NewError(CodeRateLimited, "rate limit reached; wait and retry").
			With("Bitbucket is throttling requests; retry shortly")
	case status >= 500:
		return NewError(CodeUnavailable, fmt.Sprintf("Bitbucket is unavailable (HTTP %d); retry shortly", status))
	default:
		return NewError(CodeUnknown, fmt.Sprintf("request for %s failed: %s", noun, stripStatus(upstreamStatus)))
	}
}

// stripStatus drops the leading "4xx / 5xx message: " status prefix that the
// transport prepends, so the agent-facing message is a clean sentence.
func stripStatus(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, ": "); i >= 0 {
		if isStatusLine(s[:i]) {
			return strings.TrimSpace(s[i+2:])
		}
	}
	return s
}

func isStatusLine(s string) bool {
	s = strings.TrimSpace(s)
	// transport prefixes look like "404 Not Found" or "404 Not Found: …"
	if len(s) < 3 {
		return false
	}
	for _, r := range s[:3] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// CodeForStatus returns the machine code a status maps to, without building a
// full error. Useful when the caller already has a message and only needs the
// classification.
func CodeForStatus(status int) string {
	switch {
	case status == 404:
		return CodeNotFound
	case status == 401:
		return CodeAuthRequired
	case status == 403:
		return CodeForbidden
	case status == 409:
		return CodeConflict
	case status == 422:
		return CodeValidation
	case status == 429:
		return CodeRateLimited
	case status >= 500:
		return CodeUnavailable
	default:
		return CodeUnknown
	}
}
