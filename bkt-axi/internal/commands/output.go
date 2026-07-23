package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ruttybob/bkt-axi/internal/app"
	"github.com/ruttybob/bkt-axi/internal/axi"
	"gopkg.in/yaml.v3"
)

// output.go centralizes the output-format switch. TOON is the default agent
// path; --json and --yaml are escape hatches that emit the same logical
// payload through the standard encoders. JSON/YAML payloads are built from the
// same schemas/extractors as TOON so field names stay consistent across formats.

// emit marshals payload in the format requested by ctx (toon default). For
// toon, payload must already be a TOON-ready value (axi.Object or compatible);
// for json/yaml, payload is encoded through the standard library.
func emit(ctx *app.Context, payload any, toonDoc string) {
	switch ctx.OutputFormat() {
	case "json":
		writeJSON(ctx, payload)
	case "yaml":
		writeYAML(ctx, payload)
	default:
		io.WriteString(ctx.Out(), toonDoc+"\n")
	}
}

func writeJSON(ctx *app.Context, v any) {
	enc := json.NewEncoder(ctx.Out())
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeYAML(ctx *app.Context, v any) {
	out, err := yaml.Marshal(v)
	if err != nil {
		io.WriteString(ctx.Out(), "error: failed to encode yaml ("+err.Error()+")\n")
		return
	}
	io.WriteString(ctx.Out(), string(out))
}

// toAny converts a typed slice to []any so axi.Rows / axi.Extract can consume it.
func toAny[T any](items []T) []any {
	out := make([]any, len(items))
	for i := range items {
		out[i] = items[i]
	}
	return out
}

// emitError writes an axi error document (used when a command wants to emit a
// non-fatal structured note without returning an error / changing the exit code).
func emitError(ctx *app.Context, e *axi.AxiError) {
	io.WriteString(ctx.Out(), axi.RenderError(e)+"\n")
}

// writeTempOutput writes content to a unique temp file and returns its path.
// Used by large-content commands (commit diff, pipeline logs) so an agent can
// read the complete output from disk without it flooding stdout. The caller
// renders the returned path in a TOON field.
func writeTempOutput(pattern, content string) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.WriteString(f, content); err != nil {
		return "", err
	}
	return f.Name(), nil
}

// parseFields splits a comma-separated --fields value into trimmed, lowercased
// tokens, dropping empties. Duplicates are preserved here; dedup happens in the
// per-command schema builder.
func parseFields(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(strings.ToLower(p)); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// countLine renders the `count:` value for a list. When a definitive total is
// known it reads "N of M total"; when more pages exist but no total, it reads
// "N shown (more available)"; otherwise it is the bare count N.
func countLine(shown, total int, moreAvailable bool) any {
	if total > 0 && total >= shown {
		return fmt.Sprintf("%d of %d total", shown, total)
	}
	if moreAvailable {
		return fmt.Sprintf("%d shown (more available)", shown)
	}
	return shown
}

// rfc3339 renders a time as an RFC3339 string, or "" when zero. Used to give the
// --json/--yaml escape hatch machine timestamps where the TOON view shows
// humanized relative time.
func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
