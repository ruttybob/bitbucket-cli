package axi

import (
	"strings"
	"testing"
	"time"
)

func TestTruncateTail_KeepsTailAndMarksSize(t *testing.T) {
	s := strings.Repeat("ab", 100) // 200 chars
	got := TruncateTail(s, 50)
	// Must keep the last ~50 chars (the tail) and announce the original size.
	if !strings.Contains(got, "truncated") {
		t.Fatalf("expected a truncation marker, got:\n%s", got)
	}
	// The tail content (last 50 chars) must be present; the head must be dropped.
	if !strings.HasSuffix(strings.SplitN(got, "\n", 2)[0], strings.Repeat("ab", 25)) {
		t.Fatalf("expected the preview to end with the tail of the input:\n%s", got)
	}
}

func TestTruncateTail_PassesThroughShortInput(t *testing.T) {
	s := "short diff"
	if got := TruncateTail(s, 50); got != s {
		t.Fatalf("short input should pass through unchanged, got %q", got)
	}
}

func TestTruncateTail_EmptyStaysEmpty(t *testing.T) {
	if got := TruncateTail("", 50); got != "" {
		t.Fatalf("empty input should stay empty, got %q", got)
	}
	// Trailing newlines are stripped before the empty check.
	if got := TruncateTail("\n\n", 50); got != "" {
		t.Fatalf("newline-only input should collapse to empty, got %q", got)
	}
}

func TestExceedsBudget(t *testing.T) {
	if ExceedsBudget("abc", 50) {
		t.Fatalf("short input should not exceed budget")
	}
	if !ExceedsBudget(strings.Repeat("x", 51), 50) {
		t.Fatalf("51-char input should exceed a 50-char budget")
	}
	if ExceedsBudget(strings.Repeat("x", 50), 50) {
		t.Fatalf("50-char input should not exceed a 50-char budget")
	}
	if ExceedsBudget("", 50) {
		t.Fatalf("empty input should not exceed budget")
	}
}

func TestRelativeTime_ZeroIsEmpty(t *testing.T) {
	got := RelativeTime(Pluck("t"))(map[string]any{"t": time.Time{}})
	if got != "" {
		t.Fatalf("zero time should render as empty, got %q", got)
	}
}
