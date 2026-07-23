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
	// tailTruncatedSuffix is appended to tail-truncated content: original total
	// and how many leading chars were dropped. The leading "\n  " keeps the note
	// on its own indented line so TOON per-line quoting of the field is intact.
	tailTruncatedSuffix = "\n  ...(truncated, %d chars total, showing last %d)"
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

// TruncateDiff truncates diff/patch text the same way TruncateBody does (head).
// Diffs are usually much larger than bodies, so callers pass a larger budget.
func TruncateDiff(s string, budget int) string {
	return truncateContent(s, budget, false)
}

// TruncateTail keeps the LAST budget runes of s and, when it cut content, prepends an
// inline note stating the original size. Diff and log output are most useful at the
// tail (the actual changes / the final error lines), so large-content commands use
// this instead of the head-truncating TruncateBody.
func TruncateTail(s string, budget int) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	if budget <= 0 {
		budget = DefaultBodyBudget
	}
	total := utf8.RuneCountInString(s)
	if total <= budget {
		return s
	}
	runes := []rune(s)
	tail := strings.TrimLeft(string(runes[len(runes)-budget:]), " \t\r\n")
	return tail + fmt.Sprintf(tailTruncatedSuffix, total, total-budget)
}

// ExceedsBudget reports whether s has more than budget runes (after trimming
// trailing newlines), i.e. whether TruncateTail would cut content. Commands use
// it to decide whether to attach a `--full` hint without re-counting themselves.
func ExceedsBudget(s string, budget int) bool {
	s = strings.TrimRight(s, "\n")
	if s == "" || budget <= 0 {
		return false
	}
	return utf8.RuneCountInString(s) > budget
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
