package app

import (
	"io"
	"os"

	"github.com/ruttybob/bkt-axi/internal/bitbucket"
	"github.com/ruttybob/bkt-axi/internal/config"
)

// defaultStdout/stderr let tests omit IO wiring. They are package-level so the
// App helpers can fall back to the process streams.
var (
	defaultStdout io.Writer = os.Stdout
	defaultStderr io.Writer = os.Stderr
)

// Context is what a command's Run receives: its parsed flags and positionals,
// the IO streams, and lazy accessors for the loaded config and the resolved
// Bitbucket host/scope/client. Resolution depends on flags (--context, --host,
// --repo, …) so it is deferred until a command actually needs it; a pure-help
// or auth-status command never pays for host resolution.
type Context struct {
	App    *App
	Cmd    *Command
	Args   []string
	Flags  ParsedFlags
	Stdout io.Writer
	Stderr io.Writer

	cfg        *config.Config
	cfgErr     error
	cfgDone    bool
	resolved   *bitbucket.Resolved
	resolveErr error
	client     *bitbucket.Client
	clientErr  error
}

// out/err return the context streams, falling back to the App then the process.
func (c *Context) out() io.Writer {
	if c.Stdout != nil {
		return c.Stdout
	}
	if c.App != nil && c.App.Stdout != nil {
		return c.App.Stdout
	}
	return defaultStdout
}

func (c *Context) err() io.Writer {
	if c.Stderr != nil {
		return c.Stderr
	}
	if c.App != nil && c.App.Stderr != nil {
		return c.App.Stderr
	}
	return defaultStderr
}

// Print writes s to stdout.
func (c *Context) Print(s string) {
	io.WriteString(c.out(), s)
}

// Out returns the stdout writer (for command packages that emit format-specific
// output and need the raw stream).
func (c *Context) Out() io.Writer { return c.out() }

// ErrOut returns the stderr writer.
func (c *Context) ErrOut() io.Writer { return c.err() }

// Println writes s followed by a newline to stdout.
func (c *Context) Println(s string) {
	io.WriteString(c.out(), s+"\n")
}

// OutputFormat returns "json", "yaml", or "" (TOON default) from the escape
// hatch flags.
func (c *Context) OutputFormat() string {
	if c.Flags.Bool("yaml") {
		return "yaml"
	}
	if c.Flags.Bool("json") {
		return "json"
	}
	return ""
}

// Config loads the persisted configuration once and caches it (and any error).
func (c *Context) Config() (*config.Config, error) {
	if c.cfgDone {
		return c.cfg, c.cfgErr
	}
	c.cfgDone = true
	c.cfg, c.cfgErr = config.Load()
	return c.cfg, c.cfgErr
}

// Resolve resolves the host (using --context/--host) once and caches it.
func (c *Context) Resolve() (*bitbucket.Resolved, error) {
	if c.resolved != nil || c.resolveErr != nil {
		return c.resolved, c.resolveErr
	}
	cfg, err := c.Config()
	if err != nil {
		c.resolveErr = err
		return nil, err
	}
	r, err := bitbucket.ResolveHost(cfg, c.Flags.String("context"), c.Flags.String("host"))
	if err != nil {
		c.resolveErr = err
		return nil, err
	}
	c.resolved = r
	return r, nil
}

// ScopeOverrides builds scope overrides from the standard selector flags.
func (c *Context) ScopeOverrides() bitbucket.ScopeOverrides {
	return bitbucket.ScopeOverrides{
		Workspace:  c.Flags.String("workspace"),
		ProjectKey: c.Flags.String("project"),
		RepoSlug:   c.Flags.String("repo"),
	}
}

// Scope resolves the repository scope from overrides + context + git remote.
func (c *Context) Scope() (bitbucket.Scope, error) {
	r, err := c.Resolve()
	if err != nil {
		return bitbucket.Scope{}, err
	}
	return bitbucket.ResolveScope(r, c.ScopeOverrides()), nil
}

// Client builds (and caches) the unified Bitbucket client for the resolved host.
func (c *Context) Client() (*bitbucket.Client, error) {
	if c.client != nil || c.clientErr != nil {
		return c.client, c.clientErr
	}
	r, err := c.Resolve()
	if err != nil {
		c.clientErr = err
		return nil, err
	}
	cl, err := bitbucket.NewClient(r.Host, r.HostKey)
	if err != nil {
		c.clientErr = err
		return nil, err
	}
	c.client = cl
	return cl, nil
}
