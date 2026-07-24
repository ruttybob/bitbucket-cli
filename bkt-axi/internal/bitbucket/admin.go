package bitbucket

import (
	"context"

	"github.com/ruttybob/bkt-axi/internal/bitbucket/dc"
)

// admin.go adapts salvaged Data Center admin endpoints (secrets rotation,
// logging control) into the normalized layer. These are server-admin
// operations available only on Data Center.

// RotateSecret triggers encryption-key rotation via the Data Center secrets
// manager plugin.
func (c *Client) RotateSecret(ctx context.Context) error {
	if c.Kind != KindDC {
		return DCOnly("admin secrets rotation", c.hostKindLabel())
	}
	if err := c.dc.RotateSecret(ctx); err != nil {
		return c.mapErr(err, "secret rotation")
	}
	return nil
}

// LoggingLevel is the current Data Center logging configuration.
type LoggingLevel struct {
	Level string `toon:"level"`
	Async bool   `toon:"async"`
}

// GetLoggingLevel fetches the current Data Center logging configuration.
func (c *Client) GetLoggingLevel(ctx context.Context) (*LoggingLevel, error) {
	if c.Kind != KindDC {
		return nil, DCOnly("admin logging", c.hostKindLabel())
	}
	cfg, err := c.dc.GetLoggingConfig(ctx)
	if err != nil {
		return nil, c.mapErr(err, "logging config")
	}
	return &LoggingLevel{Level: cfg.Level, Async: cfg.Async}, nil
}

// SetLoggingLevel updates the Data Center logging level.
func (c *Client) SetLoggingLevel(ctx context.Context, level string) (*LoggingLevel, error) {
	if c.Kind != KindDC {
		return nil, DCOnly("admin logging", c.hostKindLabel())
	}
	cfg := dc.LoggingConfig{Level: level}
	if err := c.dc.UpdateLoggingConfig(ctx, cfg); err != nil {
		return nil, c.mapErr(err, "logging config")
	}
	return &LoggingLevel{Level: level}, nil
}
