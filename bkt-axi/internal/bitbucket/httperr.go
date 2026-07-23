package bitbucket

import (
	"errors"

	"github.com/ruttybob/bkt-axi/internal/axi"
	"github.com/ruttybob/bkt-axi/internal/bitbucket/httpx"
)

// httperr.go bridges the salvaged transport error type (*httpx.HTTPError) to the
// axi error map. Every adapter call site funnels errors through Client.mapErr,
// so a 404/401/403/409/422/429/5xx from either Cloud or Data Center — and any
// non-HTTP transport failure (timeout, refused connection) — lands as one
// structured, agent-readable AxiError with a stable code, never as raw upstream
// text.

// asHTTPError reports whether err (or any wrapped error) is an *httpx.HTTPError
// and, if so, sets target to it.
func asHTTPError(err error, target **httpx.HTTPError) bool {
	return errors.As(err, target)
}

// mapErr is the single error-translation chokepoint for the adapter. It routes
// an upstream *httpx.HTTPError through axi.MapError (threading the host kind
// and any server-advertised Retry-After), and routes any non-HTTP transport
// error (timeout, unreachable host) through axi.MapTransportError. nil err
// returns nil.
//
// Idempotency is owned by the adapter's explicit state pre-checks (see
// pr_mutations.go's dcMutate); a residual 409 that escapes here is a genuine
// CONFLICT, not a no-op. Callers that know a mutation is idempotent should
// short-circuit before reaching this path.
func (c *Client) mapErr(err error, noun string) error {
	if err == nil {
		return nil
	}
	var he *httpx.HTTPError
	if asHTTPError(err, &he) {
		return axi.MapError(axi.ErrorContext{
			Status:     he.StatusCode,
			Upstream:   he.Error(),
			Noun:       noun,
			RetryAfter: he.RetryAfter,
			HostKind:   string(c.Kind),
		})
	}
	return axi.MapTransportError(err, noun)
}

// isNotFound reports whether err wraps a 404 transport error. Idempotent
// delete/destroy commands use it to treat an already-gone resource as a no-op.
func isNotFound(err error) bool {
	var he *httpx.HTTPError
	if asHTTPError(err, &he) {
		return he.StatusCode == 404
	}
	return false
}
