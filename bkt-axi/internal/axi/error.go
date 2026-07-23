package axi

import (
	"errors"
	"fmt"
	"strings"
)

// AXI exit codes (§6 Output channels). Success covers idempotent no-ops: the
// desired state already holds, so the agent's intent is satisfied.
const (
	ExitSuccess = 0 // success, including idempotent no-ops
	ExitError   = 1 // runtime error
	ExitUsage   = 2 // usage error (unknown flag, missing required argument)
)

// AxiError is the structured, agent-readable error type. It renders to TOON on
// stdout (an `error:` line plus a `help[N]{step}:` hint block) and carries the
// process exit code. Dependency noise (API status text, stack traces) never
// leaks: callers populate Message with an actionable sentence and Suggestions
// with self-correcting next steps.
type AxiError struct {
	// Message is the single actionable sentence. e.g. "pull request #42 not found".
	Message string
	// Code is a stable machine code (NOT_FOUND, AUTH_REQUIRED, …) for callers
	// that branch on error class without scraping prose.
	Code string
	// Suggestions are the help hints, one per line. Keep them to complete
	// commands or templates (§9 Contextual disclosure).
	Suggestions []string
	// Exit is the process exit code this error maps to (0, 1, or 2).
	Exit int
}

// Error implements the error interface.
func (e *AxiError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// Is lets errors.Is match on the concrete type regardless of the Exit code,
// so callers can wrap an AxiError and still detect it.
func (e *AxiError) Is(target error) bool {
	_, ok := target.(*AxiError)
	return ok
}

// With appends suggestion hints and returns the same error for chaining.
func (e *AxiError) With(suggestions ...string) *AxiError {
	if e == nil {
		return nil
	}
	e.Suggestions = append(e.Suggestions, suggestions...)
	return e
}

// Errorf builds a runtime error (exit 1).
func Errorf(format string, args ...any) *AxiError {
	return &AxiError{Message: fmt.Sprintf(format, args...), Exit: ExitError}
}

// NewError builds a runtime error (exit 1) with a stable machine code.
func NewError(code, message string) *AxiError {
	return &AxiError{Message: message, Code: code, Exit: ExitError}
}

// UsageError builds a usage error (exit 2). validFlags, when provided, are
// folded into a self-correcting hint so the agent fixes itself in one turn.
func UsageError(message string, validFlags ...string) *AxiError {
	e := &AxiError{Message: message, Code: "USAGE", Exit: ExitUsage}
	if len(validFlags) > 0 {
		e.Suggestions = append(e.Suggestions, "valid flags: "+strings.Join(validFlags, ", ")+" (--help always allowed)")
	}
	return e
}

// NoOp reports a successful idempotent operation (exit 0): the desired state
// already held, so the command did nothing and that is the correct outcome.
func NoOp(message string) *AxiError {
	return &AxiError{Message: message, Code: "NOOP", Exit: ExitSuccess}
}

// ExitCode maps an error to its AXI exit code. nil → 0 (success). A wrapped
// *AxiError yields its declared Exit. Anything else is a runtime error (1).
func ExitCode(err error) int {
	if err == nil {
		return ExitSuccess
	}
	var ae *AxiError
	if errors.As(err, &ae) {
		// A zero Exit (unset) on an actual error value is a programming slip;
		// treat it as a runtime error rather than success.
		if ae.Exit == ExitSuccess && ae.Code != "NOOP" {
			return ExitError
		}
		return ae.Exit
	}
	return ExitError
}
