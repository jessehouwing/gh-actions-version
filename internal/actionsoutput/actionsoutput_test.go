package actionsoutput

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/report"
)

func TestRenderGroupsAnnotationsAndWritesSummaryOutputs(t *testing.T) {
	result := report.Report{
		Repository: report.Repository{NameWithOwner: "owner/repo"},
		Mode:       "report-only",
		Config:     report.Config{Sources: map[string]string{"preview": "default"}},
		Findings: []report.Finding{
			{Rule: "release_rule", Finding: "not_immutable", Severity: "error", Version: "v1.0.0", Message: "Release v1.0.0 is mutable.", Status: "pending"},
			{Rule: "release_rule", Finding: "not_immutable", Severity: "error", Version: "v1.0.1", Message: "Release v1.0.1 is mutable.", Status: "pending"},
			{Rule: "branch_rule", Finding: "wrong_target", Severity: "warning", Version: "v2", Message: "Target: bad%\nvalue", Status: "manual_fix_required"},
			{Rule: "release_rule", Finding: "fixed", Severity: "error", Version: "v3", Message: "Already fixed", Status: "fixed"},
		},
		Skipped: []report.SkippedCheck{{Check: "settings", Reason: "permission denied"}},
		Fixes:   []report.Fix{{Rule: "release_rule", Finding: "not_immutable", Version: "v1.0.0", Status: "failed", Description: "Update release"}},
		Summary: report.Summary{Errors: 1, Warnings: 1, Fixed: 1, Failed: 1, ManualFixRequired: 1},
	}
	dir := t.TempDir()
	summaryPath := filepath.Join(dir, "summary.md")
	outputsPath := filepath.Join(dir, "outputs")
	var annotations bytes.Buffer
	if err := Render(&annotations, summaryPath, outputsPath, result, 1); err != nil {
		t.Fatal(err)
	}

	got := annotations.String()
	if strings.Count(got, "::error title=release_rule/not_immutable::") != 1 {
		t.Fatalf("same finding was not grouped into one annotation:\n%s", got)
	}
	for _, want := range []string{
		"::group::release_rule",
		"Release <tag> is mutable.%0AVersions: v1.0.0, v1.0.1",
		"::warning title=branch_rule/wrong_target::Target: bad%25%0Avalue%0AVersions: v2",
		"::endgroup::",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("annotations %q do not contain %q", got, want)
		}
	}
	if strings.Contains(got, "title=release_rule/fixed") {
		t.Fatalf("fixed finding received an annotation:\n%s", got)
	}

	summary, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"owner/repo", "## Findings", "v1.0.0, v1.0.1", "## Skipped checks", "## Fixes", "## Effective configuration"} {
		if !strings.Contains(string(summary), want) {
			t.Errorf("step summary does not contain %q:\n%s", want, summary)
		}
	}
	if strings.Contains(string(summary), "```text") {
		t.Fatalf("manual commands were included outside manual mode:\n%s", summary)
	}

	outputs, err := os.ReadFile(outputsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"errors=1", "warnings=1", "fixed=1", "remaining=2", "result=failure"} {
		if !strings.Contains(string(outputs), want) {
			t.Errorf("step outputs do not contain %q: %s", want, outputs)
		}
	}
}

func TestManualCommandsAreOnlyIncludedInManualSummary(t *testing.T) {
	result := report.Report{
		Repository: report.Repository{NameWithOwner: "owner/repo"},
		Mode:       "manual",
		Findings: []report.Finding{{
			Rule: "patch_release_required", Finding: "release_missing", Version: "v1.2.3",
			Message: "Release missing", Severity: "error", Status: "manual_fix_required",
			Manual: []string{"gh release create v1.2.3"},
		}},
	}

	path := filepath.Join(t.TempDir(), "summary.md")
	if err := Render(&bytes.Buffer{}, path, "", result, 1); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "## Manual remediation") ||
		!strings.Contains(string(content), "```text\ngh release create v1.2.3\n```") {
		t.Fatalf("manual command missing from summary:\n%s", content)
	}
}

func TestRenderIgnoredVersions(t *testing.T) {
	result := report.Report{Ignored: []report.IgnoredVersion{{Version: "v1.0.0", Scope: "selection", Reason: "broken | unsafe\nvalue"}}}
	path := filepath.Join(t.TempDir(), "summary.md")
	var annotations bytes.Buffer
	if err := Render(&annotations, path, "", result, 0); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), "## Ignored versions") || !strings.Contains(string(b), `broken \| unsafe value`) ||
		!strings.Contains(annotations.String(), "::notice") || !strings.Contains(annotations.String(), "%0A") {
		t.Fatalf("annotations %s summary %s err %v", annotations.String(), b, err)
	}
}

func TestSummaryIsCappedAndNotesTruncation(t *testing.T) {
	result := report.Report{
		Findings: []report.Finding{{
			Rule: "large_rule", Finding: "large_message", Severity: "error",
			Message: strings.Repeat("x", maxSummarySize+100), Status: "pending",
		}},
	}
	path := filepath.Join(t.TempDir(), "summary.md")
	if err := writeStepSummary(path, result, 1); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) > maxSummarySize {
		t.Fatalf("summary size = %d, exceeds %d", len(content), maxSummarySize)
	}
	if !strings.Contains(string(content), "Summary truncated to fit the 1 MiB") {
		t.Fatalf("truncation note missing:\n%s", content[len(content)-100:])
	}
}

func TestRenderReturnsStepFileErrors(t *testing.T) {
	dir := t.TempDir()
	if err := Render(&bytes.Buffer{}, filepath.Join(dir, "missing", "summary.md"), "", report.Report{}, 0); err == nil {
		t.Fatal("expected an error for an unwritable step summary")
	}
}
