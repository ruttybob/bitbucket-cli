package bitbucket

import (
	"testing"

	"github.com/ruttybob/bkt-axi/internal/bitbucket/httpx"
)

// pr_mutations_test.go covers the pure adapter helpers that the integration
// tests do not exercise directly: merge-strategy alias normalization (per
// host), reviewer set merging, and the stale-version classifier.

func TestNormalizeMergeStrategy(t *testing.T) {
	cases := []struct {
		kind Kind
		in   string
		want string
	}{
		{KindCloud, "", ""},
		{KindDC, "", ""},
		{KindCloud, "squash", "squash"},
		{KindDC, "squash", "squash"},
		{KindCloud, "merge", "merge_commit"},
		{KindDC, "merge", "no-ff"},
		{KindCloud, "rebase", "rebase_fast_forward"},
		{KindDC, "rebase", "rebase-no-ff"},
		{KindCloud, "SQUASH", "squash"},             // aliases are case-insensitive
		{KindCloud, "fast_forward", "fast_forward"}, // unknown → pass-through (exact API id)
		{KindDC, "some-raw-id", "some-raw-id"},
	}
	for _, c := range cases {
		got := normalizeMergeStrategy(c.kind, c.in)
		if got != c.want {
			t.Errorf("normalizeMergeStrategy(%s, %q) = %q, want %q", c.kind, c.in, got, c.want)
		}
	}
}

func TestMergeReviewersDedupesCaseInsensitively(t *testing.T) {
	got := mergeReviewers([]string{"alice", "Bob"}, []string{"ALICE", "carol"})
	want := []string{"alice", "Bob", "carol"}
	if len(got) != len(want) {
		t.Fatalf("mergeReviewers got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("mergeReviewers[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestMergeReviewerChanges(t *testing.T) {
	// current=[alice,bob,carol] add=[dave,Alice] remove=[BOB]
	// → drop bob, drop the "Alice" dup against existing alice, keep alice+carol+dave.
	got := mergeReviewerChanges([]string{"alice", "bob", "carol"}, []string{"dave", "Alice"}, []string{"BOB"})
	want := []string{"alice", "carol", "dave"}
	if len(got) != len(want) {
		t.Fatalf("mergeReviewerChanges got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("mergeReviewerChanges[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestIsStaleVersion(t *testing.T) {
	if !isStaleVersion(&httpx.HTTPError{StatusCode: 409}) {
		t.Errorf("409 should be classified as a stale version")
	}
	if isStaleVersion(&httpx.HTTPError{StatusCode: 404}) {
		t.Errorf("404 should not be classified as a stale version")
	}
	if isStaleVersion(nil) {
		t.Errorf("nil should not be classified as a stale version")
	}
}
