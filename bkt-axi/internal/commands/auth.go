package commands

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ruttybob/bkt-axi/internal/app"
	"github.com/ruttybob/bkt-axi/internal/axi"
	"github.com/ruttybob/bkt-axi/internal/bitbucket"
	"github.com/ruttybob/bkt-axi/internal/config"
)

// auth.go implements `auth status`: a table of every configured host with its
// kind, base URL, username, auth method, and token status/expiry.

// NewAuthCmd builds the `auth` noun. Phase 0 ships only `status`; later phases
// add login/logout under the same noun.
func NewAuthCmd() *app.Command {
	return &app.Command{
		Name:  "auth",
		Short: "Manage authentication and hosts",
		Long:  "Inspect and manage the Bitbucket hosts bkt-axi can talk to.",
		Children: []*app.Command{
			{
				Name:    "status",
				Short:   "Show configured hosts and token status",
				Long:    "List every configured host with its kind, base URL, username, auth method, and token status.",
				MinArgs: 0, MaxArgs: 0,
				Run: runAuthStatus,
			},
		},
	}
}

// hostStatus derives a human-readable token status and expiry from a loaded host.
func hostStatus(h *config.Host, loadErr error) (status, expiry string) {
	if loadErr != nil {
		return "no token", ""
	}
	if !h.OAuthExpiresAt.IsZero() {
		if time.Now().After(h.OAuthExpiresAt) {
			return "expired", h.OAuthExpiresAt.Format(time.RFC3339)
		}
		return "ok", h.OAuthExpiresAt.Format(time.RFC3339)
	}
	return "ok", ""
}

func runAuthStatus(ctx *app.Context) error {
	cfg, err := ctx.Config()
	if err != nil {
		return err
	}

	keys := make([]string, 0, len(cfg.Hosts))
	for k := range cfg.Hosts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	toonRows := make([]axi.Object, 0, len(keys))
	jsonRows := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		h := *cfg.Hosts[key] // copy: loading mutates the token field
		loadErr := bitbucket.LoadHostCredentials(key, &h)
		status, expiry := hostStatus(&h, loadErr)

		// Ordered KV for TOON and a parallel map for JSON/YAML, kept in sync.
		kvs := []axi.KV{
			{Key: "host", Value: key},
			{Key: "kind", Value: h.Kind},
			{Key: "base_url", Value: h.BaseURL},
			{Key: "username", Value: h.Username},
			{Key: "method", Value: authMethodLabel(&h)},
			{Key: "status", Value: status},
		}
		row := map[string]any{
			"host":     key,
			"kind":     h.Kind,
			"base_url": h.BaseURL,
			"username": h.Username,
			"method":   authMethodLabel(&h),
			"status":   status,
		}
		if expiry != "" {
			kvs = append(kvs, axi.KV{Key: "expires", Value: expiry})
			row["expires"] = expiry
		}
		toonRows = append(toonRows, axi.NewObject(kvs...))
		jsonRows = append(jsonRows, row)
	}

	help := axi.AfterAuthStatus(len(toonRows))

	if len(toonRows) == 0 {
		msg := "0 hosts configured"
		doc := axi.NewObject(
			axi.KV{Key: "hosts", Value: msg},
			axi.KV{Key: "help", Value: axi.HelpRows([]string{
				"Run `bkt-axi auth login` to add a host",
				"Or set BKT_HOST and BKT_TOKEN for headless use",
			})},
		)
		emit(ctx, map[string]any{"hosts": msg}, axi.Marshal(doc))
		return nil
	}

	count := fmt.Sprintf("%d hosts", len(toonRows))
	doc := axi.NewObject(
		axi.KV{Key: "count", Value: count},
		axi.KV{Key: "hosts", Value: toonRows},
		axi.KV{Key: "help", Value: axi.HelpRows(help)},
	)
	payload := map[string]any{"count": count, "hosts": jsonRows, "help": help}
	emit(ctx, payload, axi.Marshal(doc))
	return nil
}

// authMethodLabel normalizes the stored auth method for display.
func authMethodLabel(h *config.Host) string {
	if m := strings.TrimSpace(h.AuthMethod); m != "" {
		return m
	}
	return "basic"
}
