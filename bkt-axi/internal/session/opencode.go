package session

// opencode.go installs the OpenCode plugin.
//
// OpenCode loads JavaScript/TypeScript modules from ~/.config/opencode/plugins/
// at startup. The `experimental.chat.system.transform` hook receives the
// system-prompt string array and lets a plugin append ambient context, which is
// OpenCode's idiomatic way to inject the home view as system context (rather
// than adding a custom tool). The plugin runs `bkt-axi` once at load and pushes
// the cached result into every chat, so live state is fetched once per session
// rather than per turn.

import (
	"path/filepath"
	"strings"
)

const opencodePluginFile = "bkt_axi.ts"

// installOpenCode writes the managed plugin under the OpenCode plugins dir.
// Idempotent and path-repairing: the file is regenerated from the resolved
// command, so a moved/reinstalled binary is repaired on the next `setup`.
func installOpenCode(binPath string) (Result, error) {
	dir := filepath.Join(configHome(), "opencode", "plugins")
	path := filepath.Join(dir, opencodePluginFile)
	res := Result{App: AppOpenCode, Path: displayPath(path)}

	desired := []byte(opencodePluginSource(ResolveCommand(binPath)))
	action, err := writeIfChanged(path, desired)
	if err != nil {
		return res, err
	}
	res.Action = action
	return res, nil
}

// opencodePluginSource renders the managed plugin for the given command. The
// command is the only varying piece; everything else is static, so a byte
// comparison detects both a missing plugin and a stale path.
func opencodePluginSource(command string) string {
	bt := "`" // Bun shell template literal delimiter
	var b strings.Builder
	b.WriteString("// Managed by " + bt + "bkt-axi setup --app opencode" + bt + ". Re-run setup after moving or\n")
	b.WriteString("// reinstalling bkt-axi; do not edit by hand (your changes are overwritten).\n")
	b.WriteString("import type { Plugin } from \"@opencode-ai/plugin\"\n\n")
	b.WriteString("// Resolved at install time: the bare name when bkt-axi is on PATH and resolves\n")
	b.WriteString("// to the installed binary, else the absolute path.\n")
	b.WriteString("const BKT_AXI = " + bt + command + bt + "\n\n")
	b.WriteString("// Cached once per OpenCode startup so live state is fetched once, not per turn.\n")
	b.WriteString("let cached = \"\"\n\n")
	b.WriteString("async function refresh(ctx: any) {\n")
	b.WriteString("  try {\n")
	b.WriteString("    cached = (await ctx." + bt + "${BKT_AXI}" + bt + ").text().trim()\n")
	b.WriteString("  } catch {\n")
	b.WriteString("    cached = \"\" // bkt-axi missing or no auth yet; stay silent (AXI §6)\n")
	b.WriteString("  }\n")
	b.WriteString("}\n\n")
	b.WriteString("export const BktAxiPlugin: Plugin = async (ctx) => {\n")
	b.WriteString("  await refresh(ctx)\n")
	b.WriteString("  return {\n")
	b.WriteString("    // Inject the bkt-axi home view as ambient system context. This fires on\n")
	b.WriteString("    // every chat; the body is the cached startup snapshot so it is cheap.\n")
	b.WriteString("    \"experimental.chat.system.transform\": async (_input, output) => {\n")
	b.WriteString("      if (cached) output.system.push(\"# bkt-axi\\n\\n\" + cached)\n")
	b.WriteString("    },\n")
	b.WriteString("  }\n")
	b.WriteString("}\n")
	return b.String()
}
