package bitbucket

import (
	"testing"
	"time"

	"github.com/ruttybob/bkt-axi/internal/bitbucket/cloud"
	"github.com/ruttybob/bkt-axi/internal/bitbucket/dc"
)

func TestMapCloudPR(t *testing.T) {
	approved := true
	pr := &cloud.PullRequest{}
	pr.ID = 1043
	pr.Title = "Fix token refresh"
	pr.State = "OPEN"
	pr.Draft = false
	pr.Author.DisplayName = "Ada Lovelace"
	pr.Author.Username = "ada"
	pr.Source.Branch.Name = "feature/token"
	pr.Destination.Branch.Name = "main"
	pr.Description = "Refresh tokens on expiry"
	pr.Links.HTML.Href = "https://bitbucket.org/acme/api/pull-requests/1043"
	pr.CreatedOn = "2024-01-15T10:00:00Z"
	pr.Participants = []cloud.PullRequestParticipant{
		{Role: "REVIEWER", Approved: &approved},
	}
	pr.Reviewers = []cloud.User{{Nickname: "bob"}}

	got := mapCloudPR(pr)
	if got.ID != 1043 || got.Title != "Fix token refresh" {
		t.Fatalf("id/title mismatch: %+v", got)
	}
	if got.State != "open" {
		t.Fatalf("state not lowercased: %q", got.State)
	}
	if got.Author != "Ada Lovelace" {
		t.Fatalf("author mismatch: %q", got.Author)
	}
	if got.From != "feature/token" || got.To != "main" {
		t.Fatalf("branch mismatch: from=%q to=%q", got.From, got.To)
	}
	if got.Review != "approved" {
		t.Fatalf("expected approved review, got %q", got.Review)
	}
	if got.URL != "https://bitbucket.org/acme/api/pull-requests/1043" {
		t.Fatalf("url mismatch: %q", got.URL)
	}
	if !got.CreatedAt.Equal(time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("created_at mismatch: %v", got.CreatedAt)
	}
	if len(got.Reviewers) != 1 || got.Reviewers[0] != "bob" {
		t.Fatalf("reviewers mismatch: %+v", got.Reviewers)
	}
}

func TestMapCloudPR_ReviewRequiredByDefault(t *testing.T) {
	pr := &cloud.PullRequest{}
	pr.State = "OPEN"
	got := mapCloudPR(pr)
	if got.Review != "required" {
		t.Fatalf("expected required, got %q", got.Review)
	}
}

func TestMapDCPR(t *testing.T) {
	ms := int64(1705312800000) // 2024-01-15T10:00:00Z in ms
	pr := &dc.PullRequest{}
	pr.ID = 7
	pr.Title = "DC fix"
	pr.State = "OPEN"
	pr.Author.User.FullName = "Grace Hopper"
	pr.Author.User.Name = "grace"
	pr.FromRef.DisplayID = "feature/x"
	pr.ToRef.DisplayID = "master"
	pr.Description = "A description"
	pr.CreatedDate = ms
	pr.Links.Self = []struct {
		Href string `json:"href"`
	}{{Href: "https://bb.example.com/projects/ACME/repos/api/pull-requests/7"}}
	approved := true
	pr.Reviewers = []dc.PullRequestReviewer{{User: dc.User{Name: "rev1"}, Approved: &approved}}

	got := mapDCPR(pr)
	if got.ID != 7 || got.Title != "DC fix" {
		t.Fatalf("id/title mismatch: %+v", got)
	}
	if got.Author != "Grace Hopper" {
		t.Fatalf("author mismatch: %q", got.Author)
	}
	if got.From != "feature/x" || got.To != "master" {
		t.Fatalf("branch mismatch: from=%q to=%q", got.From, got.To)
	}
	if got.Review != "approved" {
		t.Fatalf("expected approved, got %q", got.Review)
	}
	if got.URL != "https://bb.example.com/projects/ACME/repos/api/pull-requests/7" {
		t.Fatalf("url mismatch: %q", got.URL)
	}
	if !got.CreatedAt.Equal(time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("created_at mismatch: %v", got.CreatedAt)
	}
}

func TestSummarizeReview(t *testing.T) {
	cases := []struct {
		approved, changes bool
		want              ReviewSummary
	}{
		{false, false, ReviewRequired},
		{true, false, ReviewApproved},
		{false, true, ReviewChanges},
		{true, true, ReviewChanges}, // changes dominate
	}
	for _, c := range cases {
		if got := summarizeReview(c.approved, c.changes); got != c.want {
			t.Fatalf("summarizeReview(%v,%v)=%s want %s", c.approved, c.changes, got, c.want)
		}
	}
}

func TestScopeString(t *testing.T) {
	if got := (Scope{Workspace: "acme", RepoSlug: "api"}).String(); got != "acme/api" {
		t.Fatalf("cloud scope string: %q", got)
	}
	if got := (Scope{ProjectKey: "ACME", RepoSlug: "api"}).String(); got != "ACME/api" {
		t.Fatalf("dc scope string: %q", got)
	}
	if (Scope{}).Empty() == false {
		t.Fatal("empty scope should report Empty")
	}
}
