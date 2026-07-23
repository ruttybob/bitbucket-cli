package cloud

import (
	"context"
	"fmt"
	"io"
	"net/url"
)

// Commit is a single Bitbucket Cloud commit as returned by
// /repositories/{workspace}/{repo_slug}/commit/{sha}.
type Commit struct {
	Hash    string `json:"hash"`
	Message string `json:"message"`
	Author  struct {
		Raw  string `json:"raw"`
		User struct {
			DisplayName string `json:"display_name"`
			Nickname    string `json:"nickname"`
			Username    string `json:"username"`
		} `json:"user"`
	} `json:"author"`
	Date    string `json:"date"`
	Parents []struct {
		Hash string `json:"hash"`
		Type string `json:"type"`
	} `json:"parents"`
	Links struct {
		HTML struct {
			Href string `json:"href"`
		} `json:"html"`
	} `json:"links"`
}

// GetCommit fetches a single commit by SHA (or any resolvable ref).
func (c *Client) GetCommit(ctx context.Context, workspace, repoSlug, sha string) (*Commit, error) {
	if workspace == "" || repoSlug == "" {
		return nil, fmt.Errorf("workspace and repository slug are required")
	}
	if sha == "" {
		return nil, fmt.Errorf("commit SHA is required")
	}

	path := fmt.Sprintf("/repositories/%s/%s/commit/%s",
		url.PathEscape(workspace),
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
// spec must be "ref1..ref2" where refs can be commit SHAs, branch names, or tags.
func (c *Client) CommitDiff(ctx context.Context, workspace, repoSlug, spec string, w io.Writer) error {
	if workspace == "" {
		return fmt.Errorf("workspace is required")
	}
	if repoSlug == "" {
		return fmt.Errorf("repository slug is required")
	}
	if spec == "" {
		return fmt.Errorf("spec is required")
	}
	if w == nil {
		return fmt.Errorf("writer is required")
	}

	path := fmt.Sprintf("/repositories/%s/%s/diff/%s",
		url.PathEscape(workspace),
		url.PathEscape(repoSlug),
		url.PathEscape(spec),
	)
	req, err := c.http.NewRequest(ctx, "GET", path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/plain")

	return c.http.Do(req, w)
}
