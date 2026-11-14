package console

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/report"
)

func sampleReport() report.Report {
	finding := report.Finding{
		Rule: "release_should_be_immutable", Finding: "release_not_immutable",
		Concern: "immutability", Severity: "error", Version: "v1.0.0",
		Message: "Release v1.0.0 is published but not immutable.",
		Cause:   "The repository setting 'Release immutability' is disabled.",
		Docs:    "https://example.test/docs/rules/release_should_be_immutable.md",
		Manual:  []string{"do-not-print-this"}, Status: "manual_fix_required",
	}
	return report.Report{
		Mode:     "report-only",
		Findings: []report.Finding{finding},
		Groups: []report.Group{
			{By: "version", Key: "v1.0.0", Count: 1, Findings: []report.Finding{finding}},
			{By: "rule", Key: finding.Rule, Rule: finding.Rule, Concern: finding.Concern, Count: 1, Findings: []report.Finding{finding}},
			{By: "concern", Key: finding.Concern, Concern: finding.Concern, Count: 1, Findings: []report.Finding{finding}},
		},
		Skipped: []report.SkippedCheck{{Check: "immutable_releases_enabled", Reason: "permission denied"}},
		Summary: report.Summary{Total: 1, Errors: 1, AutoFixable: 1},
	}
}

func TestWriteDefaultsToVersionGroupsAndOmitsRemediation(t *testing.T) {
	var out bytes.Buffer
	if err := Write(&out, sampleReport(), Options{}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"v1.0.0 — 1 error",
		"[ERROR] finding immutability / release_not_immutable: Release v1.0.0 is published but not immutable.",
		"Cause: The repository setting 'Release immutability' is disabled.",
		"Docs: https://example.test/docs/rules/release_should_be_immutable.md",
		"Summary: 1 error, 0 warnings, 0 ignored.",
		"1 finding can be fixed automatically",
		"Skipped check: immutable_releases_enabled (permission denied).",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output %q does not contain %q", got, want)
		}
	}
	if strings.Contains(got, "do-not-print-this") {
		t.Fatalf("verify output included remediation commands: %s", got)
	}
}

func TestWriteManualModeDeduplicatesCommandsWithTagPlaceholder(t *testing.T) {
	result := sampleReport()
	result.Mode = "manual"
	first := result.Findings[0]
	first.Manual = []string{"gh release edit v1.0.0 --draft=true"}
	second := first
	second.Version = "v1.0.1"
	second.Manual = []string{"gh release edit v1.0.1 --draft=true"}
	result.Findings = []report.Finding{first, second}

	var out bytes.Buffer
	if err := Write(&out, result, Options{}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if count := strings.Count(got, "gh release edit <tag> --draft=true"); count != 1 {
		t.Fatalf("deduplicated command count = %d, output:\n%s", count, got)
	}
	if strings.Contains(got, "gh release edit v1.0.") {
		t.Fatalf("manual command did not use tag placeholder:\n%s", got)
	}
}

func TestWriteManualModeVerboseExpandsCommandsPerVersion(t *testing.T) {
	result := sampleReport()
	result.Mode = "manual"
	first := result.Findings[0]
	first.Manual = []string{"gh release edit v1.0.0 --draft=true"}
	second := first
	second.Version = "v1.0.1"
	second.Manual = []string{"gh release edit v1.0.1 --draft=true"}
	result.Findings = []report.Finding{first, second}

	var out bytes.Buffer
	if err := Write(&out, result, Options{Verbose: true}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, command := range []string{
		"[v1.0.0] gh release edit v1.0.0 --draft=true",
		"[v1.0.1] gh release edit v1.0.1 --draft=true",
	} {
		if !strings.Contains(got, command) {
			t.Errorf("output %q does not contain %q", got, command)
		}
	}
	if strings.Contains(got, "<tag>") {
		t.Fatalf("verbose output unexpectedly used tag placeholder:\n%s", got)
	}
}

func TestWriteRuleGroupsDeduplicateVersions(t *testing.T) {
	result := sampleReport()
	first := result.Findings[0]
	second := first
	second.Version = "v1.0.1"
	second.Message = "Release v1.0.1 is published but not immutable."
	result.Groups[1].Findings = []report.Finding{first, second}
	result.Groups[1].Count = 2
	result.Groups[1].Versions = []string{"v1.0.0", "v1.0.1"}
	result.Summary.Total, result.Summary.Errors = 2, 2

	var out bytes.Buffer
	if err := Write(&out, result, Options{GroupBy: "rule"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"immutability / release_should_be_immutable — 2 errors",
		"release_not_immutable: Release <tag> is published but not immutable.",
		"Versions: v1.0.1, v1.0.0",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output %q does not contain %q", got, want)
		}
	}
}

func TestWriteConcernGroupsIncludeRulesAndVersions(t *testing.T) {
	result := sampleReport()
	first := result.Findings[0]
	second := first
	second.Version = "v1.0.1"
	second.Message = "Release v1.0.1 is published but not immutable."
	result.Groups[2].Findings = []report.Finding{first, second}

	var out bytes.Buffer
	if err := Write(&out, result, Options{GroupBy: "concern"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"immutability — 2 errors",
		"release_should_be_immutable / release_not_immutable",
		"Versions: v1.0.1, v1.0.0",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output %q does not contain %q", out.String(), want)
		}
	}
}

func TestWriteOrdersVersionGroupsSemantically(t *testing.T) {
	result := sampleReport()
	first := result.Findings[0]
	second := first
	first.Version = "v1.0.2"
	second.Version = "v1.0.10"
	result.Groups = []report.Group{
		{By: "version", Key: first.Version, Findings: []report.Finding{first}},
		{By: "version", Key: second.Version, Findings: []report.Finding{second}},
	}

	var out bytes.Buffer
	if err := Write(&out, result, Options{}); err != nil {
		t.Fatal(err)
	}
	if strings.Index(out.String(), "v1.0.10 —") > strings.Index(out.String(), "v1.0.2 —") {
		t.Fatalf("versions are not ordered semantically:\n%s", out.String())
	}
}

func TestWriteQuietKeepsFindingsButSuppressesSummary(t *testing.T) {
	var out bytes.Buffer
	if err := Write(&out, sampleReport(), Options{Quiet: true}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "release_not_immutable") ||
		strings.Contains(got, "Summary:") || strings.Contains(got, "Run: gh actions-version fix") {
		t.Fatalf("unexpected quiet output: %s", got)
	}
}

func TestWriteColorsSeverityWhenEnabled(t *testing.T) {
	var out bytes.Buffer
	if err := Write(&out, sampleReport(), Options{Color: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\x1b[31m[ERROR] finding\x1b[0m") {
		t.Fatalf("severity was not colored: %q", out.String())
	}
}

func TestWriteRejectsUnknownGrouping(t *testing.T) {
	if err := Write(&bytes.Buffer{}, sampleReport(), Options{GroupBy: "other"}); err == nil {
		t.Fatal("expected invalid group-by error")
	}
}
