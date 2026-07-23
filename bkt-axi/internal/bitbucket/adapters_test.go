package bitbucket

import (
	"testing"
	"time"

	"github.com/ruttybob/bkt-axi/internal/bitbucket/cloud"
	"github.com/ruttybob/bkt-axi/internal/bitbucket/dc"
)

// adapters_test.go unit-tests the Phase 1 normalized mappers (repo, branch,
// commit, pipeline, build status) the same way pr_test.go covers the PR
// mappers: feed a salvaged client struct, assert the normalized fields.

func TestMapCloudRepo(t *testing.T) {
	r := &cloud.Repository{}
	r.Slug = "api"
	r.Name = "API Service"
	r.SCM = "git"
	r.IsPrivate = true
	r.Workspace.Slug = "acme"
	r.Project.Key = "ENG"
	r.MainBranch.Name = "main"
	r.Links.HTML.Href = "https://bitbucket.org/acme/api"
	r.Links.Clone = []struct {
		Href string `json:"href"`
		Name string `json:"name"`
	}{
		{Href: "https://bitbucket.org/acme/api.git", Name: "https"},
		{Href: "git@bitbucket.org:acme/api.git", Name: "ssh"},
	}
	got := mapCloudRepo(r)
	if got.Slug != "api" || got.Name != "API Service" {
		t.Fatalf("identity mismatch: %+v", got)
	}
	if got.Visibility != "private" {
		t.Fatalf("visibility mismatch: %q", got.Visibility)
	}
	if got.DefaultBranch != "main" {
		t.Fatalf("default branch mismatch: %q", got.DefaultBranch)
	}
	if got.CloneHTTPS != "https://bitbucket.org/acme/api.git" || got.CloneSSH != "git@bitbucket.org:acme/api.git" {
		t.Fatalf("clone url mismatch: https=%q ssh=%q", got.CloneHTTPS, got.CloneSSH)
	}
}

func TestMapDCRepo(t *testing.T) {
	r := &dc.Repository{}
	r.Slug = "api"
	r.Name = "API"
	r.DefaultBranch = "master"
	r.Project = &dc.Project{Key: "ACME"}
	r.Links.Web = []struct {
		Href string `json:"href"`
	}{{Href: "https://bb.example.com/projects/ACME/repos/api"}}
	r.Links.Clone = []struct {
		Href string `json:"href"`
		Name string `json:"name"`
	}{
		{Href: "https://bb.example.com/scm/ac/api.git", Name: "http"},
		{Href: "ssh://git@bb.example.com:7999/ac/api.git", Name: "ssh"},
	}
	got := mapDCRepo(r)
	if got.Project != "ACME" || got.Slug != "api" {
		t.Fatalf("identity mismatch: %+v", got)
	}
	if got.CloneHTTPS != "https://bb.example.com/scm/ac/api.git" {
		t.Fatalf("clone https mismatch: %q", got.CloneHTTPS)
	}
}

func TestMapCloudCommit(t *testing.T) {
	c := &cloud.Commit{}
	c.Hash = "abc1234"
	c.Message = "Fix the thing"
	c.Date = "2024-01-15T10:00:00Z"
	c.Author.Raw = "Ada Lovelace <ada@example.com>"
	c.Author.User.DisplayName = "Ada Lovelace"
	c.Parents = []struct {
		Hash string `json:"hash"`
		Type string `json:"type"`
	}{{Hash: "def5678"}}
	c.Links.HTML.Href = "https://bitbucket.org/acme/api/commits/abc1234"
	got := mapCloudCommit(c)
	if got.SHA != "abc1234" || got.Message != "Fix the thing" {
		t.Fatalf("identity mismatch: %+v", got)
	}
	if got.Author != "Ada Lovelace" || got.AuthorEmail != "ada@example.com" {
		t.Fatalf("author mismatch: %q / %q", got.Author, got.AuthorEmail)
	}
	if len(got.Parents) != 1 || got.Parents[0] != "def5678" {
		t.Fatalf("parents mismatch: %+v", got.Parents)
	}
	if !got.AuthoredAt.Equal(time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("authored_at mismatch: %v", got.AuthoredAt)
	}
}

func TestMapDCCommit(t *testing.T) {
	c := &dc.Commit{}
	c.ID = "abc1234def5678"
	c.DisplayID = "abc1234"
	c.Author.DisplayName = "Grace Hopper"
	c.Author.EmailAddress = "grace@example.com"
	c.Author.Name = "grace"
	c.AuthorTimestamp = 1705312800000
	c.CommitterTimestamp = 1705312900000
	c.Message = "DC fix"
	c.Parents = []struct {
		ID        string `json:"id"`
		DisplayID string `json:"displayId"`
	}{{ID: "ffffffff", DisplayID: "fffffff"}}
	got := mapDCCommit(c)
	if got.Author != "Grace Hopper" || got.AuthorEmail != "grace@example.com" {
		t.Fatalf("author mismatch: %q / %q", got.Author, got.AuthorEmail)
	}
	if got.AuthoredAt.IsZero() {
		t.Fatalf("authored_at should be parsed from epoch millis")
	}
	if len(got.Parents) != 1 || got.Parents[0] != "fffffff" {
		t.Fatalf("parents mismatch: %+v", got.Parents)
	}
}

func TestMapCloudPipeline(t *testing.T) {
	p := &cloud.Pipeline{}
	p.UUID = "{11111111-1111-1111-1111-111111111111}"
	p.BuildNumber = 42
	p.State.Name = "COMPLETED"
	p.State.Result.Name = "SUCCESSFUL"
	p.Target.Ref.Name = "main"
	p.Trigger.Type = "manual"
	p.CreatedOn = "2024-01-15T10:00:00Z"
	p.CompletedOn = "2024-01-15T10:01:30Z"
	got := mapCloudPipeline(p)
	if got.BuildNumber != 42 || got.State != "COMPLETED" || got.Result != "SUCCESSFUL" {
		t.Fatalf("identity mismatch: %+v", got)
	}
	if got.UUID != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("uuid braces should be trimmed: %q", got.UUID)
	}
	if got.Ref != "main" || got.Trigger != "manual" {
		t.Fatalf("ref/trigger mismatch: %q / %q", got.Ref, got.Trigger)
	}
	if got.Duration != "1m30s" {
		t.Fatalf("duration mismatch: %q", got.Duration)
	}
}

func TestMapCloudPipeline_DurationEmptyWhenNotCompleted(t *testing.T) {
	p := &cloud.Pipeline{}
	p.CreatedOn = "2024-01-15T10:00:00Z"
	// CompletedOn empty → in-progress run.
	got := mapCloudPipeline(p)
	if got.Duration != "" {
		t.Fatalf("duration should be empty for an unfinished run, got %q", got.Duration)
	}
}

func TestMapCommitStatus_NormalizesState(t *testing.T) {
	s := &cloud.CommitStatus{State: "SUCCESSFUL", Key: "ci", Name: "CI Build", URL: "https://x", Description: "ok"}
	got := mapCommitStatus(s)
	if got.State != "successful" {
		t.Fatalf("state should be lowercased: %q", got.State)
	}
	if got.Key != "ci" || got.Name != "CI Build" {
		t.Fatalf("identity mismatch: %+v", got)
	}
}

func TestCloudOnly_Error(t *testing.T) {
	err := CloudOnly("pipelines", "Bitbucket Data Center")
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := err.Error(); got != "pipelines is Bitbucket Cloud only; the active host is Bitbucket Data Center" {
		t.Fatalf("cloud-only message mismatch: %q", got)
	}
}

func TestFirstLine(t *testing.T) {
	cases := map[string]string{
		"subject\n\nbody": "subject",
		"  spaced  ":      "spaced",
		"no newline":      "no newline",
		"":                "",
	}
	for in, want := range cases {
		if got := firstLine(in); got != want {
			t.Errorf("firstLine(%q) = %q, want %q", in, got, want)
		}
	}
}
