package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruttybob/bkt-axi/internal/app"
	"github.com/ruttybob/bkt-axi/internal/axi"
	"github.com/ruttybob/bkt-axi/internal/bitbucket"
)

// perms.go implements `perms project` and `perms repo` (Bitbucket Data Center
// only). Cloud manages access through workspace/group membership instead.

// NewPermsCmd builds the `perms` noun.
func NewPermsCmd() *app.Command {
	return &app.Command{
		Name:  "perms",
		Short: "Manage user permissions (Data Center)",
		Long:  "List, grant, and revoke user permissions on Data Center projects and repositories.",
		Children: []*app.Command{
			newPermsProjectCmd(),
			newPermsRepoCmd(),
		},
	}
}

var permissionSchema = []axi.Field{
	{Key: "user", Extractor: axi.Pluck("user")},
	{Key: "permission", Extractor: axi.Pluck("permission")},
}

func newPermsProjectCmd() *app.Command {
	return &app.Command{
		Name:  "project",
		Short: "Project user permissions",
		Long:  "List, grant, and revoke user permissions on a Data Center project.",
		Children: []*app.Command{
			{Name: "list", Aliases: []string{"ls"}, Short: "List project permissions",
				Flags: selectorFlags(), MinArgs: 1, MaxArgs: 1, Run: runPermsProjectList},
			{Name: "grant", Short: "Grant a project permission",
				Flags: selectorFlags(), MinArgs: 3, MaxArgs: 3, Run: runPermsProjectGrant},
			{Name: "revoke", Short: "Revoke a project permission",
				Flags: selectorFlags(), MinArgs: 2, MaxArgs: 2, Run: runPermsProjectRevoke},
		},
	}
}

func runPermsProjectList(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	key := strings.TrimSpace(ctx.Args[0])
	if key == "" {
		return axi.UsageError("`perms project list` requires a project key")
	}
	perms, err := client.ListProjectPermissions(context.Background(), key, 100)
	if err != nil {
		return err
	}
	if len(perms) == 0 {
		emitEmpty(ctx, "permissions", fmt.Sprintf("0 user permissions on project %s", key), []string{
			fmt.Sprintf("Run `bkt-axi perms project grant %s <user> <perm>` to grant access", key),
		})
		return nil
	}
	emitList(ctx, "permissions", toAny(perms), permissionSchema, len(perms), nil)
	return nil
}

func runPermsProjectGrant(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	key, user, perm := ctx.Args[0], ctx.Args[1], ctx.Args[2]
	if err := client.GrantProjectPermission(context.Background(), key, user, perm); err != nil {
		return err
	}
	emitConfirmation(ctx, fmt.Sprintf("granted %s on project %s to %s", perm, key, user))
	return nil
}

func runPermsProjectRevoke(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	key, user := ctx.Args[0], ctx.Args[1]
	if err := client.RevokeProjectPermission(context.Background(), key, user); err != nil {
		return err
	}
	emitConfirmation(ctx, fmt.Sprintf("revoked permissions on project %s from %s", key, user))
	return nil
}

func newPermsRepoCmd() *app.Command {
	return &app.Command{
		Name:  "repo",
		Short: "Repository user permissions",
		Long:  "List, grant, and revoke user permissions on a Data Center repository. The project is resolved from --project or the active context.",
		Children: []*app.Command{
			{Name: "list", Aliases: []string{"ls"}, Short: "List repository permissions",
				Flags: selectorFlags(), MinArgs: 1, MaxArgs: 1, Run: runPermsRepoList},
			{Name: "grant", Short: "Grant a repository permission",
				Flags: selectorFlags(), MinArgs: 3, MaxArgs: 3, Run: runPermsRepoGrant},
			{Name: "revoke", Short: "Revoke a repository permission",
				Flags: selectorFlags(), MinArgs: 2, MaxArgs: 2, Run: runPermsRepoRevoke},
		},
	}
}

// repoScopeFromArg resolves a scope whose project comes from the resolved
// context/--project and whose repo slug is the supplied positional.
func repoScopeFromArg(ctx *app.Context, slug string) (bitbucket.Scope, error) {
	s, err := ctx.Scope()
	if err != nil {
		return bitbucket.Scope{}, err
	}
	s.RepoSlug = slug
	return s, nil
}

func runPermsRepoList(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	s, err := repoScopeFromArg(ctx, ctx.Args[0])
	if err != nil {
		return err
	}
	perms, err := client.ListRepoPermissions(context.Background(), s, 100)
	if err != nil {
		return err
	}
	if len(perms) == 0 {
		emitEmpty(ctx, "permissions", fmt.Sprintf("0 user permissions on %s", s), []string{
			fmt.Sprintf("Run `bkt-axi perms repo grant %s <user> <perm>` to grant access", s.RepoSlug),
		})
		return nil
	}
	emitList(ctx, "permissions", toAny(perms), permissionSchema, len(perms), nil)
	return nil
}

func runPermsRepoGrant(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	s, err := repoScopeFromArg(ctx, ctx.Args[0])
	if err != nil {
		return err
	}
	user, perm := ctx.Args[1], ctx.Args[2]
	if err := client.GrantRepoPermission(context.Background(), s, user, perm); err != nil {
		return err
	}
	emitConfirmation(ctx, fmt.Sprintf("granted %s on %s to %s", perm, s, user))
	return nil
}

func runPermsRepoRevoke(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	s, err := repoScopeFromArg(ctx, ctx.Args[0])
	if err != nil {
		return err
	}
	user := ctx.Args[1]
	if err := client.RevokeRepoPermission(context.Background(), s, user); err != nil {
		return err
	}
	emitConfirmation(ctx, fmt.Sprintf("revoked permissions on %s from %s", s, user))
	return nil
}
