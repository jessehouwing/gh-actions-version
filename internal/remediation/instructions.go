package remediation

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jessehouwing/gh-actions-version/internal/output"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

var versionKeyRe = regexp.MustCompile(`^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?`)

// versionKey sorts 'latest' and unparsable versions last.
func versionKey(v string) [3]int {
	last := [3]int{math.MaxInt32, 0, 0}
	if v == "latest" {
		return last
	}
	m := versionKeyRe.FindStringSubmatch(v)
	if m == nil {
		return last
	}
	var k [3]int
	for i := 0; i < 3; i++ {
		k[i], _ = strconv.Atoi(m[i+1])
	}
	return k
}

// manualIssues returns the issues needing manual attention sorted by priority, then version.
func manualIssues(st *state.State) []*state.Issue {
	var out []*state.Issue
	for _, i := range st.Issues {
		switch i.Status {
		case state.StatusPending, state.StatusUnfixable, state.StatusFailed, state.StatusManualFixRequired:
			out = append(out, i)
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if pa, pb := out[a].Priority(), out[b].Priority(); pa != pb {
			return pa < pb
		}
		ka, kb := versionKey(out[a].Version), versionKey(out[b].Version)
		for i := range ka {
			if ka[i] != kb[i] {
				return ka[i] < kb[i]
			}
		}
		return false
	})
	return out
}

// commandsFor returns the manual commands and fence language of an issue.
func commandsFor(st *state.State, i *state.Issue) ([]string, string) {
	if i.Action != nil {
		return i.Action.ManualCommands(st), i.Action.ManualCommandsLanguage()
	}
	if i.ManualFixCommand != "" {
		return []string{i.ManualFixCommand}, "bash"
	}
	return nil, "bash"
}

func serverURL(st *state.State) string {
	host := st.Host
	if host == "" {
		host = "github.com"
	}
	return "https://" + host
}

func setupLines(st *state.State, issues []*state.Issue) []string {
	found := false
	for _, i := range issues {
		if c, _ := commandsFor(st, i); len(c) > 0 {
			found = true
			break
		}
	}
	if !found || st.Owner == "" || st.Repo == "" {
		return nil
	}
	return []string{
		fmt.Sprintf("git clone %s/%s/%s.git", serverURL(st), st.Owner, st.Repo),
		"cd " + st.Repo,
		"git fetch --all --tags",
	}
}

func actionTypeName(i *state.Issue) string {
	if i.Action == nil {
		return "Other"
	}
	n := fmt.Sprintf("%T", i.Action)
	return n[strings.LastIndex(n, ".")+1:]
}

// WriteManualInstructions prints the manual remediation commands (port of Get-ManualInstruction).
func WriteManualInstructions(st *state.State, log *output.Logger, groupByType bool) {
	issues := manualIssues(st)
	if len(issues) == 0 {
		return
	}
	log.Group("Manual Remediation Instructions")
	var lines []string
	if setup := setupLines(st, issues); setup != nil {
		lines = append(lines, "# Setup - Clone the repository and fetch all tags and branches:")
		lines = append(lines, setup...)
		lines = append(lines, "", "# Remediation Steps:")
	}
	if groupByType {
		var order []string
		groups := map[string][]*state.Issue{}
		for _, i := range issues {
			n := actionTypeName(i)
			if _, ok := groups[n]; !ok {
				order = append(order, n)
			}
			groups[n] = append(groups[n], i)
		}
		for _, n := range order {
			lines = append(lines, fmt.Sprintf("#### %s (%d issue(s))", n, len(groups[n])), "")
			for _, i := range groups[n] {
				lines = append(lines, fmt.Sprintf("**%s:** %s", i.Version, i.Message))
				cmds, lang := commandsFor(st, i)
				for _, c := range cmds {
					lines = append(lines, "  ```"+lang, "  "+c, "  ```")
				}
				lines = append(lines, "")
			}
		}
	} else {
		for _, i := range issues {
			cmds, _ := commandsFor(st, i)
			lines = append(lines, cmds...)
		}
	}
	log.SafeLines(lines)
	log.EndGroup()
}

// WriteManualInstructionsToStepSummary appends the manual instructions to GITHUB_STEP_SUMMARY
// (port of Write-ManualInstructionsToStepSummary). It does nothing when the variable is unset.
func WriteManualInstructionsToStepSummary(st *state.State) {
	issues := manualIssues(st)
	if len(issues) == 0 {
		return
	}
	var b strings.Builder
	b.WriteString("## Manual Remediation Required\n\n")
	if setup := setupLines(st, issues); setup != nil {
		b.WriteString("### Setup\nClone the repository and fetch all tags and branches:\n```bash\n")
		b.WriteString(strings.Join(setup, "\n"))
		b.WriteString("\n```\n\n### Remediation Steps\n\n")
	}
	for _, i := range issues {
		emoji := "⚠️"
		if i.Status == state.StatusFailed {
			emoji = "❌"
		}
		fmt.Fprintf(&b, "%s **%s:** %s\n", emoji, i.Version, i.Message)
		if cmds, lang := commandsFor(st, i); len(cmds) > 0 {
			b.WriteString("```" + lang + "\n" + strings.Join(cmds, "\n") + "\n```\n")
		}
		b.WriteString("\n")
	}
	output.AppendStepSummary(b.String())
}

// WriteUnresolvedIssues logs every unresolved issue as an annotation by severity (port of Write-UnresolvedIssue).
func WriteUnresolvedIssues(st *state.State, log *output.Logger) {
	for _, i := range st.Issues {
		var title string
		switch i.Status {
		case state.StatusUnfixable:
			title = "Unfixable issue"
		case state.StatusManualFixRequired:
			title = "Manual fix required issue"
		case state.StatusFailed:
			title = "Failed to fix issue"
		case state.StatusPending:
			title = "Unresolved issue"
		default:
			continue
		}
		if i.Severity == state.SeverityError || i.Severity == state.SeverityWarning {
			log.Annotate(i.Severity, title, i.Message)
		}
	}
}

// ExitCode is 1 when an error-severity issue is failed, manual_fix_required, unfixable or still pending
// (skipped in interactive mode); otherwise 0.
func ExitCode(st *state.State) int {
	for _, i := range st.Issues {
		if i.Severity != state.SeverityError {
			continue
		}
		switch i.Status {
		case state.StatusFailed, state.StatusManualFixRequired, state.StatusUnfixable, state.StatusPending:
			return 1
		}
	}
	return 0
}

func errorCount(st *state.State, status string) int {
	n := 0
	for _, i := range st.Issues {
		if i.Severity == state.SeverityError && i.Status == status {
			n++
		}
	}
	return n
}

// Finish logs unresolved issues, prints the summary and manual instructions, writes the step summary and
// returns the process exit code (the tail of main.ps1).
func Finish(st *state.State, log *output.Logger, mode Mode) int {
	WriteUnresolvedIssues(st, log)
	code := ExitCode(st)
	if mode == DryRun {
		log.Info("")
		log.Info("### Dry Run (no changes made)")
	} else if mode == AutoFix || mode == Interactive {
		log.Info("")
		log.Info("### Auto-fix Summary")
		log.Infof("✓ Fixed issues: %d", st.FixedCount())
		log.Infof("✗ Failed fixes: %d", st.FailedCount())
		log.Infof("⚠ Manual fix required: %d", st.ManualFixCount())
		log.Infof("⛔ Unfixable issues: %d", st.UnfixableCount())
		if n := st.CountStatus(state.StatusPending); n > 0 {
			log.Infof("⏭ Skipped: %d", n)
		}
		manual := errorCount(st, state.StatusManualFixRequired)
		unfixable := errorCount(st, state.StatusUnfixable)
		switch {
		case code == 1:
			log.Info("")
			if manual > 0 {
				log.Error("Some issues require manual intervention (e.g., workflow permission issues). Please fix manually.")
			}
			if unfixable > 0 {
				log.Error("Some issues cannot be fixed (e.g., immutable release conflicts). Consider adding affected versions to the ignore-versions list.")
			}
		case st.FixedCount() > 0:
			log.Info("")
			log.Notice("All issues were successfully fixed!")
		case st.ManualFixCount() > 0 || st.UnfixableCount() > 0:
			log.Info("")
			log.Notice("Some warning-level issues require manual attention. See remediation steps below.")
		default:
			log.Info("")
			log.Notice("No issues found!")
		}
	}
	WriteManualInstructions(st, log, false)
	WriteManualInstructionsToStepSummary(st)
	return code
}
