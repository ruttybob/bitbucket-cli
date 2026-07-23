package commands

import (
	"context"

	"github.com/ruttybob/bkt-axi/internal/app"
	"github.com/ruttybob/bkt-axi/internal/axi"
)

// project.go implements `project list` (Bitbucket Data Center only). Cloud uses
// workspaces instead of projects.

// NewProjectCmd builds the `project` noun.
func NewProjectCmd() *app.Command {
	return &app.Command{
		Name:  "project",
		Short: "Work with projects (Data Center)",
		Long:  "List Bitbucket Data Center projects visible to the authenticated user.",
		Children: []*app.Command{
			newProjectListCmd(),
		},
	}
}

var projectListSchema = []axi.Field{
	{Key: "key", Extractor: axi.Pluck("key")},
	{Key: "name", Extractor: axi.Pluck("name")},
	{Key: "description", Extractor: axi.Pluck("description")},
}

func newProjectListCmd() *app.Command {
	flags := append(app.FlagSet{
		{Name: "limit", Type: app.FlagInt, Default: 25, Desc: "Maximum number of projects to show"},
	}, selectorFlags()...)
	return &app.Command{
		Name:    "list",
		Aliases: []string{"ls"},
		Short:   "List projects",
		Long:    "List Data Center projects visible to the authenticated user. The default schema is {key,name,description}.",
		Flags:   flags,
		MinArgs: 0, MaxArgs: 0,
		Examples: []app.Example{{Cmd: "bkt-axi project list", What: "list visible projects"}},
		Run:      runProjectList,
	}
}

func runProjectList(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	projects, err := client.ListProjects(context.Background(), ctx.Flags.Int("limit"))
	if err != nil {
		return err
	}
	if len(projects) == 0 {
		emitEmpty(ctx, "projects", "0 projects visible", []string{
			"Run `bkt-axi auth status` to confirm the active host can list projects",
		})
		return nil
	}
	emitList(ctx, "projects", toAny(projects), projectListSchema, len(projects), nil)
	return nil
}
