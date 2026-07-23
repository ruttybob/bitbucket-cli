# bkt-axi

An **AXI-compliant** rewrite of `bitbucket-cli` — a Bitbucket Cloud and Data
Center CLI designed for autonomous agents. Output is [TOON](https://toonformat.dev)
by default (~40% fewer tokens than JSON), with `--json`/`--yaml` as escape
hatches.

> **Status:** Phase 0 — foundation + vertical slice (`pr list`, `pr view`,
> `auth status`, content-first home view). Later phases add the full command
> tree (repo, branch, commit, pipeline, issue, webhook, mutations) and session
> hooks.

## Why a separate module?

This is a new Go module (`github.com/ruttybob/bkt-axi`) living in `bkt-axi/`
inside the existing `bitbucket-cli` repo. It is a **hybrid rewrite**: the API
client substrate is salvaged verbatim from `pkg/` (retagged), while the command,
output, and session layers are rebuilt from scratch around AXI and a bespoke
dispatcher (no cobra).

## Build & test

```sh
cd bkt-axi
go build ./cmd/bkt-axi          # build the binary
go test ./...                    # full suite (substrate + new code)
go test ./internal/bitbucket/... # salvaged client tests only
go vet ./...                     # clean
```

## Architecture

```
cmd/bkt-axi/          entry point (resolves bin path, hands argv to dispatcher)
internal/
  app/                bespoke dispatcher: noun/verb/flag parsing, --help, exit codes
  axi/                AXI primitives: AxiError, field-schema DSL, TOON renderers,
                      truncation, error mapping, suggestion table
  bitbucket/          NORMALIZED domain layer — one Client, one PR model
    cloud/            salvaged Cloud client (was pkg/bbcloud)
    dc/               salvaged Data Center client (was pkg/bbdc)
    httpx/            salvaged HTTP client (was pkg/httpx)
  auth/               salvaged keyring store (was internal/secret)
  config/             salvaged Host + Context model
  git/                salvaged remote parsing + default-branch inference
  oauth/              salvaged Cloud OAuth flow
  types/              salvaged shared types
  iostreams/          salvaged IO streams
  commands/           Phase 0 commands + App wiring (home, pr, auth)
```

### The one-switch principle

Every old `pkg/cmd/*/*.go` switched on `host.Kind` per command. In `bkt-axi`
that switch lives **once**, inside the adapter (`internal/bitbucket/pr.go`):
commands call `client.ListPRs(...)` and get back a normalized `[]PR` regardless
of Cloud vs Data Center.

### Output contract (AXI)

- All stdout is **TOON by default**; `--json`/`--yaml` are escape hatches.
- Errors go to **stdout** as `error: …` + a `help[N]{step}:` hint block.
- Exit codes: `0` success (incl. idempotent no-ops), `1` error, `2` usage.
- Unknown flags are rejected by name with an inline valid-flag list (exit 2).
- Long content (descriptions) is truncated to 500 chars with a `--full` escape hatch.

### Scope resolution

`Scope{Workspace|ProjectKey, RepoSlug}` resolves once per invocation from:
explicit flags (`--repo`, `--workspace`/`--project`) → active context →
git-remote inference from the working directory.

## Authentication

Configure a host with `bkt-axi auth login`, or run headless with environment
variables: `BKT_HOST`, `BKT_TOKEN` (+ `BKT_USERNAME` for Cloud, and optionally
`BKT_WORKSPACE`/`BKT_PROJECT`/`BKT_REPO`). Inspect with `bkt-axi auth status`.
