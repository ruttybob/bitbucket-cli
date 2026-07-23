package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruttybob/bkt-axi/internal/app"
	"github.com/ruttybob/bkt-axi/internal/axi"
	"github.com/ruttybob/bkt-axi/internal/bitbucket"
)

// webhook.go implements the `webhook` noun across both platforms. Test
// delivery is supported on Data Center; Cloud has no public test-delivery API.

// NewWebhookCmd builds the `webhook` noun and its verbs.
func NewWebhookCmd() *app.Command {
	return &app.Command{
		Name:  "webhook",
		Short: "Manage repository webhooks",
		Long:  "List, create, delete, and test repository webhooks on Cloud and Data Center.",
		Children: []*app.Command{
			newWebhookListCmd(),
			newWebhookCreateCmd(),
			newWebhookDeleteCmd(),
			newWebhookTestCmd(),
		},
	}
}

var webhookAllowedFields = []string{"created", "test_delivery"}

func webhookListSchema(tokens []string, testable bool) []axi.Field {
	base := []axi.Field{
		{Key: "id", Extractor: axi.Pluck("id")},
		{Key: "url", Extractor: axi.Pluck("url")},
		{Key: "active", Extractor: axi.BoolYesNo(axi.Pluck("active"))},
		{Key: "events", Extractor: axi.JoinArray(axi.Pluck("events"), " ")},
	}
	for _, t := range tokens {
		switch t {
		case "created":
			base = append(base, axi.Field{Key: "created", Extractor: axi.Pluck("created")})
		case "test_delivery":
			label := "no"
			if testable {
				label = "yes"
			}
			base = append(base, axi.Field{Key: "test_delivery", Extractor: axi.Const(label)})
		}
	}
	return base
}

func newWebhookListCmd() *app.Command {
	flags := append(app.FlagSet{
		{Name: "fields", Type: app.FlagString, Default: "", Desc: "Extra columns (comma-sep): created,test_delivery"},
	}, selectorFlags()...)
	return &app.Command{
		Name:    "list",
		Aliases: []string{"ls"},
		Short:   "List repository webhooks",
		Long:    "List webhooks for the resolved repository. The default schema is {id,url,active,events}; use --fields to add columns.",
		Flags:   flags,
		MinArgs: 0, MaxArgs: 0,
		Examples: []app.Example{{Cmd: "bkt-axi webhook list", What: "webhooks in the current repo"}},
		Run:      runWebhookList,
	}
}

func runWebhookList(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --workspace or --project) or set a context")
	}
	tokens, err := validateFieldTokens(ctx.Flags.String("fields"), "webhook list", webhookAllowedFields)
	if err != nil {
		return err
	}
	testable := client.Kind == bitbucket.KindDC
	schema := webhookListSchema(tokens, testable)

	result, err := client.ListWebhooks(context.Background(), scope)
	if err != nil {
		return err
	}
	if len(result.Webhooks) == 0 {
		emitEmpty(ctx, "webhooks", fmt.Sprintf("0 webhooks in %s", scope), []string{
			"Run `bkt-axi webhook create --url <url> --events <events>` to add a webhook",
		})
		return nil
	}
	help := []string{"Run `bkt-axi webhook test <id>` to trigger a test delivery"}
	if !testable {
		help = []string{"Run `bkt-axi webhook delete <id>` to remove a webhook"}
	}
	emitList(ctx, "webhooks", toAny(result.Webhooks), schema, result.Shown, help)
	return nil
}

func newWebhookCreateCmd() *app.Command {
	flags := append(app.FlagSet{
		{Name: "url", Type: app.FlagString, Default: "", Desc: "Webhook payload URL (required)"},
		{Name: "events", Type: app.FlagString, Default: "", Desc: "Comma-separated events to subscribe to (e.g. repo:push)"},
		{Name: "active", Type: app.FlagBool, Default: true, Desc: "Whether the webhook is active"},
	}, selectorFlags()...)
	return &app.Command{
		Name:    "create",
		Short:   "Create a repository webhook",
		Long:    "Register a webhook. --url is required; --events is a comma-separated list. The webhook name is derived from the URL host.",
		Flags:   flags,
		MinArgs: 0, MaxArgs: 0,
		Examples: []app.Example{{Cmd: "bkt-axi webhook create --url https://hook.example/cb --events repo:push", What: "register a push webhook"}},
		Run:      runWebhookCreate,
	}
}

func runWebhookCreate(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --workspace or --project) or set a context")
	}
	url := strings.TrimSpace(ctx.Flags.String("url"))
	if url == "" {
		return axi.UsageError("`webhook create` requires --url")
	}
	var events []string
	for _, e := range strings.Split(ctx.Flags.String("events"), ",") {
		if e = strings.TrimSpace(e); e != "" {
			events = append(events, e)
		}
	}
	if len(events) == 0 {
		return axi.UsageError("`webhook create` requires at least one --events value")
	}
	hook, err := client.CreateWebhook(context.Background(), scope, bitbucket.WebhookCreateInput{
		URL:    url,
		Events: events,
		Active: ctx.Flags.Bool("active"),
	})
	if err != nil {
		return err
	}
	schema := []axi.Field{
		{Key: "id", Extractor: axi.Pluck("id")},
		{Key: "name", Extractor: axi.Pluck("name")},
		{Key: "url", Extractor: axi.Pluck("url")},
		{Key: "active", Extractor: axi.BoolYesNo(axi.Pluck("active"))},
		{Key: "events", Extractor: axi.JoinArray(axi.Pluck("events"), " ")},
	}
	emitDetail(ctx, "webhook", *hook, schema, []string{
		"Run `bkt-axi webhook test " + hook.ID + "` to trigger a test delivery",
	})
	return nil
}

func newWebhookDeleteCmd() *app.Command {
	return &app.Command{
		Name:    "delete",
		Aliases: []string{"rm"},
		Short:   "Delete a webhook",
		Long:    "Remove a webhook by id. Idempotent: a no-op when the webhook is already gone.",
		Flags:   selectorFlags(),
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{{Cmd: "bkt-axi webhook delete 42", What: "delete webhook 42"}},
		Run:      runWebhookDelete,
	}
}

func runWebhookDelete(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --workspace or --project) or set a context")
	}
	id := strings.TrimSpace(ctx.Args[0])
	changed, err := client.DeleteWebhook(context.Background(), scope, id)
	if err != nil {
		return err
	}
	if !changed {
		emitConfirmation(ctx, "webhook "+id+" already absent (no-op)")
		return nil
	}
	emitConfirmation(ctx, "deleted webhook "+id)
	return nil
}

func newWebhookTestCmd() *app.Command {
	return &app.Command{
		Name:    "test",
		Short:   "Trigger a webhook test delivery",
		Long:    "Trigger a test delivery for a webhook. Supported on Data Center; Cloud has no public test-delivery API.",
		Flags:   selectorFlags(),
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{{Cmd: "bkt-axi webhook test 42", What: "test webhook 42"}},
		Run:      runWebhookTest,
	}
}

func runWebhookTest(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	if scope.Empty() {
		return axi.Errorf("no repository resolved; use --repo (and --workspace or --project) or set a context")
	}
	id := strings.TrimSpace(ctx.Args[0])
	if err := client.TestWebhook(context.Background(), scope, id); err != nil {
		return err
	}
	emitConfirmation(ctx, "test delivery triggered for webhook "+id)
	return nil
}
