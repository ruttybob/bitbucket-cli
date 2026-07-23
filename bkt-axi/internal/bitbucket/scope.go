package bitbucket

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/ruttybob/bkt-axi/internal/auth"
	"github.com/ruttybob/bkt-axi/internal/config"
	"github.com/ruttybob/bkt-axi/internal/git"
	"github.com/ruttybob/bkt-axi/internal/oauth"
)

// Scope is the resolved repository target: a Cloud workspace or DC project key,
// plus a repository slug. It is computed once per invocation so every command
// in the same run shares the same target.
type Scope struct {
	Workspace  string // Cloud
	ProjectKey string // Data Center
	RepoSlug   string
}

// Empty reports whether the scope resolved no repository.
func (s Scope) Empty() bool { return strings.TrimSpace(s.RepoSlug) == "" }

// String renders the scope as workspace/slug (Cloud) or project/slug (DC).
func (s Scope) String() string {
	if s.Workspace != "" {
		return s.Workspace + "/" + s.RepoSlug
	}
	if s.ProjectKey != "" {
		return s.ProjectKey + "/" + s.RepoSlug
	}
	return s.RepoSlug
}

// ScopeOverrides carries explicit flag-derived scope values that take
// precedence over context and git-remote inference.
type ScopeOverrides struct {
	Workspace  string
	ProjectKey string
	RepoSlug   string
}

// Resolved bundles the per-invocation resolution: which host, which context,
// and (when applicable) which repository scope.
type Resolved struct {
	Host    *config.Host
	HostKey string
	Context *config.Context
	Scope   Scope
}

// ResolveHost selects a host from, in priority order: an explicit --host
// override, the host behind an explicit --context, the active context's host,
// the single configured host, or a synthesized BKT_HOST/BKT_TOKEN host. The
// token is loaded (keyring or env) before returning.
func ResolveHost(cfg *config.Config, contextOverride, hostOverride string) (*Resolved, error) {
	if cfg == nil {
		return nil, errors.New("no configuration loaded")
	}

	// 1. explicit host override
	if h := strings.TrimSpace(hostOverride); h != "" {
		if host, ok := cfg.Hosts[h]; ok {
			if err := loadHostToken(h, host); err != nil {
				return nil, err
			}
			return &Resolved{Host: host, HostKey: h, Context: &config.Context{Host: h}}, nil
		}
		if baseURL, err := NormalizeBaseURL(h); err == nil {
			if key, err := HostKeyFromURL(baseURL); err == nil {
				if host, ok := cfg.Hosts[key]; ok {
					if err := loadHostToken(key, host); err != nil {
						return nil, err
					}
					return &Resolved{Host: host, HostKey: key, Context: &config.Context{Host: key}}, nil
				}
			}
		}
		if key, host, err := hostFromEnv(h); err != nil {
			return nil, fmt.Errorf("--host %q: %w", h, err)
		} else if host != nil {
			return &Resolved{Host: host, HostKey: key, Context: contextFromEnv(key)}, nil
		}
		return nil, fmt.Errorf("host %q not found; run `bkt-axi auth login` first", h)
	}

	// 2. explicit context override
	if name := strings.TrimSpace(contextOverride); name != "" {
		ctx, err := cfg.Context(name)
		if err != nil {
			return nil, err
		}
		host, err := cfg.Host(ctx.Host)
		if err != nil {
			return nil, err
		}
		if err := loadHostToken(ctx.Host, host); err != nil {
			return nil, err
		}
		return &Resolved{Host: host, HostKey: ctx.Host, Context: ctx}, nil
	}

	// 3. active context
	if name := cfg.ActiveContext; name != "" {
		ctx, err := cfg.Context(name)
		if err != nil {
			return nil, err
		}
		host, err := cfg.Host(ctx.Host)
		if err != nil {
			return nil, err
		}
		if err := loadHostToken(ctx.Host, host); err != nil {
			return nil, err
		}
		return &Resolved{Host: host, HostKey: ctx.Host, Context: ctx}, nil
	}

	// 4. BKT_HOST env headless host
	if key, host, err := hostFromEnv(os.Getenv(auth.EnvHost)); err != nil {
		return nil, fmt.Errorf("BKT_HOST: %w", err)
	} else if host != nil {
		return &Resolved{Host: host, HostKey: key, Context: contextFromEnv(key)}, nil
	}

	// 5. single configured host
	switch len(cfg.Hosts) {
	case 0:
		if auth.TokenFromEnv() != "" {
			return nil, errors.New("BKT_TOKEN is set but BKT_HOST is not; set BKT_HOST to the Bitbucket server URL")
		}
		return nil, errors.New("no hosts configured; run `bkt-axi auth login` first")
	case 1:
		for key, host := range cfg.Hosts {
			if err := loadHostToken(key, host); err != nil {
				return nil, err
			}
			return &Resolved{Host: host, HostKey: key, Context: &config.Context{Host: key}}, nil
		}
	default:
		var keys []string
		for k := range cfg.Hosts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("multiple hosts configured (%s); specify --host or --context", strings.Join(keys, ", "))
	}
	return nil, errors.New("failed to resolve host configuration")
}

// ResolveScope resolves the repository scope from explicit overrides, the
// resolved context, and finally git-remote inference against the working dir.
// The returned scope may be empty (no repo); callers decide whether that is an
// error for their command.
func ResolveScope(r *Resolved, overrides ScopeOverrides) Scope {
	if r == nil || r.Context == nil {
		return Scope{
			Workspace:  overrides.Workspace,
			ProjectKey: overrides.ProjectKey,
			RepoSlug:   overrides.RepoSlug,
		}
	}

	scope := Scope{
		Workspace:  FirstNonEmpty(overrides.Workspace, r.Context.Workspace),
		ProjectKey: FirstNonEmpty(overrides.ProjectKey, r.Context.ProjectKey),
		RepoSlug:   FirstNonEmpty(overrides.RepoSlug, r.Context.DefaultRepo),
	}

	// git-remote inference fills only what is still empty, and only when the
	// remote points at the same server as the host.
	if scope.RepoSlug == "" || (r.Host != nil && r.Host.Kind == "cloud" && scope.Workspace == "") ||
		(r.Host != nil && r.Host.Kind == "dc" && scope.ProjectKey == "") {
		applyRemoteDefaults(&scope, r.Host)
	}
	return scope
}

func applyRemoteDefaults(scope *Scope, host *config.Host) {
	if host == nil {
		return
	}
	wd, err := os.Getwd()
	if err != nil {
		return
	}
	loc, err := git.Detect(wd)
	if err != nil {
		return
	}
	if !locatorMatchesHost(host, loc) {
		return
	}
	if scope.RepoSlug == "" && loc.RepoSlug != "" {
		scope.RepoSlug = loc.RepoSlug
	}
	if host.Kind == "cloud" && scope.Workspace == "" {
		scope.Workspace = loc.Workspace
	}
	if host.Kind == "dc" && scope.ProjectKey == "" {
		scope.ProjectKey = loc.ProjectKey
	}
}

func locatorMatchesHost(host *config.Host, loc git.Locator) bool {
	if host == nil {
		return false
	}
	switch host.Kind {
	case "cloud":
		return loc.Kind == "cloud" && strings.EqualFold(loc.Host, "bitbucket.org")
	case "dc":
		if loc.Kind != "dc" {
			return false
		}
		h := hostHostname(host.BaseURL)
		return h != "" && strings.EqualFold(h, loc.Host)
	}
	return false
}

// --- token loading & env host synthesis ---------------------------------

// loadHostToken resolves a host's credential: BKT_TOKEN wins; otherwise the
// keyring is consulted. OAuth blobs are parsed so the access token is used as
// a bearer credential. (Phase 0 does not refresh; an expired OAuth token
// surfaces as AUTH_REQUIRED on the first call.)
func loadHostToken(hostKey string, host *config.Host) error {
	if host == nil {
		return fmt.Errorf("host %q not configured", hostKey)
	}
	if env := auth.TokenFromEnv(); env != "" {
		host.Token = env
		if u := strings.TrimSpace(os.Getenv(auth.EnvUsername)); u != "" {
			host.Username = u
		}
		if m := strings.TrimSpace(os.Getenv(auth.EnvAuthMethod)); m != "" {
			host.AuthMethod = m
		}
		return nil
	}
	if host.Token != "" {
		return nil
	}

	opts := []auth.Option{}
	if host.AllowInsecureStore {
		opts = append(opts, auth.WithAllowFileFallback(true))
	}
	store, err := auth.Open(opts...)
	if err != nil {
		if auth.IsNoKeyringError(err) {
			return fmt.Errorf("no OS keychain backend for host %q; run `bkt-axi auth login %s --allow-insecure-store` or set BKT_TOKEN: %w", hostKey, hostKey, err)
		}
		return err
	}
	raw, err := store.Get(auth.TokenKey(hostKey))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			target := host.BaseURL
			if target == "" {
				target = hostKey
			}
			return fmt.Errorf("credentials for host %q not found; run `bkt-axi auth login %s`", hostKey, target)
		}
		return err
	}

	switch host.AuthMethod {
	case "oauth":
		tok, perr := oauth.Unmarshal(raw)
		if perr != nil {
			return fmt.Errorf("stored OAuth token for host %q is invalid: %w", hostKey, perr)
		}
		host.Token = tok.AccessToken
		host.OAuthExpiresAt = tok.ExpiresAt
		return nil
	case "basic", "bearer":
		host.Token = raw
		return nil
	}
	if oauth.IsTokenBlob(raw) {
		tok, perr := oauth.Unmarshal(raw)
		if perr != nil {
			return fmt.Errorf("stored OAuth token for host %q is invalid: %w", hostKey, perr)
		}
		host.Token = tok.AccessToken
		host.AuthMethod = "oauth"
		host.OAuthExpiresAt = tok.ExpiresAt
		return nil
	}
	host.Token = raw
	return nil
}

// hostFromEnv synthesizes an ephemeral host from BKT_HOST + BKT_TOKEN (+ optional
// BKT_USERNAME/BKT_AUTH_METHOD). Returns ("", nil, nil) when either is unset.
func hostFromEnv(rawURL string) (string, *config.Host, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", nil, nil
	}
	token := auth.TokenFromEnv()
	if token == "" {
		return "", nil, nil
	}
	baseURL, err := NormalizeBaseURL(rawURL)
	if err != nil {
		return "", nil, err
	}
	hostname := hostHostname(baseURL)
	isCloud := hostname == "bitbucket.org" || hostname == "api.bitbucket.org"
	if isCloud {
		baseURL = "https://api.bitbucket.org/2.0"
	}
	key, err := HostKeyFromURL(baseURL)
	if err != nil {
		return "", nil, err
	}
	username := strings.TrimSpace(os.Getenv(auth.EnvUsername))
	authMethod := strings.TrimSpace(os.Getenv(auth.EnvAuthMethod))
	if isCloud {
		if username == "" {
			return "", nil, errors.New("BKT_USERNAME is required for Bitbucket Cloud; set it to your Atlassian account email")
		}
		authMethod = "basic"
	} else {
		if authMethod == "" && username == "" {
			authMethod = "bearer"
		}
	}
	kind := "dc"
	if isCloud {
		kind = "cloud"
	}
	return key, &config.Host{
		Kind:       kind,
		BaseURL:    baseURL,
		Username:   username,
		AuthMethod: authMethod,
		Token:      token,
	}, nil
}

func contextFromEnv(hostKey string) *config.Context {
	return &config.Context{
		Host:        hostKey,
		ProjectKey:  strings.TrimSpace(os.Getenv(auth.EnvProject)),
		Workspace:   strings.TrimSpace(os.Getenv(auth.EnvWorkspace)),
		DefaultRepo: strings.TrimSpace(os.Getenv(auth.EnvRepo)),
	}
}

// --- url helpers ---------------------------------------------------------

// NormalizeBaseURL ensures the Bitbucket base URL includes a scheme and has no
// trailing slash.
func NormalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("host is required")
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse URL: %w", err)
	}
	if u.Scheme == "" {
		u.Scheme = "https"
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
}

// HostKeyFromURL resolves the host component used as the configuration key.
func HostKeyFromURL(baseURL string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	if u.Host == "" {
		return "", fmt.Errorf("invalid base URL %q", baseURL)
	}
	return u.Host, nil
}

func hostHostname(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		raw = u.Host
	}
	raw = strings.Trim(raw, "[]")
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, ":") {
		if h, _, err := net.SplitHostPort(raw); err == nil {
			raw = h
		}
	}
	return strings.ToLower(raw)
}

// FirstNonEmpty returns the first trimmed-non-empty string.
func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// LoadHostCredentials resolves a host's credential into host.Token (and, for
// OAuth blobs, host.AuthMethod/OAuthExpiresAt), the same way ResolveHost does.
// It is exported so the auth-status command can inspect each configured host
// without going through full scope resolution.
func LoadHostCredentials(hostKey string, host *config.Host) error {
	return loadHostToken(hostKey, host)
}
