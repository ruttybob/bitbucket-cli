package commands

import (
	"encoding/json"
	"io"

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
