---
name: bkt-axi
description: >
  Bitbucket Cloud and Data Center CLI for agents. Use when users need to manage
  repositories, pull requests, branches, commits, pipelines, issues, webhooks,
  variables, projects, permissions, or admin in Bitbucket. Triggers include
  "bitbucket", "bkt-axi", "pull request", "PR", "repo", "branch", "commit",
  "pipeline", "webhook", "Bitbucket Cloud", "Bitbucket Data Center".
---

# bkt-axi

bkt-axi is a TOON-first Bitbucket Cloud and Data Center CLI for autonomous
agents. Output is [TOON](https://toonformat.dev) by default (~40% fewer tokens
than JSON); `--json`/`--yaml` are escape hatches. It resolves the current
directory's git remote and shows state for that repository.

## Setup

Verify the binary is installed and authenticated before running commands:

```sh
bkt-axi
bkt-axi auth status
```

If `bkt-axi` is not on PATH, install it or use its absolute path. Authenticate a
host with `bkt-axi auth login`, or set `BKT_HOST` and `BKT_TOKEN` for headless use.

## Commands

The nouns below are the full command surface. Run `bkt-axi <noun> --help` for
each noun's verbs, flags, and examples; grouped nouns (e.g. `pr`) descend
further (`pr reviewer list <id>`, `perms project grant ...`). This list is generated
from the CLI's own root help, so it cannot drift:

```toon
commands[16]{name,description}:
  pr,Work with pull requests
  auth,Manage authentication and hosts
  repo,Work with repositories
  branch,Work with branches
  commit,Work with commits
  pipeline,Work with pipelines (Bitbucket Cloud)
  issue,Work with issues (Cloud)
  webhook,Manage repository webhooks
  variable,Manage pipeline variables (Cloud)
  project,Work with projects (Data Center)
  status,CI/build status and rate-limit rollup
  perms,Manage user permissions (Data Center)
  admin,Server administration (Data Center)
  api,Raw API passthrough
  context,Manage configuration contexts
  setup,Install or repair session hooks and skill
```

## Ambient context

Run `bkt-axi setup` to install a session hook (Claude Code, Codex, OpenCode)
that injects the live home view — your open pull requests and those awaiting
your review — at the start of every session, with no invocation needed. The
hook is directory-scoped and token-budget-aware.

## Notes

- Output is TOON by default; pass `--json` or `--yaml` for structured payloads.
- Some nouns are platform-restricted: `pipeline`, `issue`, and `variable` are
  Bitbucket Cloud only; `perms` and `admin` are Data Center only. A restricted
  command names the active host in its error.
- Mutations are idempotent: approving an already-approved PR or merging an
  already-merged one is a no-op that exits 0.
- Long content (diffs, logs) renders a tail-truncated preview by default;
  `--full` writes the complete output to a temp file and points at it.
