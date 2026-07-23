package git

import (
	"bytes"
	"fmt"
	"os/exec"
)

// run.go provides thin, testable wrappers over `git` for the mutation
// commands (pr checkout, repo clone). Output is captured (not streamed) so
// callers can surface it through the AXI output contract; streaming diffs are
// not part of these commands.

// Run executes git with args in dir ("" → current directory) and returns its
// combined stdout/stderr. A non-zero exit is returned as a *GitError carrying
// the output so the command layer can map it to a clean message.
func Run(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return buf.Bytes(), &GitError{Args: args, Err: err, Output: buf.String()}
	}
	return buf.Bytes(), nil
}

// RunClean is Run without output capture on success (the caller ignores git's
// stdout). It still captures stderr for error messages.
func RunClean(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return &GitError{Args: args, Err: err, Output: stderr.String()}
	}
	return nil
}

// GitError wraps a non-zero git exit with the offending args and git's output.
type GitError struct {
	Args   []string
	Err    error
	Output string
}

func (e *GitError) Error() string {
	detail := e.Output
	if detail == "" {
		detail = e.Err.Error()
	}
	return fmt.Sprintf("git %s failed: %s", joinArgs(e.Args), oneLine(detail))
}

func (e *GitError) Unwrap() error { return e.Err }

func joinArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}

func oneLine(s string) string {
	for i, r := range s {
		if r == '\n' {
			return s[:i]
		}
	}
	return s
}

// LookupRemoteURL returns the fetch URL configured for name, or "" when the
// remote is absent. Errors (e.g. not a git repo) are surfaced as empty.
func LookupRemoteURL(dir, name string) string {
	out, err := Run(dir, "remote", "get-url", name)
	if err != nil {
		return ""
	}
	return trimSpace(string(out))
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
