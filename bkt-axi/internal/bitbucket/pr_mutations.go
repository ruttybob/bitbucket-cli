package bitbucket

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ruttybob/bkt-axi/internal/bitbucket/cloud"
	"github.com/ruttybob/bkt-axi/internal/bitbucket/dc"
	"github.com/ruttybob/bkt-axi/internal/bitbucket/httpx"
)

// pr_mutations.go adapts the salvaged line-specific clients into the
// normalized mutation operations. Like pr.go, this file is the SINGLE place
// that switches on host.Kind for pull-request mutations; the command layer
// never does.
//
// Idempotency (AXI §6): every state-changing mutation pre-checks the current
// PR state and short-circuits with Already=true when the target state already
// holds, so a re-run is a clean exit-0 no-op. DC mutations additionally
// tolerate a concurrent change by re-fetching the optimistic-concurrency
// version and retrying once on a 409.

// CreatePRInput configures pull-request creation. Reviewers carry identities
// (Cloud UUID/account id/nickname, or DC username slug). When
// DefaultReviewers is set, the adapter resolves and appends the repository's
// default reviewers to Reviewers before creating.
type CreatePRInput struct {
	Title            string
	Description      string
	SourceBranch     string
	TargetBranch     string
	CloseSource      bool
	Draft            bool
	Reviewers        []string
	DefaultReviewers bool
	// DC-only fork source: when set the PR is opened from a different repo
	// than the target. Ignored for Cloud (Cloud infers the fork from the
	// authenticated user's branch).
	SourceProjectKey string
	SourceRepoSlug   string
}

// CreatePR creates a pull request and returns the normalized result.
func (c *Client) CreatePR(ctx context.Context, scope Scope, in CreatePRInput) (*PR, error) {
	switch c.Kind {
	case KindCloud:
		if scope.Workspace == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("workspace and repo are required; use --workspace/--repo or set a context")
		}
		reviewers := in.Reviewers
		if in.DefaultReviewers {
			def, err := c.cloud.GetEffectiveDefaultReviewers(ctx, scope.Workspace, scope.RepoSlug)
			if err != nil {
				return nil, c.mapErr(err, "default reviewers")
			}
			reviewers = mergeReviewers(reviewers, cloudReviewerIdentities(def))
		}
		pr, err := c.cloud.CreatePullRequest(ctx, scope.Workspace, scope.RepoSlug, cloud.CreatePullRequestInput{
			Title:       in.Title,
			Description: in.Description,
			Source:      in.SourceBranch,
			Destination: in.TargetBranch,
			CloseSource: in.CloseSource,
			Reviewers:   reviewers,
			Draft:       in.Draft,
		})
		if err != nil {
			return nil, c.mapErr(err, "pull request")
		}
		m := mapCloudPR(pr)
		return &m, nil
	case KindDC:
		if scope.ProjectKey == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("project and repo are required; use --project/--repo or set a context")
		}
		reviewers := in.Reviewers
		if in.DefaultReviewers {
			def, err := c.dc.GetDefaultReviewers(ctx, scope.ProjectKey, scope.RepoSlug, in.SourceBranch, in.TargetBranch)
			if err != nil {
				return nil, c.mapErr(err, "default reviewers")
			}
			reviewers = mergeReviewers(reviewers, dcUserSlugs(def))
		}
		pr, err := c.dc.CreatePullRequest(ctx, scope.ProjectKey, scope.RepoSlug, dc.CreatePROptions{
			Title:            in.Title,
			Description:      in.Description,
			SourceBranch:     in.SourceBranch,
			TargetBranch:     in.TargetBranch,
			SourceProjectKey: in.SourceProjectKey,
			SourceRepoSlug:   in.SourceRepoSlug,
			Reviewers:        reviewers,
			CloseSource:      in.CloseSource,
			Draft:            in.Draft,
		})
		if err != nil {
			return nil, c.mapErr(err, "pull request")
		}
		m := mapDCPR(pr)
		return &m, nil
	}
	return nil, fmt.Errorf("unsupported host kind %q", c.Kind)
}

// UpdatePRInput configures a pull-request edit. Pointer fields distinguish
// "leave unchanged" (nil) from "set to empty" (non-nil). ReviewersAdd /
// ReviewersRemove are merged against the current reviewer set; DefaultReviewers
// appends the repository's default reviewers. Publish un-drafts the PR.
type UpdatePRInput struct {
	Title            *string
	Description      *string
	ReviewersAdd     []string
	ReviewersRemove  []string
	DefaultReviewers bool
	Publish          bool
}

// UpdatePR edits an existing pull request and returns the normalized result.
// DC requires the optimistic-concurrency version; a stale-version 409 is
// retried once after re-fetching the version.
func (c *Client) UpdatePR(ctx context.Context, scope Scope, id int, in UpdatePRInput) (*PR, error) {
	switch c.Kind {
	case KindCloud:
		if scope.Workspace == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("workspace and repo are required; use --workspace/--repo or set a context")
		}
		input := cloud.UpdatePullRequestInput{
			Title:       in.Title,
			Description: in.Description,
		}
		if in.Publish {
			f := false
			input.Draft = &f
		}
		if len(in.ReviewersAdd) > 0 || len(in.ReviewersRemove) > 0 || in.DefaultReviewers {
			current, err := c.cloud.GetPullRequest(ctx, scope.Workspace, scope.RepoSlug, id)
			if err != nil {
				return nil, c.mapErr(err, fmt.Sprintf("pull request #%d", id))
			}
			merged := mergeReviewerChanges(cloudReviewerIdentities(current.Reviewers), in.ReviewersAdd, in.ReviewersRemove)
			if in.DefaultReviewers {
				def, err := c.cloud.GetEffectiveDefaultReviewers(ctx, scope.Workspace, scope.RepoSlug)
				if err != nil {
					return nil, c.mapErr(err, "default reviewers")
				}
				merged = mergeReviewers(merged, cloudReviewerIdentities(def))
			}
			input.Reviewers = merged
		}
		pr, err := c.cloud.UpdatePullRequest(ctx, scope.Workspace, scope.RepoSlug, id, input)
		if err != nil {
			return nil, c.mapErr(err, fmt.Sprintf("pull request #%d", id))
		}
		m := mapCloudPR(pr)
		return &m, nil
	case KindDC:
		if scope.ProjectKey == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("project and repo are required; use --project/--repo or set a context")
		}
		res, err := c.dcMutate(ctx, scope, id, "", fmt.Sprintf("pull request #%d", id), func(version int) error {
			current, gerr := c.dc.GetPullRequest(ctx, scope.ProjectKey, scope.RepoSlug, id)
			if gerr != nil {
				return gerr
			}
			opts := dc.UpdatePROptions{
				Title:       current.Title,
				Description: current.Description,
				Reviewers:   current.Reviewers,
				FromRef:     &current.FromRef,
				ToRef:       &current.ToRef,
			}
			if in.Title != nil {
				opts.Title = *in.Title
			}
			if in.Description != nil {
				opts.Description = *in.Description
			}
			if in.Publish {
				f := false
				opts.Draft = &f
			}
			if len(in.ReviewersAdd) > 0 || len(in.ReviewersRemove) > 0 || in.DefaultReviewers {
				merged := mergeReviewerChanges(dcReviewerSlugs(current.Reviewers), in.ReviewersAdd, in.ReviewersRemove)
				if in.DefaultReviewers {
					def, derr := c.dc.GetDefaultReviewers(ctx, scope.ProjectKey, scope.RepoSlug, current.FromRef.DisplayID, current.ToRef.DisplayID)
					if derr != nil {
						return derr
					}
					merged = mergeReviewers(merged, dcUserSlugs(def))
				}
				opts.Reviewers = dcReviewerSet(merged)
			}
			_, uerr := c.dc.UpdatePullRequest(ctx, scope.ProjectKey, scope.RepoSlug, id, version, opts)
			return uerr
		})
		if err != nil {
			return nil, err
		}
		return res.PR, nil
	}
	return nil, fmt.Errorf("unsupported host kind %q", c.Kind)
}

// PRDiff streams the unified diff for a pull request into w.
func (c *Client) PRDiff(ctx context.Context, scope Scope, id int, w io.Writer) error {
	switch c.Kind {
	case KindCloud:
		if scope.Workspace == "" || scope.RepoSlug == "" {
			return fmt.Errorf("workspace and repo are required; use --workspace/--repo or set a context")
		}
		if err := c.cloud.PullRequestDiff(ctx, scope.Workspace, scope.RepoSlug, id, w); err != nil {
			return c.mapErr(err, fmt.Sprintf("pull request #%d diff", id))
		}
		return nil
	case KindDC:
		if scope.ProjectKey == "" || scope.RepoSlug == "" {
			return fmt.Errorf("project and repo are required; use --project/--repo or set a context")
		}
		if err := c.dc.PullRequestDiff(ctx, scope.ProjectKey, scope.RepoSlug, id, w); err != nil {
			return c.mapErr(err, fmt.Sprintf("pull request #%d diff", id))
		}
		return nil
	}
	return fmt.Errorf("unsupported host kind %q", c.Kind)
}

// CheckoutRef describes what a `pr checkout` needs to materialize the PR head
// locally. Branch is the source branch display id; FetchRef is the refspec to
// fetch from the remote (DC: refs/pull-requests/<id>/from, Cloud: the source
// branch). CloneURL is set for Cloud forks so a fork remote can be added.
// IsFork reports whether the source repository differs from the target.
type CheckoutRef struct {
	Branch   string
	FetchRef string
	CloneURL string
	IsFork   bool
}

// PRCheckout resolves the checkout reference for a pull request. It does not
// touch the local working tree; the command layer performs the git fetch /
// checkout using these values.
func (c *Client) PRCheckout(ctx context.Context, scope Scope, id int, ssh bool) (CheckoutRef, error) {
	switch c.Kind {
	case KindCloud:
		if scope.Workspace == "" || scope.RepoSlug == "" {
			return CheckoutRef{}, fmt.Errorf("workspace and repo are required; use --workspace/--repo or set a context")
		}
		pr, err := c.cloud.GetPullRequest(ctx, scope.Workspace, scope.RepoSlug, id)
		if err != nil {
			return CheckoutRef{}, c.mapErr(err, fmt.Sprintf("pull request #%d", id))
		}
		branch := pr.Source.Branch.Name
		if branch == "" {
			return CheckoutRef{}, fmt.Errorf("could not determine source branch for pull request #%d", id)
		}
		isFork := pr.Source.Repository.FullName != "" &&
			pr.Destination.Repository.FullName != "" &&
			pr.Source.Repository.FullName != pr.Destination.Repository.FullName
		cloneURL := pickCloneLink(pr.Source.Repository.Links.Clone, ssh)
		return CheckoutRef{Branch: branch, FetchRef: branch, CloneURL: cloneURL, IsFork: isFork}, nil
	case KindDC:
		if scope.ProjectKey == "" || scope.RepoSlug == "" {
			return CheckoutRef{}, fmt.Errorf("project and repo are required; use --project/--repo or set a context")
		}
		pr, err := c.dc.GetPullRequest(ctx, scope.ProjectKey, scope.RepoSlug, id)
		if err != nil {
			return CheckoutRef{}, c.mapErr(err, fmt.Sprintf("pull request #%d", id))
		}
		return CheckoutRef{
			Branch:   pr.FromRef.DisplayID,
			FetchRef: fmt.Sprintf("refs/pull-requests/%d/from", id),
			CloneURL: "",
			IsFork:   false,
		}, nil
	}
	return CheckoutRef{}, fmt.Errorf("unsupported host kind %q", c.Kind)
}

// ApprovePR records the current user's approval. It is idempotent: if the user
// has already approved, it returns Already=true without re-calling the API.
func (c *Client) ApprovePR(ctx context.Context, scope Scope, id int) (*PRMutation, error) {
	switch c.Kind {
	case KindCloud:
		if scope.Workspace == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("workspace and repo are required; use --workspace/--repo or set a context")
		}
		pr, err := c.cloud.GetPullRequest(ctx, scope.Workspace, scope.RepoSlug, id)
		if err != nil {
			return nil, c.mapErr(err, fmt.Sprintf("pull request #%d", id))
		}
		identity, _ := c.cloudCurrentIdentity(ctx)
		if identity != "" && cloudApprovedBy(pr, identity) {
			m := mapCloudPR(pr)
			return &PRMutation{PR: &m, Already: true}, nil
		}
		if err := c.cloud.ApprovePullRequest(ctx, scope.Workspace, scope.RepoSlug, id); err != nil {
			return nil, c.mapErr(err, fmt.Sprintf("pull request #%d", id))
		}
		updated, err := c.cloud.GetPullRequest(ctx, scope.Workspace, scope.RepoSlug, id)
		if err != nil {
			m := mapCloudPR(pr)
			return &PRMutation{PR: &m}, nil
		}
		m := mapCloudPR(updated)
		return &PRMutation{PR: &m}, nil
	case KindDC:
		if scope.ProjectKey == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("project and repo are required; use --project/--repo or set a context")
		}
		pr, err := c.dc.GetPullRequest(ctx, scope.ProjectKey, scope.RepoSlug, id)
		if err != nil {
			return nil, c.mapErr(err, fmt.Sprintf("pull request #%d", id))
		}
		slug := c.dcUsername()
		if slug != "" && dcApprovedBy(pr, slug) {
			m := mapDCPR(pr)
			return &PRMutation{PR: &m, Already: true}, nil
		}
		if err := c.dc.ApprovePullRequest(ctx, scope.ProjectKey, scope.RepoSlug, id); err != nil {
			return nil, c.mapErr(err, fmt.Sprintf("pull request #%d", id))
		}
		m := mapDCPR(pr)
		return &PRMutation{PR: &m}, nil
	}
	return nil, fmt.Errorf("unsupported host kind %q", c.Kind)
}

// MergePRInput configures a merge. Strategy is the friendly alias (squash|merge|
// rebase) or "" for the server default; the adapter maps it to the per-host API
// value. Auto reserves Cloud auto-merge for a later phase and is recorded but
// not yet wired.
type MergePRInput struct {
	Strategy    string
	CloseSource bool
	Message     string
	Auto        bool
}

// MergePR merges a pull request. It is idempotent: an already-merged PR is a
// no-op. DC retries once on a stale-version 409.
func (c *Client) MergePR(ctx context.Context, scope Scope, id int, in MergePRInput) (*PRMutation, error) {
	strategy := normalizeMergeStrategy(c.Kind, in.Strategy)
	switch c.Kind {
	case KindCloud:
		if scope.Workspace == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("workspace and repo are required; use --workspace/--repo or set a context")
		}
		pr, err := c.cloud.GetPullRequest(ctx, scope.Workspace, scope.RepoSlug, id)
		if err != nil {
			return nil, c.mapErr(err, fmt.Sprintf("pull request #%d", id))
		}
		if strings.EqualFold(pr.State, "MERGED") {
			m := mapCloudPR(pr)
			return &PRMutation{PR: &m, Already: true}, nil
		}
		if err := c.cloud.MergePullRequest(ctx, scope.Workspace, scope.RepoSlug, id, in.Message, strategy, in.CloseSource); err != nil {
			return nil, c.mapErr(err, fmt.Sprintf("pull request #%d", id))
		}
		updated, err := c.cloud.GetPullRequest(ctx, scope.Workspace, scope.RepoSlug, id)
		if err != nil {
			m := mapCloudPR(pr)
			return &PRMutation{PR: &m}, nil
		}
		m := mapCloudPR(updated)
		return &PRMutation{PR: &m}, nil
	case KindDC:
		if scope.ProjectKey == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("project and repo are required; use --project/--repo or set a context")
		}
		opts := dc.MergePROptions{
			Message:           in.Message,
			Strategy:          strategy,
			CloseSourceBranch: in.CloseSource,
		}
		return c.dcMutate(ctx, scope, id, "MERGED", fmt.Sprintf("pull request #%d", id), func(version int) error {
			return c.dc.MergePullRequest(ctx, scope.ProjectKey, scope.RepoSlug, id, version, opts)
		})
	}
	return nil, fmt.Errorf("unsupported host kind %q", c.Kind)
}

// DeclinePR declines a pull request. It is idempotent: an already-declined PR
// is a no-op. DC retries once on a stale-version 409.
func (c *Client) DeclinePR(ctx context.Context, scope Scope, id int, message string) (*PRMutation, error) {
	switch c.Kind {
	case KindCloud:
		if scope.Workspace == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("workspace and repo are required; use --workspace/--repo or set a context")
		}
		pr, err := c.cloud.GetPullRequest(ctx, scope.Workspace, scope.RepoSlug, id)
		if err != nil {
			return nil, c.mapErr(err, fmt.Sprintf("pull request #%d", id))
		}
		if strings.EqualFold(pr.State, "DECLINED") {
			m := mapCloudPR(pr)
			return &PRMutation{PR: &m, Already: true}, nil
		}
		if err := c.cloud.DeclinePullRequest(ctx, scope.Workspace, scope.RepoSlug, id, message); err != nil {
			return nil, c.mapErr(err, fmt.Sprintf("pull request #%d", id))
		}
		updated, err := c.cloud.GetPullRequest(ctx, scope.Workspace, scope.RepoSlug, id)
		if err != nil {
			m := mapCloudPR(pr)
			return &PRMutation{PR: &m}, nil
		}
		m := mapCloudPR(updated)
		return &PRMutation{PR: &m}, nil
	case KindDC:
		if scope.ProjectKey == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("project and repo are required; use --project/--repo or set a context")
		}
		return c.dcMutate(ctx, scope, id, "DECLINED", fmt.Sprintf("pull request #%d", id), func(version int) error {
			return c.dc.DeclinePullRequest(ctx, scope.ProjectKey, scope.RepoSlug, id, version, message)
		})
	}
	return nil, fmt.Errorf("unsupported host kind %q", c.Kind)
}

// ReopenPR reopens a declined pull request. It is idempotent: an already-open
// PR is a no-op. DC retries once on a stale-version 409.
func (c *Client) ReopenPR(ctx context.Context, scope Scope, id int) (*PRMutation, error) {
	switch c.Kind {
	case KindCloud:
		if scope.Workspace == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("workspace and repo are required; use --workspace/--repo or set a context")
		}
		pr, err := c.cloud.GetPullRequest(ctx, scope.Workspace, scope.RepoSlug, id)
		if err != nil {
			return nil, c.mapErr(err, fmt.Sprintf("pull request #%d", id))
		}
		if strings.EqualFold(pr.State, "OPEN") {
			m := mapCloudPR(pr)
			return &PRMutation{PR: &m, Already: true}, nil
		}
		if err := c.cloud.ReopenPullRequest(ctx, scope.Workspace, scope.RepoSlug, id); err != nil {
			return nil, c.mapErr(err, fmt.Sprintf("pull request #%d", id))
		}
		updated, err := c.cloud.GetPullRequest(ctx, scope.Workspace, scope.RepoSlug, id)
		if err != nil {
			m := mapCloudPR(pr)
			return &PRMutation{PR: &m}, nil
		}
		m := mapCloudPR(updated)
		return &PRMutation{PR: &m}, nil
	case KindDC:
		if scope.ProjectKey == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("project and repo are required; use --project/--repo or set a context")
		}
		return c.dcMutate(ctx, scope, id, "OPEN", fmt.Sprintf("pull request #%d", id), func(version int) error {
			return c.dc.ReopenPullRequest(ctx, scope.ProjectKey, scope.RepoSlug, id, version)
		})
	}
	return nil, fmt.Errorf("unsupported host kind %q", c.Kind)
}

// CommentPR adds a top-level comment and returns the normalized comment (with
// its server-assigned id). Commenting is intentionally not idempotent: each
// call creates a new comment.
func (c *Client) CommentPR(ctx context.Context, scope Scope, id int, text string) (*Comment, error) {
	switch c.Kind {
	case KindCloud:
		if scope.Workspace == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("workspace and repo are required; use --workspace/--repo or set a context")
		}
		created, err := c.cloud.CreatePullRequestComment(ctx, scope.Workspace, scope.RepoSlug, id, cloud.CommentOptions{Text: text})
		if err != nil {
			return nil, c.mapErr(err, fmt.Sprintf("pull request #%d comment", id))
		}
		return &Comment{ID: created.ID, Text: text}, nil
	case KindDC:
		if scope.ProjectKey == "" || scope.RepoSlug == "" {
			return nil, fmt.Errorf("project and repo are required; use --project/--repo or set a context")
		}
		created, err := c.dc.CreatePullRequestComment(ctx, scope.ProjectKey, scope.RepoSlug, id, dc.CommentOptions{Text: text})
		if err != nil {
			return nil, c.mapErr(err, fmt.Sprintf("pull request #%d comment", id))
		}
		return &Comment{ID: created.ID, Text: text}, nil
	}
	return nil, fmt.Errorf("unsupported host kind %q", c.Kind)
}

// --- DC optimistic-concurrency helper -------------------------------------

// dcMutate performs a DC PR mutation with version handling and idempotency.
// It fetches the PR, short-circuits with Already=true when targetState already
// holds, calls attempt(version), and on a 409 stale-version error re-fetches
// the version (re-checking target state) and retries once. After a successful
// mutation it re-fetches so the returned PR reflects the new state.
//
// targetState is the uppercased DC state that marks "already done" (MERGED,
// DECLINED, OPEN, …); pass "" to skip the pre-check (used by edit, which has
// no single terminal state).
func (c *Client) dcMutate(ctx context.Context, scope Scope, id int, targetState, noun string, attempt func(version int) error) (*PRMutation, error) {
	pr, err := c.dc.GetPullRequest(ctx, scope.ProjectKey, scope.RepoSlug, id)
	if err != nil {
		return nil, c.mapErr(err, noun)
	}
	if targetState != "" && strings.EqualFold(pr.State, targetState) {
		m := mapDCPR(pr)
		return &PRMutation{PR: &m, Already: true}, nil
	}

	if err := attempt(pr.Version); err != nil {
		if isStaleVersion(err) {
			fresh, ferr := c.dc.GetPullRequest(ctx, scope.ProjectKey, scope.RepoSlug, id)
			if ferr != nil {
				return nil, c.mapErr(err, noun)
			}
			if targetState != "" && strings.EqualFold(fresh.State, targetState) {
				m := mapDCPR(fresh)
				return &PRMutation{PR: &m, Already: true}, nil
			}
			if err := attempt(fresh.Version); err != nil {
				return nil, c.mapErr(err, noun)
			}
			pr = fresh
		} else {
			return nil, c.mapErr(err, noun)
		}
	}

	// Re-fetch for an accurate post-mutation state; fall back to the pre-call
	// snapshot when the re-fetch fails so the caller still gets a result.
	updated, err := c.dc.GetPullRequest(ctx, scope.ProjectKey, scope.RepoSlug, id)
	if err != nil {
		m := mapDCPR(pr)
		return &PRMutation{PR: &m}, nil
	}
	m := mapDCPR(updated)
	return &PRMutation{PR: &m}, nil
}

// isStaleVersion reports whether err is an HTTP 409 from a stale
// optimistic-concurrency version.
func isStaleVersion(err error) bool {
	var he *httpx.HTTPError
	if errors.As(err, &he) {
		return he.StatusCode == 409
	}
	return false
}

// --- idempotency & strategy helpers --------------------------------------

// mergeStrategyAliases maps the friendly CLI strategy aliases to the per-host
// API values. Empty strategy preserves the server default.
var mergeStrategyAliases = map[string]map[Kind]string{
	"squash": {KindCloud: "squash", KindDC: "squash"},
	"merge":  {KindCloud: "merge_commit", KindDC: "no-ff"},
	"rebase": {KindCloud: "rebase_fast_forward", KindDC: "rebase-no-ff"},
}

// normalizeMergeStrategy maps a friendly alias (squash|merge|rebase) to the
// per-host API value. An empty strategy (server default) is preserved. Unknown
// values pass through unchanged so power users can supply an exact API id; the
// upstream rejects genuinely invalid values, which surface as a clean error.
func normalizeMergeStrategy(kind Kind, s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	if m, ok := mergeStrategyAliases[s]; ok {
		if v, ok := m[kind]; ok {
			return v
		}
	}
	return s
}

// cloudApprovedBy reports whether identity (a Cloud UUID/account id/nickname)
// has approved pr as a reviewer. Identity matching is tolerant: it accepts a
// match on uuid, account id, or nickname.
func cloudApprovedBy(pr *cloud.PullRequest, identity string) bool {
	if identity == "" {
		return false
	}
	id := strings.TrimSpace(identity)
	for _, p := range pr.Participants {
		if !strings.EqualFold(p.Role, "REVIEWER") {
			continue
		}
		if p.Approved == nil || !*p.Approved {
			continue
		}
		if cloudIdentityMatches(p.User, id) {
			return true
		}
	}
	return false
}

// cloudIdentityMatches reports whether user corresponds to identity.
func cloudIdentityMatches(u cloud.User, id string) bool {
	return strings.EqualFold(strings.TrimSpace(u.UUID), id) ||
		strings.EqualFold(strings.TrimSpace(u.AccountID), id) ||
		strings.EqualFold(strings.TrimSpace(u.Nickname), id) ||
		strings.EqualFold(strings.TrimSpace(u.Username), id)
}

// dcApprovedBy reports whether slug has approved pr (status APPROVED or the
// approved flag set) as a reviewer or participant.
func dcApprovedBy(pr *dc.PullRequest, slug string) bool {
	if slug == "" {
		return false
	}
	for _, r := range pr.Reviewers {
		if !strings.EqualFold(r.User.Slug, slug) && !strings.EqualFold(r.User.Name, slug) {
			continue
		}
		if strings.EqualFold(r.Status, "APPROVED") {
			return true
		}
		if r.Approved != nil && *r.Approved {
			return true
		}
	}
	for _, p := range pr.Participants {
		if !strings.EqualFold(p.User.Slug, slug) && !strings.EqualFold(p.User.Name, slug) {
			continue
		}
		if strings.EqualFold(p.Status, "APPROVED") || p.Approved {
			return true
		}
	}
	return false
}

// cloudCurrentIdentity returns the authenticated Cloud user's identity (UUID
// preferred) and display name. Errors degrade to empty identity; approve's
// idempotency pre-check then simply skips (the approve call still happens).
func (c *Client) cloudCurrentIdentity(ctx context.Context) (identity, display string) {
	u, err := c.cloud.CurrentUser(ctx)
	if err != nil {
		return "", ""
	}
	id := strings.TrimSpace(u.UUID)
	if id == "" {
		id = strings.TrimSpace(u.AccountID)
	}
	if id == "" {
		id = strings.TrimSpace(u.Username)
	}
	disp := u.Display
	if disp == "" {
		disp = u.Nickname
	}
	if disp == "" {
		disp = u.Username
	}
	return id, disp
}

// dcUsername returns the configured DC username slug (the identity used for
// participant matching).
func (c *Client) dcUsername() string {
	if c.Host == nil {
		return ""
	}
	return strings.TrimSpace(c.Host.Username)
}

// pickCloneLink selects the https or ssh clone URL from a Cloud repository's
// clone links. Returns "" when none is present.
func pickCloneLink(links []struct {
	Href string `json:"href"`
	Name string `json:"name"`
}, ssh bool) string {
	want := "https"
	if ssh {
		want = "ssh"
	}
	for _, l := range links {
		if strings.EqualFold(l.Name, want) && l.Href != "" {
			return l.Href
		}
	}
	if len(links) > 0 {
		return links[0].Href
	}
	return ""
}

// mergeReviewers appends items from add that are not already present (case-
// insensitive), preserving order.
func mergeReviewers(base, add []string) []string {
	out := append([]string(nil), base...)
	seen := make(map[string]bool, len(out))
	for _, r := range out {
		seen[strings.ToLower(strings.TrimSpace(r))] = true
	}
	for _, r := range add {
		key := strings.ToLower(strings.TrimSpace(r))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r)
	}
	return out
}

// mergeReviewerChanges returns the reviewer set after adding add and removing
// remove (case-insensitive), deduplicating the result.
func mergeReviewerChanges(current, add, remove []string) []string {
	rem := make(map[string]bool, len(remove))
	for _, r := range remove {
		rem[strings.ToLower(strings.TrimSpace(r))] = true
	}
	out := make([]string, 0, len(current)+len(add))
	seen := make(map[string]bool, len(current)+len(add))
	for _, r := range current {
		key := strings.ToLower(strings.TrimSpace(r))
		if key == "" || rem[key] || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r)
	}
	for _, r := range add {
		key := strings.ToLower(strings.TrimSpace(r))
		if key == "" || rem[key] || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r)
	}
	return out
}

// cloudReviewerIdentities flattens Cloud reviewer users into identity strings
// (UUID preferred) for the add/remove merge.
func cloudReviewerIdentities(reviewers []cloud.User) []string {
	out := make([]string, 0, len(reviewers))
	for _, r := range reviewers {
		switch {
		case strings.TrimSpace(r.UUID) != "":
			out = append(out, r.UUID)
		case strings.TrimSpace(r.AccountID) != "":
			out = append(out, r.AccountID)
		case strings.TrimSpace(r.Nickname) != "":
			out = append(out, r.Nickname)
		default:
			out = append(out, r.Username)
		}
	}
	return out
}

// dcReviewerSlugs flattens DC reviewers into username slugs.
func dcReviewerSlugs(reviewers []dc.PullRequestReviewer) []string {
	out := make([]string, 0, len(reviewers))
	for _, r := range reviewers {
		out = append(out, r.User.Name)
	}
	return out
}

// dcUserSlugs flattens DC default-reviewer users into username slugs.
func dcUserSlugs(users []dc.User) []string {
	out := make([]string, 0, len(users))
	for _, u := range users {
		out = append(out, u.Name)
	}
	return out
}

// dcReviewerSet rebuilds a DC reviewer slice from slugs for a PUT payload.
func dcReviewerSet(slugs []string) []dc.PullRequestReviewer {
	out := make([]dc.PullRequestReviewer, 0, len(slugs))
	for _, s := range slugs {
		out = append(out, dc.PullRequestReviewer{User: dc.User{Name: s}})
	}
	return out
}
