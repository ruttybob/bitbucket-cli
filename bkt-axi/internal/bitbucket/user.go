package bitbucket

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruttybob/bkt-axi/internal/bitbucket/cloud"
)

// user.go resolves the authenticated user's identity. The identity is what
// `--mine` and the home view need to scope author/reviewer filters; each line's
// API wants it in a different shape, so the adapter returns one canonical
// string and the line clients normalize it themselves.

// CurrentUser returns the authenticated user's identity string for the active
// host: a Cloud UUID (for BBQL author.uuid filtering) or the DC username slug
// (for the REST participant filter). Display name is returned alongside for
// the home view header.
func (c *Client) CurrentUser(ctx context.Context) (identity, display string, err error) {
	switch c.Kind {
	case KindCloud:
		u, err := c.cloud.CurrentUser(ctx)
		if err != nil {
			return "", "", c.mapErr(err, "current user")
		}
		id := strings.TrimSpace(u.UUID)
		if id == "" {
			id = strings.TrimSpace(u.AccountID)
		}
		if id == "" {
			id = strings.TrimSpace(u.Username)
		}
		disp := u.Display
		if disp == "" {
			disp = u.Nickname
		}
		if disp == "" {
			disp = u.Username
		}
		return id, disp, nil
	case KindDC:
		// DC has no "who am I" endpoint that works without a slug; the host's
		// configured username is the slug used for participant filters.
		slug := strings.TrimSpace(c.Host.Username)
		if slug == "" {
			return "", "", fmt.Errorf("Data Center host %q has no username; set --reviewer or configure a username", c.HostKey)
		}
		return slug, slug, nil
	}
	return "", "", fmt.Errorf("unsupported host kind %q", c.Kind)
}

// resolveCurrentUser is a best-effort variant: it returns empty identity on
// failure instead of erroring, so the home view can degrade to a header-only
// dashboard when identity cannot be established.
func (c *Client) resolveCurrentUser(ctx context.Context) (identity, display string) {
	id, disp, err := c.CurrentUser(ctx)
	if err != nil {
		return "", ""
	}
	return id, disp
}

// HostDisplay returns a friendly label for the active host (home view header).
func HostDisplay(r *Resolved) string {
	if r == nil || r.Host == nil {
		return ""
	}
	if r.Host.Kind == "cloud" {
		return "https://bitbucket.org"
	}
	return strings.TrimRight(r.Host.BaseURL, "/")
}

// cloudUser is retained as documentation of the Cloud user shape the adapter
// reads; it is not used directly outside CurrentUser.
var _ = cloud.User{}
