package bitbucket

import (
	"context"
	"fmt"

	"github.com/ruttybob/bkt-axi/internal/bitbucket/dc"
)

// project.go adapts the salvaged Data Center project client into the normalized
// Project model. Projects are a Data Center concept (Cloud uses workspaces), so
// any Cloud host gets a clear DC-only error.

// ListProjects enumerates projects visible to the authenticated user.
func (c *Client) ListProjects(ctx context.Context, limit int) ([]Project, error) {
	if c.Kind != KindDC {
		return nil, DCOnly("projects", c.hostKindLabel())
	}
	projects, err := c.dc.ListProjects(ctx, limit)
	if err != nil {
		return nil, c.mapErr(err, "projects")
	}
	out := make([]Project, 0, len(projects))
	for i := range projects {
		out = append(out, mapDCProject(&projects[i]))
	}
	return out, nil
}

func mapDCProject(p *dc.Project) Project {
	if p == nil {
		return Project{}
	}
	return Project{
		Key:         p.Key,
		Name:        p.Name,
		Description: p.Description,
	}
}

// projectKeyRequired returns a scope error when no DC project key resolved.
func projectKeyRequired(scope Scope) error {
	return fmt.Errorf("project key is required; use --project or set a context")
}
