package axi

import (
	"github.com/toon-format/toon-go"
)

// The TOON encoding library is an implementation detail of this package.
// Commands depend only on the AXI rendering API below; the toon import never
// escapes internal/axi, so an encoder swap touches one package.

// Object is an insertion-ordered TOON object (the only kind that preserves
// field order, which the AXI schemas rely on).
type Object = toon.Object

// KV is one key/value pair of an Object.
type KV = toon.Field

// NewObject builds an insertion-ordered TOON object from the given pairs.
func NewObject(fields ...KV) Object { return toon.NewObject(fields...) }

// Marshal encodes v as a TOON document string using the AXI core profile: no
// length markers, so array headers read name[N] (not name[#N]). The inputs
// here are always toon-encodable (objects, maps, primitives, slices), so a
// marshal failure is surfaced as text rather than crashing the output path.
func Marshal(v any) string {
	out, err := toon.MarshalString(v)
	if err != nil {
		return "error: failed to encode output (" + err.Error() + ")"
	}
	return out
}

// Rows projects items through schema into ordered TOON objects, one per item,
// preserving the schema's column order. Use the result as the value of a KV.
func Rows(items []any, schema []Field) []Object {
	rows := make([]Object, 0, len(items))
	for _, it := range items {
		rows = append(rows, NewObject(Ordered(it, schema)...))
	}
	return rows
}

// HelpRows builds the multiline help block: each hint becomes a single-field
// object so every hint lands on its own line. This is valid, decodable TOON
// that survives commas and colons inside the hint text (which a bare inline
// string array would not). The single field is named "step".
func HelpRows(lines []string) []Object {
	rows := make([]Object, 0, len(lines))
	for _, l := range lines {
		rows = append(rows, NewObject(KV{Key: "step", Value: l}))
	}
	return rows
}

// RenderList renders a collection as a standalone list document:
//
//	<pull_requests>[N]{id,title,state}:
//	  1043,Fix token refresh,open,required
//
// For documents that also need a count or help block, build a NewObject with
// Rows/HelpRows and Marshal it instead.
func RenderList(label string, items []any, schema []Field) string {
	return Marshal(NewObject(KV{Key: label, Value: Rows(items, schema)}))
}

// RenderDetail renders a single item as an ordered object document.
func RenderDetail(label string, item any, schema []Field) string {
	return Marshal(NewObject(KV{Key: label, Value: NewObject(Ordered(item, schema)...)}))
}

// RenderHelp renders a help hint block as a standalone TOON document.
func RenderHelp(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return Marshal(NewObject(KV{Key: "help", Value: HelpRows(lines)}))
}

// RenderError renders an *AxiError to a TOON document on the AXI error
// contract: an `error:` line plus an optional `help[N]{step}:` hint block.
// The machine Code is intentionally omitted from output to match §6; callers
// that need it read e.Code directly.
func RenderError(e *AxiError) string {
	if e == nil {
		return ""
	}
	fields := []KV{{Key: "error", Value: e.Message}}
	if len(e.Suggestions) > 0 {
		fields = append(fields, KV{Key: "help", Value: HelpRows(e.Suggestions)})
	}
	return Marshal(NewObject(fields...))
}
