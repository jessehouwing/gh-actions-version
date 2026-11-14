package localgit

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/actions"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

var ctx = context.Background()

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, errb.String())
	}
	return strings.TrimSpace(out.String())
}

func commit(t *testing.T, dir, msg string) string {
	t.Helper()
	git(t, dir, "commit", "--allow-empty", "-m", msg)
	return git(t, dir, "rev-parse", "HEAD")
}

func newRepo(t *testing.T) (dir, c1, c2 string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir = t.TempDir()
	git(t, dir, "init", "-b", "main")
	c1 = commit(t, dir, "one")
	c2 = commit(t, dir, "two")
	return
}

func sha(t *testing.T, dir, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--verify", "-q", ref+"^{commit}")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func TestLoad(t *testing.T) {
	t.Run("reads tags (annotated tags peeled), branches, ignores non-version refs and flags ignored versions", func(t *testing.T) {
		dir, c1, c2 := newRepo(t)
		git(t, dir, "tag", "v1.0.0", c1)
		git(t, dir, "tag", "-a", "v1.1.0", "-m", "annotated", c2)
		git(t, dir, "tag", "v1", c2)
		git(t, dir, "tag", "not-a-version", c2)
		git(t, dir, "tag", "latest", c2)
		git(t, dir, "branch", "v2", c1)
		git(t, dir, "branch", "feature", c1)
		st, warnings, err := Load(ctx, dir, state.Config{CheckReleases: "none", CheckImmutability: "none", CheckMarketplace: "none", IgnoreVersions: []string{"v1.0.*"}})
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]*state.VersionRef{}
		for _, r := range st.Tags {
			got["t:"+r.Version] = r
		}
		for _, r := range st.Branches {
			got["b:"+r.Version] = r
		}
		if len(got) != 5 || got["t:v1.1.0"].Sha != c2 || got["t:v1.0.0"].Sha != c1 || got["b:v2"].Sha != c1 || got["t:latest"] == nil {
			t.Fatalf("refs: %v", got)
		}
		if !got["t:v1.0.0"].IsIgnored || got["t:v1.1.0"].IsIgnored || !got["t:v1.0.0"].IsPatch {
			t.Fatal("ignored/parsed flags wrong")
		}
		if got["t:v1.1.0"].Ref != "refs/tags/v1.1.0" || got["b:v2"].Ref != "refs/heads/v2" || got["b:v2"].Type != state.TypeBranch {
			t.Fatal("ref/type wrong")
		}
		if len(warnings) != 0 {
			t.Fatalf("warnings %v", warnings)
		}
	})
	t.Run("disables release checks and warns", func(t *testing.T) {
		dir, _, _ := newRepo(t)
		st, warnings, err := Load(ctx, dir, state.Config{CheckReleases: "error", CheckImmutability: "error", CheckMarketplace: "error"})
		if err != nil || st.Config.CheckReleases != "none" || st.Config.CheckImmutability != "none" || st.Config.CheckMarketplace != "none" || len(warnings) != 1 {
			t.Fatalf("%v %+v %v", err, st, warnings)
		}
	})
	t.Run("fails outside a git repository", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git not available")
		}
		if _, _, err := Load(ctx, t.TempDir(), state.Config{}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("reads origin branches, parses the remote and warns when local branches are behind origin", func(t *testing.T) {
		dir, c1, _ := newRepo(t)
		git(t, dir, "branch", "v1", c1)
		clone := filepath.Join(t.TempDir(), "clone")
		git(t, dir, "clone", "-q", dir, clone)
		git(t, clone, "remote", "set-url", "origin", "https://github.com/octo/widgets.git")
		git(t, clone, "checkout", "-q", "-b", "v1", "origin/v1")
		// advance origin/v1 beyond the local v1 and add a remote-only branch
		git(t, clone, "checkout", "-q", "main")
		c3 := commit(t, clone, "three")
		git(t, clone, "update-ref", "refs/remotes/origin/v1", c3)
		git(t, clone, "update-ref", "refs/remotes/origin/v2", c3)
		st, warnings, err := Load(ctx, clone, state.Config{CheckReleases: "none", CheckImmutability: "none", CheckMarketplace: "none"})
		if err != nil {
			t.Fatal(err)
		}
		if st.Owner != "octo" || st.Repo != "widgets" || st.Host != "github.com" {
			t.Fatalf("remote %s/%s %s", st.Owner, st.Repo, st.Host)
		}
		if v := st.FindVersion("v2", state.TypeBranch); v == nil || v.Sha != c3 || v.Ref != "refs/heads/v2" {
			t.Fatalf("remote-only branch: %+v", v)
		}
		all := strings.Join(warnings, "\n")
		for _, w := range []string{"local branch v1 is behind origin/v1", "only exists as origin/v2", "git fetch --tags --force"} {
			if !strings.Contains(all, w) {
				t.Fatalf("missing warning %q in %v", w, warnings)
			}
		}
	})
}

func TestCreateRef(t *testing.T) {
	t.Run("creates a tag and a branch at the sha", func(t *testing.T) {
		dir, c1, _ := newRepo(t)
		c := New(dir)
		if !c.CreateRef(ctx, "refs/tags/v1", c1, false).Success || sha(t, dir, "refs/tags/v1") != c1 {
			t.Fatal("tag")
		}
		if !c.CreateRef(ctx, "refs/heads/v1", c1, false).Success || sha(t, dir, "refs/heads/v1") != c1 {
			t.Fatal("branch")
		}
	})
	t.Run("is a no-op success when the ref already points at the sha", func(t *testing.T) {
		dir, c1, _ := newRepo(t)
		git(t, dir, "tag", "v1", c1)
		if !New(dir).CreateRef(ctx, "refs/tags/v1", c1, false).Success {
			t.Fatal("expected success")
		}
	})
	t.Run("moves a tag forward without force and refuses non-fast-forward without force", func(t *testing.T) {
		dir, c1, c2 := newRepo(t)
		c := New(dir)
		git(t, dir, "tag", "v1", c1)
		if !c.CreateRef(ctx, "refs/tags/v1", c2, false).Success || sha(t, dir, "refs/tags/v1") != c2 {
			t.Fatal("fast-forward")
		}
		res := c.CreateRef(ctx, "refs/tags/v1", c1, false)
		if res.Success || !strings.Contains(res.Error, "fast-forward") || sha(t, dir, "refs/tags/v1") != c2 {
			t.Fatalf("%+v", res)
		}
		if !c.CreateRef(ctx, "refs/tags/v1", c1, true).Success || sha(t, dir, "refs/tags/v1") != c1 {
			t.Fatal("force")
		}
	})
	t.Run("force-moves a branch that is not checked out", func(t *testing.T) {
		dir, c1, c2 := newRepo(t)
		git(t, dir, "branch", "v1", c2)
		if !New(dir).CreateRef(ctx, "refs/heads/v1", c1, true).Success || sha(t, dir, "refs/heads/v1") != c1 {
			t.Fatal("force branch")
		}
	})
	t.Run("refuses to move or delete the checked-out branch and leaves the working tree untouched", func(t *testing.T) {
		dir, c1, c2 := newRepo(t)
		c := New(dir)
		if res := c.CreateRef(ctx, "refs/heads/main", c1, true); res.Success || !strings.Contains(res.Error, "checked out") {
			t.Fatalf("%+v", res)
		}
		if res := c.DeleteRef(ctx, "refs/heads/main"); res.Success {
			t.Fatal("deleted checked-out branch")
		}
		if sha(t, dir, "HEAD") != c2 || git(t, dir, "status", "--porcelain") != "" {
			t.Fatal("repository changed")
		}
	})
	t.Run("fails for unknown commits and unsupported refs", func(t *testing.T) {
		dir, _, _ := newRepo(t)
		c := New(dir)
		if c.CreateRef(ctx, "refs/tags/v1", strings.Repeat("a", 40), false).Success || c.CreateRef(ctx, "refs/notes/x", "HEAD", false).Success {
			t.Fatal("expected failures")
		}
	})
}

func TestDeleteRef(t *testing.T) {
	t.Run("deletes tags and branches", func(t *testing.T) {
		dir, c1, _ := newRepo(t)
		git(t, dir, "tag", "v1", c1)
		git(t, dir, "branch", "v2", c1)
		c := New(dir)
		if !c.DeleteRef(ctx, "refs/tags/v1").Success || !c.DeleteRef(ctx, "refs/heads/v2").Success {
			t.Fatal("delete")
		}
		if sha(t, dir, "refs/tags/v1") != "" || sha(t, dir, "refs/heads/v2") != "" {
			t.Fatal("refs remain")
		}
	})
	t.Run("fails for a missing ref", func(t *testing.T) {
		dir, _, _ := newRepo(t)
		if New(dir).DeleteRef(ctx, "refs/tags/nope").Success {
			t.Fatal("expected failure")
		}
	})
}

func TestReleaseMethodsAreSkipped(t *testing.T) {
	c := New(t.TempDir())
	t.Run("release writes return an unfixable skipped result", func(t *testing.T) {
		if r, err := c.CreateReleaseWithLatest(ctx, "v1.0.0", false, nil); err != nil || r.Success || !r.Unfixable {
			t.Fatal("create")
		}
		if r, err := c.CreateRelease(ctx, "v1.0.0", false, false); err != nil || r.Success || !r.Unfixable {
			t.Fatal("create")
		}
		if r, err := c.PublishRelease(ctx, 1, nil); err != nil || r.Success || !r.Unfixable {
			t.Fatal("publish")
		}
		if r := c.RepublishRelease(ctx, "v1.0.0", nil); r.Success || !r.Unfixable {
			t.Fatal("republish")
		}
		if c.DeleteRelease(ctx, 1) == nil || c.SetReleaseLatest(ctx, 1) == nil || c.EnableImmutableReleases(ctx) || c.ImmutableReleasesEnabled(ctx) != nil {
			t.Fatal("expected unsupported")
		}
	})
	t.Run("release reads return no data", func(t *testing.T) {
		if _, err := c.ReleaseImmutable(ctx, "v1"); err == nil {
			t.Fatal("expected error")
		}
		if rs, err := c.TagRulesets(ctx, "v1"); err != nil || rs != nil {
			t.Fatal("rulesets")
		}
	})
}

// TestActions runs the real remediation actions against a temp repository with an origin remote and
// verifies that nothing is pushed.
func TestActions(t *testing.T) {
	setup := func(t *testing.T) (dir, origin, c1, c2 string, st *state.State) {
		dir, c1, c2 = newRepo(t)
		origin = filepath.Join(t.TempDir(), "origin.git")
		git(t, dir, "init", "-q", "--bare", origin)
		git(t, dir, "remote", "add", "origin", origin)
		git(t, dir, "push", "-q", "origin", "main")
		st, _, err := Load(ctx, dir, state.Config{CheckReleases: "none", CheckImmutability: "none", CheckMarketplace: "none"})
		if err != nil {
			t.Fatal(err)
		}
		actions.Out = &bytes.Buffer{}
		return dir, origin, c1, c2, st
	}
	originRefs := func(t *testing.T, origin string) string { return git(t, origin, "--git-dir="+origin, "for-each-ref") }
	t.Run("CreateTagAction, UpdateTagAction, DeleteTagAction", func(t *testing.T) {
		dir, origin, c1, c2, st := setup(t)
		before := originRefs(t, origin)
		c := New(dir)
		if !actions.NewCreateTagAction("v1", c1).Execute(ctx, st, c) || sha(t, dir, "refs/tags/v1") != c1 {
			t.Fatal("create")
		}
		if !actions.NewUpdateTagAction("v1", c2, false).Execute(ctx, st, c) || sha(t, dir, "refs/tags/v1") != c2 {
			t.Fatal("update")
		}
		if !actions.NewUpdateTagAction("v1", c1, true).Execute(ctx, st, c) || sha(t, dir, "refs/tags/v1") != c1 {
			t.Fatal("force update")
		}
		if !actions.NewDeleteTagAction("v1").Execute(ctx, st, c) || sha(t, dir, "refs/tags/v1") != "" {
			t.Fatal("delete")
		}
		if originRefs(t, origin) != before {
			t.Fatal("origin changed")
		}
	})
	t.Run("CreateBranchAction, UpdateBranchAction, DeleteBranchAction", func(t *testing.T) {
		dir, origin, c1, c2, st := setup(t)
		before := originRefs(t, origin)
		c := New(dir)
		if !actions.NewCreateBranchAction("v1", c1).Execute(ctx, st, c) || sha(t, dir, "refs/heads/v1") != c1 {
			t.Fatal("create")
		}
		if !actions.NewUpdateBranchAction("v1", c2, false).Execute(ctx, st, c) || sha(t, dir, "refs/heads/v1") != c2 {
			t.Fatal("update")
		}
		if !actions.NewUpdateBranchAction("v1", c1, true).Execute(ctx, st, c) || sha(t, dir, "refs/heads/v1") != c1 {
			t.Fatal("force update")
		}
		if !actions.NewDeleteBranchAction("v1").Execute(ctx, st, c) || sha(t, dir, "refs/heads/v1") != "" {
			t.Fatal("delete")
		}
		if originRefs(t, origin) != before {
			t.Fatal("origin changed")
		}
	})
	t.Run("ConvertBranchToTagAction converts a branch to a tag", func(t *testing.T) {
		dir, origin, c1, _, _ := setup(t)
		git(t, dir, "branch", "v1", c1)
		st, _, _ := Load(ctx, dir, state.Config{})
		before := originRefs(t, origin)
		if !actions.NewConvertBranchToTagAction("v1", c1).Execute(ctx, st, New(dir)) {
			t.Fatal("convert")
		}
		if sha(t, dir, "refs/tags/v1") != c1 || sha(t, dir, "refs/heads/v1") != "" || originRefs(t, origin) != before {
			t.Fatal("conversion result wrong")
		}
	})
	t.Run("ConvertBranchToTagAction only deletes the branch when the tag already exists", func(t *testing.T) {
		dir, _, c1, c2, _ := setup(t)
		git(t, dir, "branch", "v1", c1)
		git(t, dir, "tag", "v1", c2)
		st, _, _ := Load(ctx, dir, state.Config{})
		if !actions.NewConvertBranchToTagAction("v1", c1).Execute(ctx, st, New(dir)) || sha(t, dir, "refs/tags/v1") != c2 || sha(t, dir, "refs/heads/v1") != "" {
			t.Fatal("expected only branch deleted")
		}
	})
	t.Run("ConvertTagToBranchAction converts a tag to a branch", func(t *testing.T) {
		dir, origin, c1, _, _ := setup(t)
		git(t, dir, "tag", "v1", c1)
		st, _, _ := Load(ctx, dir, state.Config{})
		before := originRefs(t, origin)
		if !actions.NewConvertTagToBranchAction("v1", c1).Execute(ctx, st, New(dir)) {
			t.Fatal("convert")
		}
		if sha(t, dir, "refs/heads/v1") != c1 || sha(t, dir, "refs/tags/v1") != "" || originRefs(t, origin) != before {
			t.Fatal("conversion result wrong")
		}
	})
	t.Run("ConvertTagToBranchAction only deletes the tag when the branch already exists", func(t *testing.T) {
		dir, _, c1, c2, _ := setup(t)
		git(t, dir, "tag", "v1", c1)
		git(t, dir, "branch", "v1", c2)
		st, _, _ := Load(ctx, dir, state.Config{})
		if !actions.NewConvertTagToBranchAction("v1", c1).Execute(ctx, st, New(dir)) || sha(t, dir, "refs/heads/v1") != c2 || sha(t, dir, "refs/tags/v1") != "" {
			t.Fatal("expected only tag deleted")
		}
	})
	t.Run("release actions are reported as unfixable and change nothing", func(t *testing.T) {
		dir, _, _, _, st := setup(t)
		a := actions.NewCreateReleaseAction("v1.0.0", false, false, "")
		i := state.NewIssue("missing_release", "error", "Release required")
		i.Version, i.Action = "v1.0.0", a
		st.AddIssue(i)
		if a.Execute(ctx, st, New(dir)) || i.Status == state.StatusFixed {
			t.Fatal("release must not be created locally")
		}
	})
}
