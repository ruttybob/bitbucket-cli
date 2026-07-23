package session

// skill.go generates the installable SKILL.md from the same content the CLI
// prints (AXI §7 "single source of truth"). extractCommandsBlock pulls the
// commands[N] TOON block out of the app's root help, so the committed skill can
// never drift from the CLI's own command surface. A CI freshness check
// (commands/skill_freshness_test.go) regenerates the skill and fails when the
// committed file is stale.
//
// The skill is static: it carries no live state (no open PRs, no counts), only
// the command surface and orienting guidance. Command examples use the PATH
// form `bkt-axi` since a skill may be installed without the binary on PATH; the
// Setup section tells the agent how to verify the install.

import (
	"strings"

	"github.com/ruttybob/bkt-axi/internal/app"
)

// SkillContent returns the generated SKILL.md text for the given app. The
// output is fully deterministic — it depends only on the registered command
// tree, not on the binary path or any live state — so it can be committed and
// CI-checked for freshness.
func SkillContent(a *app.App) string {
	commands := extractCommandsBlock(a.RootHelp())
	var b strings.Builder
	b.WriteString(skillFrontmatter)
	b.WriteString(skillBody)
	b.WriteString("```toon\n")
	b.WriteString(commands)
	b.WriteString("\n```\n\n")
	b.WriteString(skillTail)
	return b.String()
}

// extractCommandsBlock pulls the commands[N]{...} TOON block (the header line
// plus its indented rows) out of a root-help string. This is the join between
// "what the CLI prints" and "what the skill documents", so the skill can never
// list a command the CLI does not have (or omit one it does).
func extractCommandsBlock(rootHelp string) string {
	var (
		out      strings.Builder
		inBlock  bool
		wroteAny bool
	)
	for _, line := range strings.Split(rootHelp, "\n") {
		if !inBlock {
			if strings.HasPrefix(line, "commands[") {
				inBlock = true
				out.WriteString(line)
				out.WriteByte('\n')
				wroteAny = true
			}
			continue
		}
		// The block continues while rows remain indented (TOON list rows are
		// two-space indented). A blank or unindented line ends the block.
		if line == "" || !strings.HasPrefix(line, "  ") {
			break
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	res := strings.TrimRight(out.String(), "\n")
	if !wroteAny {
		return ""
	}
	return res
}

// WriteSkill writes the generated SKILL.md into dir, returning the full path.
// It is idempotent: a directory that already holds an identical skill is a
// no-op.
func WriteSkill(a *app.App, dir string) (string, Action, error) {
	path := dir + "/SKILL.md"
	action, err := writeIfChanged(path, []byte(SkillContent(a)))
	return path, action, err
}

// SkillDirFor returns the conventional skill directory for an app, where a
// harness loads installable skills on demand. AppAll is invalid here (a single
// concrete harness is required).
func SkillDirFor(app App) (string, error) {
	switch app {
	case AppClaude:
		home, err := userHome()
		if err != nil {
			return "", err
		}
		return home + "/.claude/skills/bkt-axi", nil
	case AppCodex:
		home, err := userHome()
		if err != nil {
			return "", err
		}
		return home + "/.codex/skills/bkt-axi", nil
	case AppOpenCode:
		return configHome() + "/opencode/skills/bkt-axi", nil
	default:
		return "", errUnsupportedSkillApp
	}
}

// skillFrontmatter is the trigger-shaped YAML frontmatter (AXI §7).
const skillFrontmatter = `---
name: bkt-axi
description: >
  Bitbucket Cloud and Data Center CLI for agents. Use when users need to manage
  repositories, pull requests, branches, commits, pipelines, issues, webhooks,
  variables, projects, permissions, or admin in Bitbucket. Triggers include
  "bitbucket", "bkt-axi", "pull request", "PR", "repo", "branch", "commit",
  "pipeline", "webhook", "Bitbucket Cloud", "Bitbucket Data Center".
---

# bkt-axi

`

// skillBody is the static intro + setup + commands preamble.
const skillBody = `bkt-axi is a TOON-first Bitbucket Cloud and Data Center CLI for autonomous
agents. Output is [TOON](https://toonformat.dev) by default (~40% fewer tokens
than JSON); ` + "`--json`" + `/` + "`--yaml`" + ` are escape hatches. It resolves the current
directory's git remote and shows state for that repository.

## Setup

Verify the binary is installed and authenticated before running commands:

` + "```sh\nbkt-axi\nbkt-axi auth status\n```" + `

If ` + "`bkt-axi`" + ` is not on PATH, install it or use its absolute path. Authenticate a
host with ` + "`bkt-axi auth login`" + `, or set ` + "`BKT_HOST`" + ` and ` + "`BKT_TOKEN`" + ` for headless use.

## Commands

The nouns below are the full command surface. Run ` + "`bkt-axi <noun> --help`" + ` for
each noun's verbs, flags, and examples; grouped nouns (e.g. ` + "`pr`" + `) descend
further (` + "`pr reviewer list <id>`" + `, ` + "`perms project grant ...`" + `). This list is generated
from the CLI's own root help, so it cannot drift:

`

// skillTail is the ambient-context + notes section after the command block.
const skillTail = `## Ambient context

Run ` + "`bkt-axi setup`" + ` to install a session hook (Claude Code, Codex, OpenCode)
that injects the live home view — your open pull requests and those awaiting
your review — at the start of every session, with no invocation needed. The
hook is directory-scoped and token-budget-aware.

## Notes

- Output is TOON by default; pass ` + "`--json`" + ` or ` + "`--yaml`" + ` for structured payloads.
- Some nouns are platform-restricted: ` + "`pipeline`" + `, ` + "`issue`" + `, and ` + "`variable`" + ` are
  Bitbucket Cloud only; ` + "`perms`" + ` and ` + "`admin`" + ` are Data Center only. A restricted
  command names the active host in its error.
- Mutations are idempotent: approving an already-approved PR or merging an
  already-merged one is a no-op that exits 0.
- Long content (diffs, logs) renders a tail-truncated preview by default;
  ` + "`--full`" + ` writes the complete output to a temp file and points at it.
`

// errUnsupportedSkillApp is returned for AppAll, which needs a concrete harness.
var errUnsupportedSkillApp = errString("skill install requires a single app (claude, codex, or opencode), not all")

// errString lets a package-level error value carry a message without an extra
// type; it is only ever compared by value within this package.
type errString string

func (e errString) Error() string { return string(e) }
