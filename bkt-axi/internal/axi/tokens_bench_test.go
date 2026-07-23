package axi

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// tokens_bench_test.go measures the token budget TOON saves over the old
// `bkt --json` output on representative payloads (spec §11, Appendix A). It is
// the evidence behind the "~40% fewer tokens than JSON" claim: the same fields,
// rendered two ways, counted two ways.
//
// Run:  go test ./internal/axi/ -run TestTokenSavings -v
//       go test ./internal/axi/ -bench Benchmark
//
// Token estimation: there is no offline BPE tokenizer here, so two defensible
// proxies are reported together. Both are computed for every payload so a
// reviewer can sanity-check the headline against either model:
//
//   estChars4  — len(s)/4, the industry-standard "≈4 chars per token" heuristic
//                used by context-budget calculators. Lower bound for structure.
//   estTokens  — count of word / punctuation / whitespace runs via the regex
//                below, which approximates where BPE boundaries fall. This
//                captures JSON's per-symbol overhead (each `"`, `{`, `,`, `:`).
//
// Compact JSON is used (no indentation), the most favorable encoding for JSON,
// so the measured TOON saving is a conservative lower bound.

var tokenApprox = regexp.MustCompile(`[A-Za-z0-9_]+|[^\sA-Za-z0-9_]|\s+`)

func estChars4(s string) float64 { return float64(len(s)) / 4.0 }
func estTokens(s string) int     { return len(tokenApprox.FindAllString(s, -1)) }

// --- representative data ---------------------------------------------------

type prRow struct {
	ID     int
	Title  string
	State  string
	Review string
}

type repoRow struct {
	Slug string
	Name string
	SCM  string
}

// synthPRs builds n pull-request rows with varied, realistic text so neither
// encoding benefits from pathological repetition.
func synthPRs(n int, rng *rand.Rand) []prRow {
	states := []string{"open", "open", "open", "merged", "declined"}
	reviews := []string{"approved", "required", "changes_requested"}
	titles := []string{
		"Fix token refresh on expiry", "Add pagination to repo list",
		"Refactor PR mutations adapter", "Update OAuth callback handling",
		"Truncate long descriptions in TOON", "Guard against stale DC version",
		"Normalize merge strategy aliases", "Wire reviewer subcommands",
	}
	out := make([]prRow, n)
	for i := 0; i < n; i++ {
		out[i] = prRow{
			ID:     1000 + i,
			Title:  titles[rng.Intn(len(titles))] + " #" + strconv.Itoa(1000+i),
			State:  states[rng.Intn(len(states))],
			Review: reviews[rng.Intn(len(reviews))],
		}
	}
	return out
}

func synthRepos(n int, rng *rand.Rand) []repoRow {
	orgs := []string{"acme", "platform", "infra", "data", "edge"}
	names := []string{"api-gateway", "auth-service", "billing", "search-index",
		"webhooks", "ci-runner", "config-loader", "feature-flags"}
	out := make([]repoRow, n)
	for i := 0; i < n; i++ {
		name := names[rng.Intn(len(names))]
		out[i] = repoRow{
			Slug: orgs[rng.Intn(len(orgs))] + "/" + name,
			Name: name,
			SCM:  "git",
		}
	}
	return out
}

// --- encodings of the same data -------------------------------------------

func prRowsAsAny(rows []prRow) []any {
	out := make([]any, len(rows))
	for i, r := range rows {
		out[i] = map[string]any{"id": r.ID, "title": r.Title, "state": r.State, "review": r.Review}
	}
	return out
}

func repoRowsAsAny(rows []repoRow) []any {
	out := make([]any, len(rows))
	for i, r := range rows {
		out[i] = map[string]any{"slug": r.Slug, "name": r.Name, "scm": r.SCM}
	}
	return out
}

var prListSchema = []Field{
	{Key: "id", Extractor: Pluck("id")},
	{Key: "title", Extractor: Pluck("title")},
	{Key: "state", Extractor: Pluck("state")},
	{Key: "review", Extractor: Pluck("review")},
}

var repoListSchema = []Field{
	{Key: "slug", Extractor: Pluck("slug")},
	{Key: "name", Extractor: Pluck("name")},
	{Key: "scm", Extractor: Pluck("scm")},
}

// renderTOON encodes a list the way `bkt-axi` does: one header, comma rows.
func renderTOON(label string, items []any, schema []Field) string {
	return Marshal(NewObject(KV{Key: label, Value: Rows(items, schema)}))
}

// renderJSON encodes the same items as a compact JSON array (the old default).
func renderJSON(items []any) string {
	b, err := json.Marshal(items)
	if err != nil {
		return ""
	}
	return string(b)
}

// savings reports the percent reduction of TOON vs JSON under one estimator.
func savings(jsonN, toonN int) float64 {
	if jsonN == 0 {
		return 0
	}
	return 100.0 * float64(jsonN-toonN) / float64(jsonN)
}

// --- the test + report -----------------------------------------------------

func TestTokenSavings(t *testing.T) {
	rng := rand.New(rand.NewSource(1)) // deterministic fixtures

	cases := []struct {
		name   string
		label  string
		schema []Field
		toon   []any
	}{
		{"pr list x30", "pull_requests", prListSchema, prRowsAsAny(synthPRs(30, rng))},
		{"pr list x10", "pull_requests", prListSchema, prRowsAsAny(synthPRs(10, rng))},
		{"repo list x50", "repositories", repoListSchema, repoRowsAsAny(synthRepos(50, rng))},
		{"repo list x20", "repositories", repoListSchema, repoRowsAsAny(synthRepos(20, rng))},
	}

	var report strings.Builder
	fmt.Fprintln(&report, "token budget: TOON (bkt-axi default) vs compact JSON (old bkt --json)")
	fmt.Fprintln(&report, "same fields, two encodings, two estimators (lower TOON is better)")
	fmt.Fprintln(&report, strings.Repeat("-", 78))
	fmt.Fprintf(&report, "%-16s %8s %8s %8s %8s %10s %10s\n",
		"payload", "toon_ch", "json_ch", "toon_tk", "json_tk", "save_chars", "save_toks")

	aggregateToonTok, aggregateJSONTok := 0, 0
	for _, c := range cases {
		toon := renderTOON(c.label, c.toon, c.schema)
		js := renderJSON(c.toon)

		toonTok := estTokens(toon)
		jsonTok := estTokens(js)
		aggregateToonTok += toonTok
		aggregateJSONTok += jsonTok

		fmt.Fprintf(&report, "%-16s %8d %8d %8d %8d %9.1f%% %9.1f%%\n",
			c.name, len(toon), len(js), toonTok, jsonTok,
			savings(len(js), len(toon)), savings(jsonTok, toonTok))

		// Hard guarantee: TOON must never exceed JSON for these list payloads.
		if len(toon) >= len(js) {
			t.Errorf("%s: TOON chars (%d) not smaller than JSON (%d)", c.name, len(toon), len(js))
		}
		if toonTok >= jsonTok {
			t.Errorf("%s: TOON tokens (%d) not smaller than JSON (%d)", c.name, toonTok, jsonTok)
		}
	}
	fmt.Fprintln(&report, strings.Repeat("-", 78))
	fmt.Fprintf(&report, "aggregate token saving (estTokens): %.1f%%\n",
		savings(aggregateJSONTok, aggregateToonTok))

	t.Log("\n" + report.String())
}

// --- benchmarks for `go test -bench` --------------------------------------

func benchEncode(b *testing.B, enc func() string) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = enc()
	}
}

func BenchmarkPRTOON(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	items := prRowsAsAny(synthPRs(30, rng))
	benchEncode(b, func() string { return renderTOON("pull_requests", items, prListSchema) })
}

func BenchmarkPRJSON(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	items := prRowsAsAny(synthPRs(30, rng))
	benchEncode(b, func() string { return renderJSON(items) })
}

func BenchmarkRepoTOON(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	items := repoRowsAsAny(synthRepos(50, rng))
	benchEncode(b, func() string { return renderTOON("repositories", items, repoListSchema) })
}

func BenchmarkRepoJSON(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	items := repoRowsAsAny(synthRepos(50, rng))
	benchEncode(b, func() string { return renderJSON(items) })
}
