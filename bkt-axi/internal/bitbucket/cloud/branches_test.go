package cloud_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ruttybob/bkt-axi/internal/bitbucket/cloud"
)

func TestListBranchesEscapesBBQLFilter(t *testing.T) {
	var capturedURL string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"values": []map[string]any{}})
	}))
	t.Cleanup(server.Close)

	client, err := cloud.New(cloud.Options{BaseURL: server.URL, Username: "u", Token: "t"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.ListBranches(context.Background(), "workspace", "repo", cloud.BranchListOptions{
		Filter: `release "1"\draft`,
	})
	if err != nil {
		t.Fatalf("ListBranches: %v", err)
	}

	parsedURL, err := url.Parse(capturedURL)
	if err != nil {
		t.Fatalf("Parse URL: %v", err)
	}
	rawQuery, _ := url.QueryUnescape(parsedURL.RawQuery)
	if !strings.Contains(rawQuery, `q=name ~ "release \"1\"\\draft"`) {
		t.Fatalf("raw query = %q", rawQuery)
	}
}
