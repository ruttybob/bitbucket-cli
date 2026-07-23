package commands

import (
	"context"
	"strings"

	"github.com/ruttybob/bkt-axi/internal/app"
	"github.com/ruttybob/bkt-axi/internal/axi"
)

// admin.go implements `admin secrets rotate` and `admin logging` (Bitbucket
// Data Center only). These are server-admin operations.

// NewAdminCmd builds the `admin` noun.
func NewAdminCmd() *app.Command {
	return &app.Command{
		Name:  "admin",
		Short: "Server administration (Data Center)",
		Long:  "Data Center server-administration operations: secrets rotation and logging control.",
		Children: []*app.Command{
			newAdminSecretsCmd(),
			newAdminLoggingCmd(),
		},
	}
}

func newAdminSecretsCmd() *app.Command {
	return &app.Command{
		Name:  "secrets",
		Short: "Manage encryption keys",
		Long:  "Manage the Data Center secrets-manager encryption keys.",
		Children: []*app.Command{
			{Name: "rotate", Short: "Rotate the encryption key", Flags: selectorFlags(),
				MinArgs: 0, MaxArgs: 0, Run: runAdminSecretsRotate},
		},
	}
}

func runAdminSecretsRotate(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	if err := client.RotateSecret(context.Background()); err != nil {
		return err
	}
	emitConfirmation(ctx, "encryption key rotation triggered")
	return nil
}

func newAdminLoggingCmd() *app.Command {
	return &app.Command{
		Name:  "logging",
		Short: "Inspect and set logging level",
		Long:  "Get or set the Data Center logging level.",
		Children: []*app.Command{
			{Name: "get", Short: "Show the logging level", Flags: selectorFlags(),
				MinArgs: 0, MaxArgs: 0, Run: runAdminLoggingGet},
			{Name: "set", Short: "Set the logging level", Flags: selectorFlags(),
				MinArgs: 1, MaxArgs: 1, Run: runAdminLoggingSet},
		},
	}
}

func runAdminLoggingGet(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	cfg, err := client.GetLoggingLevel(context.Background())
	if err != nil {
		return err
	}
	schema := []axi.Field{
		{Key: "level", Extractor: axi.Pluck("level")},
		{Key: "async", Extractor: axi.BoolYesNo(axi.Pluck("async"))},
	}
	emitDetail(ctx, "logging", *cfg, schema, nil)
	return nil
}

func runAdminLoggingSet(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	level := strings.TrimSpace(ctx.Args[0])
	if level == "" {
		return axi.UsageError("`admin logging set` requires a level (e.g. DEBUG, INFO, WARN)")
	}
	cfg, err := client.SetLoggingLevel(context.Background(), level)
	if err != nil {
		return err
	}
	schema := []axi.Field{{Key: "level", Extractor: axi.Pluck("level")}}
	emitDetail(ctx, "logging", *cfg, schema, nil)
	return nil
}
