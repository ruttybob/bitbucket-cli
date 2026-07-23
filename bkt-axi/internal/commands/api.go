package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ruttybob/bkt-axi/internal/app"
	"github.com/ruttybob/bkt-axi/internal/axi"
	"github.com/ruttybob/bkt-axi/internal/bitbucket/httpx"
)

// api.go implements `api <path>` — a raw passthrough to the active host's REST
// API. It exists for endpoints the normalized adapter layer does not yet
// model. Output is the raw decoded JSON rendered as TOON by default (or JSON
// via the escape hatches). --field/-F builds a JSON body (write methods) or
// query params (GET); --paginate follows Cloud-style `next` links accumulating
// `values`.

// NewAPICmd builds the `api` noun.
func NewAPICmd() *app.Command {
	return &app.Command{
		Name:  "api",
		Short: "Raw API passthrough",
		Long:  "Issue an arbitrary request to the active host's REST API and print the decoded response. Use for endpoints bkt-axi does not yet model.",
		Flags: append(app.FlagSet{
			{Name: "method", Type: app.FlagString, Default: "GET", Desc: "HTTP method"},
			{Name: "field", Short: "F", Type: app.FlagStringSlice, Default: []string{}, Desc: "key=value field (repeatable): JSON body for writes, query params for GET"},
			{Name: "paginate", Type: app.FlagBool, Default: false, Desc: "Follow `next` links, accumulating `values`"},
		}, selectorFlags()...),
		MinArgs: 1, MaxArgs: 1,
		Examples: []app.Example{
			{Cmd: "bkt-axi api /2.0/user", What: "fetch the authenticated user"},
			{Cmd: "bkt-axi api /rest/api/1.0/projects --paginate", What: "page through DC projects"},
			{Cmd: "bkt-axi api /2.0/repositories/acme/api --method POST --field name=newrepo", What: "create a repo"},
		},
		Run: runAPI,
	}
}

func runAPI(ctx *app.Context) error {
	client, err := ctx.Client()
	if err != nil {
		return err
	}
	httpc := client.HTTP()
	if httpc == nil {
		return axi.Errorf("no HTTP client available for the active host")
	}
	method := strings.ToUpper(strings.TrimSpace(ctx.Flags.String("method")))
	if method == "" {
		method = "GET"
	}
	path := ctx.Args[0]
	fields := ctx.Flags.StringSlice("field")
	body, query, ferr := parseAPIFields(fields, method)
	if ferr != nil {
		return ferr
	}
	if len(query) > 0 {
		path = appendQuery(path, query)
	}

	bg := context.Background()
	var result any
	if ctx.Flags.Bool("paginate") && method == "GET" {
		result, err = apiPaginate(bg, httpc, path)
	} else {
		result, err = apiDo(bg, httpc, method, path, body)
	}
	if err != nil {
		return mapRawError(err)
	}
	emitRaw(ctx, result)
	return nil
}

// parseAPIFields turns repeatable key=value fields into either a JSON body
// (write methods) or query params (GET).
func parseAPIFields(fields []string, method string) (body any, query map[string]string, err error) {
	if len(fields) == 0 {
		return nil, nil, nil
	}
	if method == "GET" {
		query = make(map[string]string, len(fields))
	} else {
		body = make(map[string]any, len(fields))
	}
	for _, f := range fields {
		eq := strings.IndexByte(f, '=')
		if eq <= 0 {
			return nil, nil, axi.UsageError(fmt.Sprintf("invalid --field %q (expected key=value)", f))
		}
		key := strings.TrimSpace(f[:eq])
		val := f[eq+1:]
		if method == "GET" {
			query[key] = val
		} else {
			body.(map[string]any)[key] = coerceJSON(val)
		}
	}
	return body, query, nil
}

// coerceJSON parses a field value as JSON when possible (numbers, bools,
// objects, arrays); otherwise it stays a string.
func coerceJSON(v string) any {
	v = strings.TrimSpace(v)
	var parsed any
	if json.Unmarshal([]byte(v), &parsed) == nil {
		return parsed
	}
	return v
}

// appendQuery merges query params into path, preserving any existing query.
func appendQuery(path string, query map[string]string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	parts := make([]string, 0, len(query))
	for k, v := range query {
		parts = append(parts, k+"="+v)
	}
	return path + sep + strings.Join(parts, "&")
}

// apiDo executes one request and decodes the JSON body into a generic value.
func apiDo(ctx context.Context, httpc *httpx.Client, method, path string, body any) (any, error) {
	req, err := httpc.NewRequest(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	var result any
	if err := httpc.Do(req, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// apiPaginate follows Cloud-style `next` links, accumulating every page's
// `values` array into a single merged object.
func apiPaginate(ctx context.Context, httpc *httpx.Client, path string) (any, error) {
	first, err := apiDo(ctx, httpc, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	fm, ok := first.(map[string]any)
	if !ok {
		return first, nil
	}
	if _, ok := fm["values"].([]any); !ok {
		return first, nil // not a paginated collection
	}
	accumulated := make([]any, 0)
	cur := fm
	for {
		if vals, ok := cur["values"].([]any); ok {
			accumulated = append(accumulated, vals...)
		}
		next, _ := cur["next"].(string)
		if strings.TrimSpace(next) == "" {
			break
		}
		page, err := apiDo(ctx, httpc, "GET", next, nil)
		if err != nil {
			return nil, err
		}
		pm, ok := page.(map[string]any)
		if !ok {
			break
		}
		cur = pm
	}
	return map[string]any{"values": accumulated}, nil
}

// emitRaw renders the decoded API value as TOON by default, or JSON/YAML.
func emitRaw(ctx *app.Context, v any) {
	switch ctx.OutputFormat() {
	case "json":
		writeJSON(ctx, v)
	case "yaml":
		writeYAML(ctx, v)
	default:
		// nil (e.g. a 204) renders as an empty acknowledgment.
		if v == nil {
			emitConfirmation(ctx, "ok (empty response body)")
			return
		}
		io.WriteString(ctx.Out(), axi.Marshal(v)+"\n")
	}
}

// mapRawError translates an httpx error from a raw passthrough into an axi
// error, naming the path so the agent can correlate.
func mapRawError(err error) error {
	var he *httpx.HTTPError
	if errors.As(err, &he) {
		return axi.MapHTTPError(he.StatusCode, he.Error(), "api request", false)
	}
	return axi.Errorf("%s", err.Error())
}
