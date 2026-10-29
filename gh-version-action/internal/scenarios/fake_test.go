package scenarios

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/jessehouwing/gh-version-action/internal/actions"
	"github.com/jessehouwing/gh-version-action/internal/options"
	"github.com/jessehouwing/gh-version-action/internal/remediation"
	"github.com/jessehouwing/gh-version-action/internal/rules"
	"github.com/jessehouwing/gh-version-action/internal/state"
	"github.com/jessehouwing/gh-version-action/internal/version"
)

func TestMain(m *testing.M) {
	actions.Out = io.Discard
	os.Exit(m.Run())
}

func sha(c byte) string { return strings.Repeat(string(c), 40) }

var (
	c1 = sha('1')
	c2 = sha('2')
)

// repo is an in-memory GitHub repository: the fake state.Client applies writes to it so
// post-fix state can be asserted by reloading it.
type repo struct {
	tags, branches map[string]string
	releases       []*state.ReleaseInfo
	nextID         int64

	immutableEnabled bool
	deletedTags      map[string]bool // tags used by a deleted immutable release (HTTP 422 on release writes)
	failWrites       bool            // every ref write fails
	failRefs         map[string]bool // refs whose writes fail (e.g. "refs/tags/v2")
	blockTagDelete   bool
	rulesets         []state.Ruleset

	writes []string // log of write calls
}

func newRepo() *repo {
	return &repo{tags: map[string]string{}, branches: map[string]string{}, nextID: 100, deletedTags: map[string]bool{}, failRefs: map[string]bool{}}
}

func (r *repo) tag(sha string, names ...string) *repo {
	for _, n := range names {
		r.tags[n] = sha
	}
	return r
}

func (r *repo) branch(sha string, names ...string) *repo {
	for _, n := range names {
		r.branches[n] = sha
	}
	return r
}

type relOpt func(*state.ReleaseInfo)

func draft(r *state.ReleaseInfo)      { r.IsDraft = true }
func prerelease(r *state.ReleaseInfo) { r.IsPrerelease = true }
func immutable(r *state.ReleaseInfo)  { r.IsImmutable = true }
func latest(r *state.ReleaseInfo)     { r.IsLatest = true }

func (r *repo) release(tag string, opts ...relOpt) *state.ReleaseInfo {
	r.nextID++
	rel := &state.ReleaseInfo{TagName: tag, ID: r.nextID, Sha: r.tags[tag]}
	for _, o := range opts {
		o(rel)
	}
	r.releases = append(r.releases, rel)
	return rel
}

func (r *repo) findRelease(tag string) *state.ReleaseInfo {
	for _, rel := range r.releases {
		if rel.TagName == tag {
			return rel
		}
	}
	return nil
}

func (r *repo) refMap(ref string) (map[string]string, string) {
	if n, ok := strings.CutPrefix(ref, "refs/tags/"); ok {
		return r.tags, n
	}
	return r.branches, strings.TrimPrefix(ref, "refs/heads/")
}

// load builds the state the CLI would build from the API (non-version refs are skipped).
func (r *repo) load(cfg state.Config) *state.State {
	st := state.New()
	st.Owner, st.Repo = "o", "r"
	st.Config = cfg
	add := func(m map[string]string, typ, prefix string) []*state.VersionRef {
		names := make([]string, 0, len(m))
		for n := range m {
			names = append(names, n)
		}
		sort.Strings(names)
		var out []*state.VersionRef
		for _, n := range names {
			v := state.NewVersionRef(n, prefix+n, m[n], typ)
			if !v.IsPatch && !v.IsMinor && !v.IsMajor {
				continue
			}
			v.IsIgnored = version.IsIgnored(n, cfg.IgnoreVersions)
			out = append(out, v)
		}
		return out
	}
	st.Tags = add(r.tags, state.TypeTag, "refs/tags/")
	st.Branches = add(r.branches, state.TypeBranch, "refs/heads/")
	for _, rel := range r.releases {
		c := *rel
		c.Sha = r.tags[rel.TagName]
		c.IsIgnored = version.IsIgnored(rel.TagName, cfg.IgnoreVersions)
		st.Releases = append(st.Releases, &c)
	}
	return st
}

func (r *repo) CreateRef(_ context.Context, ref, sha string, force bool) state.RefResult {
	r.writes = append(r.writes, fmt.Sprintf("create %s %s force=%v", ref, sha, force))
	if r.failWrites || r.failRefs[ref] {
		return state.RefResult{Error: "boom"}
	}
	m, n := r.refMap(ref)
	if _, ok := m[n]; ok && !force {
		return state.RefResult{Error: "Reference already exists"}
	}
	m[n] = sha
	return state.RefResult{Success: true}
}

func (r *repo) DeleteRef(_ context.Context, ref string) state.RefResult {
	r.writes = append(r.writes, "delete "+ref)
	if r.failWrites || r.failRefs[ref] || (r.blockTagDelete && strings.HasPrefix(ref, "refs/tags/")) {
		return state.RefResult{Error: "blocked"}
	}
	m, n := r.refMap(ref)
	delete(m, n)
	return state.RefResult{Success: true}
}

func (r *repo) CreateRelease(ctx context.Context, tag string, d bool, _ bool) (state.ReleaseResult, error) {
	return r.CreateReleaseWithLatest(ctx, tag, d, nil)
}

func (r *repo) CreateReleaseWithLatest(_ context.Context, tag string, d bool, _ *bool) (state.ReleaseResult, error) {
	r.writes = append(r.writes, fmt.Sprintf("create-release %s draft=%v", tag, d))
	if r.deletedTags[tag] {
		return state.ReleaseResult{Unfixable: true, Error: "tag_name was used by an immutable release"}, state.ErrUnfixable
	}
	var opts []relOpt
	if d {
		opts = append(opts, draft)
	} else if r.immutableEnabled {
		opts = append(opts, immutable)
	}
	rel := r.release(tag, opts...)
	return state.ReleaseResult{Success: true, ID: rel.ID}, nil
}

func (r *repo) byID(id int64) *state.ReleaseInfo {
	for _, rel := range r.releases {
		if rel.ID == id {
			return rel
		}
	}
	return nil
}

func (r *repo) PublishRelease(_ context.Context, id int64, _ *bool) (state.ReleaseResult, error) {
	rel := r.byID(id)
	if rel == nil {
		return state.ReleaseResult{}, errors.New("not found")
	}
	r.writes = append(r.writes, "publish-release "+rel.TagName)
	if r.deletedTags[rel.TagName] {
		return state.ReleaseResult{Unfixable: true}, state.ErrUnfixable
	}
	rel.IsDraft = false
	rel.IsImmutable = r.immutableEnabled
	return state.ReleaseResult{Success: true, ID: id}, nil
}

func (r *repo) DeleteRelease(_ context.Context, id int64) error {
	for i, rel := range r.releases {
		if rel.ID == id {
			r.writes = append(r.writes, "delete-release "+rel.TagName)
			r.releases = append(r.releases[:i], r.releases[i+1:]...)
			return nil
		}
	}
	return errors.New("not found")
}

func (r *repo) SetReleaseLatest(_ context.Context, id int64) error {
	rel := r.byID(id)
	if rel == nil {
		return errors.New("not found")
	}
	r.writes = append(r.writes, "set-latest "+rel.TagName)
	for _, o := range r.releases {
		o.IsLatest = o == rel
	}
	return nil
}

func (r *repo) TagProtection(context.Context, string) ([]string, error) { return nil, nil }

func (r *repo) ReleaseImmutable(_ context.Context, tag string) (bool, error) {
	if rel := r.findRelease(tag); rel != nil {
		return rel.IsImmutable, nil
	}
	return false, nil
}

func (r *repo) ImmutableReleasesEnabled(context.Context) *bool {
	v := r.immutableEnabled
	return &v
}

func (r *repo) EnableImmutableReleases(context.Context) bool {
	r.writes = append(r.writes, "enable-immutable-releases")
	r.immutableEnabled = true
	return true
}

func (r *repo) RepublishRelease(_ context.Context, tag string, _ *bool) state.RepublishResult {
	r.writes = append(r.writes, "republish-release "+tag)
	rel := r.findRelease(tag)
	if rel == nil {
		return state.RepublishResult{Reason: "not found"}
	}
	if r.deletedTags[tag] {
		return state.RepublishResult{Unfixable: true, Reason: "422"}
	}
	rel.IsDraft = false
	rel.IsImmutable = r.immutableEnabled
	return state.RepublishResult{Success: true}
}

func (r *repo) TagRulesets(context.Context, string) ([]state.Ruleset, error) { return r.rulesets, nil }

var _ state.Client = (*repo)(nil)

// cfgOpt mutates a configuration.
type cfgOpt func(*state.Config)

// tagCfg is the default configuration with release and marketplace checks off so scenarios focus on refs.
func tagCfg(opts ...cfgOpt) state.Config {
	c := options.DefaultConfig()
	c.CheckReleases, c.CheckImmutability, c.CheckMarketplace = "none", "none", "none"
	c.IgnorePreviewReleases = false
	for _, o := range opts {
		o(&c)
	}
	return c
}

// relCfg is the default configuration with release checks on.
func relCfg(opts ...cfgOpt) state.Config {
	c := tagCfg()
	c.CheckReleases, c.CheckImmutability = "error", "error"
	for _, o := range opts {
		o(&c)
	}
	return c
}

func minor(v string) cfgOpt        { return func(c *state.Config) { c.CheckMinorVersion = v } }
func releases(v string) cfgOpt     { return func(c *state.Config) { c.CheckReleases = v } }
func immutability(v string) cfgOpt { return func(c *state.Config) { c.CheckImmutability = v } }
func floating(v string) cfgOpt     { return func(c *state.Config) { c.FloatingVersionsUse = v } }
func ignorePreview(b bool) cfgOpt  { return func(c *state.Config) { c.IgnorePreviewReleases = b } }
func ignoreVersions(s ...string) cfgOpt {
	return func(c *state.Config) { c.IgnoreVersions = s }
}

// validate loads the repo and runs every rule, returning the state.
func validate(t *testing.T, r *repo, cfg state.Config) *state.State {
	t.Helper()
	st := r.load(cfg)
	rules.Run(st, &st.Config, nil)
	return st
}

func fixAll(t *testing.T, r *repo, st *state.State) {
	t.Helper()
	if err := remediation.Fix(context.Background(), st, remediation.Options{Mode: remediation.AutoFix, Client: r}); err != nil {
		t.Fatal(err)
	}
}

// commands returns every manual command of every issue, one per line, like the PowerShell remediation output.
func commands(st *state.State) string {
	var out []string
	for _, i := range st.Issues {
		if i.Action != nil {
			out = append(out, i.Action.ManualCommands(st)...)
		}
	}
	return strings.Join(out, "\n")
}

func messages(st *state.State) string {
	var out []string
	for _, i := range st.Issues {
		out = append(out, i.Message)
	}
	return strings.Join(out, "\n")
}

func types(st *state.State) []string {
	var out []string
	for _, i := range st.Issues {
		out = append(out, i.Type)
	}
	return out
}

func issuesOfType(st *state.State, typ string) []*state.Issue {
	var out []*state.Issue
	for _, i := range st.Issues {
		if i.Type == typ {
			out = append(out, i)
		}
	}
	return out
}

func issuesFor(st *state.State, prefix string) []*state.Issue {
	var out []*state.Issue
	for _, i := range st.Issues {
		if strings.HasPrefix(i.Version, prefix) {
			out = append(out, i)
		}
	}
	return out
}

func has(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected %q in:\n%s", needle, haystack)
	}
}

func hasNot(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("did not expect %q in:\n%s", needle, haystack)
	}
}

func count(s, line string) int {
	n := 0
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) == line {
			n++
		}
	}
	return n
}

func equal[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func clean(t *testing.T, r *repo, cfg state.Config) {
	t.Helper()
	st := validate(t, r, cfg)
	if len(st.Issues) != 0 {
		t.Errorf("expected no issues after fix, got: %v (%s)", types(st), messages(st))
	}
}
