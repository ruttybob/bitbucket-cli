package commands

import (
	"io"
	"os"
	"strings"

	"github.com/ruttybob/bkt-axi/internal/app"
	"github.com/ruttybob/bkt-axi/internal/axi"
)

// helpers.go centralizes the TOON/JSON list/detail renderers and the --fields
// validator shared by every Phase 3 command, so each noun stays a thin
// adapter over the normalized layer. The renderers build the TOON document and
// the parallel JSON/YAML payload from the same schema so field names stay
// consistent across formats.

// fieldEntry pairs an allowed --fields token with its schema column, in the
// order tokens should appear in the "allowed --fields values" hint.
type fieldEntry struct {
	Token  string
	Column axi.Field
}

// resolveExtraFields parses a comma-separated --fields value, validates each
// token against allowed (in order), and returns the extra schema columns
// (deduped, preserving first-seen order). An unknown token yields a usage
// error (exit 2) with the allowed-values hint, matching the Phase 0 pattern.
func resolveExtraFields(raw, cmdPath string, allowed []fieldEntry) ([]axi.Field, error) {
	var extras []axi.Field
	seen := map[string]bool{}
	names := make([]string, 0, len(allowed))
	lookup := make(map[string]axi.Field, len(allowed))
	for _, e := range allowed {
		names = append(names, e.Token)
		lookup[e.Token] = e.Column
	}
	for _, raw := range strings.Split(raw, ",") {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			continue
		}
		if seen[name] {
			continue
		}
		col, ok := lookup[name]
		if !ok {
			e := axi.UsageError("unknown --fields value `" + name + "` for `" + cmdPath + "`")
			e.Suggestions = []string{"allowed --fields values: " + strings.Join(names, ", ")}
			return nil, e
		}
		extras = append(extras, col)
		seen[name] = true
	}
	return extras, nil
}

// schemaColumns builds the full column list: base first, then extras.
func schemaColumns(base, extras []axi.Field) []axi.Field {
	out := make([]axi.Field, 0, len(base)+len(extras))
	out = append(out, base...)
	out = append(out, extras...)
	return out
}

// validateFieldTokens parses and validates a comma-separated --fields value
// against allowed (the permitted tokens, in hint order), returning the
// lowercased, deduped token list. Use it when a command needs to build columns
// itself (e.g. a column whose value depends on host kind); otherwise prefer
// resolveExtraFields, which returns ready-made columns.
func validateFieldTokens(raw, cmdPath string, allowed []string) ([]string, error) {
	allowedLower := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		allowedLower[strings.ToLower(strings.TrimSpace(a))] = true
	}
	var out []string
	seen := map[string]bool{}
	for _, raw := range strings.Split(raw, ",") {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			continue
		}
		if seen[name] {
			continue
		}
		if !allowedLower[name] {
			e := axi.UsageError("unknown --fields value `" + name + "` for `" + cmdPath + "`")
			e.Suggestions = []string{"allowed --fields values: " + strings.Join(allowed, ", ")}
			return nil, e
		}
		out = append(out, name)
		seen[name] = true
	}
	return out, nil
}

// emitList renders a collection with a count line, the rows, and an optional
// help block, in TOON by default and JSON/YAML via the escape hatches.
func emitList(ctx *app.Context, label string, items []any, schema []axi.Field, count any, help []string) {
	rows := make([]map[string]any, 0, len(items))
	for _, it := range items {
		rows = append(rows, axi.Extract(it, schema))
	}
	kvs := []axi.KV{
		{Key: "count", Value: count},
		{Key: label, Value: axi.Rows(items, schema)},
	}
	payload := map[string]any{"count": count, label: rows}
	if len(help) > 0 {
		kvs = append(kvs, axi.KV{Key: "help", Value: axi.HelpRows(help)})
		payload["help"] = help
	}
	emit(ctx, payload, axi.Marshal(axi.NewObject(kvs...)))
}

// emitEmpty renders a definitive empty state: a single message field plus an
// optional help block.
func emitEmpty(ctx *app.Context, label, msg string, help []string) {
	kvs := []axi.KV{{Key: label, Value: msg}}
	payload := map[string]any{label: msg}
	if len(help) > 0 {
		kvs = append(kvs, axi.KV{Key: "help", Value: axi.HelpRows(help)})
		payload["help"] = help
	}
	emit(ctx, payload, axi.Marshal(axi.NewObject(kvs...)))
}

// emitDetail renders a single normalized item as an ordered object with an
// optional help block.
func emitDetail(ctx *app.Context, label string, item any, schema []axi.Field, help []string) {
	kvs := []axi.KV{{Key: label, Value: axi.NewObject(axi.Ordered(item, schema)...)}}
	payload := map[string]any{label: axi.Extract(item, schema)}
	if len(help) > 0 {
		kvs = append(kvs, axi.KV{Key: "help", Value: axi.HelpRows(help)})
		payload["help"] = help
	}
	emit(ctx, payload, axi.Marshal(axi.NewObject(kvs...)))
}

// emitConfirmation renders a one-line mutation confirmation.
func emitConfirmation(ctx *app.Context, msg string) {
	emit(ctx, map[string]any{"result": msg}, axi.Marshal(axi.NewObject(axi.KV{Key: "result", Value: msg})))
}

// bodyFromFlags resolves a text body from --body or --body-file. --body-file
// "-" reads stdin. Returns "" when neither flag is set.
func bodyFromFlags(ctx *app.Context) (string, error) {
	if raw := strings.TrimSpace(ctx.Flags.String("body-file")); raw != "" {
		var r io.Reader = os.Stdin
		if raw != "-" {
			f, err := os.Open(raw)
			if err != nil {
				return "", axi.Errorf("cannot read --body-file %q: %s", raw, err)
			}
			defer f.Close()
			r = f
		}
		b, err := io.ReadAll(r)
		if err != nil {
			return "", axi.Errorf("reading --body-file: %s", err)
		}
		return string(b), nil
	}
	return ctx.Flags.String("body"), nil
}
