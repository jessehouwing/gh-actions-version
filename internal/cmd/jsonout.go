package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/jq"
	"github.com/cli/go-gh/v2/pkg/jsonpretty"
	"github.com/cli/go-gh/v2/pkg/template"
	"github.com/cli/go-gh/v2/pkg/term"

	"github.com/jessehouwing/gh-actions-version/internal/actions"
	"github.com/jessehouwing/gh-actions-version/internal/report"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

// jsonFields are the top-level fields of the JSON report.
var jsonFields = []string{"schemaVersion", "repository", "mode", "local", "config", "filters", "generatedAt", "refs", "findings", "ignored", "groups", "skipped", "fixes", "summary", "timings", "exitCode"}

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

func buildFindingsReport(st *state.State, r *run, code int, versions []string) (report.Report, error) {
	out := buildReport(st, r, code)
	out.GeneratedAt = time.Now().UTC()
	out.Filters = report.Filters{
		Rules: append([]string{}, r.f.ruleFilter...), SkipRule: append([]string{}, r.f.skipRule...),
		Findings: append([]string{}, r.f.findingFilter...), Versions: append([]string{}, versions...),
		Ignored: append([]string{}, st.Config.IgnoreVersions...),
	}
	out.Refs = report.Refs{Tags: []report.Ref{}, Branches: []report.Ref{}, Releases: []report.Release{}}
	for _, ref := range st.Tags {
		out.Refs.Tags = append(out.Refs.Tags, report.Ref{Name: ref.Version, Ref: ref.Ref, SHA: ref.Sha, Ignored: ref.IsIgnored})
	}
	for _, ref := range st.Branches {
		out.Refs.Branches = append(out.Refs.Branches, report.Ref{Name: ref.Version, Ref: ref.Ref, SHA: ref.Sha, Ignored: ref.IsIgnored})
	}
	for _, release := range st.Releases {
		out.Refs.Releases = append(out.Refs.Releases, report.Release{
			TagName: release.TagName, SHA: release.Sha, ID: release.ID, Draft: release.IsDraft,
			Prerelease: release.IsPrerelease, Immutable: release.IsImmutable, Ignored: release.IsIgnored,
			Latest: release.IsLatest, HTMLURL: release.HtmlUrl,
		})
	}
	for i, issue := range st.Issues {
		if issue.Action == nil {
			continue
		}
		if manual, ok := issue.Action.(savedManualAction); ok {
			out.Findings[i].Action = manual.serialized
			continue
		}
		action, err := actions.Serialize(st, issue.Action)
		if err != nil {
			return report.Report{}, fmt.Errorf("serialize %s/%s: %w", issue.Rule, issue.Finding, err)
		}
		out.Findings[i].Action = &action
	}
	return out, nil
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
