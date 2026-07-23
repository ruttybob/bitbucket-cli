package bitbucket

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruttybob/bkt-axi/internal/bitbucket/cloud"
)

// variable.go adapts the salvaged Cloud pipeline-variable client into the
// normalized Variable model. Pipeline variables are a Cloud-only feature.

const (
	VariableScopeRepo       = "repo"
	VariableScopeWorkspace  = "workspace"
	VariableScopeDeployment = "deployment"
)

// VariableScopeOpts selects where a variable lives.
type VariableScopeOpts struct {
	Scope string // repo (default), workspace, deployment
	Env   string // deployment environment name/slug/uuid (deployment only)
}

func (o VariableScopeOpts) normalized() VariableScopeOpts {
	s := strings.ToLower(strings.TrimSpace(o.Scope))
	if s == "" {
		s = VariableScopeRepo
	}
	return VariableScopeOpts{Scope: s, Env: strings.TrimSpace(o.Env)}
}

// ListVariables enumerates variables for the selected scope.
func (c *Client) ListVariables(ctx context.Context, scope Scope, opts VariableScopeOpts) ([]Variable, error) {
	if c.Kind != KindCloud {
		return nil, CloudOnly("pipeline variables", c.hostKindLabel())
	}
	o := opts.normalized()
	switch o.Scope {
	case VariableScopeWorkspace:
		if scope.Workspace == "" {
			return nil, fmt.Errorf("workspace is required; use --workspace or set a context")
		}
		vars, err := c.cloud.ListWorkspaceVariables(ctx, scope.Workspace, cloud.VariableListOptions{})
		if err != nil {
			return nil, mapHTTPError(err, "variables")
		}
		return mapVars(vars, VariableScopeWorkspace), nil
	case VariableScopeDeployment:
		envUUID, err := c.resolveDeploymentEnv(ctx, scope, o.Env)
		if err != nil {
			return nil, err
		}
		vars, err := c.cloud.ListDeploymentVariables(ctx, scope.Workspace, scope.RepoSlug, envUUID, cloud.VariableListOptions{})
		if err != nil {
			return nil, mapHTTPError(err, "variables")
		}
		return mapVars(vars, VariableScopeDeployment), nil
	default: // repo
		if scope.Workspace == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("workspace and repo are required; use --workspace/--repo or set a context")
		}
		vars, err := c.cloud.ListRepositoryVariables(ctx, scope.Workspace, scope.RepoSlug, cloud.VariableListOptions{})
		if err != nil {
			return nil, mapHTTPError(err, "variables")
		}
		return mapVars(vars, VariableScopeRepo), nil
	}
}

// GetVariable returns the variable whose key matches name, or a not-found error.
func (c *Client) GetVariable(ctx context.Context, scope Scope, name string, opts VariableScopeOpts) (*Variable, error) {
	vars, err := c.ListVariables(ctx, scope, opts)
	if err != nil {
		return nil, err
	}
	for i := range vars {
		if vars[i].Key == name {
			return &vars[i], nil
		}
	}
	return nil, fmt.Errorf("variable %q not found in %s scope", name, opts.normalized().Scope)
}

// SetVariable upserts a variable. created is true when a new variable was
// created, false when an existing one was updated. (Running twice yields the
// same end state, so the operation is idempotent.)
func (c *Client) SetVariable(ctx context.Context, scope Scope, name, value string, secured bool, opts VariableScopeOpts) (*Variable, bool, error) {
	if c.Kind != KindCloud {
		return nil, false, CloudOnly("pipeline variables", c.hostKindLabel())
	}
	o := opts.normalized()
	if strings.TrimSpace(name) == "" {
		return nil, false, fmt.Errorf("variable name is required")
	}
	existing, _ := c.findVariable(ctx, scope, name, o)

	switch o.Scope {
	case VariableScopeWorkspace:
		if scope.Workspace == "" {
			return nil, false, fmt.Errorf("workspace is required; use --workspace or set a context")
		}
		if existing != nil {
			v, err := c.cloud.UpdateWorkspaceVariable(ctx, scope.Workspace, existing.UUID, cloud.UpdateWorkspaceVariableInput{
				Key: name, Value: value, Secured: secured,
			})
			if err != nil {
				return nil, false, mapHTTPError(err, "variable "+name)
			}
			m := mapVar(*v, VariableScopeWorkspace)
			return &m, false, nil
		}
		v, err := c.cloud.CreateWorkspaceVariable(ctx, scope.Workspace, cloud.CreateWorkspaceVariableInput{
			Key: name, Value: value, Secured: secured,
		})
		if err != nil {
			return nil, false, mapHTTPError(err, "variable "+name)
		}
		m := mapVar(*v, VariableScopeWorkspace)
		return &m, true, nil
	case VariableScopeDeployment:
		envUUID, err := c.resolveDeploymentEnv(ctx, scope, o.Env)
		if err != nil {
			return nil, false, err
		}
		if existing != nil {
			v, err := c.cloud.UpdateDeploymentVariable(ctx, scope.Workspace, scope.RepoSlug, envUUID, existing.UUID, cloud.UpdateDeploymentVariableInput{
				Key: name, Value: value, Secured: secured,
			})
			if err != nil {
				return nil, false, mapHTTPError(err, "variable "+name)
			}
			m := mapVar(*v, VariableScopeDeployment)
			return &m, false, nil
		}
		v, err := c.cloud.CreateDeploymentVariable(ctx, scope.Workspace, scope.RepoSlug, envUUID, cloud.CreateDeploymentVariableInput{
			Key: name, Value: value, Secured: secured,
		})
		if err != nil {
			return nil, false, mapHTTPError(err, "variable "+name)
		}
		m := mapVar(*v, VariableScopeDeployment)
		return &m, true, nil
	default: // repo
		if scope.Workspace == "" || scope.RepoSlug == "" {
			return nil, false, fmt.Errorf("workspace and repo are required; use --workspace/--repo or set a context")
		}
		if existing != nil {
			v, err := c.cloud.UpdateRepositoryVariable(ctx, scope.Workspace, scope.RepoSlug, existing.UUID, cloud.UpdateRepositoryVariableInput{
				Key: name, Value: value, Secured: secured,
			})
			if err != nil {
				return nil, false, mapHTTPError(err, "variable "+name)
			}
			m := mapVar(*v, VariableScopeRepo)
			return &m, false, nil
		}
		v, err := c.cloud.CreateRepositoryVariable(ctx, scope.Workspace, scope.RepoSlug, cloud.CreateRepositoryVariableInput{
			Key: name, Value: value, Secured: secured,
		})
		if err != nil {
			return nil, false, mapHTTPError(err, "variable "+name)
		}
		m := mapVar(*v, VariableScopeRepo)
		return &m, true, nil
	}
}

// DeleteVariable removes the variable whose key matches name. changed is false
// (idempotent no-op) when no such variable exists.
func (c *Client) DeleteVariable(ctx context.Context, scope Scope, name string, opts VariableScopeOpts) (bool, error) {
	if c.Kind != KindCloud {
		return false, CloudOnly("pipeline variables", c.hostKindLabel())
	}
	o := opts.normalized()
	existing, ferr := c.findVariable(ctx, scope, name, o)
	if ferr != nil {
		return false, ferr
	}
	if existing == nil {
		return false, nil
	}
	switch o.Scope {
	case VariableScopeWorkspace:
		if err := c.cloud.DeleteWorkspaceVariable(ctx, scope.Workspace, existing.UUID); err != nil {
			if isNotFound(err) {
				return false, nil
			}
			return false, mapHTTPError(err, "variable "+name)
		}
	case VariableScopeDeployment:
		envUUID, err := c.resolveDeploymentEnv(ctx, scope, o.Env)
		if err != nil {
			return false, err
		}
		if err := c.cloud.DeleteDeploymentVariable(ctx, scope.Workspace, scope.RepoSlug, envUUID, existing.UUID); err != nil {
			if isNotFound(err) {
				return false, nil
			}
			return false, mapHTTPError(err, "variable "+name)
		}
	default: // repo
		if err := c.cloud.DeleteRepositoryVariable(ctx, scope.Workspace, scope.RepoSlug, existing.UUID); err != nil {
			if isNotFound(err) {
				return false, nil
			}
			return false, mapHTTPError(err, "variable "+name)
		}
	}
	return true, nil
}

// findVariable lists the scope's variables and returns the one matching name,
// or nil when absent (a list error is returned).
func (c *Client) findVariable(ctx context.Context, scope Scope, name string, o VariableScopeOpts) (*Variable, error) {
	vars, err := c.ListVariables(ctx, scope, VariableScopeOpts{Scope: o.Scope, Env: o.Env})
	if err != nil {
		return nil, err
	}
	for i := range vars {
		if vars[i].Key == name {
			return &vars[i], nil
		}
	}
	return nil, nil
}

// resolveDeploymentEnv resolves a deployment environment name/slug/uuid to the
// UUID the API requires.
func (c *Client) resolveDeploymentEnv(ctx context.Context, scope Scope, env string) (string, error) {
	if scope.Workspace == "" || scope.RepoSlug == "" {
		return "", fmt.Errorf("workspace and repo are required for deployment variables; use --workspace/--repo or set a context")
	}
	env = strings.TrimSpace(env)
	if env == "" {
		return "", fmt.Errorf("--env is required for deployment-scoped variables")
	}
	if cloud.NormalizeUUID(env) != "" {
		return cloud.NormalizeUUID(env), nil
	}
	envs, err := c.cloud.ListDeploymentEnvironments(ctx, scope.Workspace, scope.RepoSlug)
	if err != nil {
		return "", mapHTTPError(err, "deployment environments")
	}
	for _, e := range envs {
		if strings.EqualFold(e.Name, env) || strings.EqualFold(e.Slug, env) {
			return e.UUID, nil
		}
	}
	return "", fmt.Errorf("deployment environment %q not found in %s/%s", env, scope.Workspace, scope.RepoSlug)
}

// --- mappers -------------------------------------------------------------

func mapVar(v cloud.PipelineVariable, scope string) Variable {
	return Variable{
		Key:     v.Key,
		Value:   v.Value,
		Secured: v.Secured,
		Scope:   scope,
		UUID:    v.UUID,
	}
}

func mapVars(vs []cloud.PipelineVariable, scope string) []Variable {
	out := make([]Variable, 0, len(vs))
	for i := range vs {
		out = append(out, mapVar(vs[i], scope))
	}
	return out
}
