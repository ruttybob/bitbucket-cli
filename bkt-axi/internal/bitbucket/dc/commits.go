package dc

import (
	"context"
	"fmt"
	"io"
	"net/url"
)

// Commit is a single Bitbucket Data Center commit as returned by
// /rest/api/1.0/projects/{key}/repos/{slug}/commits/{sha}.
type Commit struct {
	ID        string `json:"id"`
	DisplayID string `json:"displayId"`
	Author    struct {
		DisplayName  string `json:"displayName"`
		EmailAddress string `json:"emailAddress"`
		Name         string `json:"name"`
	} `json:"author"`
	AuthorTimestamp    int64  `json:"authorTimestamp"`
	CommitterTimestamp int64  `json:"committerTimestamp"`
	Message            string `json:"message"`
	Parents            []struct {
		ID        string `json:"id"`
		DisplayID string `json:"displayId"`
	} `json:"parents"`
}

// GetCommit fetches a single commit by SHA (or any resolvable ref).
func (c *Client) GetCommit(ctx context.Context, projectKey, repoSlug, sha string) (*Commit, error) {
	if projectKey == "" || repoSlug == "" {
		return nil, fmt.Errorf("project key and repository slug are required")
	}
	if sha == "" {
		return nil, fmt.Errorf("commit SHA is required")
	}

	path := fmt.Sprintf("/rest/api/1.0/projects/%s/repos/%s/commits/%s",
		url.PathEscape(projectKey),
		url.PathEscape(repoSlug),
		url.PathEscape(sha),
	)
	req, err := c.http.NewRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	var commit Commit
	if err := c.http.Do(req, &commit); err != nil {
		return nil, err
	}
	return &commit, nil
}

// CommitDiff streams the raw unified diff between two refs into w.
// from and to can be commit SHAs, branch names, or tags.
func (c *Client) CommitDiff(ctx context.Context, projectKey, repoSlug, from, to string, w io.Writer) error {
	if projectKey == "" {
		return fmt.Errorf("project key is required")
	}
	if repoSlug == "" {
		return fmt.Errorf("repository slug is required")
	}
	if from == "" {
		return fmt.Errorf("from ref is required")
	}
	if to == "" {
		return fmt.Errorf("to ref is required")
	}
	if w == nil {
		return fmt.Errorf("writer is required")
	}

	params := url.Values{}
	params.Set("from", from)
	params.Set("to", to)
	path := fmt.Sprintf("/rest/api/1.0/projects/%s/repos/%s/compare/diff?%s",
		url.PathEscape(projectKey),
		url.PathEscape(repoSlug),
		params.Encode(),
	)

	req, err := c.http.NewRequest(ctx, "GET", path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/plain")

	return c.http.Do(req, w)
}
