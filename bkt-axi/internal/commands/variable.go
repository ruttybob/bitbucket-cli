package commands

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ruttybob/bkt-axi/internal/app"
	"github.com/ruttybob/bkt-axi/internal/axi"
	"github.com/ruttybob/bkt-axi/internal/bitbucket"
)

// variable.go implements the `variable` noun (Bitbucket Cloud pipeline
// variables). `set` is idempotent: it updates an existing variable or creates
// a new one.

// NewVariableCmd builds the `variable` noun and its verbs.
func NewVariableCmd() *app.Command {
	return &app.Command{
		Name:  "variable",
		Short: "Manage pipeline variables (Cloud)",
		Long:  "List, get, set, and delete Bitbucket Cloud pipeline variables at repo, workspace, or deployment scope.",
		Children: []*app.Command{
			newVariableListCmd(),
			newVariableGetCmd(),
			newVariableSetCmd(),
			newVariableDeleteCmd(),
		},
	}
}

func variableScopeFlags() app.FlagSet {
	return append(app.FlagSet{
		{Name: "scope", Type: app.FlagString, Default: "repo", Desc: "Variable scope: repo, workspace, deployment"},
		{Name: "env", Type: app.FlagString, Default: "", Desc: "Deployment environment name/slug/uuid (deployment scope)"},
	}, selectorFlags()...)
}

// validateVariableScope returns an axi usage error when --scope is invalid.
func validateVariableScope(s string) error {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case bitbucket.VariableScopeRepo, bitbucket.VariableScopeWorkspace, bitbucket.VariableScopeDeployment, "":
		return nil
	}
	return axi.UsageError(fmt.Sprintf("invalid --scope %q; use repo, workspace, or deployment", s))
}

func variableOpts(ctx *app.Context) (bitbucket.VariableScopeOpts, error) {
	if err := validateVariableScope(ctx.Flags.String("scope")); err != nil {
		return bitbucket.VariableScopeOpts{}, err
	}
	return bitbucket.VariableScopeOpts{
		Scope: ctx.Flags.String("scope"),
		Env:   ctx.Flags.String("env"),
	}, nil
}

var variableListSchema = []axi.Field{
	{Key: "key", Extractor: axi.Pluck("key")},
	{Key: "scope", Extractor: axi.Pluck("scope")},
	{Key: "secured", Extractor: axi.BoolYesNo(axi.Pluck("secured"))},
}

func newVariableListCmd() *app.Command {
	return &app.Command{
		Name:    "list",
		Aliases: []string{"ls"},
		Short:   "List pipeline variables",
		Long:    "List pipeline variables for the selected scope. The default schema is {key,scope,secured}.",
		Flags:   variableScopeFlags(),
		MinArgs: 0, MaxArgs: 0,
		Examples: []app.Example{
			{Cmd: "bkt-axi variable list", What: "repo-scoped variables"},
			{Cmd: "bkt-axi variable list --scope workspace", What: "workspace variables"},
		},
		Run: runVariableList,
	}
}

func runVariableList(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	opts, err := variableOpts(ctx)
	if err != nil {
		return err
	}
	vars, err := client.ListVariables(context.Background(), scope, opts)
	if err != nil {
		return err
	}
	if len(vars) == 0 {
		emitEmpty(ctx, "variables", fmt.Sprintf("0 %s variables", opts.Scope), []string{
			"Run `bkt-axi variable set <name> --value <value>` to add a variable",
		})
		return nil
	}
	emitList(ctx, "variables", toAny(vars), variableListSchema, len(vars), nil)
	return nil
}

func newVariableGetCmd() *app.Command {
	return &app.Command{
		Name:    "get",
		Short:   "Show a pipeline variable",
		Long:    "Show a single pipeline variable by key. Secured variables return an empty value.",
		Flags:   variableScopeFlags(),
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{{Cmd: "bkt-axi variable get DATABASE_URL", What: "show the DATABASE_URL variable"}},
		Run:      runVariableGet,
	}
}

func runVariableGet(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	opts, err := variableOpts(ctx)
	if err != nil {
		return err
	}
	v, err := client.GetVariable(context.Background(), scope, ctx.Args[0], opts)
	if err != nil {
		return err
	}
	schema := []axi.Field{
		{Key: "key", Extractor: axi.Pluck("key")},
		{Key: "value", Extractor: axi.Pluck("value")},
		{Key: "secured", Extractor: axi.BoolYesNo(axi.Pluck("secured"))},
		{Key: "scope", Extractor: axi.Pluck("scope")},
	}
	emitDetail(ctx, "variable", *v, schema, nil)
	return nil
}

func newVariableSetCmd() *app.Command {
	flags := append(app.FlagSet{
		{Name: "value", Type: app.FlagString, Default: "", Desc: "Variable value (or read from stdin)"},
		{Name: "secured", Type: app.FlagBool, Default: false, Desc: "Mark the variable as secured (value hidden)"},
	}, variableScopeFlags()...)
	return &app.Command{
		Name:    "set",
		Short:   "Set a pipeline variable",
		Long:    "Create or update a pipeline variable (idempotent). With a name, set one variable from --value or stdin; without a name, read KEY=VALUE lines from stdin and set each.",
		Flags:   flags,
		MinArgs: 0, MaxArgs: 1,
		Examples: []app.Example{
			{Cmd: "bkt-axi variable set TOKEN --value sekret --secured", What: "set a secured repo variable"},
			{Cmd: "echo sekret | bkt-axi variable set TOKEN", What: "set a variable from stdin"},
		},
		Run: runVariableSet,
	}
}

func runVariableSet(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	opts, err := variableOpts(ctx)
	if err != nil {
		return err
	}
	secured := ctx.Flags.Bool("secured")

	// Named single-variable set.
	if len(ctx.Args) == 1 {
		name := ctx.Args[0]
		value, verr := variableValue(ctx)
		if verr != nil {
			return verr
		}
		_, created, serr := client.SetVariable(context.Background(), scope, name, value, secured, opts)
		if serr != nil {
			return serr
		}
		verb := "updated"
		if created {
			verb = "created"
		}
		emitConfirmation(ctx, fmt.Sprintf("%s %s variable %s", verb, opts.Scope, name))
		return nil
	}

	// Bulk set: KEY=VALUE lines from stdin.
	pairs, perr := readKeyValueStdin()
	if perr != nil {
		return perr
	}
	if len(pairs) == 0 {
		return axi.UsageError("`variable set` requires a name, or KEY=VALUE pairs on stdin")
	}
	results := make([]string, 0, len(pairs))
	for k, v := range pairs {
		_, _, serr := client.SetVariable(context.Background(), scope, k, v, secured, opts)
		if serr != nil {
			return serr
		}
		results = append(results, k)
	}
	emitConfirmation(ctx, fmt.Sprintf("set %d %s variable(s): %s", len(results), opts.Scope, strings.Join(results, ", ")))
	return nil
}

// variableValue resolves a single variable's value from --value or stdin.
func variableValue(ctx *app.Context) (string, error) {
	if v := ctx.Flags.String("value"); v != "" {
		return v, nil
	}
	return readStdinValue()
}

// readStdinValue reads all of stdin as a value when input is piped; it errors
// when stdin is a terminal (no value provided) so the command never blocks.
func readStdinValue() (string, error) {
	if !stdinIsPiped() {
		return "", axi.UsageError("`variable set <name>` requires --value or piped stdin")
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", axi.Errorf("reading stdin: %s", err)
	}
	return strings.TrimRight(string(b), "\n"), nil
}

// readKeyValueStdin reads KEY=VALUE lines from piped stdin.
func readKeyValueStdin() (map[string]string, error) {
	if !stdinIsPiped() {
		return nil, nil
	}
	out := make(map[string]string)
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			return nil, axi.UsageError(fmt.Sprintf("invalid KEY=VALUE line %q", line))
		}
		out[strings.TrimSpace(line[:eq])] = line[eq+1:]
	}
	return out, sc.Err()
}

// stdinIsPiped reports whether stdin is connected to a pipe/file (not a TTY).
func stdinIsPiped() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) == 0
}

func newVariableDeleteCmd() *app.Command {
	return &app.Command{
		Name:    "delete",
		Aliases: []string{"rm"},
		Short:   "Delete a pipeline variable",
		Long:    "Remove a pipeline variable by key. Idempotent: a no-op when no such variable exists.",
		Flags:   variableScopeFlags(),
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{{Cmd: "bkt-axi variable delete TOKEN", What: "delete the TOKEN variable"}},
		Run:      runVariableDelete,
	}
}

func runVariableDelete(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	scope, err := ctx.Scope()
	if err != nil {
		return err
	}
	opts, err := variableOpts(ctx)
	if err != nil {
		return err
	}
	name := ctx.Args[0]
	changed, err := client.DeleteVariable(context.Background(), scope, name, opts)
	if err != nil {
		return err
	}
	if !changed {
		emitConfirmation(ctx, fmt.Sprintf("variable %s already absent (no-op)", name))
		return nil
	}
	emitConfirmation(ctx, fmt.Sprintf("deleted %s variable %s", opts.Scope, name))
	return nil
}
