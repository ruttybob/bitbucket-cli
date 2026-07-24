package session

import (
	"os"
	"strings"
)

// collapseHome rewrites a leading home directory to ~ for compact reporting.
func collapseHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+"/") {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

// configHome returns the OpenCode/XDG config home, defaulting to ~/.config.
func configHome() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return x
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return home + "/.config"
}

// userHome returns the user home directory.
func userHome() (string, error) {
	return os.UserHomeDir()
}
