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
	"github.com/jessehouwing/gh-actions-version/internal/report"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

// jsonFields are the top-level fields of the JSON report.
var jsonFields = []string{"schemaVersion", "repository", "mode", "local", "config", "findings", "groups", "skipped", "fixes", "summary", "timings", "exitCode"}

func parseJSONFields(in []string) ([]string, error) {
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

func buildReport(st *state.State, r *run, code int) report.Report {
	out := report.Build(st, report.BuildOptions{Mode: r.mode.String(), DocsVersion: Version, Timings: r.progress.Timings()})
	out.Local = r.f.local
	out.ExitCode = code
	return out
}

// writeJSON prints the report (optionally restricted to fields) through --jq, --template or as
// JSON: indented with colors on a terminal, compact otherwise.
func writeJSON(env Env, f *flags, fields []string, reportData any) error {
	data, err := json.Marshal(reportData)
	if err != nil {
		return err
	}
	if len(fields) > 0 {
		var values map[string]json.RawMessage
		if err := json.Unmarshal(data, &values); err != nil {
			return err
		}
		filtered := make(map[string]json.RawMessage, len(fields))
		for _, k := range fields {
			filtered[k] = values[k]
		}
		data, err = json.Marshal(filtered)
		if err != nil {
			return err
		}
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

// writeStepSummary appends a markdown report to GITHUB_STEP_SUMMARY.
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
