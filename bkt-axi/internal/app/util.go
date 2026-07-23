package app

import (
	"fmt"
	"os"
)

// util.go holds small shared helpers for the dispatcher.

// toa is a tiny int→string to avoid strconv imports in help.go.
func toa(n int) string { return fmt.Sprintf("%d", n) }

// formatAny is a last-resort stringifier for flag default display.
func formatAny(v any) string { return fmt.Sprintf("%v", v) }

// userHomeDir returns the user's home directory, or "" when unset.
func userHomeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return ""
}
