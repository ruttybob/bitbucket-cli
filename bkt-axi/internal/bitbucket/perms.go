package bitbucket

import (
	"context"
	"fmt"

	"github.com/ruttybob/bkt-axi/internal/bitbucket/dc"
)

// perms.go adapts the salvaged Data Center permission client into the
// normalized Permission model. User-permission management is a Data Center
// concept; Cloud manages access through workspace/group membership instead.

// ListProjectPermissions enumerates user permissions for a project.
func (c *Client) ListProjectPermissions(ctx context.Context, projectKey string, limit int) ([]Permission, error) {
	if c.Kind != KindDC {
		return nil, DCOnly("project permissions", c.hostKindLabel())
	}
	if projectKey == "" {
		return nil, projectKeyRequired(Scope{})
	}
	perms, err := c.dc.ListProjectPermissions(ctx, projectKey, limit)
	if err != nil {
		return nil, c.mapErr(err, "project permissions")
	}
	return mapPerms(perms), nil
}

// GrantProjectPermission grants a permission to a user for a project.
func (c *Client) GrantProjectPermission(ctx context.Context, projectKey, user, perm string) error {
	if c.Kind != KindDC {
		return DCOnly("project permissions", c.hostKindLabel())
	}
	if projectKey == "" {
		return projectKeyRequired(Scope{})
	}
	if err := c.dc.GrantProjectPermission(ctx, projectKey, user, perm); err != nil {
		return c.mapErr(err, "project permission for "+user)
	}
	return nil
}

// RevokeProjectPermission removes a user's project permission.
func (c *Client) RevokeProjectPermission(ctx context.Context, projectKey, user string) error {
	if c.Kind != KindDC {
		return DCOnly("project permissions", c.hostKindLabel())
	}
	if projectKey == "" {
		return projectKeyRequired(Scope{})
	}
	if err := c.dc.RevokeProjectPermission(ctx, projectKey, user); err != nil {
		return c.mapErr(err, "project permission for "+user)
	}
	return nil
}

// ListRepoPermissions enumerates user permissions for a repository.
func (c *Client) ListRepoPermissions(ctx context.Context, scope Scope, limit int) ([]Permission, error) {
	if c.Kind != KindDC {
		return nil, DCOnly("repository permissions", c.hostKindLabel())
	}
	if scope.ProjectKey == "" || scope.RepoSlug == "" {
		return nil, fmt.Errorf("project and repo are required; use --project/--repo or set a context")
	}
	perms, err := c.dc.ListRepoPermissions(ctx, scope.ProjectKey, scope.RepoSlug, limit)
	if err != nil {
		return nil, c.mapErr(err, "repository permissions")
	}
	return mapPerms(perms), nil
}

// GrantRepoPermission grants a permission to a user for a repository.
func (c *Client) GrantRepoPermission(ctx context.Context, scope Scope, user, perm string) error {
	if c.Kind != KindDC {
		return DCOnly("repository permissions", c.hostKindLabel())
	}
	if scope.ProjectKey == "" || scope.RepoSlug == "" {
		return fmt.Errorf("project and repo are required; use --project/--repo or set a context")
	}
	if err := c.dc.GrantRepoPermission(ctx, scope.ProjectKey, scope.RepoSlug, user, perm); err != nil {
		return c.mapErr(err, "repository permission for "+user)
	}
	return nil
}

// RevokeRepoPermission removes a user's repository permission.
func (c *Client) RevokeRepoPermission(ctx context.Context, scope Scope, user string) error {
	if c.Kind != KindDC {
		return DCOnly("repository permissions", c.hostKindLabel())
	}
	if scope.ProjectKey == "" || scope.RepoSlug == "" {
		return fmt.Errorf("project and repo are required; use --project/--repo or set a context")
	}
	if err := c.dc.RevokeRepoPermission(ctx, scope.ProjectKey, scope.RepoSlug, user); err != nil {
		return c.mapErr(err, "repository permission for "+user)
	}
	return nil
}

func mapPerms(perms []dc.Permission) []Permission {
	out := make([]Permission, 0, len(perms))
	for i := range perms {
		out = append(out, Permission{
			User:       dcDisplayName(perms[i].User),
			Permission: perms[i].Permission,
		})
	}
	return out
}
