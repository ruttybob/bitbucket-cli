// Package main is the bkt-axi entry point. It resolves its own executable path
// (so the home view can show a portable `bin:` line) and hands argv to the
// dispatcher, exiting with the AXI exit code it returns.
package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ruttybob/bkt-axi/internal/commands"
)

func main() {
	os.Exit(commands.NewApp(binPath()).Run(os.Args[1:]))
}

// binPath returns the absolute path of the running executable, collapsing the
// user's home directory to ~ for a compact, portable `bin:` line. It falls back
// to "bkt-axi" when the executable cannot be resolved (e.g. `go run`).
func binPath() string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return "bkt-axi"
	}
	if abs, err := filepath.Abs(exe); err == nil {
		exe = abs
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(exe, home) {
		return "~" + strings.TrimPrefix(exe, home)
	}
	return exe
}
