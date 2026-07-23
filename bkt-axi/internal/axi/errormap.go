package axi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// Stable machine codes for translated errors. Callers branch on these instead
// of scraping status text. See MapError for the status→code translation table.
const (
	CodeNotFound               = "NOT_FOUND"
	CodeAuthRequired           = "AUTH_REQUIRED"
	CodeForbidden              = "FORBIDDEN"
	CodeConflict               = "CONFLICT"
	CodeConflictWithSuggestion = "CONFLICT_WITH_SUGGESTION"
	CodeRateLimited            = "RATE_LIMITED"
	CodeUnavailable            = "UNAVAILABLE"
	CodeUnknown                = "UNKNOWN"
	CodeValidation             = "VALIDATION"
	CodeNetworkError           = "NETWORK_ERROR"
	CodeIdempotentNoop         = "NOOP"
	CodeDeprecated             = "DEPRECATED"
)

// ErrorContext carries the structured signals an error path needs to translate
// a raw upstream condition into a precise, agent-actionable AxiError. Only
// Status and Noun are required; the optional fields refine the message and the
// self-correcting hints (Retry-After countdown, host-aware login instructions,
// stale-version retry suggestion).
type ErrorContext struct {
	// Status is the upstream HTTP status code (0 for a non-HTTP transport error).
	Status int
	// Upstream is the raw upstream status/message text, stripped of dependency
	// noise before it reaches the agent-facing message.
	Upstream string
	// Noun is the acted-on resource, folded into the message ("pull request #42").
	// Defaults to "resource" when blank.
	Noun string
	// Idempotent marks a mutation (approve/merge/reopen) whose residual 409 means
	// the desired state already held → a no-op (exit 0). Idempotency is normally
	// owned by explicit adapter pre-checks; this is the residual safety net.
	Idempotent bool
	// StaleVersion marks a Data Center optimistic-concurrency 409 (the resource's
	// version moved since it was read) → CONFLICT_WITH_SUGGESTION with a retry
	// hint, distinct from a plain conflict.
	StaleVersion bool
	// RetryAfter, when positive, is the server-advertised cooldown (from the
	// Retry-After header) folded into the RATE_LIMITED message and hint.
	RetryAfter time.Duration
	// HostKind is "cloud" or "dc" (or "" when unknown); it picks host-aware
	// login/scope hints for 401/403.
	HostKind string
}

// MapError translates ErrorContext into a structured AxiError. This is the
// comprehensive Bitbucket→AxiError table (Cloud + Data Center).
//
//	Status → Code                      Exit
//	404    → NOT_FOUND                 1
//	401    → AUTH_REQUIRED             1
//	403    → FORBIDDEN                 1
//	409 + Idempotent   → NOOP          0   (desired state already held)
//	409 + StaleVersion → CONFLICT_WITH_SUGGESTION  1  (retry with fresh version)
//	409                 → CONFLICT      1
//	422    → VALIDATION                1
//	429    → RATE_LIMITED              1   (Retry-After folded in when known)
//	5xx    → UNAVAILABLE               1
//	other  → UNKNOWN                   1
func MapError(c ErrorContext) *AxiError {
	if strings.TrimSpace(c.Noun) == "" {
		c.Noun = "resource"
	}
	switch {
	case c.Status == 404:
		e := NewError(CodeNotFound, fmt.Sprintf("%s not found", c.Noun))
		e.Suggestions = notFoundHints(c.Noun)
		return e
	case c.Status == 401:
		e := NewError(CodeAuthRequired, fmt.Sprintf("authentication required for %s", c.Noun))
		e.Suggestions = authHints(c.HostKind)
		return e
	case c.Status == 403:
		e := NewError(CodeForbidden, fmt.Sprintf("permission denied for %s", c.Noun))
		e.Suggestions = forbiddenHints(c.HostKind)
		return e
	case c.Status == 409 && c.Idempotent:
		return NoOp(fmt.Sprintf("%s already in the requested state (no-op)", c.Noun))
	case c.Status == 409 && c.StaleVersion:
		return NewError(CodeConflictWithSuggestion,
			fmt.Sprintf("%s changed since you last read it; retry with a fresh version", c.Noun)).
			With("Re-run the command; the resource was modified by another client").
			With("The Data Center server rejected the change because its version moved")
	case c.Status == 409:
		return NewError(CodeConflict, fmt.Sprintf("%s conflict: %s", c.Noun, stripStatus(c.Upstream))).
			With("The resource changed since you last read it; retry after refreshing")
	case c.Status == 422:
		return NewError(CodeValidation, fmt.Sprintf("invalid request for %s: %s", c.Noun, stripStatus(c.Upstream)))
	case c.Status == 429:
		e := NewError(CodeRateLimited, rateLimitedMessage(c.RetryAfter))
		e.Suggestions = rateLimitHints(c.RetryAfter)
		return e
	case c.Status >= 500:
		return NewError(CodeUnavailable, fmt.Sprintf("Bitbucket is unavailable (HTTP %d); retry shortly", c.Status))
	default:
		return NewError(CodeUnknown, fmt.Sprintf("request for %s failed: %s", c.Noun, stripStatus(c.Upstream)))
	}
}

// MapHTTPError is the legacy two-status entry point retained for callers that
// only know the status code and noun (e.g. the raw `api` passthrough). It
// delegates to MapError with a minimal context. Prefer MapError when you have
// retry-after or host-kind signals.
func MapHTTPError(status int, upstreamStatus, noun string, idempotent bool) *AxiError {
	return MapError(ErrorContext{
		Status:     status,
		Upstream:   upstreamStatus,
		Noun:       noun,
		Idempotent: idempotent,
	})
}

// MapTransportError maps a non-HTTP transport error (network timeout, refused
// connection, DNS failure) to NETWORK_ERROR. HTTP errors are left untouched so
// callers can route them through MapError first; pass an err that is *not* an
// upstream status failure. A nil err returns nil.
//
//	deadline/context timeout → "… timed out"
//	any other transport error → "could not reach Bitbucket for …"
func MapTransportError(err error, noun string) *AxiError {
	if err == nil {
		return nil
	}
	if strings.TrimSpace(noun) == "" {
		noun = "resource"
	}
	if isTimeout(err) {
		return NewError(CodeNetworkError, fmt.Sprintf("request for %s timed out", noun)).
			With("Retry the command; the request exceeded its timeout").
			With("Check network connectivity and the configured host URL with `bkt-axi auth status`")
	}
	return NewError(CodeNetworkError, fmt.Sprintf("could not reach Bitbucket for %s", noun)).
		With("Check network connectivity and that the host URL is reachable").
		With("Run `bkt-axi auth status` to confirm the configured host")
}

// isTimeout reports whether err represents a timeout: a context deadline
// (the stdlib sentinel) or any net.Error whose Timeout() is true. url.Error
// implements net.Error, so an HTTP client's own deadline is covered, and a
// context.Canceled is treated as user-initiated cancellation rather than a
// network timeout so the message stays honest.
func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}

// --- hint builders --------------------------------------------------------

func notFoundHints(noun string) []string {
	// The pr-focused discovery hint is the common case; otherwise fall back to
	// repo/auth discovery. Match the full phrase so a noun like "project" does
	// not trip a "pr" substring false positive.
	if strings.Contains(noun, "pull request") {
		return []string{"Run `bkt-axi pr list` to see available pull requests"}
	}
	return []string{
		"Run `bkt-axi pr list` or `bkt-axi repo list` to discover resources",
		"Confirm the resolved repository with `bkt-axi context list`",
	}
}

// authHints returns host-aware login instructions for a 401.
func authHints(hostKind string) []string {
	switch strings.ToLower(strings.TrimSpace(hostKind)) {
	case "cloud":
		return []string{
			"Run `bkt-axi auth login --kind cloud --web` to authenticate with Bitbucket Cloud",
			"Run `bkt-axi auth status` to inspect configured hosts",
		}
	case "dc":
		return []string{
			"Run `bkt-axi auth login <https://your-bitbucket.example.com>` to authenticate with Data Center",
			"Run `bkt-axi auth status` to inspect configured hosts",
		}
	default:
		return []string{
			"Run `bkt-axi auth login --kind cloud --web` for Bitbucket Cloud",
			"Run `bkt-axi auth login <host-url>` for Bitbucket Data Center",
		}
	}
}

// forbiddenHints returns scope/permission hints for a 403.
func forbiddenHints(hostKind string) []string {
	base := []string{
		"Run `bkt-axi auth status` to check your token and its scopes",
		"Confirm the active context has access to this repository",
	}
	if strings.ToLower(strings.TrimSpace(hostKind)) == "cloud" {
		base = append(base, "Bitbucket Cloud tokens need the relevant scope (e.g. read:user:bitbucket) for this action")
	}
	return base
}

func rateLimitedMessage(retryAfter time.Duration) string {
	if retryAfter > 0 {
		return fmt.Sprintf("rate limit reached; retry in %s", humanDuration(retryAfter))
	}
	return "rate limit reached; wait and retry"
}

func rateLimitHints(retryAfter time.Duration) []string {
	if retryAfter > 0 {
		return []string{fmt.Sprintf("Wait about %s, then retry (Retry-After advertised by Bitbucket)", humanDuration(retryAfter))}
	}
	return []string{"Wait a moment and retry; Bitbucket is throttling requests"}
}

// humanDuration renders a duration as a short, human-friendly countdown used in
// rate-limit messages (e.g. 42s, 1m30s). Sub-second precision is dropped.
func humanDuration(d time.Duration) string {
	if d <= 0 {
		return "a moment"
	}
	return d.Round(time.Second).String()
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
