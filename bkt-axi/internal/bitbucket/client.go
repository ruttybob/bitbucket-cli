package bitbucket

import (
	"fmt"
	"time"

	"github.com/ruttybob/bkt-axi/internal/bitbucket/cloud"
	"github.com/ruttybob/bkt-axi/internal/bitbucket/dc"
	"github.com/ruttybob/bkt-axi/internal/bitbucket/httpx"
	"github.com/ruttybob/bkt-axi/internal/config"
)

// Kind distinguishes the two Bitbucket product lines a single invocation can
// target. Every host resolves to exactly one.
type Kind string

const (
	KindCloud Kind = "cloud"
	KindDC    Kind = "dc"
)

// Client is the unified Bitbucket client. It wraps exactly one of the
// salvaged line-specific clients (internal/bitbucket/cloud or /dc) and exposes
// normalized adapter methods (see pr.go). The command layer never switches on
// host kind: it calls Client.ListPRs and the adapter does the dispatch once.
type Client struct {
	Kind    Kind
	cloud   *cloud.Client
	dc      *dc.Client
	Host    *config.Host
	HostKey string
}

// retryPolicy is shared by both line clients and matches the salvaged
// cmdutil defaults (4 attempts, 250ms→2s backoff).
var retryPolicy = httpx.RetryPolicy{
	MaxAttempts:    4,
	InitialBackoff: 250 * time.Millisecond,
	MaxBackoff:     2 * time.Second,
}

// NewClient builds the line-specific client for host and wraps it in the
// unified Client. OAuth tokens authenticate as bearer; everything else is
// basic (Cloud) or the host's declared method (DC). Phase 0 uses the
// credential as-is: an expired OAuth token surfaces as AUTH_REQUIRED rather
// than being auto-refreshed.
func NewClient(host *config.Host, hostKey string) (*Client, error) {
	if host == nil {
		return nil, fmt.Errorf("missing host configuration")
	}
	switch Kind(host.Kind) {
	case KindCloud:
		cc, err := newCloudClient(host)
		if err != nil {
			return nil, err
		}
		return &Client{Kind: KindCloud, cloud: cc, Host: host, HostKey: hostKey}, nil
	case KindDC:
		dcc, err := newDCClient(host)
		if err != nil {
			return nil, err
		}
		return &Client{Kind: KindDC, dc: dcc, Host: host, HostKey: hostKey}, nil
	default:
		return nil, fmt.Errorf("unsupported host kind %q", host.Kind)
	}
}

func newCloudClient(host *config.Host) (*cloud.Client, error) {
	baseURL := host.BaseURL
	if baseURL == "" {
		baseURL = "https://api.bitbucket.org/2.0"
	}
	opts := cloud.Options{
		BaseURL:     baseURL,
		Username:    host.Username,
		Token:       host.Token,
		EnableCache: true,
		Retry:       retryPolicy,
	}
	if host.AuthMethod == "oauth" {
		opts.AuthMethod = "bearer"
	}
	return cloud.New(opts)
}

func newDCClient(host *config.Host) (*dc.Client, error) {
	if host.BaseURL == "" {
		return nil, fmt.Errorf("Data Center host has no base URL configured")
	}
	opts := dc.Options{
		BaseURL:     host.BaseURL,
		Username:    host.Username,
		Token:       host.Token,
		AuthMethod:  host.AuthMethod,
		EnableCache: true,
		Retry:       retryPolicy,
	}
	return dc.New(opts)
}
