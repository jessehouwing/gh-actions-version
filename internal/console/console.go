package console

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/jessehouwing/gh-actions-version/internal/report"
	"github.com/jessehouwing/gh-actions-version/internal/version"
)

type Options struct {
	GroupBy string
	Quiet   bool
	Color   bool
	Verbose bool
}

type findingKey struct {
	rule, finding, severity, message, cause, docs, status string
	autoFixable                                           bool
}

type findingGroup struct {
	report.Finding
	versions []string
}

func Write(w io.Writer, result report.Report, options Options) error {
	if options.GroupBy == "" {
		options.GroupBy = "version"
	}
	if options.GroupBy != "version" && options.GroupBy != "rule" && options.GroupBy != "concern" {
		return fmt.Errorf("group-by must be version, rule or concern")
	}

	var b strings.Builder
	groups := groupsBy(result, options.GroupBy)
	for _, group := range groups {
		writeGroup(&b, group, result.Mode, options.Color)
	}
	if result.Mode == "manual" {
		writeManualInstructions(&b, result.Findings, options.Verbose)
	}
	if !options.Quiet {
		writeSummary(&b, result)
	}
	for _, skipped := range result.Skipped {
		fmt.Fprintf(&b, "Skipped check: %s (%s).\n", skipped.Check, skipped.Reason)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

type manualFinding struct {
	rule, finding string
	commands      []string
	seen          map[string]bool
}

func writeManualInstructions(b *strings.Builder, findings []report.Finding, verbose bool) {
	var grouped []*manualFinding
	byFinding := make(map[string]*manualFinding)
	for _, finding := range findings {
		if len(finding.Manual) == 0 {
			continue
		}
		key := finding.Rule + "\x00" + finding.Finding
		group := byFinding[key]
		if group == nil {
			group = &manualFinding{rule: finding.Rule, finding: finding.Finding, seen: make(map[string]bool)}
			byFinding[key] = group
			grouped = append(grouped, group)
		}
		for _, command := range finding.Manual {
			versioned := finding.Version != "" && strings.Contains(command, finding.Version)
			if verbose && versioned {
				command = "[" + finding.Version + "] " + command
			} else if !verbose && finding.Version != "" {
				command = strings.ReplaceAll(command, finding.Version, "<tag>")
			}
			if group.seen[command] {
				continue
			}
			group.seen[command] = true
			group.commands = append(group.commands, command)
		}
	}
	if len(grouped) == 0 {
		return
	}
	fmt.Fprintln(b, "Manual instructions:")
	for _, group := range grouped {
		fmt.Fprintf(b, "  %s / %s:\n", group.rule, group.finding)
		for _, command := range group.commands {
			fmt.Fprintf(b, "    Fix: %s\n", strings.ReplaceAll(command, "\n", "\n    "))
		}
	}
}

func groupsBy(result report.Report, by string) []report.Group {
	var out []report.Group
	for _, group := range result.Groups {
		if group.By == by {
			out = append(out, group)
		}
	}
	if by == "version" {
		sort.SliceStable(out, func(i, j int) bool {
			return versionLess(out[i].Key, out[j].Key)
		})
	}
	return out
}

func writeGroup(b *strings.Builder, group report.Group, mode string, color bool) {
	errors, warnings := 0, 0
	for _, finding := range group.Findings {
		switch finding.Severity {
		case "error":
			errors++
		case "warning":
			warnings++
		}
	}
	fmt.Fprintf(b, "%s — %s\n", heading(group), countLabel(errors, warnings))

	if group.By == "version" {
		for _, finding := range group.Findings {
			label := statusLabel(finding, mode)
			fmt.Fprintf(b, "  %s %s / %s: %s\n", colorize(label, severityColor(finding.Severity), color),
				finding.Concern, finding.Finding, finding.Message)
			writeDetails(b, finding, "  ")
		}
		return
	}

	for _, finding := range groupFindings(group) {
		issue := finding.Finding
		label := statusLabel(issue, mode)
		title := issue.Finding
		if group.By == "concern" {
			title = issue.Rule + " / " + title
		}
		fmt.Fprintf(b, "  %s %s: %s\n", colorize(label, severityColor(issue.Severity), color), title, issue.Message)
		if len(finding.versions) > 0 {
			fmt.Fprintf(b, "    Versions: %s\n", strings.Join(finding.versions, ", "))
		}
		writeDetails(b, issue, "    ")
	}
}

func heading(group report.Group) string {
	switch group.By {
	case "rule":
		if group.Concern != "" {
			return group.Concern + " / " + group.Rule
		}
		return group.Rule
	case "concern":
		return group.Key
	default:
		return group.Key
	}
}

func groupFindings(group report.Group) []findingGroup {
	byKey := make(map[findingKey]*findingGroup)
	for _, finding := range group.Findings {
		message := finding.Message
		if len(group.Findings) > 1 && finding.Version != "" {
			message = strings.ReplaceAll(message, finding.Version, "<tag>")
		}
		key := findingKey{
			rule: finding.Rule, finding: finding.Finding, severity: finding.Severity,
			message: message, cause: finding.Cause, docs: finding.Docs, status: finding.Status,
			autoFixable: finding.AutoFixable,
		}
		item := byKey[key]
		if item == nil {
			item = &findingGroup{Finding: finding}
			item.Message = message
			byKey[key] = item
		}
		if finding.Version != "" && !contains(item.versions, finding.Version) {
			item.versions = append(item.versions, finding.Version)
		}
	}
	keys := make([]findingKey, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].finding != keys[j].finding {
			return keys[i].finding < keys[j].finding
		}
		if keys[i].severity != keys[j].severity {
			return keys[i].severity < keys[j].severity
		}
		return keys[i].message < keys[j].message
	})
	out := make([]findingGroup, 0, len(keys))
	for _, key := range keys {
		item := byKey[key]
		sort.SliceStable(item.versions, func(i, j int) bool {
			return versionLess(item.versions[i], item.versions[j])
		})
		out = append(out, *item)
	}
	return out
}

func versionLess(a, b string) bool {
	left, leftErr := version.ConvertTo(strings.TrimPrefix(a, "v"))
	right, rightErr := version.ConvertTo(strings.TrimPrefix(b, "v"))
	switch {
	case leftErr == nil && rightErr == nil:
		if comparison := left.Compare(right); comparison != 0 {
			return comparison > 0
		}
		return a < b
	case leftErr == nil:
		return true
	case rightErr == nil:
		return false
	default:
		return a < b
	}
}

func writeDetails(b *strings.Builder, finding report.Finding, indent string) {
	if finding.Cause != "" {
		fmt.Fprintf(b, "%sCause: %s\n", indent, finding.Cause)
	}
	if finding.Docs != "" {
		fmt.Fprintf(b, "%sDocs: %s\n", indent, finding.Docs)
	}
}

func statusLabel(finding report.Finding, mode string) string {
	severity := strings.ToUpper(finding.Severity)
	if severity != "" {
		severity = "[" + severity + "] "
	}
	if mode == "report-only" {
		return severity + "finding"
	}
	if mode == "dry-run" && finding.AutoFixable {
		return severity + "would fix"
	}
	switch finding.Status {
	case "fixed":
		return severity + "fixed"
	case "failed":
		return severity + "failed fix"
	case "manual_fix_required":
		return severity + "manual step"
	case "unfixable":
		return severity + "unfixable"
	default:
		return severity + "finding"
	}
}

func severityColor(severity string) string {
	if severity == "warning" {
		return "33"
	}
	return "31"
}

func colorize(message, code string, enabled bool) string {
	if !enabled {
		return message
	}
	return "\x1b[" + code + "m" + message + "\x1b[0m"
}

func countLabel(errors, warnings int) string {
	parts := make([]string, 0, 2)
	if errors > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", errors, plural(errors, "error", "errors")))
	}
	if warnings > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", warnings, plural(warnings, "warning", "warnings")))
	}
	if len(parts) == 0 {
		return "0 findings"
	}
	return strings.Join(parts, ", ")
}

func plural(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

func writeSummary(b *strings.Builder, result report.Report) {
	if result.Summary.Total == 0 && result.Summary.Ignored == 0 {
		fmt.Fprintln(b, "No findings.")
		return
	}
	fmt.Fprintf(b, "\nSummary: %d %s, %d %s, %d ignored.\n",
		result.Summary.Errors, plural(result.Summary.Errors, "error", "errors"),
		result.Summary.Warnings, plural(result.Summary.Warnings, "warning", "warnings"),
		result.Summary.Ignored)
	switch result.Mode {
	case "report-only":
		if result.Summary.AutoFixable > 0 {
			fmt.Fprintf(b, "%d %s can be fixed automatically (run: gh actions-version fix --mode auto).\n",
				result.Summary.AutoFixable, plural(result.Summary.AutoFixable, "finding", "findings"))
		} else if result.Summary.Total > 0 {
			fmt.Fprintln(b, "Run: gh actions-version fix")
		}
	case "dry-run":
		fmt.Fprintln(b, "Dry run: no changes made.")
	default:
		if result.Summary.Fixed+result.Summary.Failed+result.Summary.ManualFixRequired+result.Summary.Unfixable > 0 {
			fmt.Fprintf(b, "Fixes: %d fixed, %d failed, %d manual steps, %d unfixable.\n",
				result.Summary.Fixed, result.Summary.Failed, result.Summary.ManualFixRequired, result.Summary.Unfixable)
		}
	}
}

func contains(values []string, value string) bool {
	for _, current := range values {
		if current == value {
			return true
		}
	}
	return false
}
