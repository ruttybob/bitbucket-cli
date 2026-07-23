package axi

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/toon-format/toon-go"
)

// Field is one column in a TOON schema: a lowercase snake_case name and the
// extractor that derives its value from a single item. Schema order becomes
// the TOON array header order (name[N]{a,b,c}), so declare columns in the
// order agents should read them.
type Field struct {
	Key       string
	Extractor Extractor
}

// Extractor derives a scalar TOON value (string, int, bool, …) from one item.
// Items are typically the normalized domain models (see internal/bitbucket),
// but maps and pointers are handled so the same schema works on JSON-decoded
// payloads.
type Extractor func(item any) any

// Const returns an extractor that always yields v.
func Const(v any) Extractor { return func(_ any) any { return v } }

// Custom wraps an arbitrary function as an extractor.
func Custom(fn func(item any) any) Extractor { return fn }

// Extract projects one item through a schema into a value map keyed by the
// schema field names. Order is preserved by the schema itself (see Ordered).
func Extract(item any, schema []Field) map[string]any {
	out := make(map[string]any, len(schema))
	for _, f := range schema {
		out[f.Key] = applyExtractor(f, item)
	}
	return out
}

// Ordered projects one item through a schema into ordered TOON field pairs,
// preserving the schema's column order for deterministic rendering.
func Ordered(item any, schema []Field) []toon.Field {
	out := make([]toon.Field, 0, len(schema))
	for _, f := range schema {
		out = append(out, toon.Field{Key: f.Key, Value: applyExtractor(f, item)})
	}
	return out
}

func applyExtractor(f Field, item any) any {
	ex := f.Extractor
	if ex == nil {
		ex = Pluck(f.Key)
	}
	return ex(item)
}

// --- Extractor DSL -------------------------------------------------------

// Pluck reads a value from an item by key. Keys map to struct fields through
// their `toon` or `json` tags (snake_case) or, for maps, the literal key.
// Dotted keys ("author.name") descend into nested structs and maps.
func Pluck(key string) Extractor {
	return func(item any) any { return pluckPath(item, key) }
}

// Lower lowercases a string-valued extractor's result. Non-strings pass through.
func Lower(e Extractor) Extractor {
	return compose(e, func(v any) any {
		if s, ok := v.(string); ok {
			return strings.ToLower(s)
		}
		return v
	})
}

// BoolYesNo renders a bool as "yes"/"no" (and nil as "").
func BoolYesNo(e Extractor) Extractor {
	return compose(e, func(v any) any {
		switch b := v.(type) {
		case bool:
			if b {
				return "yes"
			}
			return "no"
		case *bool:
			if b == nil {
				return ""
			}
			if *b {
				return "yes"
			}
			return "no"
		}
		return v
	})
}

// MapEnum translates a value through a table; unknown values pass through
// unchanged so the agent still sees the raw state.
func MapEnum(e Extractor, table map[string]string) Extractor {
	return compose(e, func(v any) any {
		if s, ok := v.(string); ok {
			if mapped, ok := table[s]; ok {
				return mapped
			}
		}
		return v
	})
}

// JoinArray joins a string slice with sep. Non-slice values pass through.
func JoinArray(e Extractor, sep string) Extractor {
	return compose(e, func(v any) any {
		switch s := v.(type) {
		case []string:
			return strings.Join(s, sep)
		case []any:
			parts := make([]string, 0, len(s))
			for _, p := range s {
				parts = append(parts, fmt.Sprint(p))
			}
			return strings.Join(parts, sep)
		}
		return v
	})
}

// RelativeTime renders a time value as a compact "3h ago" / "in 2d" string.
// Accepts time.Time, unix seconds, unix milliseconds, and RFC3339 strings.
// Unparseable values pass through unchanged.
func RelativeTime(e Extractor) Extractor {
	return compose(e, func(v any) any {
		t, ok := toTime(v)
		if !ok {
			return v
		}
		return humanizeTime(t)
	})
}

// ChecksSummary renders a Checks aggregate ("3/3 passed", "1/2 failed") from a
// value exposing Total/Passed/Failed int fields or a map with those keys.
func ChecksSummary(e Extractor) Extractor {
	return compose(e, func(v any) any {
		total, passed, failed, ok := readChecks(v)
		if !ok {
			return v
		}
		return checksLabel(total, passed, failed)
	})
}

// compose chains two extractors: first e, then transform on its result.
func compose(e Extractor, transform func(any) any) Extractor {
	if e == nil {
		return nil
	}
	return func(item any) any { return transform(e(item)) }
}

// --- reflection helpers --------------------------------------------------

// pluckPath resolves a possibly-dotted key against structs (by toon/json tag
// or name) and maps (by literal key). Returns nil when any segment is absent.
func pluckPath(item any, key string) any {
	current := item
	for _, seg := range strings.Split(key, ".") {
		val := pluckSegment(current, seg)
		if val == nil {
			return nil
		}
		current = val
	}
	return current
}

func pluckSegment(item any, key string) any {
	v := deref(reflect.ValueOf(item))
	switch v.Kind() {
	case reflect.Map:
		if ev := v.MapIndex(reflect.ValueOf(key)); ev.IsValid() {
			return ev.Interface()
		}
		return nil
	case reflect.Struct:
		return pluckStruct(v, key)
	}
	return nil
}

// pluckStruct finds a struct field whose toon/json tag or name matches key
// (snake_case). Tag-exact first, then case-insensitive with snake→camel
// folding so "created_at" matches "CreatedAt".
func pluckStruct(v reflect.Value, key string) any {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if tag := fieldTag(f, "toon"); tag == key {
			return v.Field(i).Interface()
		}
		if tag := fieldTag(f, "json"); tag == key {
			return v.Field(i).Interface()
		}
	}
	want := snakeToCamel(key)
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if strings.EqualFold(f.Name, want) || strings.EqualFold(f.Name, key) {
			return v.Field(i).Interface()
		}
	}
	return nil
}

func fieldTag(f reflect.StructField, tag string) string {
	raw := f.Tag.Get(tag)
	if i := strings.IndexByte(raw, ','); i >= 0 {
		raw = raw[:i]
	}
	return raw
}

func snakeToCamel(s string) string {
	var b strings.Builder
	for _, p := range strings.Split(s, "_") {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		if len(p) > 1 {
			b.WriteString(p[1:])
		}
	}
	return b.String()
}

func deref(v reflect.Value) reflect.Value {
	for v.IsValid() && v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	return v
}

func toTime(v any) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return t, true
	case *time.Time:
		if t == nil {
			return time.Time{}, false
		}
		return *t, true
	case string:
		if ts, err := time.Parse(time.RFC3339, t); err == nil {
			return ts, true
		}
	case int64:
		return unixSecondsOrMillis(t), true
	case int:
		return unixSecondsOrMillis(int64(t)), true
	case float64:
		return unixSecondsOrMillis(int64(t)), true
	}
	return time.Time{}, false
}

// unixSecondsOrMillis splits seconds from milliseconds: values past the year
// 2286 in seconds are almost certainly millisecond epochs.
func unixSecondsOrMillis(v int64) time.Time {
	if v > 1_000_000_000_000 {
		return time.UnixMilli(v)
	}
	return time.Unix(v, 0)
}

func readChecks(v any) (total, passed, failed int, ok bool) {
	rv := deref(reflect.ValueOf(v))
	switch rv.Kind() {
	case reflect.Struct:
		total = intField(rv, "Total")
		passed = intField(rv, "Passed")
		failed = intField(rv, "Failed")
	case reflect.Map:
		total = mapInt(rv, "total")
		passed = mapInt(rv, "passed")
		failed = mapInt(rv, "failed")
	}
	ok = total > 0 || passed > 0 || failed > 0
	return
}

func intField(rv reflect.Value, name string) int {
	f := rv.FieldByName(name)
	if !f.IsValid() {
		return 0
	}
	switch f.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return int(f.Int())
	}
	return 0
}

func mapInt(rv reflect.Value, key string) int {
	ev := rv.MapIndex(reflect.ValueOf(key))
	if !ev.IsValid() {
		return 0
	}
	switch n := ev.Interface().(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

func checksLabel(total, passed, failed int) string {
	if total == 0 {
		return ""
	}
	if failed == 0 {
		return fmt.Sprintf("%d/%d passed", passed, total)
	}
	return fmt.Sprintf("%d/%d failed", failed, total)
}

func humanizeTime(t time.Time) string {
	// A zero time means "absent" (e.g. a Cloud commit with no committer
	// timestamp); render it as empty rather than the ~2000-year "54y ago" that
	// time.Since(zero) would otherwise produce.
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	abs := d
	future := false
	if abs < 0 {
		abs = -abs
		future = true
	}
	var s string
	switch {
	case abs < time.Minute:
		return "just now"
	case abs < time.Hour:
		s = fmt.Sprintf("%dm", int(abs.Minutes()))
	case abs < 24*time.Hour:
		s = fmt.Sprintf("%dh", int(abs.Hours()))
	case abs < 30*24*time.Hour:
		s = fmt.Sprintf("%dd", int(abs.Hours()/24))
	case abs < 365*24*time.Hour:
		s = fmt.Sprintf("%dmo", int(abs.Hours()/(24*30)))
	default:
		s = fmt.Sprintf("%dy", int(abs.Hours()/(24*365)))
	}
	if future {
		return "in " + s
	}
	return s + " ago"
}
