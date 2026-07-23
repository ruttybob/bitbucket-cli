package bitbucket

import (
	"errors"

	"github.com/ruttybob/bkt-axi/internal/axi"
	"github.com/ruttybob/bkt-axi/internal/bitbucket/httpx"
)

// httperr.go bridges the salvaged transport error type (*httpx.HTTPError) to
// the axi error map, so adapter callers receive a structured, agent-readable
// error with a stable code instead of raw upstream status text.

// asHTTPError reports whether err (or any wrapped error) is an *httpx.HTTPError
// and, if so, sets target to it.
func asHTTPError(err error, target **httpx.HTTPError) bool {
	return errors.As(err, target)
}

// axiMap routes an *httpx.HTTPError through axi.MapHTTPError. The mutation
// verbs (approve/merge) are not in Phase 0's read-only slice, so idempotent is
// always false here; mutation commands added later will call axi.MapHTTPError
// directly with idempotent=true.
func axiMap(he *httpx.HTTPError, noun string) error {
	return axi.MapHTTPError(he.StatusCode, he.Error(), noun, false)
}
