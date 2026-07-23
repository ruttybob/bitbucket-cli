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
the adapter: `pr.go`, `repo.go`, `branch.go`, `commit.go`, `pipeline.go`
(`internal/bitbucket/`). Commands call e.g. `client.ListPRs(...)` and get
normalized `[]bitbucket.PR`; `client.ListRepos`, `ListBranches`, `GetCommit`,
`CommitDiff`, `CommitStatuses`, `ListPipelines`, … all return normalized
domain models. Never sprinkle `cloud`/`dc` checks into command files — extend
the adapter.

- **Cloud-only nouns** (pipelines): the adapter guards with `bitbucket.CloudOnly(feature, c.hostKindLabel())`, which renders `pipelines is Bitbucket Cloud only; the active host is Bitbucket Data Center`. Use the same helper for any DC-only command added later (swap the wording to "Data Center only").
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
  (`internal/commands/app.go`). The dispatcher (`internal/app`) needs no changes
  for new commands.
- Help blocks and the home view render automatically; do not hand-write TOON
  help strings outside `internal/axi` + `internal/app/help.go`.
- **List-command checklist**: default minimal schema; `--fields` extras extend it via `extendSchema` (one `extraFields` map per command; unknown values exit 2 with the allowed list); a `count:` line via `countLine` (`N of M total` / `N shown (more available)` / bare `N`); and a `help[]` block from `internal/axi/suggest.go`. Detail commands truncate large fields with a `--full` escape hatch. `--json`/`--yaml` payloads are built from the same schema via `listPayloadRows`/`detailExtracted`.

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
