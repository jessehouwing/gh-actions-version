package report

import (
	"context"
	"testing"
	"time"

	"github.com/jessehouwing/gh-actions-version/internal/state"
)

type testAction struct{}

func (testAction) Description() string                                            { return "Create the missing tag" }
func (testAction) Version() string                                                { return "v1" }
func (testAction) Priority() int                                                  { return 1 }
func (testAction) ManualCommandsLanguage() string                                 { return "bash" }
func (testAction) Execute(_ context.Context, _ *state.State, _ state.Client) bool { return false }
func (testAction) ManualCommands(*state.State) []string {
	return []string{"git tag v1"}
}

func TestBuildCreatesSharedReportAndGroups(t *testing.T) {
	st := state.New()
	st.Host, st.Owner, st.Repo = "github.com", "owner", "repo"
	st.Config = state.Config{
		CheckMinorVersion: "warning", FloatingVersionsUse: "tags",
		IgnoreVersions: []string{"v2.*"}, Sources: map[string]string{"preview": "file:action-version.yml"},
		Rules: map[string]state.RulePolicy{
			"major_tag_missing": {
				Level: "warning", AutoFix: boolPointer(false),
				Findings: map[string]state.FindingPolicy{"major_tag_missing": {Level: "error"}},
			},
		},
	}
	st.Issues = []*state.Issue{
		{
			Rule: "major_tag_missing", Finding: "major_tag_missing", Severity: state.SeverityError,
			Version: "v1", Message: "missing major tag", Status: state.StatusFixed, Action: testAction{},
		},
		{
			Rule: "major_tag_missing", Finding: "major_tag_missing", Severity: state.SeverityWarning,
			Version: "v2", Message: "missing major tag", Status: state.StatusManualFixRequired,
		},
	}
	timings := map[string]time.Duration{"load-refs": time.Second}
	out := Build(st, BuildOptions{Mode: "manual", DocsVersion: "dev", Timings: timings})

	if out.Repository.NameWithOwner != "owner/repo" || out.Mode != "manual" {
		t.Fatalf("repository/mode = %+v/%q", out.Repository, out.Mode)
	}
	if out.Config.Sources["preview"] != "file:action-version.yml" || out.Config.Rules["major_tag_missing"].Fix != "manual" ||
		out.Config.Rules["major_tag_missing"].Findings["major_tag_missing"].Level != "error" {
		t.Fatalf("effective config missing from report: %+v", out.Config)
	}
	if out.Findings[0].Docs != "https://github.com/jessehouwing/gh-actions-version/blob/main/docs/rules/major_tag_missing.md" ||
		!out.Findings[0].AutoFixable || len(out.Findings[0].Manual) != 1 {
		t.Fatalf("finding fields missing: %+v", out.Findings[0])
	}
	if out.Summary.Total != 2 || out.Summary.Errors != 1 || out.Summary.Warnings != 1 ||
		out.Summary.AutoFixable != 1 || out.Summary.Fixed != 1 || out.Summary.ManualFixRequired != 1 {
		t.Fatalf("unexpected summary: %+v", out.Summary)
	}
	if len(out.Groups) != 4 || out.Groups[0].By != "version" || out.Groups[2].By != "rule" ||
		out.Groups[2].Count != 2 || out.Groups[3].By != "concern" || out.Groups[3].Key != "major" {
		t.Fatalf("unexpected groups: %+v", out.Groups)
	}
	if len(out.Fixes) != 2 || out.Fixes[0].Description != "Create the missing tag" {
		t.Fatalf("unexpected fixes: %+v", out.Fixes)
	}

	out.Config.Sources["preview"] = "changed"
	out.Config.IgnoreVersions[0] = "v9"
	out.Timings["load-refs"] = 0
	if st.Config.Sources["preview"] != "file:action-version.yml" || st.Config.IgnoreVersions[0] != "v2.*" ||
		timings["load-refs"] != time.Second {
		t.Fatal("Build mutated its input")
	}
}

func TestBuildPinsDocsToReleaseTag(t *testing.T) {
	st := state.New()
	st.Issues = []*state.Issue{{
		Rule: "release_should_be_immutable", Finding: "release_not_immutable",
		Severity: state.SeverityError, Version: "v1.0.0", Status: state.StatusPending,
	}}
	out := Build(st, BuildOptions{DocsVersion: "v2.3.4", Mode: "report-only"})
	if got, want := out.Findings[0].Docs,
		"https://github.com/jessehouwing/gh-actions-version/blob/v2.3.4/docs/rules/release_should_be_immutable.md"; got != want {
		t.Fatalf("docs URL = %q, want %q", got, want)
	}
	if len(out.Fixes) != 0 {
		t.Fatalf("report-only should not include fixes: %+v", out.Fixes)
	}
}

func boolPointer(value bool) *bool { return &value }
