# AGENTS.md — bkt-axi

`bkt-axi` is an AXI-compliant rewrite of this repo's `bitbucket-cli`, living in
its own Go module (`github.com/ruttybob/bkt-axi`) under `bkt-axi/`. See
[`README.md`](./README.md) for the full overview; this file records only what an
agent must know to work here without re-deriving it.

## Layout & module boundary

- New module, **not** part of the root `github.com/avivsinai/bitbucket-cli`
  module. Its `go.mod`, `go.sum`, and dependencies are independent.
- Salvaged substrate packages were copied verbatim from `pkg/`/`internal/` and
  retagged: `pkg/bbcloud`→`internal/bitbucket/cloud` (package `cloud`),
  `pkg/bbdc`→`internal/bitbucket/dc` (package `dc`), `pkg/httpx`→`internal/bitbucket/httpx`,
  `internal/secret`→`internal/auth`, `internal/remote`→`internal/git`,
  `pkg/oauth`→`internal/oauth`, `pkg/types`→`internal/types`,
  `pkg/iostreams`→`internal/iostreams`, `internal/config`→`internal/config`.
- **Not ported**: `pkg/cmd/*`, `pkg/format`, `pkg/cmdutil/output.go`,
  `internal/mcpserver`, `internal/docgen`, `pkg/prompter`/`pager`/`progress`.

## The one-switch principle

Every old command switched on `host.Kind`. Here that switch lives **once**, in
the adapter (`internal/bitbucket/pr.go`): commands call `client.ListPRs(...)` and
get normalized `[]bitbucket.PR`. Never sprinkle `cloud`/`dc` checks into command
files — extend the adapter.

## Output contract (do not regress)

- TOON on stdout by default; `--json`/`--yaml` are escape hatches only.
- All rendering goes through `internal/axi` (the only package that imports
  `toon-go`). Keep TOON confined there.
- Errors → stdout as `error:` + `help[N]{step}:`; exit `0` success/no-op, `1`
  error, `2` usage. `internal/axi.ExitCode` is the single source for codes.
- Unknown flags must be rejected by name with an inline valid-flag list (exit 2).
  New commands declare their flag set on `app.Command.Flags`; globals are
  `--help`/`--json`/`--yaml`.

## Commands

- Add nouns/verbs in `internal/commands/` and register them in `NewApp`
  (`internal/commands/app.go`). The dispatcher (`internal/app`) needs no changes
  for new commands.
- Help blocks and the home view render automatically; do not hand-write TOON
  help strings outside `internal/axi` + `internal/app/help.go`.

## Build & test

```sh
cd bkt-axi
go build ./cmd/bkt-axi && go vet ./... && go test ./...
```

Salvaged client tests must stay green: `go test ./internal/bitbucket/...`.

## Maintaining this file

Keep this file for knowledge useful to almost every future agent session in this project.
Do not repeat what the codebase already shows; point to the authoritative file or command instead.
Prefer rewriting or pruning existing entries over appending new ones.
When updating this file, preserve this bar for all agents and keep entries concise.
