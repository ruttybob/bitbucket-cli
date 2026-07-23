# bkt-axi

An **AXI-compliant** Bitbucket Cloud and Data Center CLI built for autonomous
agents. Output is [TOON](https://toonformat.dev) by default — roughly **40%
fewer tokens than JSON** for the same data (see
[Token savings](#token-savings)) — with `--json`/`--yaml` as escape hatches and
structured, self-correcting errors on stdout.

`bkt-axi` is a new Go module (`github.com/ruttybob/bkt-axi`) living inside the
existing `bitbucket-cli` repo. It is a **hybrid rewrite**: the API-client
substrate is salvaged verbatim from `pkg/` (retagged), while the command,
output, and error layers are rebuilt from scratch around AXI and a bespoke
dispatcher (no cobra). One binary talks to **both** Bitbucket Cloud and Data
Center through a single normalized command surface.

> **Status:** Phases 0–5 complete — full read/write command surface, structured
> error translation, a `bkt`→`bkt-axi` deprecation layer, and token benchmarks.
> See [AGENTS.md](./AGENTS.md) for the engineering notes agents need to work here.

---

## Quick start

```sh
cd bkt-axi
go build ./cmd/bkt-axi          # produces ./bkt-axi
```

### Authenticate

The working headless path is environment variables (inspect with
`bkt-axi auth status`):

```sh
export BKT_HOST=https://api.bitbucket.org/2.0      # Cloud base URL, or your DC URL
export BKT_TOKEN=<app-password-or-PAT>
export BKT_USERNAME=<your-atlassian-email>         # Cloud app passwords need this
export BKT_WORKSPACE=<your-workspace>              # Cloud; or BKT_PROJECT=<KEY> for DC
export BKT_REPO=<repo-slug>                        # optional default repo
```

### First pull request

```sh
bkt-axi                          # content-first dashboard for the current repo
bkt-axi pr list                  # 50 open PRs, 4 default columns
bkt-axi pr view 1043             # detail with truncated description + next-step hints
bkt-axi pr list --fields author,branch,updated   # opt in to extra columns
bkt-axi pr approve 1043          # idempotent: approving twice is a no-op (exit 0)
```

No-args prints a live dashboard (your PRs, your reviews, recent pipelines)
resolved from the current directory's git remote.

---

## Command reference

Noun/verb, mirroring `gh` ergonomics: `bkt-axi <noun> <verb> [args] [flags]`.
One unified tree for Cloud **and** Data Center; a platform-limited verb run
against the wrong host prints a single-line error (`branch create is Data Center
only; the active host is Bitbucket Cloud`) rather than silently no-op'ing.

| Noun | Verbs | Notes |
|---|---|---|
| `pr` | `list` `view` `create` `edit` `diff` `checkout` `approve` `merge` `decline` `reopen` `comment` `reviewer` `task` `suggestions` `checks` | reviewer/task/suggestions/checks are grouped nouns; approve/merge/decline/reopen are idempotent |
| `repo` | `list` `view` `create` `clone` | |
| `branch` | `list` `create` `delete` | create/delete are Data Center only |
| `commit` | `view` `diff` `status` | |
| `pipeline` | `list` `view` | Cloud only |
| `issue` | `list` `view` `create` `edit` `close` `reopen` `comment` `attachment` | Cloud only; close/reopen idempotent |
| `webhook` | `list` `create` `delete` `test` | |
| `variable` | `list` `get` `set` `delete` | Cloud only |
| `project` | `list` | Data Center only |
| `status` | `commit` `pr` `pipeline` `rate-limit` | CI/build + rate-limit rollup |
| `perms` | `project` `repo` | Data Center only (grant/revoke) |
| `admin` | `secrets` `logging` | Data Center only |
| `auth` | `status` | inspect configured hosts and token status |
| `context` | `list` `create` `use` `delete` | named scope presets |
| `api` | `<path>` | raw REST passthrough (`--method`, `--field/-F`, `--paginate`) |

Every command supports `--help` (a TOON block: usage, flags with defaults,
required args, examples), the global `--context <name>` scope override, and the
`--json`/`--yaml` escape hatches. Lists default to a minimal schema and accept
`--fields a,b,c` to opt into extra columns.

---

## Output contract (AXI)

- **TOON on stdout by default.** Lists declare a schema once and render compact
  comma rows; `--json`/`--yaml` are escape hatches only.
- **Errors are structured on stdout**, never cobra-style stderr. An `error:`
  line plus a `help[N]{step}:` hint block; dependency noise (status text, stack
  traces) never leaks.
- **Exit codes:** `0` success (including idempotent no-ops), `1` error, `2`
  usage. `internal/axi.ExitCode` is the single source.
- **Unknown flags are rejected by name** with an inline valid-flag list (exit 2),
  so an agent self-corrects in one turn.
- **Truncation:** descriptions cap at 500 chars; diffs/logs tail-truncate with a
  temp-file `--full` escape hatch; lists show `count:` + a "more available" hint.

### Error translation

Every HTTP status from both Cloud and Data Center maps to a structured
`AxiError` with a stable machine code. `internal/axi/errormap.go` owns the table:

| Condition | Code | Exit |
|---|---|---|
| 404 | `NOT_FOUND` (+ discovery hint) | 1 |
| 401 | `AUTH_REQUIRED` (+ host-aware login instructions) | 1 |
| 403 | `FORBIDDEN` (+ token/scope hints) | 1 |
| 409, approve/merge/reopen residual | `NOOP` | 0 |
| 409, stale DC version | `CONFLICT_WITH_SUGGESTION` (+ retry hint) | 1 |
| 409, plain conflict | `CONFLICT` | 1 |
| 422 | `VALIDATION` | 1 |
| 429 | `RATE_LIMITED` (+ Retry-After countdown when advertised) | 1 |
| 5xx | `UNAVAILABLE` | 1 |
| timeout / unreachable host | `NETWORK_ERROR` (+ connectivity hint) | 1 |

Idempotency is owned by explicit adapter state pre-checks (`dcMutate` GETs the
PR, compares the target state, retries once on a stale-version 409); the
409→no-op path in the error map is the residual safety net. Non-HTTP transport
failures route through `MapTransportError` so a timeout never leaks a raw
`*url.Error`.

---

## Migration from `bkt`

`bkt-axi` is a clean break: TOON is the default output (not human prose), and
several old commands were consolidated. A **deprecation layer**
(`internal/app/deprecated.go`) intercepts the old forms and prints a one-line
notice naming the new command (exit 2), so scripts and agents get pointed at the
right replacement rather than a bare "unknown command":

| Old (`bkt`) | New (`bkt-axi`) | Change |
|---|---|---|
| `pr reviewer-group …` | `pr reviewer …` | reviewer groups + plain reviewers consolidated |
| `pr reaction …` | `pr comment …` | reactions folded into the comment surface |
| `pr suggestion …` | `pr suggestions …` | renamed (singular → plural) |
| `pr auto-merge …` | `pr merge <id> --auto` | auto-merge is now a flag |
| `pr publish <id>` | `pr edit <id> --publish` | publishing (un-drafting) is now a flag |
| `repo browse` | `repo view` | dropped — agents copy URLs; `repo view` prints a `url` field |

Other surface notes for migrators:

- **Output default changed.** `bkt` defaulted to human prose and opted into JSON;
  `bkt-axi` defaults to TOON. Use `--json` for the old machine-readable output.
- **One tree, both platforms.** The old `host.Kind` switch is gone; the same
  command works against Cloud or DC (platform-limited verbs error clearly).
- **Context is the scope resolver.** `--context <name>` (plus `--repo`,
  `--workspace`/`--project` overrides) replaces ad-hoc flag juggling.

The deprecation layer is for discoverability, not full backwards compatibility:
the old verbs are not emulated, only redirected.

---

## Token savings

TOON declares a schema once and renders compact rows; JSON repeats every key per
object. The benchmark in `internal/axi/tokens_bench_test.go` renders the same
fields both ways on representative payloads:

```
go test ./internal/axi/ -run TestTokenSavings -v
```

Measured (compact JSON, the most favorable encoding for JSON):

| payload | TOON chars | JSON chars | saving | saving (token est.) |
|---|---|---|---|---|
| pr list ×30 | 1873 | 2913 | 35.7% | 56.4% |
| repo list ×50 | 1839 | 3108 | 40.8% | 63.0% |

The character-based saving (~36–41%) matches the AXI headline (~40%); the
structure-aware token estimate is higher because it counts JSON's per-symbol
overhead (`"`, `{`, `,`, `:`). Over a session that lists PRs, pipelines, and
branches repeatedly, this is the difference between an agent fitting its context
budget and not.

---

## Architecture

```
cmd/bkt-axi/          entry point (resolves bin path, hands argv to dispatcher)
internal/
  app/                bespoke dispatcher: noun/verb/flag parsing, --help, exit
                      codes, deprecation alias layer
  axi/                AXI primitives: AxiError, field-schema DSL, TOON renderers,
                      truncation, the Bitbucket→AxiError table, suggestion table
  bitbucket/          NORMALIZED domain layer — one Client, one PR model
    cloud/            salvaged Cloud client (was pkg/bbcloud)
    dc/               salvaged Data Center client (was pkg/bbdc)
    httpx/            salvaged HTTP client (was pkg/httpx)
  auth/               salvaged keyring store
  config/             salvaged Host + Context model
  git/                salvaged remote parsing + default-branch inference
  oauth/              salvaged Cloud OAuth flow
  commands/           full command tree + App wiring
```

**The one-switch principle:** every old command switched on `host.Kind`. In
`bkt-axi` that switch lives **once**, inside the adapter
(`internal/bitbucket/*.go`): commands call `client.ListPRs(...)` and get back a
normalized model regardless of Cloud vs Data Center. Never sprinkle cloud/dc
checks into command files — extend the adapter.

## Build & test

```sh
cd bkt-axi
go build ./cmd/bkt-axi && go vet ./... && go test ./...
```

Salvaged client tests must stay green: `go test ./internal/bitbucket/...`.
