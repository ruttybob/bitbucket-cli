package commands

import (
	"strings"
	"testing"
)

// home_integration_test.go proves the no-args dashboard renders live PR data
// when auth + a repo resolve, completing the content-first home acceptance.

func TestHome_RendersDashboardWithPRs(t *testing.T) {
	srv := startMockCloud(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, nil) // no args → home
	if code != 0 {
		t.Fatalf("home exit %d: %s", code, out)
	}
	if !strings.Contains(out, "bin: ~/test/bkt-axi") {
		t.Fatalf("home missing bin:\n%s", out)
	}
	if !strings.Contains(out, "description: Bitbucket Cloud and Data Center CLI for agents") {
		t.Fatalf("home missing description:\n%s", out)
	}
	if !strings.Contains(out, "kind: cloud") {
		t.Fatalf("home missing kind:\n%s", out)
	}
	if !strings.Contains(out, "repo: acme/api") {
		t.Fatalf("home missing resolved repo:\n%s", out)
	}
	if !strings.Contains(out, "prs_mine[2]{id,title,state,review}:") {
		t.Fatalf("home missing prs_mine section:\n%s", out)
	}
	if !strings.Contains(out, "prs_review[2]{id,title,author,state}:") {
		t.Fatalf("home missing prs_review section:\n%s", out)
	}
	if !strings.Contains(out, "help[") {
		t.Fatalf("home missing help block:\n%s", out)
	}
}

func TestHome_SetupNudgeWhenNoAuth(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BKT_CONFIG_DIR", dir) // empty config dir → no hosts

	out, code := runAppCapture(t, nil)
	if code != 0 {
		t.Fatalf("home exit %d: %s", code, out)
	}
	if !strings.Contains(out, "Run `bkt-axi auth login` to authenticate") {
		t.Fatalf("missing setup nudge:\n%s", out)
	}
	// A nudge must NOT claim a resolved host/repo.
	if strings.Contains(out, "repo:") {
		t.Fatalf("nudge should not show a resolved repo:\n%s", out)
	}
}

func TestAuthStatus_ShowsConfiguredHost(t *testing.T) {
	srv := startMockCloud(t)
	defer srv.Close()
	writeTestConfig(t, srv.URL)

	out, code := runAppCapture(t, []string{"auth", "status"})
	if code != 0 {
		t.Fatalf("auth status exit %d: %s", code, out)
	}
	if !strings.Contains(out, "hosts[1]{host,kind,base_url,username,method,status}:") {
		t.Fatalf("auth status missing hosts table:\n%s", out)
	}
	if !strings.Contains(out, "testcloud") || !strings.Contains(out, "cloud") {
		t.Fatalf("auth status missing host row:\n%s", out)
	}
}
