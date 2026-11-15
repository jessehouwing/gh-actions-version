package actionsoutput

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/jessehouwing/gh-actions-version/internal/report"
)

const maxSummarySize = 1 << 20

type findingGroup struct {
	rule, finding, severity string
	messages, versions      []string
	statuses                []string
	unresolved              bool
}

func Render(w io.Writer, summaryPath, outputsPath string, result report.Report, exitCode int) error {
	if err := writeAnnotations(w, result.Findings); err != nil {
		return err
	}
	for _, ignored := range result.Ignored {
		message := fmt.Sprintf("%s (%s)", ignored.Version, ignored.Scope)
		if ignored.Reason != "" {
			message += " - " + ignored.Reason
		}
		if err := writeLine(w, "::notice title=Ignored version::"+escapeData(message)); err != nil {
			return err
		}
	}
	if summaryPath != "" {
		if err := writeStepSummary(summaryPath, result, exitCode); err != nil {
			return err
		}
	}
	if outputsPath != "" {
		if err := writeOutputs(outputsPath, result, exitCode); err != nil {
			return err
		}
	}
	return nil
}

func writeAnnotations(w io.Writer, findings []report.Finding) error {
	groups := groupFindings(findings)
	annotationCounts := map[string]int{}
	currentRule := ""
	for _, group := range groups {
		if !group.unresolved {
			continue
		}
		level := annotationLevel(group.severity)
		if annotationCounts[level] >= 10 {
			continue
		}
		if group.rule != currentRule {
			if currentRule != "" {
				if err := writeLine(w, "::endgroup::"); err != nil {
					return err
				}
			}
			if err := writeLine(w, "::group::"+escapeData(group.rule)); err != nil {
				return err
			}
			currentRule = group.rule
		}
		message := strings.Join(group.messages, "; ")
		if len(group.versions) > 0 {
			message += "\nVersions: " + strings.Join(group.versions, ", ")
		}
		command := "::" + level + " title=" + escapeProperty(group.rule+"/"+group.finding) + "::" + escapeData(message)
		if err := writeLine(w, command); err != nil {
			return err
		}
		annotationCounts[level]++
	}
	if currentRule != "" {
		return writeLine(w, "::endgroup::")
	}
	return nil
}

func groupFindings(findings []report.Finding) []findingGroup {
	type key struct{ rule, finding string }
	byKey := make(map[key]*findingGroup)
	var groups []*findingGroup
	for _, finding := range findings {
		k := key{finding.Rule, finding.Finding}
		group := byKey[k]
		if group == nil {
			group = &findingGroup{rule: finding.Rule, finding: finding.Finding, severity: finding.Severity}
			byKey[k] = group
			groups = append(groups, group)
		}
		if severityRank(finding.Severity) > severityRank(group.severity) {
			group.severity = finding.Severity
		}
		message := finding.Message
		if finding.Version != "" {
			message = strings.ReplaceAll(message, finding.Version, "<tag>")
			group.versions = appendUnique(group.versions, finding.Version)
		}
		group.messages = appendUnique(group.messages, message)
		group.statuses = appendUnique(group.statuses, finding.Status)
		if finding.Status != "fixed" {
			group.unresolved = true
		}
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].rule != groups[j].rule {
			return groups[i].rule < groups[j].rule
		}
		return groups[i].finding < groups[j].finding
	})
	for _, group := range groups {
		sort.Strings(group.versions)
	}
	out := make([]findingGroup, 0, len(groups))
	for _, group := range groups {
		out = append(out, *group)
	}
	return out
}

func annotationLevel(severity string) string {
	switch severity {
	case "error":
		return "error"
	case "warning":
		return "warning"
	default:
		return "notice"
	}
}

func severityRank(severity string) int {
	switch severity {
	case "error":
		return 3
	case "warning":
		return 2
	default:
		return 1
	}
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func escapeData(value string) string {
	value = strings.ReplaceAll(value, "%", "%25")
	value = strings.ReplaceAll(value, "\r", "%0D")
	return strings.ReplaceAll(value, "\n", "%0A")
}

func escapeProperty(value string) string {
	value = escapeData(value)
	value = strings.ReplaceAll(value, ":", "%3A")
	return strings.ReplaceAll(value, ",", "%2C")
}

func writeLine(w io.Writer, line string) error {
	_, err := io.WriteString(w, line+"\n")
	return err
}

func writeStepSummary(path string, result report.Report, exitCode int) error {
	var b strings.Builder
	repository := result.Repository.NameWithOwner
	if repository == "" {
		repository = result.Repository.Name
	}
	outcome := "success"
	if exitCode != 0 {
		outcome = "failure"
	}
	fmt.Fprintf(&b, "# Version validation: %s\n\n", mdCell(repository))
	fmt.Fprintf(&b, "**Result:** %s (exit code %d)<br>\n**Mode:** %s<br>\n**Errors:** %d · **Warnings:** %d · **Fixed:** %d · **Remaining:** %d\n\n",
		outcome, exitCode, mdCell(result.Mode), result.Summary.Errors, result.Summary.Warnings, result.Summary.Fixed, remaining(result.Summary))

	if len(result.Findings) == 0 {
		b.WriteString("No findings.\n\n")
	} else {
		b.WriteString("## Findings\n\n| Rule | Finding | Versions | Severity | Status | Message |\n|---|---|---|---|---|---|\n")
		for _, group := range groupFindings(result.Findings) {
			versions := strings.Join(group.versions, ", ")
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
				mdCell(group.rule), mdCell(group.finding), mdCell(versions), mdCell(group.severity),
				mdCell(strings.Join(group.statuses, ", ")), mdCell(strings.Join(group.messages, "; ")))
		}
		b.WriteString("\n")
	}

	if len(result.Ignored) > 0 {
		b.WriteString("## Ignored versions\n\n| Version | Scope | Reason | Until | Rules |\n|---|---|---|---|---|\n")
		for _, ignored := range result.Ignored {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", mdCell(ignored.Version), mdCell(ignored.Scope),
				mdCell(ignored.Reason), mdCell(ignored.Until), mdCell(strings.Join(ignored.MatchedRules, ", ")))
		}
		b.WriteString("\n")
	}
	if len(result.Skipped) > 0 {
		b.WriteString("## Skipped checks\n\n")
		for _, skipped := range result.Skipped {
			fmt.Fprintf(&b, "- `%s`: %s\n", mdCell(skipped.Check), mdCell(skipped.Reason))
		}
		b.WriteString("\n")
	}
	if len(result.Fixes) > 0 {
		b.WriteString("## Fixes\n\n| Rule | Finding | Version | Status | Description |\n|---|---|---|---|---|\n")
		for _, fix := range result.Fixes {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
				mdCell(fix.Rule), mdCell(fix.Finding), mdCell(fix.Version), mdCell(fix.Status), mdCell(fix.Description))
		}
		b.WriteString("\n")
	}
	if result.Mode == "manual" {
		writeManualSummary(&b, result.Findings)
	}

	config, err := json.MarshalIndent(result.Config, "", "  ")
	if err != nil {
		return err
	}
	b.WriteString("## Effective configuration\n\n```json\n")
	b.Write(config)
	b.WriteString("\n```\n")
	return appendCapped(path, b.String())
}

func writeManualSummary(b *strings.Builder, findings []report.Finding) {
	wroteHeader := false
	for _, finding := range findings {
		if len(finding.Manual) == 0 {
			continue
		}
		if !wroteHeader {
			b.WriteString("## Manual remediation\n\n")
			wroteHeader = true
		}
		fmt.Fprintf(b, "### %s / %s (%s)\n\n", mdCell(finding.Rule), mdCell(finding.Finding), mdCell(finding.Version))
		b.WriteString("```text\n")
		b.WriteString(strings.Join(finding.Manual, "\n"))
		b.WriteString("\n```\n\n")
	}
}

func remaining(summary report.Summary) int {
	return summary.Failed + summary.ManualFixRequired + summary.Unfixable + summary.Pending
}

func mdCell(value string) string {
	value = strings.NewReplacer("|", "\\|", "\r", " ", "\n", " ").Replace(value)
	return value
}

func appendCapped(path, content string) error {
	existingSize := int64(0)
	if info, err := os.Stat(path); err == nil {
		existingSize = info.Size()
	} else if !os.IsNotExist(err) {
		return err
	}
	remainingBytes := maxSummarySize - int(existingSize)
	if remainingBytes <= 0 {
		return nil
	}
	if len(content) > remainingBytes {
		const note = "\n\n> Summary truncated to fit the 1 MiB GitHub Actions step summary limit.\n"
		if remainingBytes >= len(note) {
			content = truncateUTF8(content, remainingBytes-len(note)) + note
		} else {
			content = truncateUTF8(content, remainingBytes)
		}
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(file, content); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func truncateUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func writeOutputs(path string, result report.Report, exitCode int) error {
	outcome := "success"
	if exitCode != 0 {
		outcome = "failure"
	}
	text := fmt.Sprintf("errors=%d\nwarnings=%d\nfixed=%d\nremaining=%d\nresult=%s\n",
		result.Summary.Errors, result.Summary.Warnings, result.Summary.Fixed, remaining(result.Summary), outcome)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(file, text); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
