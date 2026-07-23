package axi

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Default truncation budgets (§3 Content truncation). Bodies and descriptions
// are large and rarely needed in full; previews let the agent decide whether
// the follow-up --full call is worth it.
const (
	DefaultBodyBudget = 500
	truncatedSuffix   = "\n  ...(truncated, %d chars total)"
)

// TruncateBody shortens s to at most budget runes and, when it actually cut
// content, appends an inline note stating the original size and implying the
// --full escape hatch. s is returned unchanged when it already fits. A blank
// string returns blank (truncation is a content property, not a placeholder).
//
// The note is folded onto its own indented line so TOON's per-line quoting for
// the field value is unaffected: the agent sees the preview, then the size.
func TruncateBody(s string, budget int) string {
	return truncateContent(s, budget, false)
}

// TruncateDiff truncates diff/patch text the same way TruncateBody does. Diffs
// are usually much larger than bodies, so callers pass a larger budget.
func TruncateDiff(s string, budget int) string {
	return truncateContent(s, budget, false)
}

// truncateContent performs rune-aware truncation. forceNote is reserved for
// callers that always want the size note (e.g. when the caller knows more
// exists beyond a page boundary even if the local string fits the budget).
func truncateContent(s string, budget int, forceNote bool) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	if budget <= 0 {
		budget = DefaultBodyBudget
	}
	total := utf8.RuneCountInString(s)
	if total <= budget && !forceNote {
		return s
	}
	if budget > total {
		budget = total
	}
	runes := []rune(s)
	preview := strings.TrimRight(string(runes[:budget]), " \t\r\n")
	return preview + fmt.Sprintf(truncatedSuffix, total)
}
