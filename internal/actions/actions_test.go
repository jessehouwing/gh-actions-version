package actions

import (
	"context"
	"errors"
	"io"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/state"
)

type fakeClient struct {
	createRef      state.RefResult
	deleteRef      state.RefResult
	createRelease  state.ReleaseResult
	createErr      error
	publish        state.ReleaseResult
	deleteRelease  error
	setLatest      error
	republish      state.RepublishResult
	immutable      bool
	immutableErr   error
	enabled        *bool
	enableOK       bool
	rulesets       []state.Ruleset
	createRefCalls []refCall
	deleteRefCalls []string
	createRelCalls []relCall
	publishCalls   []publishCall
	deleteRelIDs   []int64
	setLatestIDs   []int64
	republishCalls int
	immutableCalls int
	enableCalls    int
}

type refCall struct {
	ref, sha string
	force    bool
}
type relCall struct {
	tag        string
	draft      bool
	makeLatest *bool
}
type publishCall struct {
	id         int64
	makeLatest *bool
}

func newFake() *fakeClient {
	return &fakeClient{
		createRef:     state.RefResult{Success: true},
		deleteRef:     state.RefResult{Success: true},
		createRelease: state.ReleaseResult{Success: true},
		publish:       state.ReleaseResult{Success: true},
		republish:     state.RepublishResult{Success: true},
	}
}

func (f *fakeClient) CreateRef(_ context.Context, ref, sha string, force bool) state.RefResult {
	f.createRefCalls = append(f.createRefCalls, refCall{ref, sha, force})
	return f.createRef
}
func (f *fakeClient) DeleteRef(_ context.Context, ref string) state.RefResult {
	f.deleteRefCalls = append(f.deleteRefCalls, ref)
	return f.deleteRef
}
func (f *fakeClient) CreateRelease(context.Context, string, bool, bool) (state.ReleaseResult, error) {
	return state.ReleaseResult{}, errors.New("unused")
}
func (f *fakeClient) PublishRelease(_ context.Context, id int64, ml *bool) (state.ReleaseResult, error) {
	f.publishCalls = append(f.publishCalls, publishCall{id, ml})
	return f.publish, nil
}
func (f *fakeClient) DeleteRelease(_ context.Context, id int64) error {
	f.deleteRelIDs = append(f.deleteRelIDs, id)
	return f.deleteRelease
}
func (f *fakeClient) SetReleaseLatest(_ context.Context, id int64) error {
	f.setLatestIDs = append(f.setLatestIDs, id)
	return f.setLatest
}
func (f *fakeClient) TagProtection(context.Context, string) ([]string, error) { return nil, nil }
func (f *fakeClient) CreateReleaseWithLatest(_ context.Context, tag string, draft bool, ml *bool) (state.ReleaseResult, error) {
	f.createRelCalls = append(f.createRelCalls, relCall{tag, draft, ml})
	return f.createRelease, f.createErr
}
func (f *fakeClient) ReleaseImmutable(context.Context, string) (bool, error) {
	f.immutableCalls++
	return f.immutable, f.immutableErr
}
func (f *fakeClient) ImmutableReleasesEnabled(context.Context) *bool { return f.enabled }
func (f *fakeClient) EnableImmutableReleases(context.Context) bool {
	f.enableCalls++
	return f.enableOK
}
func (f *fakeClient) RepublishRelease(context.Context, string, *bool) state.RepublishResult {
	f.republishCalls++
	return f.republish
}
func (f *fakeClient) TagRulesets(context.Context, string) ([]state.Ruleset, error) {
	return f.rulesets, nil
}

func newState() *state.State {
	st := state.New()
	st.Owner = "test-owner"
	st.Repo = "test-repo"
	return st
}

func boolp(b bool) *bool { return &b }

func addIssue(st *state.State, typ, version, status string, a state.Action) *state.Issue {
	i := state.NewIssue(typ, state.SeverityError, "msg")
	i.Version = version
	i.Status = status
	i.Action = a
	st.AddIssue(i)
	return i
}

func match(t *testing.T, s, pattern string) {
	t.Helper()
	if !regexp.MustCompile(pattern).MatchString(s) {
		t.Errorf("%q does not match %q", s, pattern)
	}
}

func notMatch(t *testing.T, s, pattern string) {
	t.Helper()
	if regexp.MustCompile(pattern).MatchString(s) {
		t.Errorf("%q unexpectedly matches %q", s, pattern)
	}
}

func eq[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMain(m *testing.M) {
	Out = io.Discard
	m.Run()
}

var ctx = context.Background()

func TestRemediationActionBase(t *testing.T) {
	t.Run("Should set properties correctly", func(t *testing.T) {
		a := NewCreateTagAction("v1.0.0", "abc123")
		eq(t, a.Description(), "Create tag")
		eq(t, a.Version(), "v1.0.0")
		eq(t, a.ManualCommandsLanguage(), "bash")
	})
}

func TestReleaseRemediationActionBase(t *testing.T) {
	t.Run("Should set TagName property", func(t *testing.T) {
		a := NewCreateReleaseAction("v1.0.0", true, false, "")
		eq(t, a.TagName, "v1.0.0")
		eq(t, a.Version(), "v1.0.0")
	})
	t.Run("Should reclassify the issue as manually fixable when Execute encounters 422", func(t *testing.T) {
		st := newState()
		i := addIssue(st, "release_missing", "v1.0.0", state.StatusPending, nil)
		f := newFake()
		f.createRelease = state.ReleaseResult{Unfixable: true}
		f.createErr = state.ErrUnfixable
		a := NewCreateReleaseAction("v1.0.0", true, false, "")
		eq(t, a.Execute(ctx, st, f), false)
		eq(t, i.Finding, "patch_tag_locked_by_deleted_release")
		eq(t, i.Status, state.StatusManualFixRequired)
	})
	t.Run("Should not treat a generic unfixable result as a burned tag", func(t *testing.T) {
		st := newState()
		st.Config.AllowDestructiveTagDeletion = true
		i := addIssue(st, "release_missing", "v1.0.0", state.StatusPending, nil)
		f := newFake()
		f.createRelease = state.ReleaseResult{Unfixable: true}
		eq(t, NewCreateReleaseAction("v1.0.0", false, true, "").Execute(ctx, st, f), false)
		eq(t, i.Finding, "release_missing")
		eq(t, len(f.deleteRefCalls), 0)
	})
	t.Run("Should return an ignore-versions YAML snippet instead of a gh command when issue is unfixable", func(t *testing.T) {
		st := newState()
		addIssue(st, "release_missing", "v1.0.0", state.StatusUnfixable, nil)
		cmds := NewCreateReleaseAction("v1.0.0", true, false, "").ManualCommands(st)
		if len(cmds) == 0 {
			t.Fatal("no commands")
		}
		match(t, strings.Join(cmds, "\n"), `versions:\s*\["v1\.0\.0"\]`)
		match(t, strings.Join(cmds, "\n"), `scope: selection`)
	})
	t.Run("Should return manual commands when issue is not unfixable", func(t *testing.T) {
		st := newState()
		addIssue(st, "release_missing", "v1.0.0", state.StatusPending, nil)
		if len(NewCreateReleaseAction("v1.0.0", true, false, "").ManualCommands(st)) == 0 {
			t.Error("expected commands")
		}
	})
}

func TestCreateTagAction(t *testing.T) {
	t.Run("Should create action with correct properties", func(t *testing.T) {
		a := NewCreateTagAction("v1.0.0", "abc123")
		eq(t, a.TagName, "v1.0.0")
		eq(t, a.Sha, "abc123")
		eq(t, a.Priority(), 20)
		eq(t, a.Description(), "Create tag")
		eq(t, a.Version(), "v1.0.0")
	})
	t.Run("Should generate correct manual commands", func(t *testing.T) {
		cmds := NewCreateTagAction("v1.0.0", "abc123").ManualCommands(newState())
		eq(t, len(cmds), 1)
		eq(t, cmds[0], "git push origin abc123:refs/tags/v1.0.0")
	})
	t.Run("Should include full SHA in command", func(t *testing.T) {
		cmds := NewCreateTagAction("v2.1.3", "deadbeef1234567890").ManualCommands(newState())
		match(t, cmds[0], "deadbeef1234567890:refs/tags/v2.1.3")
	})
	t.Run("Should call CreateRef with correct parameters", func(t *testing.T) {
		f := newFake()
		NewCreateTagAction("v1.0.0", "abc123").Execute(ctx, newState(), f)
		eq(t, len(f.createRefCalls), 1)
		eq(t, f.createRefCalls[0], refCall{"refs/tags/v1.0.0", "abc123", false})
	})
	t.Run("Should return true on success", func(t *testing.T) {
		eq(t, NewCreateTagAction("v1.0.0", "abc123").Execute(ctx, newState(), newFake()), true)
	})
	t.Run("Should return false on failure", func(t *testing.T) {
		f := newFake()
		f.createRef = state.RefResult{}
		eq(t, NewCreateTagAction("v1.0.0", "abc123").Execute(ctx, newState(), f), false)
	})
	t.Run("Should mark issue as manual_fix_required when workflow permission error occurs", func(t *testing.T) {
		f := newFake()
		f.createRef = state.RefResult{RequiresManualFix: true}
		st := newState()
		a := NewCreateTagAction("v1.0.0", "abc123")
		i := addIssue(st, "missing_tag", "v1.0.0", state.StatusPending, a)
		eq(t, a.Execute(ctx, st, f), false)
		eq(t, i.Status, state.StatusManualFixRequired)
		match(t, i.Message, "workflows.*permission")
	})
}

func TestUpdateTagAction(t *testing.T) {
	t.Run("Should create action with force flag", func(t *testing.T) {
		a := NewUpdateTagAction("v1.0.0", "abc123", true)
		eq(t, a.TagName, "v1.0.0")
		eq(t, a.Sha, "abc123")
		eq(t, a.Force, true)
		eq(t, a.Priority(), 20)
	})
	t.Run("Should create action without force flag", func(t *testing.T) {
		eq(t, NewUpdateTagAction("v1.0.0", "abc123", false).Force, false)
	})
	t.Run("Should generate correct manual commands with force", func(t *testing.T) {
		cmds := NewUpdateTagAction("v1.0.0", "abc123", true).ManualCommands(newState())
		eq(t, len(cmds), 1)
		match(t, cmds[0], "--force")
		match(t, cmds[0], "abc123:refs/tags/v1.0.0")
	})
	t.Run("Should generate correct manual commands without force", func(t *testing.T) {
		cmds := NewUpdateTagAction("v1.0.0", "abc123", false).ManualCommands(newState())
		eq(t, len(cmds), 1)
		notMatch(t, cmds[0], "--force")
	})
	t.Run("Should call CreateRef with force=true when specified", func(t *testing.T) {
		f := newFake()
		NewUpdateTagAction("v1.0.0", "abc123", true).Execute(ctx, newState(), f)
		eq(t, len(f.createRefCalls), 1)
		eq(t, f.createRefCalls[0], refCall{"refs/tags/v1.0.0", "abc123", true})
	})
	t.Run("Should call CreateRef with force=false when not specified", func(t *testing.T) {
		f := newFake()
		NewUpdateTagAction("v1.0.0", "abc123", false).Execute(ctx, newState(), f)
		eq(t, len(f.createRefCalls), 1)
		eq(t, f.createRefCalls[0].force, false)
	})
	t.Run("Should return true on success", func(t *testing.T) {
		eq(t, NewUpdateTagAction("v1.0.0", "abc123", true).Execute(ctx, newState(), newFake()), true)
	})
	t.Run("Should mark issue as manual_fix_required when workflow permission error occurs", func(t *testing.T) {
		f := newFake()
		f.createRef = state.RefResult{RequiresManualFix: true}
		st := newState()
		a := NewUpdateTagAction("v1", "abc123", true)
		i := addIssue(st, "floating_version_mismatch", "v1", state.StatusPending, a)
		eq(t, a.Execute(ctx, st, f), false)
		eq(t, i.Status, state.StatusManualFixRequired)
		match(t, i.Message, "workflows.*permission")
	})
}

func TestDeleteTagAction(t *testing.T) {
	t.Run("Should have highest priority (delete first)", func(t *testing.T) {
		eq(t, NewDeleteTagAction("v1.0.0").Priority(), 10)
	})
	t.Run("Should set TagName correctly", func(t *testing.T) {
		a := NewDeleteTagAction("v1.0.0")
		eq(t, a.TagName, "v1.0.0")
		eq(t, a.Description(), "Delete tag")
		eq(t, a.Version(), "v1.0.0")
	})
	t.Run("Should generate correct manual commands", func(t *testing.T) {
		cmds := NewDeleteTagAction("v1.0.0").ManualCommands(newState())
		eq(t, len(cmds), 2)
		eq(t, cmds[0], "git tag -d v1.0.0")
		eq(t, cmds[1], "git push origin :refs/tags/v1.0.0")
	})
	t.Run("Should call DeleteRef with correct parameters", func(t *testing.T) {
		f := newFake()
		NewDeleteTagAction("v1.0.0").Execute(ctx, newState(), f)
		eq(t, strings.Join(f.deleteRefCalls, ","), "refs/tags/v1.0.0")
	})
	t.Run("Should return true on success", func(t *testing.T) {
		eq(t, NewDeleteTagAction("v1.0.0").Execute(ctx, newState(), newFake()), true)
	})
	t.Run("Should return false on failure", func(t *testing.T) {
		f := newFake()
		f.deleteRef = state.RefResult{}
		eq(t, NewDeleteTagAction("v1.0.0").Execute(ctx, newState(), f), false)
	})
}

func TestCreateBranchAction(t *testing.T) {
	t.Run("Should create branch action with correct properties", func(t *testing.T) {
		a := NewCreateBranchAction("v1", "abc123")
		eq(t, a.BranchName, "v1")
		eq(t, a.Sha, "abc123")
		eq(t, a.Priority(), 20)
		eq(t, a.Description(), "Create branch")
		eq(t, a.Version(), "v1")
	})
	t.Run("Should generate correct branch commands", func(t *testing.T) {
		cmds := NewCreateBranchAction("v1", "abc123").ManualCommands(newState())
		eq(t, len(cmds), 1)
		eq(t, cmds[0], "git push origin abc123:refs/heads/v1")
	})
	t.Run("Should call CreateRef with refs/heads/ prefix", func(t *testing.T) {
		f := newFake()
		NewCreateBranchAction("v1", "abc123").Execute(ctx, newState(), f)
		eq(t, len(f.createRefCalls), 1)
		eq(t, f.createRefCalls[0], refCall{"refs/heads/v1", "abc123", false})
	})
	t.Run("Should return true on success", func(t *testing.T) {
		eq(t, NewCreateBranchAction("v1", "abc123").Execute(ctx, newState(), newFake()), true)
	})
	t.Run("Should mark issue as manual_fix_required when workflow permission error occurs", func(t *testing.T) {
		f := newFake()
		f.createRef = state.RefResult{RequiresManualFix: true}
		st := newState()
		a := NewCreateBranchAction("v1", "abc123")
		i := addIssue(st, "missing_branch", "v1", state.StatusPending, a)
		eq(t, a.Execute(ctx, st, f), false)
		eq(t, i.Status, state.StatusManualFixRequired)
		match(t, i.Message, "workflows.*permission")
	})
}

func TestUpdateBranchAction(t *testing.T) {
	t.Run("Should create action with force flag", func(t *testing.T) {
		a := NewUpdateBranchAction("v1", "abc123", true)
		eq(t, a.BranchName, "v1")
		eq(t, a.Sha, "abc123")
		eq(t, a.Force, true)
		eq(t, a.Priority(), 20)
	})
	t.Run("Should generate command with force flag", func(t *testing.T) {
		cmds := NewUpdateBranchAction("v1", "abc123", true).ManualCommands(newState())
		eq(t, len(cmds), 1)
		match(t, cmds[0], "--force")
		match(t, cmds[0], "refs/heads/v1")
	})
	t.Run("Should generate command without force flag", func(t *testing.T) {
		notMatch(t, NewUpdateBranchAction("v1", "abc123", false).ManualCommands(newState())[0], "--force")
	})
	t.Run("Should call CreateRef with refs/heads/ prefix", func(t *testing.T) {
		f := newFake()
		NewUpdateBranchAction("v1", "abc123", true).Execute(ctx, newState(), f)
		eq(t, len(f.createRefCalls), 1)
		eq(t, f.createRefCalls[0], refCall{"refs/heads/v1", "abc123", true})
	})
	t.Run("Should return true on success", func(t *testing.T) {
		eq(t, NewUpdateBranchAction("v1", "abc123", true).Execute(ctx, newState(), newFake()), true)
	})
	t.Run("Should mark issue as manual_fix_required when workflow permission error occurs", func(t *testing.T) {
		f := newFake()
		f.createRef = state.RefResult{RequiresManualFix: true}
		st := newState()
		a := NewUpdateBranchAction("v1", "abc123", true)
		i := addIssue(st, "floating_version_mismatch", "v1", state.StatusPending, a)
		eq(t, a.Execute(ctx, st, f), false)
		eq(t, i.Status, state.StatusManualFixRequired)
	})
}

func TestDeleteBranchAction(t *testing.T) {
	t.Run("Should delete branch with high priority", func(t *testing.T) {
		eq(t, NewDeleteBranchAction("v1").Priority(), 10)
	})
	t.Run("Should set BranchName correctly", func(t *testing.T) {
		a := NewDeleteBranchAction("v1")
		eq(t, a.BranchName, "v1")
		eq(t, a.Description(), "Delete branch")
		eq(t, a.Version(), "v1")
	})
	t.Run("Should generate correct manual commands", func(t *testing.T) {
		cmds := NewDeleteBranchAction("v1").ManualCommands(newState())
		eq(t, len(cmds), 2)
		eq(t, cmds[0], "git branch -d v1")
		eq(t, cmds[1], "git push origin :refs/heads/v1")
	})
	t.Run("Should call DeleteRef with refs/heads/ prefix", func(t *testing.T) {
		f := newFake()
		NewDeleteBranchAction("v1").Execute(ctx, newState(), f)
		eq(t, strings.Join(f.deleteRefCalls, ","), "refs/heads/v1")
	})
	t.Run("Should return true on success", func(t *testing.T) {
		eq(t, NewDeleteBranchAction("v1").Execute(ctx, newState(), newFake()), true)
	})
	t.Run("Should return false on failure", func(t *testing.T) {
		f := newFake()
		f.deleteRef = state.RefResult{}
		eq(t, NewDeleteBranchAction("v1").Execute(ctx, newState(), f), false)
	})
}

func TestConvertBranchToTagAction(t *testing.T) {
	tagState := func() *state.State {
		st := newState()
		st.Tags = []*state.VersionRef{state.NewVersionRef("v1.0.0", "refs/tags/v1.0.0", "abc123", state.TypeTag)}
		return st
	}
	t.Run("Should create action with correct properties", func(t *testing.T) {
		a := NewConvertBranchToTagAction("v1.0.0", "abc123")
		eq(t, a.Name, "v1.0.0")
		eq(t, a.Sha, "abc123")
		eq(t, a.Priority(), 25)
		eq(t, a.Description(), "Convert branch to tag")
		eq(t, a.Version(), "v1.0.0")
	})
	t.Run("Should generate delete-only commands when tag already exists", func(t *testing.T) {
		cmds := NewConvertBranchToTagAction("v1.0.0", "abc123").ManualCommands(tagState())
		eq(t, len(cmds), 1)
		eq(t, cmds[0], "git push origin :refs/heads/v1.0.0")
	})
	t.Run("Should generate create-then-delete commands when tag does not exist", func(t *testing.T) {
		cmds := NewConvertBranchToTagAction("v1.0.0", "abc123").ManualCommands(newState())
		eq(t, len(cmds), 2)
		eq(t, cmds[0], "git push origin abc123:refs/tags/v1.0.0")
		eq(t, cmds[1], "git push origin :refs/heads/v1.0.0")
	})
	t.Run("Should delete branch only when tag already exists", func(t *testing.T) {
		f := newFake()
		eq(t, NewConvertBranchToTagAction("v1.0.0", "abc123").Execute(ctx, tagState(), f), true)
		eq(t, strings.Join(f.deleteRefCalls, ","), "refs/heads/v1.0.0")
		eq(t, len(f.createRefCalls), 0)
	})
	t.Run("Should create tag then delete branch when tag does not exist", func(t *testing.T) {
		f := newFake()
		eq(t, NewConvertBranchToTagAction("v1.0.0", "abc123").Execute(ctx, newState(), f), true)
		eq(t, len(f.createRefCalls), 1)
		eq(t, f.createRefCalls[0].ref, "refs/tags/v1.0.0")
		eq(t, strings.Join(f.deleteRefCalls, ","), "refs/heads/v1.0.0")
	})
	t.Run("Should mark issue manual_fix_required when workflow permission error occurs", func(t *testing.T) {
		f := newFake()
		f.createRef = state.RefResult{RequiresManualFix: true}
		st := newState()
		a := NewConvertBranchToTagAction("v1.0.0", "abc123")
		i := addIssue(st, "floating_ref_should_be_tag", "v1.0.0", state.StatusPending, a)
		eq(t, a.Execute(ctx, st, f), false)
		eq(t, i.Status, state.StatusManualFixRequired)
		match(t, i.Message, "workflows.*permission")
	})
	t.Run("Should return false when tag creation succeeds but branch deletion fails", func(t *testing.T) {
		f := newFake()
		f.deleteRef = state.RefResult{}
		eq(t, NewConvertBranchToTagAction("v1.0.0", "abc123").Execute(ctx, newState(), f), false)
	})
}

func TestConvertTagToBranchAction(t *testing.T) {
	branchState := func() *state.State {
		st := newState()
		st.Branches = []*state.VersionRef{state.NewVersionRef("v1", "refs/heads/v1", "abc123", state.TypeBranch)}
		return st
	}
	t.Run("Should create action with correct properties", func(t *testing.T) {
		a := NewConvertTagToBranchAction("v1", "abc123")
		eq(t, a.Name, "v1")
		eq(t, a.Sha, "abc123")
		eq(t, a.Priority(), 25)
		eq(t, a.Description(), "Convert tag to branch")
		eq(t, a.Version(), "v1")
	})
	t.Run("Should generate delete-only commands when branch already exists", func(t *testing.T) {
		cmds := NewConvertTagToBranchAction("v1", "abc123").ManualCommands(branchState())
		eq(t, len(cmds), 1)
		eq(t, cmds[0], "git push origin :refs/tags/v1")
	})
	t.Run("Should generate create-then-delete commands when branch does not exist", func(t *testing.T) {
		cmds := NewConvertTagToBranchAction("v1", "abc123").ManualCommands(newState())
		eq(t, len(cmds), 2)
		eq(t, cmds[0], "git push origin abc123:refs/heads/v1")
		eq(t, cmds[1], "git push origin :refs/tags/v1")
	})
	t.Run("Should return empty commands when issue is unfixable", func(t *testing.T) {
		st := newState()
		a := NewConvertTagToBranchAction("v1", "abc123")
		addIssue(st, "floating_ref_should_be_branch", "v1", state.StatusUnfixable, a)
		eq(t, len(a.ManualCommands(st)), 0)
	})
	t.Run("Should mark issue unfixable when tag has immutable release", func(t *testing.T) {
		f := newFake()
		f.immutable = true
		st := newState()
		a := NewConvertTagToBranchAction("v1", "abc123")
		i := addIssue(st, "floating_ref_should_be_branch", "v1", state.StatusPending, a)
		eq(t, a.Execute(ctx, st, f), false)
		eq(t, i.Status, state.StatusUnfixable)
		match(t, i.Message, "immutable")
	})
	t.Run("Should delete tag only when branch already exists", func(t *testing.T) {
		f := newFake()
		eq(t, NewConvertTagToBranchAction("v1", "abc123").Execute(ctx, branchState(), f), true)
		eq(t, strings.Join(f.deleteRefCalls, ","), "refs/tags/v1")
		eq(t, len(f.createRefCalls), 0)
	})
	t.Run("Should create branch then delete tag when branch does not exist", func(t *testing.T) {
		f := newFake()
		eq(t, NewConvertTagToBranchAction("v1", "abc123").Execute(ctx, newState(), f), true)
		eq(t, len(f.createRefCalls), 1)
		eq(t, f.createRefCalls[0].ref, "refs/heads/v1")
		eq(t, strings.Join(f.deleteRefCalls, ","), "refs/tags/v1")
	})
	t.Run("Should mark issue manual_fix_required when workflow permission error occurs", func(t *testing.T) {
		f := newFake()
		f.createRef = state.RefResult{RequiresManualFix: true}
		st := newState()
		a := NewConvertTagToBranchAction("v1", "abc123")
		i := addIssue(st, "floating_ref_should_be_branch", "v1", state.StatusPending, a)
		eq(t, a.Execute(ctx, st, f), false)
		eq(t, i.Status, state.StatusManualFixRequired)
		match(t, i.Message, "workflows.*permission")
	})
}

func TestCreateReleaseAction(t *testing.T) {
	t.Run("Should create action for draft release (isDraft=true)", func(t *testing.T) {
		a := NewCreateReleaseAction("v1.0.0", true, false, "")
		eq(t, a.TagName, "v1.0.0")
		eq(t, a.AutoPublish, false)
		eq(t, a.Priority(), 30)
	})
	t.Run("Should create action with explicit auto-publish", func(t *testing.T) {
		a := NewCreateReleaseAction("v1.0.0", true, true, "")
		eq(t, a.AutoPublish, true)
		eq(t, a.Priority(), 30)
	})
	t.Run("Should generate correct manual commands for draft", func(t *testing.T) {
		cmds := NewCreateReleaseAction("v1.0.0", true, false, "").ManualCommands(newState())
		eq(t, len(cmds), 1)
		match(t, cmds[0], "gh release create v1.0.0")
		match(t, cmds[0], "--repo test-owner/test-repo")
		match(t, cmds[0], "--draft")
		notMatch(t, cmds[0], "^#")
	})
	t.Run("Should generate correct manual commands for auto-publish", func(t *testing.T) {
		cmds := NewCreateReleaseAction("v1.0.0", true, true, "").ManualCommands(newState())
		eq(t, len(cmds), 1)
		match(t, cmds[0], "gh release create v1.0.0")
		match(t, cmds[0], "--repo test-owner/test-repo")
		notMatch(t, cmds[0], "--draft")
	})
	t.Run("Should include the target SHA in manual commands", func(t *testing.T) {
		cmds := NewCreateReleaseAction("v1.0.0", false, true, "abc123").ManualCommands(newState())
		eq(t, cmds[0], `gh release create v1.0.0 --repo test-owner/test-repo --title "v1.0.0" --notes "Release v1.0.0" --target abc123`)
	})
	t.Run("Should return an ignore-versions YAML snippet (not a gh command) when issue is unfixable", func(t *testing.T) {
		st := newState()
		a := NewCreateReleaseAction("v1.0.0", false, true, "")
		addIssue(st, "release_missing", "v1.0.0", state.StatusUnfixable, nil)
		joined := strings.Join(a.ManualCommands(st), "\n")
		match(t, joined, `versions:\s*\["v1\.0\.0"\]`)
		match(t, joined, `scope: selection`)
		notMatch(t, joined, "gh release")
		eq(t, a.ManualCommandsLanguage(), "yaml")
	})
	t.Run("Should call CreateReleaseWithLatest with draft=true when AutoPublish is false", func(t *testing.T) {
		f := newFake()
		NewCreateReleaseAction("v1.0.0", true, false, "").Execute(ctx, newState(), f)
		eq(t, len(f.createRelCalls), 1)
		eq(t, f.createRelCalls[0].tag, "v1.0.0")
		eq(t, f.createRelCalls[0].draft, true)
	})
	t.Run("Should call CreateReleaseWithLatest with draft=false when AutoPublish is true", func(t *testing.T) {
		f := newFake()
		NewCreateReleaseAction("v1.0.0", false, true, "").Execute(ctx, newState(), f)
		eq(t, len(f.createRelCalls), 1)
		eq(t, f.createRelCalls[0].draft, false)
	})
	t.Run("Should reclassify a 422 and require destructive permission before deleting the tag", func(t *testing.T) {
		f := newFake()
		f.createRelease = state.ReleaseResult{Unfixable: true}
		f.createErr = state.ErrUnfixable
		st := newState()
		i := addIssue(st, "release_missing", "v1.0.0", state.StatusPending, nil)
		eq(t, NewCreateReleaseAction("v1.0.0", false, true, "").Execute(ctx, st, f), false)
		eq(t, i.Finding, "patch_tag_locked_by_deleted_release")
		eq(t, i.Status, state.StatusManualFixRequired)
		eq(t, len(f.deleteRefCalls), 0)
		match(t, i.Message, "immutable release")

		f = newFake()
		f.createRelease = state.ReleaseResult{Unfixable: true}
		f.createErr = state.ErrUnfixable
		st = newState()
		st.Config.AllowDestructiveTagDeletion = true
		i = addIssue(st, "release_missing", "v1.0.0", state.StatusPending, nil)
		eq(t, NewCreateReleaseAction("v1.0.0", false, true, "").Execute(ctx, st, f), true)
		eq(t, i.Finding, "patch_tag_locked_by_deleted_release")
		eq(t, f.deleteRefCalls[0], "refs/tags/v1.0.0")
	})
	t.Run("Should suggest selection-scoped ignore on 422 error", func(t *testing.T) {
		f := newFake()
		f.createRelease = state.ReleaseResult{Unfixable: true}
		f.createErr = state.ErrUnfixable
		st := newState()
		st.Config.IgnoreVersions = []string{"v1.*", "v2.0.0"}
		i := addIssue(st, "release_missing", "v3.0.0", state.StatusPending, nil)
		NewCreateReleaseAction("v3.0.0", false, true, "").Execute(ctx, st, f)
		if !strings.Contains(i.Message, "selection") {
			t.Errorf("message %q", i.Message)
		}
	})
	t.Run("MakeLatest/Should default MakeLatest to null (let GitHub decide)", func(t *testing.T) {
		if NewCreateReleaseAction("v1.0.0", false, true, "").MakeLatest != nil {
			t.Error("expected nil")
		}
	})
	t.Run("MakeLatest/Should pass MakeLatest=false when explicitly set", func(t *testing.T) {
		f := newFake()
		a := NewCreateReleaseAction("v1.1.0", false, true, "")
		a.MakeLatest = boolp(false)
		a.Execute(ctx, newState(), f)
		if f.createRelCalls[0].makeLatest == nil || *f.createRelCalls[0].makeLatest {
			t.Error("expected makeLatest=false")
		}
	})
	t.Run("MakeLatest/Should not pass MakeLatest when null (let GitHub use default)", func(t *testing.T) {
		f := newFake()
		NewCreateReleaseAction("v1.0.0", false, true, "").Execute(ctx, newState(), f)
		if f.createRelCalls[0].makeLatest != nil {
			t.Error("expected nil")
		}
	})
	t.Run("MakeLatest/Should include --latest=false in manual command when MakeLatest is false", func(t *testing.T) {
		a := NewCreateReleaseAction("v1.0.0", false, true, "")
		a.MakeLatest = boolp(false)
		match(t, a.ManualCommands(newState())[0], "--latest=false")
	})
	t.Run("MakeLatest/Should include --latest in manual command when MakeLatest is true", func(t *testing.T) {
		a := NewCreateReleaseAction("v1.0.0", false, true, "")
		a.MakeLatest = boolp(true)
		c := a.ManualCommands(newState())[0]
		match(t, c, "--latest")
		notMatch(t, c, "--latest=false")
	})
	t.Run("MakeLatest/Should not include --latest in manual command when MakeLatest is null", func(t *testing.T) {
		notMatch(t, NewCreateReleaseAction("v1.0.0", false, true, "").ManualCommands(newState())[0], "--latest")
	})
}

func TestDeleteReleaseAction(t *testing.T) {
	t.Run("Should have high priority (delete early)", func(t *testing.T) {
		eq(t, NewDeleteReleaseAction("v1.0.0", 12345).Priority(), 10)
	})
	t.Run("Should set properties correctly", func(t *testing.T) {
		a := NewDeleteReleaseAction("v1.0.0", 12345)
		eq(t, a.TagName, "v1.0.0")
		eq(t, a.ReleaseID, int64(12345))
		eq(t, a.Description(), "Delete release")
	})
	t.Run("Should generate correct manual commands", func(t *testing.T) {
		cmds := NewDeleteReleaseAction("v1.0.0", 12345).ManualCommands(newState())
		eq(t, len(cmds), 1)
		match(t, cmds[0], "gh release delete v1.0.0")
		match(t, cmds[0], "--repo test-owner/test-repo")
		match(t, cmds[0], "--yes")
	})
	t.Run("Should call DeleteRelease with correct parameters", func(t *testing.T) {
		f := newFake()
		NewDeleteReleaseAction("v1.0.0", 12345).Execute(ctx, newState(), f)
		eq(t, len(f.deleteRelIDs), 1)
		eq(t, f.deleteRelIDs[0], int64(12345))
	})
	t.Run("Should return true on success", func(t *testing.T) {
		eq(t, NewDeleteReleaseAction("v1.0.0", 12345).Execute(ctx, newState(), newFake()), true)
	})
	t.Run("Should return false on failure", func(t *testing.T) {
		f := newFake()
		f.deleteRelease = errors.New("boom")
		eq(t, NewDeleteReleaseAction("v1.0.0", 12345).Execute(ctx, newState(), f), false)
	})
}

func TestPublishReleaseAction(t *testing.T) {
	t.Run("Should create action with release ID", func(t *testing.T) {
		a := NewPublishReleaseAction("v1.0.0", 12345)
		eq(t, a.TagName, "v1.0.0")
		eq(t, a.ReleaseID, int64(12345))
		eq(t, a.Priority(), 40)
	})
	t.Run("Should create action without release ID", func(t *testing.T) {
		eq(t, NewPublishReleaseAction("v1.0.0", 0).ReleaseID, int64(0))
	})
	t.Run("Should generate correct manual commands", func(t *testing.T) {
		cmds := NewPublishReleaseAction("v1.0.0", 12345).ManualCommands(newState())
		eq(t, len(cmds), 1)
		match(t, cmds[0], "gh release edit v1.0.0")
		match(t, cmds[0], "--repo test-owner/test-repo")
		match(t, cmds[0], "--draft=false")
		notMatch(t, cmds[0], "^#")
		notMatch(t, cmds[0], "# Or edit at")
	})
	t.Run("Should return an ignore-versions YAML snippet (not a gh command) when issue is unfixable", func(t *testing.T) {
		st := newState()
		a := NewPublishReleaseAction("v1.0.0", 12345)
		addIssue(st, "release_is_draft", "v1.0.0", state.StatusUnfixable, nil)
		joined := strings.Join(a.ManualCommands(st), "\n")
		match(t, joined, `versions:\s*\["v1\.0\.0"\]`)
		match(t, joined, `scope: selection`)
		notMatch(t, joined, "gh release")
		eq(t, a.ManualCommandsLanguage(), "yaml")
	})
	t.Run("Should call PublishRelease with correct parameters", func(t *testing.T) {
		f := newFake()
		NewPublishReleaseAction("v1.0.0", 12345).Execute(ctx, newState(), f)
		eq(t, len(f.publishCalls), 1)
		eq(t, f.publishCalls[0].id, int64(12345))
	})
	t.Run("Should look up the release ID from state when not provided", func(t *testing.T) {
		f := newFake()
		st := newState()
		st.Releases = []*state.ReleaseInfo{{TagName: "v1.0.0", ID: 77}}
		NewPublishReleaseAction("v1.0.0", 0).Execute(ctx, st, f)
		eq(t, f.publishCalls[0].id, int64(77))
	})
	t.Run("Should return true on success", func(t *testing.T) {
		eq(t, NewPublishReleaseAction("v1.0.0", 12345).Execute(ctx, newState(), newFake()), true)
	})
	t.Run("Should mark issue as unfixable on 422 error", func(t *testing.T) {
		f := newFake()
		f.publish = state.ReleaseResult{Unfixable: true}
		st := newState()
		i := addIssue(st, "release_is_draft", "v1.0.0", state.StatusPending, nil)
		eq(t, NewPublishReleaseAction("v1.0.0", 12345).Execute(ctx, st, f), false)
		eq(t, i.Status, state.StatusUnfixable)
		match(t, i.Message, "immutable release")
	})
	t.Run("MakeLatest/Should default MakeLatest to null (let GitHub decide)", func(t *testing.T) {
		if NewPublishReleaseAction("v1.0.0", 12345).MakeLatest != nil {
			t.Error("expected nil")
		}
	})
	t.Run("MakeLatest/Should pass MakeLatest=false when explicitly set", func(t *testing.T) {
		f := newFake()
		a := NewPublishReleaseAction("v1.1.0", 111)
		a.MakeLatest = boolp(false)
		a.Execute(ctx, newState(), f)
		if f.publishCalls[0].makeLatest == nil || *f.publishCalls[0].makeLatest {
			t.Error("expected makeLatest=false")
		}
	})
	t.Run("MakeLatest/Should not pass MakeLatest when null (let GitHub use default)", func(t *testing.T) {
		f := newFake()
		NewPublishReleaseAction("v1.0.0", 12345).Execute(ctx, newState(), f)
		if f.publishCalls[0].makeLatest != nil {
			t.Error("expected nil")
		}
	})
	t.Run("MakeLatest/Should include --latest=false in manual command when MakeLatest is false", func(t *testing.T) {
		a := NewPublishReleaseAction("v1.0.0", 12345)
		a.MakeLatest = boolp(false)
		match(t, a.ManualCommands(newState())[0], "--latest=false")
	})
	t.Run("MakeLatest/Should include --latest in manual command when MakeLatest is true", func(t *testing.T) {
		a := NewPublishReleaseAction("v1.0.0", 12345)
		a.MakeLatest = boolp(true)
		c := a.ManualCommands(newState())[0]
		match(t, c, "--latest")
		notMatch(t, c, "--latest=false")
	})
	t.Run("MakeLatest/Should not include --latest in manual command when MakeLatest is null", func(t *testing.T) {
		notMatch(t, NewPublishReleaseAction("v1.0.0", 12345).ManualCommands(newState())[0], "--latest")
	})
}

func TestSetLatestReleaseAction(t *testing.T) {
	t.Run("Should create action with tagName only", func(t *testing.T) {
		a := NewSetLatestReleaseAction("v1.0.0", 0)
		eq(t, a.TagName, "v1.0.0")
		eq(t, a.ReleaseID, int64(0))
		eq(t, a.Priority(), 50)
	})
	t.Run("Should create action with tagName and releaseId", func(t *testing.T) {
		a := NewSetLatestReleaseAction("v1.0.0", 123)
		eq(t, a.ReleaseID, int64(123))
		eq(t, a.Priority(), 50)
	})
	t.Run("Should have correct description", func(t *testing.T) {
		match(t, NewSetLatestReleaseAction("v1.0.0", 123).Description(), "Set.*latest")
	})
	t.Run("Should generate correct gh release edit command", func(t *testing.T) {
		cmds := NewSetLatestReleaseAction("v1.0.0", 123).ManualCommands(newState())
		eq(t, len(cmds), 1)
		match(t, cmds[0], "gh release edit v1.0.0")
		match(t, cmds[0], "--latest")
	})
	t.Run("Should return empty array when issue is unfixable", func(t *testing.T) {
		st := newState()
		addIssue(st, "latest_release_wrong", "v1.0.0", state.StatusUnfixable, nil)
		eq(t, len(NewSetLatestReleaseAction("v1.0.0", 123).ManualCommands(st)), 0)
	})
	t.Run("Should call SetReleaseLatest with correct parameters", func(t *testing.T) {
		f := newFake()
		NewSetLatestReleaseAction("v1.0.0", 123).Execute(ctx, newState(), f)
		eq(t, len(f.setLatestIDs), 1)
		eq(t, f.setLatestIDs[0], int64(123))
	})
	t.Run("Should return true on success", func(t *testing.T) {
		eq(t, NewSetLatestReleaseAction("v1.0.0", 123).Execute(ctx, newState(), newFake()), true)
	})
	t.Run("Should return false on failure", func(t *testing.T) {
		f := newFake()
		f.setLatest = errors.New("boom")
		eq(t, NewSetLatestReleaseAction("v1.0.0", 123).Execute(ctx, newState(), f), false)
	})
	t.Run("Should mark issue as unfixable on 422 error", func(t *testing.T) {
		f := newFake()
		f.setLatest = state.ErrUnfixable
		st := newState()
		i := addIssue(st, "latest_release_wrong", "v1.0.0", state.StatusPending, nil)
		eq(t, NewSetLatestReleaseAction("v1.0.0", 123).Execute(ctx, st, f), false)
		eq(t, i.Status, state.StatusUnfixable)
	})
	t.Run("Should lookup release ID if not provided", func(t *testing.T) {
		f := newFake()
		st := newState()
		st.Releases = []*state.ReleaseInfo{{TagName: "v1.0.0", ID: 456}}
		NewSetLatestReleaseAction("v1.0.0", 0).Execute(ctx, st, f)
		eq(t, f.setLatestIDs[0], int64(456))
	})
}

func TestRepublishReleaseAction(t *testing.T) {
	immutableState := func() *state.State {
		st := newState()
		st.Releases = []*state.ReleaseInfo{{TagName: "v2.0.0", ID: 456, IsImmutable: true}}
		return st
	}
	t.Run("Should create action with correct priority", func(t *testing.T) {
		a := NewRepublishReleaseAction("v1.0.0")
		eq(t, a.TagName, "v1.0.0")
		eq(t, a.Priority(), 45)
		eq(t, a.Description(), "Republish release for immutability")
	})
	t.Run("Should generate manual commands without latest flag by default", func(t *testing.T) {
		cmds := NewRepublishReleaseAction("v1.0.0").ManualCommands(immutableState())
		eq(t, len(cmds), 2)
		match(t, cmds[0], "gh release edit v1.0.0")
		match(t, cmds[0], "--repo test-owner/test-repo")
		match(t, cmds[0], "--draft=true")
		match(t, cmds[1], "--draft=false")
		notMatch(t, cmds[1], "--latest")
	})
	t.Run("Should include --latest when MakeLatest is true", func(t *testing.T) {
		a := NewRepublishReleaseAction("v1.0.0")
		a.MakeLatest = boolp(true)
		match(t, a.ManualCommands(immutableState())[1], "--draft=false --latest")
	})
	t.Run("Should include --latest=false when MakeLatest is false", func(t *testing.T) {
		a := NewRepublishReleaseAction("v1.0.0")
		a.MakeLatest = boolp(false)
		match(t, a.ManualCommands(immutableState())[1], "--draft=false --latest=false")
	})
	t.Run("Should return the remote tag delete command and ruleset guidance after tag deletion failed", func(t *testing.T) {
		a := NewRepublishReleaseAction("v1.0.0")
		a.TagDeletionFailed = true
		a.BlockingRulesets = []state.Ruleset{{Name: "Protect tags", Enforcement: "active", Source: "repository test-owner/test-repo", Url: "https://github.com/test-owner/test-repo/rules/1"}}
		cmds := a.ManualCommands(newState())
		joined := strings.Join(cmds, "\n")
		match(t, joined, "Protect tags")
		match(t, joined, "bypass list")
		notMatch(t, joined, "gh release")
		found := false
		for _, c := range cmds {
			found = found || c == "git push origin :refs/tags/v1.0.0"
		}
		if !found {
			t.Error("missing delete command")
		}
	})
	enableCmd := func(t *testing.T, cmds []string, tag string) {
		t.Helper()
		eq(t, len(cmds), 4)
		match(t, cmds[0], "^# Enable 'Release immutability'")
		match(t, cmds[0], "admin")
		eq(t, cmds[1], "gh api -X PUT repos/test-owner/test-repo/immutable-releases")
		match(t, cmds[2], "gh release edit "+tag)
		match(t, cmds[2], "--draft=true")
		match(t, cmds[3], "gh release edit "+tag)
		match(t, cmds[3], "--draft=false")
	}
	t.Run("Should return the enable-immutability command AND remediation commands when no immutable releases exist", func(t *testing.T) {
		st := newState()
		addIssue(st, "release_not_immutable", "v1.0.0", state.StatusManualFixRequired, nil)
		enableCmd(t, NewRepublishReleaseAction("v1.0.0").ManualCommands(st), "v1.0.0")
	})
	t.Run("Should return the enable-immutability command AND remediation commands when auto-fix is false", func(t *testing.T) {
		st := newState()
		addIssue(st, "release_not_immutable", "v1.0.0", state.StatusPending, nil)
		enableCmd(t, NewRepublishReleaseAction("v1.0.0").ManualCommands(st), "v1.0.0")
	})
	t.Run("Should return gh release edit commands when repo already has immutable releases", func(t *testing.T) {
		st := immutableState()
		addIssue(st, "release_not_immutable", "v1.0.0", state.StatusManualFixRequired, nil)
		cmds := NewRepublishReleaseAction("v1.0.0").ManualCommands(st)
		eq(t, len(cmds), 2)
		match(t, cmds[0], "--draft=true")
		match(t, cmds[1], "--draft=false")
	})
	t.Run("Should return gh release edit commands when repo has immutable releases even in manual_fix_required state", func(t *testing.T) {
		st := immutableState()
		addIssue(st, "release_not_immutable", "v1.0.0", state.StatusManualFixRequired, nil)
		eq(t, len(NewRepublishReleaseAction("v1.0.0").ManualCommands(st)), 2)
	})
	t.Run("Should not show settings URL when another immutability fix already succeeded", func(t *testing.T) {
		st := newState()
		addIssue(st, "release_not_immutable", "v1.0.8", state.StatusFixed, nil)
		addIssue(st, "release_not_immutable", "v1.0.7", state.StatusFailed, nil)
		cmds := NewRepublishReleaseAction("v1.0.7").ManualCommands(st)
		eq(t, len(cmds), 2)
		match(t, cmds[0], "gh release edit v1.0.7")
		match(t, cmds[0], "--draft=true")
		match(t, cmds[1], "--draft=false")
	})
	t.Run("Should generate manual commands including the enable command when no state is known (unit)", func(t *testing.T) {
		cmds := NewRepublishReleaseAction("v1.0.0").ManualCommands(newState())
		enableCmd(t, cmds, "v1.0.0")
		a := NewRepublishReleaseAction("v1.0.0")
		a.MakeLatest = boolp(true)
		match(t, a.ManualCommands(newState())[3], `gh release edit v1\.0\.0.*--draft=false.*--latest`)
	})
	t.Run("Should republish and verify immutability", func(t *testing.T) {
		f := newFake()
		f.immutable = true
		eq(t, NewRepublishReleaseAction("v1.0.0").Execute(ctx, newState(), f), true)
		eq(t, f.republishCalls, 1)
		eq(t, f.immutableCalls, 1)
	})
	t.Run("Should mark as manual_fix_required when republish succeeds but still mutable", func(t *testing.T) {
		f := newFake()
		st := newState()
		i := addIssue(st, "release_not_immutable", "v1.0.0", state.StatusPending, nil)
		eq(t, NewRepublishReleaseAction("v1.0.0").Execute(ctx, st, f), false)
		eq(t, i.Status, state.StatusManualFixRequired)
		match(t, i.Message, "Enable 'Release immutability'")
		match(t, i.Message, "gh api -X PUT repos/test-owner/test-repo/immutable-releases")
	})
	t.Run("Should enable release immutability via the dedicated action", func(t *testing.T) {
		f := newFake()
		f.enableOK = true
		eq(t, NewEnableImmutableReleasesAction().Execute(ctx, newState(), f), true)
		eq(t, f.enableCalls, 1)
	})
	t.Run("Should require manual fix with the gh api command when immutability cannot be enabled", func(t *testing.T) {
		f := newFake()
		a := NewEnableImmutableReleasesAction()
		st := newState()
		i := addIssue(st, "immutable_releases_disabled", "", state.StatusPending, a)
		eq(t, a.Execute(ctx, st, f), false)
		eq(t, i.Status, state.StatusManualFixRequired)
		match(t, i.Message, "gh api -X PUT repos/test-owner/test-repo/immutable-releases")
	})
	t.Run("Should list the enable command in manual steps after an enablement failure even if other releases are immutable", func(t *testing.T) {
		f := newFake()
		f.enabled = boolp(false)
		st := immutableState()
		addIssue(st, "release_not_immutable", "v1.0.0", "", nil)
		a := NewRepublishReleaseAction("v1.0.0")
		eq(t, a.Execute(ctx, st, f), false)
		match(t, strings.Join(a.ManualCommands(st), "\n"), "gh api -X PUT repos/test-owner/test-repo/immutable-releases")
	})
	t.Run("Should not delete the tag on 422 error unless destructive deletion is allowed", func(t *testing.T) {
		f := newFake()
		f.republish = state.RepublishResult{Unfixable: true}
		st := newState()
		i := addIssue(st, "release_not_immutable", "v1.0.0", state.StatusPending, nil)
		a := NewRepublishReleaseAction("v1.0.0")
		eq(t, a.Execute(ctx, st, f), false)
		eq(t, len(f.deleteRefCalls), 0)
		eq(t, i.Status, state.StatusManualFixRequired)
		match(t, i.Message, "--dangerously-allow-destructive-tag-deletion")
		cmds := a.ManualCommands(st)
		eq(t, cmds[len(cmds)-1], "git push origin :refs/tags/v1.0.0")
	})
	t.Run("Should delete the tag on 422 error when destructive deletion is allowed", func(t *testing.T) {
		f := newFake()
		f.republish = state.RepublishResult{Unfixable: true}
		st := newState()
		st.Config.AllowDestructiveTagDeletion = true
		eq(t, NewRepublishReleaseAction("v1.0.0").Execute(ctx, st, f), true)
		eq(t, strings.Join(f.deleteRefCalls, ","), "refs/tags/v1.0.0")
	})
	t.Run("Should require manual fix and report blocking rulesets when tag deletion fails", func(t *testing.T) {
		f := newFake()
		f.republish = state.RepublishResult{Unfixable: true}
		f.deleteRef = state.RefResult{}
		f.rulesets = []state.Ruleset{{Name: "Protect tags", Enforcement: "active", Source: "repository test-owner/test-repo", Url: "https://github.com/test-owner/test-repo/rules/1"}}
		st := newState()
		st.Config.AllowDestructiveTagDeletion = true
		i := addIssue(st, "release_not_immutable", "v1.0.0", state.StatusPending, nil)
		a := NewRepublishReleaseAction("v1.0.0")
		eq(t, a.Execute(ctx, st, f), false)
		eq(t, i.Status, state.StatusManualFixRequired)
		match(t, i.Message, "Protect tags")
		eq(t, a.TagDeletionFailed, true)
		cmds := a.ManualCommands(st)
		eq(t, cmds[len(cmds)-1], "git push origin :refs/tags/v1.0.0")
	})
}

func TestPriorityOrdering(t *testing.T) {
	t.Run("Should order actions correctly by priority", func(t *testing.T) {
		acts := []state.Action{
			NewCreateTagAction("v1.0.0", "abc123"),
			NewPublishReleaseAction("v1.0.0", 12345),
			NewDeleteTagAction("v1.0.0"),
			NewCreateReleaseAction("v1.0.0", true, false, ""),
			NewUpdateTagAction("v1.0.0", "def456", true),
		}
		sort.SliceStable(acts, func(i, j int) bool { return acts[i].Priority() < acts[j].Priority() })
		if _, ok := acts[0].(*DeleteTagAction); !ok {
			t.Errorf("first is %T", acts[0])
		}
		eq(t, acts[1].Priority(), 20)
		eq(t, acts[2].Priority(), 20)
		if _, ok := acts[3].(*CreateReleaseAction); !ok {
			t.Errorf("fourth is %T", acts[3])
		}
		if _, ok := acts[4].(*PublishReleaseAction); !ok {
			t.Errorf("fifth is %T", acts[4])
		}
	})
}

func TestParameterizedPriorities(t *testing.T) {
	cases := []struct {
		name string
		a    state.Action
		want int
	}{
		{"DeleteTagAction", NewDeleteTagAction("v1.0.0"), 10},
		{"DeleteBranchAction", NewDeleteBranchAction("v1"), 10},
		{"DeleteReleaseAction", NewDeleteReleaseAction("v1.0.0", 123), 10},
		{"CreateTagAction", NewCreateTagAction("v1.0.0", "abc123"), 20},
		{"CreateBranchAction", NewCreateBranchAction("v1", "abc123"), 20},
		{"UpdateTagAction", NewUpdateTagAction("v1.0.0", "abc123", true), 20},
		{"UpdateBranchAction", NewUpdateBranchAction("v1", "abc123", true), 20},
		{"CreateReleaseAction", NewCreateReleaseAction("v1.0.0", true, false, ""), 30},
		{"PublishReleaseAction", NewPublishReleaseAction("v1.0.0", 123), 40},
		{"RepublishReleaseAction", NewRepublishReleaseAction("v1.0.0"), 45},
	}
	for _, c := range cases {
		t.Run("Action "+c.name+" should have priority", func(t *testing.T) { eq(t, c.a.Priority(), c.want) })
	}
}

func TestParameterizedManualCommandFormats(t *testing.T) {
	cases := []struct {
		name, pattern string
		a             state.Action
	}{
		{"DeleteTagAction", "git tag -d v1.0.0", NewDeleteTagAction("v1.0.0")},
		{"DeleteBranchAction", "git branch -d v1", NewDeleteBranchAction("v1")},
		{"DeleteReleaseAction", `gh release delete v1\.0\.0.*--yes`, NewDeleteReleaseAction("v1.0.0", 123)},
		{"CreateTagAction", "git push origin abc123:refs/tags/v1.0.0", NewCreateTagAction("v1.0.0", "abc123")},
		{"CreateBranchAction", "git push origin abc123:refs/heads/v1", NewCreateBranchAction("v1", "abc123")},
		{"PublishReleaseAction", `gh release edit v1\.0\.0.*--draft=false`, NewPublishReleaseAction("v1.0.0", 123)},
	}
	for _, c := range cases {
		t.Run(c.name+" should generate command matching pattern", func(t *testing.T) {
			match(t, strings.Join(c.a.ManualCommands(newState()), "\n"), c.pattern)
		})
	}
}
