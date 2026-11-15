package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jessehouwing/gh-actions-version/internal/actions"
	"github.com/jessehouwing/gh-actions-version/internal/api"
	"github.com/jessehouwing/gh-actions-version/internal/localgit"
	"github.com/jessehouwing/gh-actions-version/internal/options"
	"github.com/jessehouwing/gh-actions-version/internal/policy"
	"github.com/jessehouwing/gh-actions-version/internal/remediation"
	"github.com/jessehouwing/gh-actions-version/internal/report"
	"github.com/jessehouwing/gh-actions-version/internal/rules"
	"github.com/jessehouwing/gh-actions-version/internal/state"
	"github.com/jessehouwing/gh-actions-version/internal/version"
)

func (r *run) loadFindings() (*state.State, state.Client, *api.Client, error) {
	if r.f.changed["config"] || r.f.noConfig {
		return nil, nil, nil, fmt.Errorf("--config and --no-config cannot be used with --from; the file records the policy")
	}
	var reader io.Reader = r.env.Stdin
	if r.f.from != "-" {
		file, err := os.Open(r.f.from)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("open findings: %w", err)
		}
		defer file.Close()
		reader = file
	}
	var saved report.Report
	dec := json.NewDecoder(reader)
	if err := dec.Decode(&saved); err != nil {
		return nil, nil, nil, fmt.Errorf("read findings: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, nil, nil, fmt.Errorf("findings file must contain one JSON document")
	}
	if saved.SchemaVersion != report.SchemaVersion {
		return nil, nil, nil, fmt.Errorf("unsupported findings schemaVersion %d", saved.SchemaVersion)
	}
	if saved.Mode != "report-only" || saved.GeneratedAt.IsZero() || saved.Findings == nil || saved.Refs.Tags == nil || saved.Refs.Branches == nil || saved.Refs.Releases == nil || saved.Config.Rules == nil || saved.Filters.Rules == nil || saved.Filters.SkipRule == nil || saved.Filters.Findings == nil || saved.Filters.Versions == nil || saved.Filters.Ignored == nil {
		return nil, nil, nil, fmt.Errorf("incomplete findings file; use an unfiltered verify JSON report")
	}
	if saved.Local != r.f.local {
		return nil, nil, nil, fmt.Errorf("saved local mode differs: use --local only with a local findings file")
	}
	repo := api.Repo{Host: saved.Repository.Host, Owner: saved.Repository.Owner, Name: saved.Repository.Name}
	if !saved.Local && (repo.Host == "" || repo.Owner == "" || repo.Name == "" || saved.Repository.NameWithOwner != repo.Owner+"/"+repo.Name) {
		return nil, nil, nil, fmt.Errorf("invalid repository in findings file")
	}
	if r.f.repo != "" {
		want, err := r.env.ResolveRepo(r.f.repo)
		if err != nil {
			return nil, nil, nil, err
		}
		if !strings.EqualFold(want.Host, repo.Host) || !strings.EqualFold(want.Owner, repo.Owner) || !strings.EqualFold(want.Name, repo.Name) {
			return nil, nil, nil, fmt.Errorf("--repo does not match the findings file")
		}
	}
	cfg, err := configFromReport(saved.Config)
	if err != nil {
		return nil, nil, nil, err
	}
	recordedState := &state.State{Config: cfg, CheckedAt: saved.GeneratedAt}
	if err := applyIgnoreFlags(&cfg, r.f); err != nil {
		return nil, nil, nil, err
	}
	if r.f.changed["dangerously-allow-destructive-tag-deletion"] {
		cfg.AllowDestructiveTagDeletion = r.f.allowTagDeletion
		cfg.Sources["permanently-delete-immutable-tags"] = "flag"
	}
	if r.f.changed["preview-releases"] {
		switch strings.ToLower(strings.TrimSpace(r.f.previewReleases)) {
		case "included":
			cfg.IgnorePreviewReleases = false
		case "excluded":
			cfg.IgnorePreviewReleases = true
		default:
			return nil, nil, nil, fmt.Errorf("--preview-releases must be either 'included' or 'excluded'")
		}
		cfg.Sources["preview"] = "flag"
	}
	if r.f.changed["floating-versions-use"] {
		cfg.FloatingVersionsUse = strings.ToLower(strings.TrimSpace(r.f.floatingUse))
		cfg.Sources["floating-versions-use"] = "flag"
	}
	if errs := options.Validate(cfg); len(errs) != 0 {
		return nil, nil, nil, fmt.Errorf("invalid recorded policy: %s", strings.Join(errs, "; "))
	}
	cfg.AutoFix = r.mode == remediation.AutoFix || r.mode == remediation.Interactive
	st := state.New()
	st.Host, st.Owner, st.Repo, st.Config = repo.Host, repo.Owner, repo.Name, cfg
	for _, entry := range saved.Refs.Tags {
		ref := state.NewVersionRef(entry.Name, entry.Ref, entry.SHA, state.TypeTag)
		ref.IsIgnored = entry.Ignored
		st.Tags = append(st.Tags, ref)
	}
	for _, entry := range saved.Refs.Branches {
		ref := state.NewVersionRef(entry.Name, entry.Ref, entry.SHA, state.TypeBranch)
		ref.IsIgnored = entry.Ignored
		st.Branches = append(st.Branches, ref)
	}
	for _, release := range saved.Refs.Releases {
		st.Releases = append(st.Releases, &state.ReleaseInfo{TagName: release.TagName, Sha: release.SHA, ID: release.ID,
			IsDraft: release.Draft, IsPrerelease: release.Prerelease, IsImmutable: release.Immutable,
			IsIgnored: release.Ignored, IsLatest: release.Latest, HtmlUrl: release.HTMLURL})
	}
	overrides, err := parseLevelOverrides(r.f.levels, r.f.ruleLevels, r.f.findingLevels)
	if err != nil {
		return nil, nil, nil, err
	}
	for concern, settings := range overrides.Concerns {
		switch concern {
		case "minor":
			cfg.CheckMinorVersion = settings.Level
		case "releases":
			cfg.CheckReleases = settings.Level
		case "immutability":
			cfg.CheckImmutability = settings.Level
		case "marketplace":
			cfg.CheckMarketplace = settings.Level
		}
	}
	for key, source := range overrides.Sources {
		cfg.Sources[key] = source
	}
	for _, registered := range rules.All() {
		concern, _ := policy.ConcernForRule(registered.Name)
		p := cfg.Rules[registered.Name]
		if level := overrides.Concerns[concern].Level; level != "" {
			p.Level = level
			for name, finding := range p.Findings {
				finding.Level = ""
				p.Findings[name] = finding
			}
		}
		if level := overrides.Rules[registered.Name].Level; level != "" {
			p.Level = level
			for name, finding := range p.Findings {
				finding.Level = ""
				p.Findings[name] = finding
			}
		}
		for name, finding := range overrides.Rules[registered.Name].Findings {
			if p.Findings == nil {
				p.Findings = map[string]state.FindingPolicy{}
			}
			current := p.Findings[name]
			current.Level = finding.Level
			p.Findings[name] = current
		}
		cfg.Rules[registered.Name] = p
	}
	r.cfg, st.Config = cfg, cfg
	for _, finding := range saved.Findings {
		if !knownFinding(finding.Rule, finding.Finding) || (finding.Severity != state.SeverityError && finding.Severity != state.SeverityWarning) {
			return nil, nil, nil, fmt.Errorf("invalid finding %q/%q", finding.Rule, finding.Finding)
		}
		if version.ShouldBeIgnored(finding.Version, r.f.ignoreVersions) {
			continue
		}
		concern, _ := policy.ConcernForRule(finding.Rule)
		level := overrides.Concerns[concern].Level
		if override := overrides.Rules[finding.Rule].Level; override != "" {
			level = override
		}
		if override := overrides.Rules[finding.Rule].Findings[finding.Finding].Level; override != "" {
			level = override
		}
		if level == "none" {
			continue
		}
		if level == "" {
			level = finding.Severity
		}
		issue := &state.Issue{Rule: finding.Rule, Finding: finding.Finding, Severity: level, Version: finding.Version,
			Message: finding.Message, CurrentSha: finding.CurrentSHA, ExpectedSha: finding.ExpectedSHA,
			Status: state.StatusPending, ManualOnly: !finding.AutoFixable}
		if r.f.noIgnore && finding.Action != nil && recordedState.MatchesIgnore(finding.Version, "fix", finding.Rule, finding.Finding) {
			p := cfg.Rules[finding.Rule]
			autoFix := p.AutoFix
			if override := p.Findings[finding.Finding].AutoFix; override != nil {
				autoFix = override
			}
			issue.ManualOnly = autoFix != nil && !*autoFix
		}
		if finding.Action != nil {
			if finding.Action.Kind != "enable-immutable-releases" && finding.Action.Name != finding.Version {
				return nil, nil, nil, fmt.Errorf("action for %s/%s targets a different version", finding.Rule, finding.Finding)
			}
			if finding.Action.RequiresReload != "" && finding.Action.Kind != "delete-release" && finding.Action.Kind != "publish-release" && finding.Action.Kind != "set-latest-release" {
				return nil, nil, nil, fmt.Errorf("invalid requiresReload action %q", finding.Action.Kind)
			}
			action, err := actions.Deserialize(*finding.Action)
			if err != nil {
				if finding.Action.RequiresReload == "" {
					return nil, nil, nil, fmt.Errorf("invalid action for %s/%s: %w", finding.Rule, finding.Finding, err)
				}
				issue.Action = savedManualAction{commands: finding.Manual, reason: err.Error(), serialized: finding.Action}
				issue.ManualOnly = true
				r.log.Warning(err.Error())
			} else {
				issue.Action = action
			}
		} else if finding.AutoFixable {
			return nil, nil, nil, fmt.Errorf("finding %s/%s is missing its action", finding.Rule, finding.Finding)
		} else if len(finding.Manual) != 0 {
			issue.Action = savedManualAction{commands: finding.Manual}
			issue.ManualOnly = true
		}
		st.AddIssue(issue)
	}
	r.log.Infof("Using findings generated at %s; recorded filters: rules=%v skip-rules=%v findings=%v versions=%v ignored=%v",
		saved.GeneratedAt.Format("2006-01-02T15:04:05Z07:00"), saved.Filters.Rules, saved.Filters.SkipRule,
		saved.Filters.Findings, saved.Filters.Versions, saved.Filters.Ignored)
	if r.mode == remediation.Manual || r.mode == remediation.DryRun {
		return st, nil, nil, nil
	}
	if saved.Local {
		dir := r.env.Dir
		if dir == "" {
			dir = "."
		}
		return st, localgit.New(dir), nil, nil
	}
	c, err := r.newClient(repo)
	if err != nil {
		return nil, nil, nil, err
	}
	if r.token == "" {
		return nil, nil, nil, fmt.Errorf("fixing requires a GitHub token")
	}
	return st, c, c, nil
}

func knownFinding(rule, finding string) bool {
	for _, registered := range rules.All() {
		if registered.Name == rule {
			for _, def := range registered.Findings {
				if def.ID == finding {
					return true
				}
			}
		}
	}
	return false
}

func configFromReport(in report.Config) (state.Config, error) {
	out := state.Config{CheckMinorVersion: in.CheckMinorVersion, CheckReleases: in.CheckReleases,
		CheckImmutability: in.CheckReleaseImmutability, CheckMarketplace: in.CheckMarketplace,
		IgnorePreviewReleases: in.IgnorePreviewReleases, FloatingVersionsUse: in.FloatingVersionsUse,
		AllowDestructiveTagDeletion: in.AllowDestructiveTagDeletion, IgnoreVersions: append([]string{}, in.IgnoreVersions...),
		IgnoreEntries: append([]state.IgnoreEntry(nil), in.IgnoreEntries...),
		Rules:         map[string]state.RulePolicy{}, Sources: map[string]string{}}
	for key, source := range in.Sources {
		out.Sources[key] = source
	}
	parseFix := func(value string) (*bool, error) {
		switch value {
		case "":
			return nil, nil
		case "auto", "manual":
			auto := value == "auto"
			return &auto, nil
		default:
			return nil, fmt.Errorf("invalid recorded fix policy %q", value)
		}
	}
	for name, rule := range in.Rules {
		fix, err := parseFix(rule.Fix)
		if err != nil {
			return state.Config{}, err
		}
		p := state.RulePolicy{Level: rule.Level, AutoFix: fix, Findings: map[string]state.FindingPolicy{}}
		for name, finding := range rule.Findings {
			fix, err := parseFix(finding.Fix)
			if err != nil {
				return state.Config{}, err
			}
			p.Findings[name] = state.FindingPolicy{Level: finding.Level, AutoFix: fix}
		}
		out.Rules[name] = p
	}
	return out, nil
}

type savedManualAction struct {
	commands   []string
	reason     string
	serialized *actions.SerializedAction
}

func (a savedManualAction) Description() string                  { return "Manual remediation" }
func (a savedManualAction) Version() string                      { return "" }
func (a savedManualAction) Priority() int                        { return 100 }
func (a savedManualAction) ManualCommandsLanguage() string       { return "bash" }
func (a savedManualAction) ManualCommands(*state.State) []string { return a.commands }
func (a savedManualAction) Execute(context.Context, *state.State, state.Client) bool {
	panic("manual-only saved action cannot be executed: " + a.reason)
}
