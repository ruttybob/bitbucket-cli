package bitbucket

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/ruttybob/bkt-axi/internal/bitbucket/cloud"
	"github.com/ruttybob/bkt-axi/internal/bitbucket/dc"
)

// webhook.go adapts the salvaged Cloud and Data Center webhook clients into the
// normalized Webhook model. This is the SINGLE place that switches on host.Kind
// for webhooks.

// WebhookCreateInput configures webhook creation. Name is optional (auto-derived
// from the URL host when empty, since DC requires it and the task surface only
// exposes --url).
type WebhookCreateInput struct {
	URL    string
	Events []string
	Active bool
	Name   string
}

// ListWebhooks enumerates repository webhooks for scope.
func (c *Client) ListWebhooks(ctx context.Context, scope Scope) (*WebhookListResult, error) {
	switch c.Kind {
	case KindCloud:
		if scope.Workspace == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("workspace and repo are required; use --workspace/--repo or set a context")
		}
		hooks, err := c.cloud.ListWebhooks(ctx, scope.Workspace, scope.RepoSlug)
		if err != nil {
			return nil, c.mapErr(err, "webhooks")
		}
		out := make([]Webhook, 0, len(hooks))
		for i := range hooks {
			out = append(out, mapCloudWebhook(&hooks[i]))
		}
		return &WebhookListResult{Webhooks: out, Shown: len(out)}, nil
	case KindDC:
		if scope.ProjectKey == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("project and repo are required; use --project/--repo or set a context")
		}
		hooks, err := c.dc.ListWebhooks(ctx, scope.ProjectKey, scope.RepoSlug)
		if err != nil {
			return nil, c.mapErr(err, "webhooks")
		}
		out := make([]Webhook, 0, len(hooks))
		for i := range hooks {
			out = append(out, mapDCWebhook(&hooks[i]))
		}
		return &WebhookListResult{Webhooks: out, Shown: len(out)}, nil
	}
	return nil, fmt.Errorf("unsupported host kind %q", c.Kind)
}

// CreateWebhook registers a webhook. Returns the normalized result.
func (c *Client) CreateWebhook(ctx context.Context, scope Scope, in WebhookCreateInput) (*Webhook, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = webhookNameFromURL(in.URL)
	}
	switch c.Kind {
	case KindCloud:
		if scope.Workspace == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("workspace and repo are required; use --workspace/--repo or set a context")
		}
		hook, err := c.cloud.CreateWebhook(ctx, scope.Workspace, scope.RepoSlug, cloud.WebhookInput{
			Description: name,
			URL:         in.URL,
			Events:      in.Events,
			Active:      in.Active,
		})
		if err != nil {
			return nil, c.mapErr(err, "webhook")
		}
		m := mapCloudWebhook(hook)
		return &m, nil
	case KindDC:
		if scope.ProjectKey == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("project and repo are required; use --project/--repo or set a context")
		}
		hook, err := c.dc.CreateWebhook(ctx, scope.ProjectKey, scope.RepoSlug, dc.CreateWebhookInput{
			Name:   name,
			URL:    in.URL,
			Events: in.Events,
			Active: in.Active,
		})
		if err != nil {
			return nil, c.mapErr(err, "webhook")
		}
		m := mapDCWebhook(hook)
		return &m, nil
	}
	return nil, fmt.Errorf("unsupported host kind %q", c.Kind)
}

// DeleteWebhook removes a webhook by its identifier (Cloud UUID or DC numeric
// id). changed is false (idempotent no-op) when the webhook is already gone
// (404). Any other error is returned.
func (c *Client) DeleteWebhook(ctx context.Context, scope Scope, id string) (bool, error) {
	switch c.Kind {
	case KindCloud:
		if scope.Workspace == "" || scope.RepoSlug == "" {
			return false, fmt.Errorf("workspace and repo are required")
		}
		if err := c.cloud.DeleteWebhook(ctx, scope.Workspace, scope.RepoSlug, id); err != nil {
			if isNotFound(err) {
				return false, nil
			}
			return false, c.mapErr(err, "webhook "+id)
		}
		return true, nil
	case KindDC:
		if scope.ProjectKey == "" || scope.RepoSlug == "" {
			return false, fmt.Errorf("project and repo are required")
		}
		nid, perr := strconv.Atoi(strings.TrimSpace(id))
		if perr != nil {
			return false, fmt.Errorf("webhook id %q is not a number", id)
		}
		if err := c.dc.DeleteWebhook(ctx, scope.ProjectKey, scope.RepoSlug, nid); err != nil {
			if isNotFound(err) {
				return false, nil
			}
			return false, c.mapErr(err, "webhook "+id)
		}
		return true, nil
	}
	return false, fmt.Errorf("unsupported host kind %q", c.Kind)
}

// TestWebhook triggers a test delivery. Supported on Data Center; Cloud has no
// public test-delivery API, so it returns a clear error.
func (c *Client) TestWebhook(ctx context.Context, scope Scope, id string) error {
	switch c.Kind {
	case KindCloud:
		return fmt.Errorf("Bitbucket Cloud does not expose a webhook test-delivery API; test webhooks from the repository settings UI")
	case KindDC:
		if scope.ProjectKey == "" || scope.RepoSlug == "" {
			return fmt.Errorf("project and repo are required")
		}
		nid, err := strconv.Atoi(strings.TrimSpace(id))
		if err != nil {
			return fmt.Errorf("webhook id %q is not a number", id)
		}
		if err := c.dc.TestWebhook(ctx, scope.ProjectKey, scope.RepoSlug, nid); err != nil {
			return c.mapErr(err, "webhook "+id)
		}
		return nil
	}
	return fmt.Errorf("unsupported host kind %q", c.Kind)
}

// --- mappers -------------------------------------------------------------

func mapCloudWebhook(h *cloud.Webhook) Webhook {
	return Webhook{
		ID:      strings.Trim(h.UUID, "{}"),
		Name:    h.Description,
		URL:     h.URL,
		Active:  h.Active,
		Events:  h.Events,
		Created: "",
	}
}

func mapDCWebhook(h *dc.Webhook) Webhook {
	return Webhook{
		ID:      strconv.Itoa(h.ID),
		Name:    h.Name,
		URL:     h.URL,
		Active:  h.Active,
		Events:  h.Events,
		Created: "",
	}
}

// webhookNameFromURL derives a webhook name from the URL's host (or the full
// URL when no host parses), so DC's required name field can be satisfied from
// the --url the agent provides.
func webhookNameFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "bkt-axi webhook"
	}
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}
