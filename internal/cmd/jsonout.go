package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/cli/go-gh/v2/pkg/jq"
	"github.com/cli/go-gh/v2/pkg/jsonpretty"
	"github.com/cli/go-gh/v2/pkg/template"
	"github.com/cli/go-gh/v2/pkg/term"

	"github.com/jessehouwing/gh-actions-version/internal/remediation"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

// jsonFields are the top-level fields of the --json document.
var jsonFields = []string{"repository", "mode", "local", "config", "issues", "summary", "exitCode"}

func parseJSONFields(in []string, set bool) ([]string, error) {
	var out []string
	for _, f := range in {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if f == "all" {
			return nil, nil
		}
		known := false
		for _, k := range jsonFields {
			known = known || k == f
		}
		if !known {
			return nil, fmt.Errorf("unknown JSON field %q; available fields: %s", f, strings.Join(jsonFields, ", "))
		}
		out = append(out, f)
	}
	return out, nil
}

type jsonAction struct {
	Description    string   `json:"description"`
	ManualCommands []string `json:"manualCommands"`
}

type jsonIssue struct {
	Type        string      `json:"type"`
	Severity    string      `json:"severity"`
	Message     string      `json:"message"`
	Version     string      `json:"version"`
	CurrentSha  string      `json:"currentSha"`
	ExpectedSha string      `json:"expectedSha"`
	Status      string      `json:"status"`
	AutoFixable bool        `json:"autoFixable"`
	Action      *jsonAction `json:"action"`
}

type jsonSummary struct {
	Total             int `json:"total"`
	Errors            int `json:"errors"`
	Warnings          int `json:"warnings"`
	Fixed             int `json:"fixed"`
	Failed            int `json:"failed"`
	ManualFixRequired int `json:"manualFixRequired"`
	Unfixable         int `json:"unfixable"`
	Pending           int `json:"pending"`
}

func buildReport(st *state.State, r *run, code int) map[string]any {
	issues := make([]jsonIssue, 0, len(st.Issues))
	for _, i := range st.Issues {
		ji := jsonIssue{
			Type: i.Type, Severity: i.Severity, Message: i.Message, Version: i.Version,
			CurrentSha: i.CurrentSha, ExpectedSha: i.ExpectedSha, Status: i.Status, AutoFixable: i.IsAutoFixable(),
		}
		if i.Action != nil {
			cmds := i.Action.ManualCommands(st)
			if cmds == nil {
				cmds = []string{}
			}
			ji.Action = &jsonAction{Description: i.Action.Description(), ManualCommands: cmds}
		}
		issues = append(issues, ji)
	}
	cfg := st.Config
	ignore := cfg.IgnoreVersions
	if ignore == nil {
		ignore = []string{}
	}
	return map[string]any{
		"repository": map[string]string{"host": st.Host, "owner": st.Owner, "name": st.Repo, "nameWithOwner": st.Owner + "/" + st.Repo},
		"mode":       r.mode.String(),
		"local":      r.f.local,
		"config": map[string]any{
			"checkMinorVersion":        cfg.CheckMinorVersion,
			"checkReleases":            cfg.CheckReleases,
			"checkReleaseImmutability": cfg.CheckImmutability,
			"checkMarketplace":         cfg.CheckMarketplace,
			"ignorePreviewReleases":    cfg.IgnorePreviewReleases,
			"floatingVersionsUse":      cfg.FloatingVersionsUse,
			"ignoreVersions":           ignore,
		},
		"issues": issues,
		"summary": jsonSummary{
			Total: len(st.Issues), Errors: len(st.ErrorIssues()), Warnings: len(st.WarningIssues()),
			Fixed: st.FixedCount(), Failed: st.FailedCount(), ManualFixRequired: st.ManualFixCount(),
			Unfixable: st.UnfixableCount(), Pending: st.CountStatus(state.StatusPending),
		},
		"exitCode": code,
	}
}

// writeJSON prints the report (optionally restricted to fields) through --jq, --template or as
// JSON: indented with colors on a terminal, compact otherwise.
func writeJSON(env Env, f *flags, fields []string, report map[string]any) error {
	if len(fields) > 0 {
		filtered := map[string]any{}
		for _, k := range fields {
			filtered[k] = report[k]
		}
		report = filtered
	}
	data, err := json.Marshal(report)
	if err != nil {
		return err
	}
	switch {
	case f.jq != "":
		return jq.Evaluate(bytes.NewReader(data), env.Stdout, f.jq)
	case f.template != "":
		t := template.New(env.Stdout, 80, false)
		if err := t.Parse(f.template); err != nil {
			return err
		}
		if err := t.Execute(bytes.NewReader(data)); err != nil {
			return err
		}
		return t.Flush()
	case env.IsTTY:
		return jsonpretty.Format(env.Stdout, bytes.NewReader(data), "  ", !term.IsColorDisabled())
	}
	_, err = env.Stdout.Write(append(data, '\n'))
	return err
}

// writeStepSummary appends a markdown report to GITHUB_STEP_SUMMARY. Manual remediation steps are
// added separately by remediation.Finish.
func writeStepSummary(env Env, st *state.State, mode remediation.Mode, code int) {
	path := env.Getenv("GITHUB_STEP_SUMMARY")
	if path == "" {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## Version validation: %s/%s\n\n", st.Owner, st.Repo)
	if len(st.Issues) == 0 {
		b.WriteString("✅ No issues found.\n\n")
	} else {
		fmt.Fprintf(&b, "Mode: **%s** · errors: %d · warnings: %d · fixed: %d · failed: %d · manual: %d · unfixable: %d\n\n",
			mode, len(st.ErrorIssues()), len(st.WarningIssues()), st.FixedCount(), st.FailedCount(), st.ManualFixCount(), st.UnfixableCount())
		b.WriteString("| Version | Severity | Status | Message |\n|---|---|---|---|\n")
		for _, i := range st.Issues {
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", mdCell(i.Version), i.Severity, i.Status, mdCell(i.Message))
		}
		b.WriteString("\n")
	}
	if code != 0 {
		b.WriteString("❌ Validation errors remain.\n")
	}
	appendFile(path, b.String())
}

func mdCell(s string) string {
	s = strings.NewReplacer("|", "\\|", "\r", " ", "\n", " ").Replace(s)
	return s
}

func appendFile(path, text string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(text)
}
