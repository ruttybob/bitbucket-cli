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
the adapter: `pr.go`, `repo.go`, `branch.go`, `commit.go`, `pipeline.go`,
`issue.go`, `webhook.go`, `variable.go`, `project.go`, `status.go`, `perms.go`,
`admin.go`, `pr_ext.go` (`internal/bitbucket/`). Commands call e.g.
`client.ListPRs(...)` / `ListIssues(...)` / `ListRepos` / `GetCommit` /
`ListPipelines` / … and get normalized domain models. Never sprinkle `cloud`/`dc`
checks into command files — extend the adapter.

- **Platform-restricted nouns**: the adapter guards with the shared `CloudOnly(feature, c.hostKindLabel())` / `DCOnly(feature, c.hostKindLabel())` helpers (`internal/bitbucket/guards.go`), which render `pipelines is Bitbucket Cloud only; the active host is Bitbucket Data Center` (and the DC-only mirror). `c.hostKindLabel()` resolves the active host to its product name. Use these for any platform-restricted command.
- **Opt-in commit-derived fields**: branch `message`/`author`/`updated` and pipeline `steps` are not on the list payload, so the adapter fetches them per row only when requested via `--fields` (one extra request per row). Keep such extras opt-in.

## Output contract (do not regress)

- TOON on stdout by default; `--json`/`--yaml` are escape hatches only.
- All rendering goes through `internal/axi` (the only package that imports
  `toon-go`). Keep TOON confined there.
- Errors → stdout as `error:` + `help[N]{step}:`; exit `0` success/no-op, `1`
  error, `2` usage. `internal/axi.ExitCode` is the single source for codes.
- Unknown flags must be rejected by name with an inline valid-flag list (exit 2).
  New commands declare their flag set on `app.Command.Flags`; globals are
  `--help`/`--json`/`--yaml`.
- **Large content** (diffs, logs): default renders a **tail-truncated** preview (`axi.TruncateTail`, budgets in each command); `--full` writes the complete output to a temp file via `writeTempOutput` and emits `full_path:` + a `help[]` pointer. `axi.ExceedsBudget` gates the `--full` hint. Head truncation (`axi.TruncateBody`) is for bodies/descriptions.
- `--text` (pipe-friendly) prints bare lines and short-circuits before schema rendering; declare it per command when useful.

## Commands

- Add nouns/verbs in `internal/commands/` and register them in `NewApp`
  (`internal/commands/app.go`).
- The dispatcher (`internal/app`) walks the command tree to **arbitrary depth**:
  it descends through named children until a leaf (`Run != nil`) is reached, then
  parses the rest as flags+positionals. So both shallow commands (`pr list`,
  `api <path>`) and grouped nouns (`pr reviewer list <id>`, `perms project grant
  <k> <u> <p>`) need no dispatcher changes. Leaf nouns (`api`, `context`'s
  verbs) run directly.
- **Deprecation alias layer** (`internal/app/deprecated.go`): old `bkt`/pre-consolidation
  forms (`pr suggestion`, `pr reviewer-group`, `pr publish`, `pr auto-merge`,
  `pr reaction`, `repo browse`) are intercepted at the unknown-child point and
  emit a one-line notice naming the new command (exit 2). Migration aid only —
  old verbs are not emulated.
- Flag types: `FlagString`, `FlagInt`, `FlagBool`, and `FlagStringSlice`
  (repeatable). A `Flag.Short` (single char) gives a `-X` alias; the only one in
  use is `api --field`/`-F`.
- Command rendering helpers (`emitList`/`emitEmpty`/`emitDetail`/
  `emitConfirmation`, `resolveExtraFields`, `bodyFromFlags`) live in
  `internal/commands/helpers.go`; reuse them instead of re-deriving the TOON/JSON
  dual payload.
- Help blocks and the home view render automatically; do not hand-write TOON
  help strings outside `internal/axi` + `internal/app/help.go`.
- **List-command checklist**: default minimal schema; `--fields` extras extend it via `extendSchema` (one `extraFields` map per command; unknown values exit 2 with the allowed list); a `count:` line via `countLine` (`N of M total` / `N shown (more available)` / bare `N`); and a `help[]` block from `internal/axi/suggest.go`. Detail commands truncate large fields with a `--full` escape hatch. `--json`/`--yaml` payloads are built from the same schema via `listPayloadRows`/`detailExtracted`.

## Mutations & idempotency (AXI §6)

- Mutation adapters live in `internal/bitbucket/pr_mutations.go`, `repo.go`,
  `branch.go` — same one-switch principle as `pr.go`. They return the normalized
  resource plus an `Already bool` (`PRMutation`, `BranchMutation`); commands
  render the `(already — no-op)` suffix when `Already` is true and exit 0.
- **Idempotency is implemented with explicit state pre-checks** in the adapter
  (GET, compare target state), NOT by mapping 409s to no-ops. A residual 409
  that is not "already in target state" is a real `CONFLICT` (exit 1). This is
  why the adapter — not `errormap.go`'s `idempotent=true` path — owns no-ops.
- **Error translation chokepoint is `Client.mapErr`** (`internal/bitbucket/httperr.go`):
  every adapter call site funnels errors through it. It routes `*httpx.HTTPError`
  through `axi.MapError` (threading host kind + Retry-After) and any non-HTTP
  transport failure (timeout, refused connection) through `axi.MapTransportError`
  → `NETWORK_ERROR`. `axi.MapError` is the comprehensive status→code table; `axi.MapHTTPError` is the legacy thin wrapper kept for the raw `api` passthrough.
- DC optimistic concurrency: `dcMutate` GETs the version, retries once on a 409
  stale-version after re-fetching (and re-checking state). Use it for any
  version-gated DC mutation (merge/decline/reopen/edit).
- Git shell-out for `pr checkout` / `repo clone` and inference (`--source`,
  `--title` defaults) live in `internal/git/run.go` and `inference.go`.

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
