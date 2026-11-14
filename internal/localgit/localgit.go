// Package localgit implements state.Client on top of the git CLI so tag and branch fixes can be applied to
// a local checkout. It never pushes and never touches the working tree: refs are only changed with
// `git update-ref`, and a branch that is checked out in any worktree is never moved or deleted.
package localgit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/jessehouwing/gh-version-action/internal/state"
	"github.com/jessehouwing/gh-version-action/internal/version"
)

// IsIgnored reports whether a version matches the ignore-versions patterns; replaceable for tests or wiring.
var IsIgnored = version.ShouldBeIgnored

// ErrLocalOnly is returned by operations that need the GitHub API.
var ErrLocalOnly = errors.New("not available in local mode: requires the GitHub API")

const releasesSkipped = "release operations are skipped in local mode"

// Client applies ref changes to a local repository.
type Client struct {
	Dir string
}

var _ state.Client = (*Client)(nil)

// New returns a Client for the repository at dir.
func New(dir string) *Client { return &Client{Dir: dir} }

func (c *Client) git(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", c.Dir}, args...)...)
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return strings.TrimSpace(out.String()), errors.New(msg)
	}
	return strings.TrimSpace(out.String()), nil
}

// resolve returns the commit a ref or sha points to, or "" when it does not exist.
func (c *Client) resolve(ctx context.Context, rev string) string {
	out, err := c.git(ctx, "rev-parse", "--verify", "-q", rev+"^{commit}")
	if err != nil {
		return ""
	}
	return out
}

// checkedOut reports whether the branch ref is checked out in any worktree.
func (c *Client) checkedOut(ctx context.Context, ref string) bool {
	out, err := c.git(ctx, "worktree", "list", "--porcelain")
	if err != nil {
		return false
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) == "branch "+ref {
			return true
		}
	}
	return false
}

func validRef(ref string) bool {
	return strings.HasPrefix(ref, "refs/tags/") || strings.HasPrefix(ref, "refs/heads/")
}

func fail(format string, args ...any) state.RefResult {
	return state.RefResult{Error: fmt.Sprintf(format, args...)}
}

// CreateRef creates the tag or branch, or moves it when it exists. Without force an existing ref only
// moves forward (fast-forward), like the GitHub API.
func (c *Client) CreateRef(ctx context.Context, ref, sha string, force bool) state.RefResult {
	if !validRef(ref) {
		return fail("unsupported ref %q", ref)
	}
	target := c.resolve(ctx, sha)
	if target == "" {
		return fail("commit %s does not exist in the local repository (try git fetch --all --tags)", sha)
	}
	current := c.resolve(ctx, ref)
	if current == target {
		return state.RefResult{Success: true}
	}
	if current != "" {
		if strings.HasPrefix(ref, "refs/heads/") && c.checkedOut(ctx, ref) {
			return fail("%s is checked out; refusing to move it", ref)
		}
		if !force {
			if _, err := c.git(ctx, "merge-base", "--is-ancestor", current, target); err != nil {
				return fail("update of %s is not a fast-forward; force is required", ref)
			}
		}
	}
	if _, err := c.git(ctx, "update-ref", ref, target); err != nil {
		return fail("%v", err)
	}
	return state.RefResult{Success: true}
}

// DeleteRef deletes a local tag or branch.
func (c *Client) DeleteRef(ctx context.Context, ref string) state.RefResult {
	if !validRef(ref) {
		return fail("unsupported ref %q", ref)
	}
	if _, err := c.git(ctx, "rev-parse", "--verify", "-q", ref); err != nil {
		return fail("%s does not exist", ref)
	}
	if strings.HasPrefix(ref, "refs/heads/") && c.checkedOut(ctx, ref) {
		return fail("%s is checked out; refusing to delete it", ref)
	}
	if _, err := c.git(ctx, "update-ref", "-d", ref); err != nil {
		return fail("%v", err)
	}
	return state.RefResult{Success: true}
}

func skipped() state.ReleaseResult {
	return state.ReleaseResult{Unfixable: true, Error: releasesSkipped}
}

func (c *Client) CreateRelease(context.Context, string, bool, bool) (state.ReleaseResult, error) {
	return skipped(), nil
}

func (c *Client) PublishRelease(context.Context, int64, *bool) (state.ReleaseResult, error) {
	return skipped(), nil
}

func (c *Client) CreateReleaseWithLatest(context.Context, string, bool, *bool) (state.ReleaseResult, error) {
	return skipped(), nil
}

func (c *Client) DeleteRelease(context.Context, int64) error { return ErrLocalOnly }

func (c *Client) SetReleaseLatest(context.Context, int64) error { return ErrLocalOnly }

func (c *Client) TagProtection(context.Context, string) ([]string, error) { return nil, nil }

func (c *Client) ReleaseImmutable(context.Context, string) (bool, error) { return false, ErrLocalOnly }

func (c *Client) ImmutableReleasesEnabled(context.Context) *bool { return nil }

func (c *Client) EnableImmutableReleases(context.Context) bool { return false }

func (c *Client) RepublishRelease(context.Context, string, *bool) state.RepublishResult {
	return state.RepublishResult{Unfixable: true, Reason: releasesSkipped}
}

func (c *Client) TagRulesets(context.Context, string) ([]state.Ruleset, error) { return nil, nil }

var (
	reLatest  = regexp.MustCompile(`^latest$`)
	reVersion = regexp.MustCompile(`^v\d+(\.\d+){0,2}$`)
	reRemote  = regexp.MustCompile(`^(?:[a-z+]+://)?(?:[^@/]+@)?([^/:]+)(?::\d+)?[/:]([^/]+)/([^/]+?)(?:\.git)?/?$`)
)

type rawRef struct{ name, sha string }

// forEachRef lists refs under prefix with annotated tags peeled to their commit.
func (c *Client) forEachRef(ctx context.Context, prefix string) ([]rawRef, error) {
	out, err := c.git(ctx, "for-each-ref", "--format=%(refname)%09%(objectname)%09%(*objectname)", prefix)
	if err != nil {
		return nil, err
	}
	var refs []rawRef
	for _, l := range strings.Split(out, "\n") {
		f := strings.Split(strings.TrimRight(l, "\r"), "\t")
		if len(f) < 2 || f[0] == "" {
			continue
		}
		sha := f[1]
		if len(f) > 2 && f[2] != "" {
			sha = f[2]
		}
		refs = append(refs, rawRef{f[0], sha})
	}
	return refs, nil
}

// Load reads tags and branches of the repository at dir with git for-each-ref (no network) into a new
// State and returns warnings (local refs behind origin, release checks disabled). Release and
// marketplace data are not available locally, so those checks are set to "none" in the state's config.
func Load(ctx context.Context, dir string, cfg state.Config) (*state.State, []string, error) {
	c := New(dir)
	if _, err := c.git(ctx, "rev-parse", "--git-dir"); err != nil {
		return nil, nil, fmt.Errorf("%s is not a git repository: %w", dir, err)
	}
	var warnings []string

	tagRefs, err := c.forEachRef(ctx, "refs/tags")
	if err != nil {
		return nil, nil, err
	}
	localRefs, err := c.forEachRef(ctx, "refs/heads")
	if err != nil {
		return nil, nil, err
	}
	remoteRefs, err := c.forEachRef(ctx, "refs/remotes/origin")
	if err != nil {
		return nil, nil, err
	}

	var tags, branches []*state.VersionRef
	for _, r := range tagRefs {
		name := strings.TrimPrefix(r.name, "refs/tags/")
		tags = append(tags, newRef(name, r.name, r.sha, state.TypeTag, cfg))
	}

	localSha := map[string]string{}
	for _, r := range localRefs {
		name := strings.TrimPrefix(r.name, "refs/heads/")
		localSha[name] = r.sha
		branches = append(branches, newRef(name, r.name, r.sha, state.TypeBranch, cfg))
	}
	behind := false
	for _, r := range remoteRefs {
		name := strings.TrimPrefix(r.name, "refs/remotes/origin/")
		if name == "HEAD" {
			continue
		}
		local, ok := localSha[name]
		switch {
		case !ok:
			branches = append(branches, newRef(name, "refs/heads/"+name, r.sha, state.TypeBranch, cfg))
			if isVersionOrLatest(name) {
				warnings = append(warnings, fmt.Sprintf("branch %s only exists as origin/%s; using the origin commit", name, name))
			}
		case local != r.sha:
			if _, err := c.git(ctx, "merge-base", "--is-ancestor", local, r.sha); err == nil {
				behind = true
				warnings = append(warnings, fmt.Sprintf("local branch %s is behind origin/%s", name, name))
			}
		}
	}
	if behind {
		warnings = append(warnings, "local refs are behind origin; run `git fetch --tags --force` before applying fixes")
	}

	st := state.New()
	st.Tags, st.Branches = splitVersionRefs(tags), splitVersionRefs(branches)
	if url, err := c.git(ctx, "remote", "get-url", "origin"); err == nil {
		if m := reRemote.FindStringSubmatch(url); m != nil {
			st.Host, st.Owner, st.Repo = m[1], m[2], m[3]
		}
	}

	if cfg.CheckReleases != "none" || cfg.CheckImmutability != "none" || cfg.CheckMarketplace != "none" {
		warnings = append(warnings, "release and marketplace checks need the GitHub API and are skipped in local mode")
	}
	cfg.CheckReleases, cfg.CheckImmutability, cfg.CheckMarketplace = "none", "none", "none"
	st.Config = cfg
	return st, warnings, nil
}

func newRef(name, ref, sha, typ string, cfg state.Config) *state.VersionRef {
	v := state.NewVersionRef(name, ref, sha, typ)
	v.IsIgnored = IsIgnored(name, cfg.IgnoreVersions)
	return v
}

func isVersionOrLatest(name string) bool {
	return reVersion.MatchString(name) || reLatest.MatchString(name)
}

// splitVersionRefs keeps version refs (vN, vN.N, vN.N.N) and at most one 'latest'.
func splitVersionRefs(all []*state.VersionRef) []*state.VersionRef {
	var out []*state.VersionRef
	var latest *state.VersionRef
	for _, r := range all {
		switch {
		case reVersion.MatchString(r.Version):
			out = append(out, r)
		case latest == nil && reLatest.MatchString(r.Version):
			latest = r
		}
	}
	if latest != nil {
		out = append(out, latest)
	}
	return out
}
