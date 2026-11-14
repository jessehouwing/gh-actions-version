package remediation

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/actions"
	"github.com/jessehouwing/gh-actions-version/internal/output"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

type fakeClient struct {
	state.Client
	calls         []string
	createRelease state.ReleaseResult
	failRefs      bool
}

func (f *fakeClient) CreateRef(_ context.Context, ref, sha string, force bool) state.RefResult {
	f.calls = append(f.calls, "create "+ref)
	return state.RefResult{Success: !f.failRefs}
}

func (f *fakeClient) DeleteRef(_ context.Context, ref string) state.RefResult {
	f.calls = append(f.calls, "delete "+ref)
	return state.RefResult{Success: !f.failRefs}
}

func (f *fakeClient) CreateReleaseWithLatest(context.Context, string, bool, *bool) (state.ReleaseResult, error) {
	if f.createRelease.Unfixable {
		return f.createRelease, state.ErrUnfixable
	}
	return f.createRelease, nil
}

type testAction struct {
	desc  string
	ver   string
	prio  int
	run   func(*state.State) bool
	cmds  []string
	lang  string
	order *[]string
}

func (a *testAction) Description() string { return a.desc }
func (a *testAction) Version() string     { return a.ver }
func (a *testAction) Priority() int       { return a.prio }
func (a *testAction) ManualCommandsLanguage() string {
	if a.lang == "" {
		return "bash"
	}
	return a.lang
}
func (a *testAction) ManualCommands(*state.State) []string { return a.cmds }
func (a *testAction) Execute(_ context.Context, st *state.State, _ state.Client) bool {
	if a.order != nil {
		*a.order = append(*a.order, a.ver)
	}
	if a.run != nil {
		return a.run(st)
	}
	return true
}

func issue(version, sev string, a state.Action) *state.Issue {
	i := state.NewIssue("test", sev, "problem with "+version)
	i.Version = version
	i.Action = a
	return i
}

func newState(issues ...*state.Issue) *state.State {
	st := state.New()
	st.Owner, st.Repo, st.Host = "test-owner", "test-repo", "github.com"
	st.Issues = issues
	return st
}

func capture() (*output.Logger, *bytes.Buffer) {
	var b bytes.Buffer
	return &output.Logger{Out: &b, Actions: true}, &b
}

type fakePrompter struct {
	answers []string
	err     error
	asked   int
}

type progressRecorder struct {
	updates []string
}

func (p *progressRecorder) Update(message string) { p.updates = append(p.updates, message) }
func (*progressRecorder) Done(string)             {}

func (p *fakePrompter) Select(_ string, options []string) (int, error) {
	if p.err != nil {
		return 0, p.err
	}
	a := p.answers[p.asked]
	p.asked++
	for i, o := range options {
		if o == a {
			return i, nil
		}
	}
	return -1, nil
}

func TestUnfixableReleaseRemediation(t *testing.T) {
	setup := func() (*state.State, *actions.CreateReleaseAction, *state.Issue) {
		st := newState()
		st.Config.IgnoreVersions = []string{"v1.*"}
		a := actions.NewCreateReleaseAction("v6.3.0", false, false, "")
		i := state.NewIssue("release_missing", "error", "Release required for patch version v6.3.0")
		i.Version, i.Status, i.Action = "v6.3.0", state.StatusPending, a
		st.Issues = []*state.Issue{i}
		return st, a, i
	}
	t.Run("Reclassifies a burned tag and attaches a YAML manual-fix snippet with the suggested ignore-versions value", func(t *testing.T) {
		st, a, i := setup()
		actions.Out = &bytes.Buffer{}
		if a.Execute(context.Background(), st, &fakeClient{createRelease: state.ReleaseResult{Unfixable: true}}) {
			t.Fatal("expected failure")
		}
		if i.Finding != "patch_tag_locked_by_deleted_release" || i.Status != state.StatusManualFixRequired || !strings.Contains(i.Message, "immutable release") {
			t.Fatalf("finding %s status %s msg %s", i.Finding, i.Status, i.Message)
		}
		cmds := a.ManualCommands(st)
		if a.ManualCommandsLanguage() != "yaml" || !regexp.MustCompile(`ignore-versions:\s*"v1\.\*,v6\.3\.0"`).MatchString(strings.Join(cmds, "\n")) {
			t.Fatalf("lang %s cmds %v", a.ManualCommandsLanguage(), cmds)
		}
	})
	t.Run("Renders the snippet inside a fenced ```yaml block (not ```bash) in the GitHub Actions job summary", func(t *testing.T) {
		st, a, _ := setup()
		actions.Out = &bytes.Buffer{}
		a.Execute(context.Background(), st, &fakeClient{createRelease: state.ReleaseResult{Unfixable: true}})
		p := filepath.Join(t.TempDir(), "step-summary.md")
		t.Setenv("GITHUB_STEP_SUMMARY", p)
		WriteManualInstructionsToStepSummary(st)
		raw, _ := os.ReadFile(p)
		s := string(raw)
		if !regexp.MustCompile("(?s)```yaml.*?ignore-versions:\\s*\"v1\\.\\*,v6\\.3\\.0\".*?```").MatchString(s) {
			t.Fatalf("summary: %s", s)
		}
	})
}

func TestFixAutoFix(t *testing.T) {
	ctx := context.Background()
	t.Run("marks pending issues manual_fix_required when auto-fix is disabled", func(t *testing.T) {
		i := issue("v1", "error", &testAction{prio: 20})
		_ = Fix(ctx, newState(i), Options{Mode: ReportOnly})
		if i.Status != state.StatusManualFixRequired {
			t.Fatal(i.Status)
		}
	})
	t.Run("executes actions in priority order", func(t *testing.T) {
		var order []string
		st := newState(
			issue("v3", "error", &testAction{ver: "v3", prio: 40, order: &order}),
			issue("v1", "error", &testAction{ver: "v1", prio: 10, order: &order}),
			issue("v2", "error", &testAction{ver: "v2", prio: 20, order: &order}),
		)
		_ = Fix(ctx, st, Options{Mode: AutoFix})
		if strings.Join(order, ",") != "v1,v2,v3" || st.FixedCount() != 3 {
			t.Fatalf("order %v fixed %d", order, st.FixedCount())
		}
	})
	t.Run("reports progress for each applied fix", func(t *testing.T) {
		i := issue("v1", "error", &testAction{ver: "v1"})
		i.Finding = "test_finding"
		st := newState(i)
		reporter := &progressRecorder{}
		_ = Fix(ctx, st, Options{Mode: AutoFix, Progress: reporter})
		if len(reporter.updates) != 1 || reporter.updates[0] != "[1/1] test_finding v1" {
			t.Fatalf("progress updates %v", reporter.updates)
		}
	})
	t.Run("marks failed when the action returns false", func(t *testing.T) {
		i := issue("v1", "error", &testAction{run: func(*state.State) bool { return false }})
		_ = Fix(ctx, newState(i), Options{Mode: AutoFix})
		if i.Status != state.StatusFailed {
			t.Fatal(i.Status)
		}
	})
	t.Run("keeps a status the action set itself", func(t *testing.T) {
		i := issue("v1", "error", nil)
		i.Action = &testAction{run: func(*state.State) bool { i.Status = state.StatusUnfixable; return false }}
		_ = Fix(ctx, newState(i), Options{Mode: AutoFix})
		if i.Status != state.StatusUnfixable {
			t.Fatal(i.Status)
		}
	})
	t.Run("marks failed when the action panics", func(t *testing.T) {
		i := issue("v1", "error", &testAction{run: func(*state.State) bool { panic("boom") }})
		_ = Fix(ctx, newState(i), Options{Mode: AutoFix})
		if i.Status != state.StatusFailed {
			t.Fatal(i.Status)
		}
	})
	t.Run("marks issues without action unfixable", func(t *testing.T) {
		i := issue("v1", "error", nil)
		_ = Fix(ctx, newState(i), Options{Mode: AutoFix})
		if i.Status != state.StatusUnfixable {
			t.Fatal(i.Status)
		}
	})
	t.Run("does not re-run issues that are not pending", func(t *testing.T) {
		var order []string
		i := issue("v1", "error", &testAction{ver: "v1", order: &order})
		i.Status = state.StatusFixed
		_ = Fix(ctx, newState(i), Options{Mode: AutoFix})
		if len(order) != 0 {
			t.Fatal("executed")
		}
	})
	t.Run("applies real tag actions through the client", func(t *testing.T) {
		actions.Out = &bytes.Buffer{}
		fc := &fakeClient{}
		i := issue("v1", "error", actions.NewCreateTagAction("v1", "abc"))
		_ = Fix(ctx, newState(i), Options{Mode: AutoFix, Client: fc})
		if i.Status != state.StatusFixed || len(fc.calls) != 1 || fc.calls[0] != "create refs/tags/v1" {
			t.Fatalf("%s %v", i.Status, fc.calls)
		}
	})
}

func TestFixInteractive(t *testing.T) {
	ctx := context.Background()
	build := func() (*state.State, *[]string) {
		var order []string
		return newState(
			issue("v3", "error", &testAction{ver: "v3", prio: 30, order: &order, cmds: []string{"git push v3"}}),
			issue("v1", "error", &testAction{ver: "v1", prio: 10, order: &order}),
			issue("v2", "error", &testAction{ver: "v2", prio: 20, order: &order}),
			issue("v9", "error", nil),
		), &order
	}
	statuses := func(st *state.State) string {
		var s []string
		for _, i := range st.Issues {
			s = append(s, i.Version+"="+i.Status)
		}
		return strings.Join(s, " ")
	}
	t.Run("Apply and Skip are handled per issue in priority order; skipped stay pending", func(t *testing.T) {
		st, order := build()
		log, out := capture()
		p := &fakePrompter{answers: []string{OptApply, OptSkip, OptApply}}
		if err := Fix(ctx, st, Options{Mode: Interactive, Prompter: p, Log: log}); err != nil {
			t.Fatal(err)
		}
		if strings.Join(*order, ",") != "v1,v3" || statuses(st) != "v3=fixed v1=fixed v2=pending v9=unfixable" {
			t.Fatalf("%v %s", *order, statuses(st))
		}
		if !strings.Contains(out.String(), "git push v3") {
			t.Fatalf("manual commands not shown: %s", out.String())
		}
	})
	t.Run("Apply all remaining stops prompting", func(t *testing.T) {
		st, order := build()
		p := &fakePrompter{answers: []string{OptSkip, OptApplyAll}}
		_ = Fix(ctx, st, Options{Mode: Interactive, Prompter: p})
		if p.asked != 2 || strings.Join(*order, ",") != "v2,v3" {
			t.Fatalf("asked %d order %v", p.asked, *order)
		}
	})
	t.Run("Skip all remaining leaves issues pending", func(t *testing.T) {
		st, order := build()
		p := &fakePrompter{answers: []string{OptApply, OptSkipAll}}
		_ = Fix(ctx, st, Options{Mode: Interactive, Prompter: p})
		if len(*order) != 1 || statuses(st) != "v3=pending v1=fixed v2=pending v9=unfixable" {
			t.Fatalf("%v %s", *order, statuses(st))
		}
	})
	t.Run("Quit stops without applying anything further", func(t *testing.T) {
		st, order := build()
		p := &fakePrompter{answers: []string{OptQuit}}
		_ = Fix(ctx, st, Options{Mode: Interactive, Prompter: p})
		if len(*order) != 0 || p.asked != 1 {
			t.Fatalf("%v %d", *order, p.asked)
		}
	})
	t.Run("returns prompter errors and keeps issues pending", func(t *testing.T) {
		st, _ := build()
		err := Fix(ctx, st, Options{Mode: Interactive, Prompter: &fakePrompter{err: errors.New("interrupted")}})
		if err == nil || st.CountStatus(state.StatusPending) != 3 {
			t.Fatalf("%v %s", err, statuses(st))
		}
	})
	t.Run("requires a prompter", func(t *testing.T) {
		st, _ := build()
		if Fix(ctx, st, Options{Mode: Interactive}) == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("skipped issues count toward the exit code", func(t *testing.T) {
		st, _ := build()
		_ = Fix(ctx, st, Options{Mode: Interactive, Prompter: &fakePrompter{answers: []string{OptSkipAll}}})
		if ExitCode(st) != 1 {
			t.Fatal("expected exit 1")
		}
	})
}

func TestManualInstructions(t *testing.T) {
	mk := func() *state.State {
		a := issue("v2.0.0", "error", &testAction{prio: 20, cmds: []string{"cmd-v2"}})
		b := issue("v10.0.0", "error", &testAction{prio: 20, cmds: []string{"cmd-v10"}})
		c := issue("v1.0.0", "error", &testAction{prio: 10, cmds: []string{"cmd-v1-first"}})
		d := issue("latest", "error", &testAction{prio: 20, cmds: []string{"cmd-latest"}})
		e := issue("v3", "error", nil)
		e.ManualFixCommand = "manual-cmd"
		f := issue("v4", "error", &testAction{cmds: []string{"never"}})
		f.Status = state.StatusFixed
		return newState(a, b, c, d, e, f)
	}
	t.Run("orders by priority then version, includes setup and skips fixed issues", func(t *testing.T) {
		log, out := capture()
		WriteManualInstructions(mk(), log, false)
		s := out.String()
		want := []string{"##[group]Manual Remediation Instructions", "# Setup", "git clone https://github.com/test-owner/test-repo.git", "cmd-v1-first", "cmd-v2", "cmd-v10", "cmd-latest", "##[endgroup]"}
		pos := -1
		for _, w := range want {
			i := strings.Index(s[pos+1:], w)
			if i < 0 {
				t.Fatalf("missing/out of order %q in:\n%s", w, s)
			}
			pos += i + 1
		}
		if strings.Contains(s, "never") || !strings.Contains(s, "::stop-commands::") || !strings.Contains(s, "manual-cmd") {
			t.Fatalf("%s", s)
		}
	})
	t.Run("groups by action type", func(t *testing.T) {
		log, out := capture()
		WriteManualInstructions(mk(), log, true)
		if !strings.Contains(out.String(), "#### testAction (4 issue(s))") || !strings.Contains(out.String(), "#### Other (1 issue(s))") {
			t.Fatal(out.String())
		}
	})
	t.Run("prints nothing when there are no issues", func(t *testing.T) {
		log, out := capture()
		WriteManualInstructions(newState(), log, false)
		if out.Len() != 0 {
			t.Fatal(out.String())
		}
	})
	t.Run("step summary does nothing without GITHUB_STEP_SUMMARY", func(t *testing.T) {
		t.Setenv("GITHUB_STEP_SUMMARY", "")
		WriteManualInstructionsToStepSummary(mk())
	})
	t.Run("step summary marks failed issues with a cross", func(t *testing.T) {
		st := mk()
		st.Issues[0].Status = state.StatusFailed
		p := filepath.Join(t.TempDir(), "s.md")
		t.Setenv("GITHUB_STEP_SUMMARY", p)
		WriteManualInstructionsToStepSummary(st)
		raw, _ := os.ReadFile(p)
		if !strings.Contains(string(raw), "❌ **v2.0.0:**") || !strings.Contains(string(raw), "## Manual Remediation Required") {
			t.Fatal(string(raw))
		}
	})
}

func TestWriteUnresolvedIssues(t *testing.T) {
	t.Run("logs failed, manual, unfixable and pending issues by severity", func(t *testing.T) {
		mk := func(status, sev, msg string) *state.Issue {
			i := state.NewIssue("t", sev, msg)
			i.Status = status
			return i
		}
		st := newState(
			mk(state.StatusUnfixable, "error", "a"), mk(state.StatusManualFixRequired, "warning", "b"),
			mk(state.StatusFailed, "error", "c"), mk(state.StatusPending, "warning", "d"), mk(state.StatusFixed, "error", "e"))
		log, out := capture()
		WriteUnresolvedIssues(st, log)
		want := "::error title=Unfixable issue::a\n::warning title=Manual fix required issue::b\n::error title=Failed to fix issue::c\n::warning title=Unresolved issue::d\n"
		if out.String() != want {
			t.Fatalf("got %q", out.String())
		}
	})
}

func TestFinishExitCodeAndSummary(t *testing.T) {
	ctx := context.Background()
	run := func(mode Mode, issues ...*state.Issue) (int, string) {
		st := newState(issues...)
		_ = Fix(ctx, st, Options{Mode: mode})
		log, out := capture()
		return Finish(st, log, mode), out.String()
	}
	t.Run("auto-fix with everything fixed exits 0 and prints notice", func(t *testing.T) {
		code, out := run(AutoFix, issue("v1", "error", &testAction{}))
		if code != 0 || !strings.Contains(out, "### Auto-fix Summary") || !strings.Contains(out, "Fixed issues: 1") || !strings.Contains(out, "::notice::All issues were successfully fixed!") {
			t.Fatalf("%d %s", code, out)
		}
	})
	t.Run("auto-fix with no issues prints No issues found", func(t *testing.T) {
		code, out := run(AutoFix)
		if code != 0 || !strings.Contains(out, "::notice::No issues found!") {
			t.Fatalf("%d %s", code, out)
		}
	})
	t.Run("failed error issue exits 1", func(t *testing.T) {
		code, _ := run(AutoFix, issue("v1", "error", &testAction{run: func(*state.State) bool { return false }}))
		if code != 1 {
			t.Fatal(code)
		}
	})
	t.Run("unfixable error issue exits 1 and suggests ignore-versions", func(t *testing.T) {
		code, out := run(AutoFix, issue("v1", "error", nil))
		if code != 1 || !strings.Contains(out, "::error::Some issues cannot be fixed") {
			t.Fatalf("%d %s", code, out)
		}
	})
	t.Run("warning-only unresolved issues exit 0 with a notice", func(t *testing.T) {
		code, out := run(AutoFix, issue("v1", "warning", nil))
		if code != 0 || !strings.Contains(out, "::notice::Some warning-level issues require manual attention") {
			t.Fatalf("%d %s", code, out)
		}
	})
	t.Run("report-only with error issues exits 1 and prints instructions without summary", func(t *testing.T) {
		code, out := run(ReportOnly, issue("v1", "error", &testAction{cmds: []string{"do-it"}}))
		if code != 1 || strings.Contains(out, "Auto-fix Summary") || !strings.Contains(out, "do-it") {
			t.Fatalf("%d %s", code, out)
		}
	})
	t.Run("manual_fix_required error mentions manual intervention", func(t *testing.T) {
		i := issue("v1", "error", nil)
		i.Action = &testAction{run: func(*state.State) bool { i.Status = state.StatusManualFixRequired; return false }}
		code, out := run(AutoFix, i)
		if code != 1 || !strings.Contains(out, "::error::Some issues require manual intervention") {
			t.Fatalf("%d %s", code, out)
		}
	})
}
