// Package cmd is the `gh actions-version` command line: flag parsing, mode selection, startup auth
// checks and the validate → fix → report pipeline.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/term"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/jessehouwing/gh-actions-version/internal/api"
	"github.com/jessehouwing/gh-actions-version/internal/console"
	"github.com/jessehouwing/gh-actions-version/internal/localgit"
	"github.com/jessehouwing/gh-actions-version/internal/options"
	"github.com/jessehouwing/gh-actions-version/internal/output"
	"github.com/jessehouwing/gh-actions-version/internal/policy"
	"github.com/jessehouwing/gh-actions-version/internal/progress"
	"github.com/jessehouwing/gh-actions-version/internal/remediation"
	"github.com/jessehouwing/gh-actions-version/internal/report"
	"github.com/jessehouwing/gh-actions-version/internal/rules" // importing registers the validation rules
	"github.com/jessehouwing/gh-actions-version/internal/state"
	"github.com/jessehouwing/gh-actions-version/internal/version"
)

// Version is set at build time (-ldflags "-X .../internal/cmd.Version=v1.2.3").
var Version = "dev"

// Exit codes.
const (
	ExitOK         = 0
	ExitValidation = 1 // validation errors remain
	ExitUsage      = 2 // usage, auth or rate limit errors
)

type flags struct {
	repo                                             string
	output                                           string
	groupBy                                          string
	fix, interactive, autoMode, manualMode, yes      bool
	dryRun, noVerifyAfter                            bool
	local, fetchReleases, noConfig, verbose, noColor bool
	noProgress, quiet                                bool
	previewReleases                                  string
	allowTagDeletion                                 bool
	floatingUse                                      string
	ignoreVersions                                   string
	maxWait                                          time.Duration
	config                                           string
	mode                                             string
	levels, ruleLevels, findingLevels                []string
	ruleFilter, skipRule, findingFilter              []string
	failOn                                           string
	jsonFields                                       []string
	jq, template                                     string
	changed                                          map[string]bool // flags given on the command line
}

// Run executes the command line (args exclude the program name) and returns the exit code.
func Run(ctx context.Context, args []string, env Env) int {
	env = env.withDefaults()
	var f flags
	code := ExitOK
	root := &cobra.Command{
		Use:   "actions-version",
		Short: "Validate and fix the version tags, branches and releases of an action repository",
		Long: `Validate the semantic version tags, branches and releases of a GitHub Action repository
against GitHub's immutable release strategy: patch versions (v1.0.0) need an immutable release,
floating versions (v1, v1.0, latest) must point at the latest compatible patch.

Use verify for read-only validation and fix to apply remediations. Both commands accept optional
version arguments (v1, v1.0, v1.*) to limit the report and fixes to those versions.

Exit codes: 0 ok, 1 validation errors remaining, 2 usage, authentication or rate limit errors,
3 fixes applied but qualifying findings remain.`,
		Version:           Version,
		SilenceUsage:      true,
		SilenceErrors:     true,
		Args:              cobra.NoArgs,
		DisableAutoGenTag: true,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := c.Help(); err != nil {
				return err
			}
			code = ExitUsage
			return nil
		},
	}
	root.SetOut(env.Stdout)
	root.SetErr(env.Stderr)
	root.SetIn(env.Stdin)
	root.SetArgs(args)
	addGlobalFlags(root.PersistentFlags(), &f)

	runCommand := func(c *cobra.Command, versions []string) {
		f.changed = map[string]bool{}
		c.Flags().Visit(func(fl *pflag.Flag) { f.changed[fl.Name] = true })
		c.InheritedFlags().Visit(func(fl *pflag.Flag) { f.changed[fl.Name] = true })
		code = execute(ctx, &f, versions, env)
	}
	verify := &cobra.Command{
		Use:   "verify [versions...]",
		Short: "Validate versions without making changes",
		Args:  cobra.ArbitraryArgs,
		Run: func(c *cobra.Command, versions []string) {
			runCommand(c, versions)
		},
	}
	fix := &cobra.Command{
		Use:   "fix [versions...]",
		Short: "Validate versions and apply remediations",
		Args:  cobra.ArbitraryArgs,
		Run: func(c *cobra.Command, versions []string) {
			f.fix = true
			runCommand(c, versions)
		},
	}
	addCommandFlags(verify.Flags(), &f)
	addCommandFlags(fix.Flags(), &f)
	addFixFlags(fix.Flags(), &f)
	root.AddCommand(verify, fix)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(env.Stderr, "Error:", err)
		return ExitUsage
	}
	return code
}

func addGlobalFlags(fl *pflag.FlagSet, f *flags) {
	fl.StringVarP(&f.repo, "repo", "R", "", "Target repository as [HOST/]OWNER/REPO (default: GH_REPO, GITHUB_REPOSITORY or the git remote)")
	fl.StringVarP(&f.output, "output", "o", "", "Output format: console, json or actions (defaults by environment)")
	fl.StringVar(&f.groupBy, "group-by", "version", "Group console findings by version, rule or concern")
	fl.BoolVar(&f.local, "local", false, "Read and fix refs in the local git repository; never pushes")
	fl.BoolVar(&f.local, "local-only", false, "Alias of --local")
	fl.BoolVar(&f.fetchReleases, "fetch-releases", false, "With --local: also read releases from GitHub (read-only)")
	fl.DurationVar(&f.maxWait, "max-wait", 60*time.Second, "Longest rate limit wait tolerated before failing with exit code 2")
	fl.BoolVarP(&f.verbose, "verbose", "v", false, "Write debug details to stderr")
	fl.BoolVar(&f.noColor, "no-color", false, "Disable colored output")
	fl.BoolVar(&f.noProgress, "no-progress", false, "Use plain progress lines instead of a terminal spinner")
	fl.BoolVar(&f.quiet, "quiet", false, "Suppress progress and informational output")
	fl.StringVar(&f.config, "config", "", "Read policy from this repository file instead of searching default locations")
	fl.BoolVar(&f.noConfig, "no-config", false, "Skip repository policy files and use built-in defaults")
}

func addCommandFlags(fl *pflag.FlagSet, f *flags) {
	fl.StringVar(&f.failOn, "fail-on", "error", "Fail on error, warning or never")
	fl.StringArrayVar(&f.levels, "level", nil, "Set a concern level (repeatable KEY=LEVEL)")
	fl.StringArrayVar(&f.ruleLevels, "rule-level", nil, "Set a rule level (repeatable KEY=LEVEL)")
	fl.StringArrayVar(&f.findingLevels, "finding-level", nil, "Set a finding level (repeatable KEY=LEVEL)")
	fl.StringArrayVar(&f.ruleFilter, "rule", nil, "Report or fix only these rules (repeatable)")
	fl.StringArrayVar(&f.skipRule, "skip-rule", nil, "Skip these rules (repeatable)")
	fl.StringArrayVar(&f.findingFilter, "finding", nil, "Report or fix only these findings (repeatable)")
	fl.StringVar(&f.previewReleases, "preview-releases", "", "Include or exclude prereleases when picking the latest patch: included or excluded")
	fl.BoolVar(&f.allowTagDeletion, "dangerously-allow-destructive-tag-deletion", false, "Allow deleting a tag whose immutable release was deleted; this cannot be undone")
	fl.StringVar(&f.floatingUse, "floating-versions-use", "tags", "Use tags or branches for floating versions")
	fl.StringVar(&f.ignoreVersions, "ignore-versions", "", "Comma separated versions to skip; wildcards like v1.* are supported")
	fl.StringSliceVar(&f.jsonFields, "json-fields", nil, "Select JSON fields (available: "+strings.Join(jsonFields, ", ")+")")
	fl.StringVarP(&f.jq, "jq", "q", "", "Filter the JSON output with a jq expression")
	fl.StringVarP(&f.template, "template", "t", "", "Format the JSON output with a Go template")
}

func addFixFlags(fl *pflag.FlagSet, f *flags) {
	fl.StringVar(&f.mode, "mode", "", "Fix mode: interactive, auto or manual")
	fl.BoolVarP(&f.interactive, "interactive", "i", false, "Review and apply fixes one by one")
	fl.BoolVar(&f.autoMode, "auto", false, "Apply every auto-fixable remediation without prompting")
	fl.BoolVar(&f.manualMode, "manual", false, "Print remediation instructions without applying fixes")
	fl.BoolVar(&f.yes, "yes", false, "Answer yes to every interactive prompt")
	fl.BoolVar(&f.dryRun, "dry-run", false, "Show what auto mode would apply without making changes")
	fl.BoolVar(&f.noVerifyAfter, "no-verify-after", false, "Skip re-validation after applying fixes")
}

func parseLevelOverrides(levels, ruleLevels, findingLevels []string) (*policy.File, error) {
	over := &policy.File{}
	set := func(flag, kind, value string, known map[string]string) (string, string, error) {
		key, level, ok := strings.Cut(value, "=")
		key, level = strings.TrimSpace(key), strings.TrimSpace(level)
		if !ok || key == "" || level == "" {
			return "", "", fmt.Errorf("%s must be KEY=LEVEL", flag)
		}
		if level != "error" && level != "warning" && level != "none" {
			return "", "", fmt.Errorf("%s level must be 'error', 'warning' or 'none', got %q", flag, level)
		}
		if _, ok := known[key]; !ok {
			return "", "", fmt.Errorf("%s has unknown %s %q", flag, kind, key)
		}
		return key, level, nil
	}

	concerns := make(map[string]string)
	for _, concern := range policy.Concerns() {
		concerns[concern] = "concern"
	}
	for _, value := range levels {
		key, level, err := set("--level", "concern", value, concerns)
		if err != nil {
			return nil, err
		}
		if over.Concerns == nil {
			over.Concerns = map[string]policy.Settings{}
		}
		over.Concerns[key] = policy.Settings{Level: level}
	}

	knownRules := map[string]string{}
	knownFindings := map[string]string{}
	for _, rule := range rules.All() {
		knownRules[rule.Name] = "rule"
		for _, finding := range rule.Findings {
			knownFindings[finding.ID] = rule.Name
		}
	}
	for _, value := range ruleLevels {
		key, level, err := set("--rule-level", "rule", value, knownRules)
		if err != nil {
			return nil, err
		}
		if over.Rules == nil {
			over.Rules = map[string]policy.Settings{}
		}
		setting := over.Rules[key]
		setting.Level = level
		over.Rules[key] = setting
	}
	for _, value := range findingLevels {
		key, level, err := set("--finding-level", "finding", value, knownFindings)
		if err != nil {
			return nil, err
		}
		rule := knownFindings[key]
		if over.Rules == nil {
			over.Rules = map[string]policy.Settings{}
		}
		setting := over.Rules[rule]
		if setting.Findings == nil {
			setting.Findings = map[string]policy.Settings{}
		}
		setting.Findings[key] = policy.Settings{Level: level}
		over.Rules[rule] = setting
	}
	over.MarkSource("flag")
	return over, nil
}

func validateFilters(f *flags) error {
	if f.failOn != "error" && f.failOn != "warning" && f.failOn != "never" {
		return fmt.Errorf("--fail-on must be error, warning or never")
	}
	knownRules := map[string]bool{}
	knownFindings := map[string]bool{}
	for _, rule := range rules.All() {
		knownRules[rule.Name] = true
		for _, finding := range rule.Findings {
			knownFindings[finding.ID] = true
		}
	}
	for _, name := range f.ruleFilter {
		if !knownRules[name] {
			return fmt.Errorf("--rule has unknown rule %q", name)
		}
	}
	for _, name := range f.skipRule {
		if !knownRules[name] {
			return fmt.Errorf("--skip-rule has unknown rule %q", name)
		}
	}
	for _, name := range f.findingFilter {
		if !knownFindings[name] {
			return fmt.Errorf("--finding has unknown finding %q", name)
		}
	}
	return nil
}

func filterIssues(st *state.State, f *flags) {
	includeRules := make(map[string]bool, len(f.ruleFilter))
	skipRules := make(map[string]bool, len(f.skipRule))
	includeFindings := make(map[string]bool, len(f.findingFilter))
	for _, rule := range f.ruleFilter {
		includeRules[rule] = true
	}
	for _, rule := range f.skipRule {
		skipRules[rule] = true
	}
	for _, finding := range f.findingFilter {
		includeFindings[finding] = true
	}
	issues := st.Issues[:0]
	for _, issue := range st.Issues {
		if len(includeRules) > 0 && !includeRules[issue.Rule] {
			continue
		}
		if skipRules[issue.Rule] {
			continue
		}
		if len(includeFindings) > 0 && !includeFindings[issue.Finding] {
			continue
		}
		issues = append(issues, issue)
	}
	st.Issues = issues
}

func usageError(log *output.Logger, msgs ...string) int {
	for _, m := range msgs {
		log.Infof("Error: %s", m)
	}
	return ExitUsage
}

// run holds the state shared by the pipeline steps.
type run struct {
	ctx      context.Context
	f        *flags
	env      Env
	log      *output.Logger
	jsonMode bool
	mode     remediation.Mode
	cfg      state.Config
	progress *progress.Reporter
	token    string
	// tokenSource is where go-gh found the token: an environment variable name, "oauth_token" or "gh".
	tokenSource string
}

func execute(ctx context.Context, f *flags, versions []string, env Env) int {
	if !f.changed["output"] {
		switch {
		case f.changed["json-fields"] || f.jq != "" || f.template != "":
			f.output = "json"
		case env.Getenv("GITHUB_ACTIONS") == "true":
			f.output = "actions"
		default:
			f.output = "console"
		}
	}
	jsonMode := f.output == "json"
	human := env.Stdout
	if jsonMode {
		human = env.Stderr
	}
	log := &output.Logger{
		Out:      human,
		DebugOut: env.Stderr,
		Verbose:  f.verbose,
		Quiet:    f.quiet,
		Actions:  f.output == "actions",
		Color:    !f.noColor && (env.IsTTY || term.IsColorForced()) && !term.IsColorDisabled(),
	}
	progressWriter := env.Stderr
	if f.output == "actions" {
		progressWriter = env.Stdout
	}
	r := &run{
		ctx: ctx, f: f, env: env, log: log, jsonMode: jsonMode,
		progress: progress.New(progress.Options{
			Writer: progressWriter, TTY: env.IsTTY, NoProgress: f.noProgress,
			Quiet: f.quiet, Actions: f.output == "actions",
		}),
	}

	// Flag validation.
	if f.output != "console" && f.output != "json" && f.output != "actions" {
		return usageError(log, "--output must be console, json or actions")
	}
	if f.groupBy != "version" && f.groupBy != "rule" && f.groupBy != "concern" {
		return usageError(log, "--group-by must be version, rule or concern")
	}
	if !jsonMode && (f.changed["json-fields"] || f.jq != "" || f.template != "") {
		return usageError(log, "--json-fields, --jq and --template require --output=json")
	}
	if f.jq != "" && f.template != "" {
		return usageError(log, "--jq and --template are mutually exclusive")
	}
	fields, err := parseJSONFields(f.jsonFields)
	if err != nil {
		return usageError(log, err.Error())
	}
	for _, v := range versions {
		if !version.IsValidPattern(v) {
			return usageError(log, fmt.Sprintf("invalid version argument %q (expected vX, vX.Y, vX.Y.Z or a wildcard like v1.*; use --json-fields to select JSON fields)", v))
		}
	}
	if f.fetchReleases && !f.local {
		return usageError(log, "--fetch-releases requires --local")
	}
	if f.maxWait < 0 {
		return usageError(log, "--max-wait must not be negative")
	}
	if f.noConfig && f.changed["config"] {
		return usageError(log, "--config and --no-config cannot be used together")
	}
	if f.changed["config"] && strings.TrimSpace(f.config) == "" {
		return usageError(log, "--config requires a path")
	}
	if err := validateFilters(f); err != nil {
		return usageError(log, err.Error())
	}
	if err := r.buildConfig(policy.Default()); err != nil {
		return usageError(log, err.Error())
	}

	// Machine output must never block on a prompt unless the user explicitly asked for one.
	explicitInteractive := f.interactive || f.mode == "interactive"
	tty := env.IsTTY && (!jsonMode || explicitInteractive)
	mode, err := options.ResolveMode(options.ModeFlags{
		Fix: f.fix, Mode: f.mode, Interactive: f.interactive, Auto: f.autoMode, Manual: f.manualMode,
		Yes: f.yes, DryRun: f.dryRun, IsTTY: tty,
	})
	if err != nil {
		return usageError(log, err.Error())
	}
	r.mode = mode
	r.cfg.AutoFix = mode == remediation.AutoFix || mode == remediation.Interactive

	var (
		st     *state.State
		client state.Client
		apiC   *api.Client
	)
	if f.local {
		st, client, apiC, err = r.loadLocal()
	} else {
		st, client, apiC, err = r.loadRemote()
	}
	if err != nil {
		return r.fail(err)
	}
	if f.verbose {
		options.WriteDebugInfo(r.cfg, log)
		options.WriteRepositoryDebugInfo(st, r.token != "", log)
	}

	registeredRules := rules.All()
	ruleStage := r.progress.Start(fmt.Sprintf("Running %d rules", len(registeredRules)))
	rules.Run(st, &st.Config, registeredRules)
	ruleStage.Done(fmt.Sprintf("%d/%d rules", len(registeredRules), len(registeredRules)))
	if len(versions) > 0 {
		scopeIssues(st, versions)
	}
	filterIssues(st, f)
	if len(st.Issues) == 0 && mode == remediation.ReportOnly && f.output != "console" {
		log.Notice("No issues found!")
	}

	var fixStage progress.Stage = progress.NoopStage{}
	if mode != remediation.ReportOnly {
		fixStage = r.progress.Start("Applying fixes")
	}
	opt := remediation.Options{Mode: mode, Client: client, Log: log, Progress: fixStage}
	if mode == remediation.Interactive {
		opt.Prompter = env.Prompter
		if opt.Prompter == nil {
			opt.Prompter = processPrompter(jsonMode)
		}
	}
	if err := remediation.Fix(ctx, st, opt); err != nil {
		fixStage.Done("failed")
		return r.fail(err)
	}
	if mode != remediation.ReportOnly {
		fixStage.Done(fmt.Sprintf("%d fixed, %d failed", st.FixedCount(), st.FailedCount()))
	}
	if !f.noVerifyAfter && (mode == remediation.AutoFix || mode == remediation.Interactive) {
		st, err = r.reverify(st, apiC, versions)
		if err != nil {
			return r.fail(err)
		}
	}
	var code int
	if f.output == "console" {
		code = remediation.ExitCodeWithFailOn(st, mode, f.failOn)
		consoleReport := report.Build(st, report.BuildOptions{Mode: mode.String(), DocsVersion: Version})
		if err := console.Write(env.Stdout, consoleReport, console.Options{
			GroupBy: f.groupBy, Quiet: f.quiet, Color: log.Color, Verbose: f.verbose,
		}); err != nil {
			return r.fail(err)
		}
	} else {
		code = remediation.FinishWithFailOn(st, log, mode, f.failOn)
	}
	if f.local {
		printPushCommands(st, log)
	}
	if apiC != nil {
		if ferr := apiC.Fatal(); ferr != nil {
			return r.fail(ferr)
		}
	}

	if f.output == "actions" && env.Getenv("GITHUB_ACTIONS") == "true" {
		remediation.WriteManualInstructionsToStepSummary(st)
		writeStepSummary(env, st, mode, code)
	}
	if jsonMode {
		if err := writeJSON(env, f, fields, buildReport(st, r, code)); err != nil {
			return usageError(log, err.Error())
		}
	}
	return code
}

func (r *run) reverify(previous *state.State, apiC *api.Client, versions []string) (*state.State, error) {
	var verified *state.State
	if r.f.local {
		dir := r.env.Dir
		if dir == "" {
			dir = "."
		}
		var warnings []string
		var err error
		verified, warnings, err = localgit.Load(r.ctx, dir, r.cfg)
		if err != nil {
			return nil, err
		}
		for _, warning := range warnings {
			r.log.Warning(warning)
		}
		if r.f.fetchReleases {
			if apiC == nil {
				return nil, errors.New("cannot re-verify local releases without a GitHub client")
			}
			verified.Releases, err = apiC.GetReleases(r.ctx)
			if err != nil {
				return nil, err
			}
			verified.Owner, verified.Repo, verified.Host = apiC.Repo.Owner, apiC.Repo.Name, apiC.Repo.Host
		}
	} else {
		if apiC == nil {
			return nil, errors.New("cannot re-verify without a GitHub client")
		}
		var err error
		verified, err = api.Load(r.ctx, apiC, r.cfg)
		if err != nil {
			return nil, err
		}
	}

	rules.Run(verified, &verified.Config, nil)
	if len(versions) > 0 {
		scopeIssues(verified, versions)
	}
	filterIssues(verified, r.f)
	reconcileVerifiedIssues(previous, verified)
	return verified, nil
}

type issueIdentity struct {
	rule, finding, version, currentSHA, expectedSHA string
}

func reconcileVerifiedIssues(previous, verified *state.State) {
	byIdentity := map[issueIdentity][]*state.Issue{}
	for _, issue := range previous.Issues {
		key := issueIdentity{issue.Rule, issue.Finding, issue.Version, issue.CurrentSha, issue.ExpectedSha}
		byIdentity[key] = append(byIdentity[key], issue)
	}
	matched := map[*state.Issue]bool{}
	for _, issue := range verified.Issues {
		key := issueIdentity{issue.Rule, issue.Finding, issue.Version, issue.CurrentSha, issue.ExpectedSha}
		previousIssues := byIdentity[key]
		if len(previousIssues) == 0 {
			continue
		}
		old := previousIssues[0]
		byIdentity[key] = previousIssues[1:]
		matched[old] = true
		issue.Status = old.Status
		switch old.Status {
		case state.StatusFixed:
			issue.Status = state.StatusFailed
			issue.Message += " (still present after applying the fix)"
		case state.StatusManualFixRequired, state.StatusUnfixable:
			issue.Message = old.Message
		}
	}
	for _, issue := range previous.Issues {
		if issue.Status == state.StatusFixed && !matched[issue] {
			verified.AddIssue(issue)
		}
	}
}

// fail reports an operational error (authentication, rate limit, API failure) with exit code 2.
func (r *run) fail(err error) int {
	var ae *api.AuthError
	var rateLimit *api.RateLimitError
	switch {
	case errors.Is(err, errAlreadyReported):
	case errors.As(err, &ae):
		r.log.Error(ae.Message)
		if ae.Refresh != "" {
			r.log.Infof("Run: %s", ae.Refresh)
		}
	case api.IsRateLimit(err):
		if errors.As(err, &rateLimit) && rateLimit.MaxWait > 0 && rateLimit.Wait > rateLimit.MaxWait {
			r.log.Error(fmt.Sprintf("Rate limited; required wait %s exceeds --max-wait %s",
				rateLimit.Wait.Round(time.Second), rateLimit.MaxWait.Round(time.Second)))
		} else {
			r.log.Error("GitHub API rate limit: " + err.Error())
		}
		r.log.Info("Wait for the limit to reset, use a token with a higher limit, or raise --max-wait.")
	default:
		r.log.Error(err.Error())
	}
	return ExitUsage
}

func (r *run) resolveRepo() (api.Repo, error) {
	switch {
	case r.f.repo != "":
		return r.env.ResolveRepo(r.f.repo)
	case r.env.Getenv("GH_REPO") != "":
		return r.env.ResolveRepo(r.env.Getenv("GH_REPO"))
	case r.env.Getenv("GITHUB_REPOSITORY") != "":
		return repoFromEnv(r.env.Getenv("GITHUB_REPOSITORY"), r.env.Getenv)
	}
	repo, err := r.env.ResolveRepo("")
	if err != nil {
		return repo, fmt.Errorf("could not determine the repository: %w (use -R OWNER/REPO or set GITHUB_REPOSITORY)", err)
	}
	return repo, nil
}

func (r *run) newClient(repo api.Repo) (*api.Client, error) {
	token, source := r.env.Token(repo.Host)
	r.token, r.tokenSource = token, source
	r.log.Mask(token)
	o := api.Options{
		Repo: repo, Token: token, IgnoreVersions: r.cfg.IgnoreVersions,
		Transport: api.TransportOptions{
			MaxWait: r.f.maxWait, Warn: r.log.Warning,
			OnWait: func(_ api.RateLimitKind, delay time.Duration) {
				message := fmt.Sprintf("Rate limited, resuming in %s (limit: --max-wait %s)",
					delay.Round(time.Second), r.f.maxWait.Round(time.Second))
				r.progress.Message(message)
			},
		},
		Debug:    r.log.Debug,
		Progress: r.progress,
	}
	if s := r.env.Getenv("GITHUB_SERVER_URL"); s != "" {
		o.ServerURL = strings.TrimRight(s, "/")
	}
	if r.env.ClientOptions != nil {
		r.env.ClientOptions(&o)
	}
	return api.New(o)
}

// connect creates the client and verifies the token before any API work. Missing scopes are
// reported with a `gh auth refresh` suggestion; on a terminal in interactive mode the refresh is
// offered and run for the user.
func (r *run) connect(repo api.Repo, authMode api.Mode) (*api.Client, error) {
	for attempt := 0; ; attempt++ {
		c, err := r.newClient(repo)
		if err != nil {
			return nil, err
		}
		res, err := api.CheckAuth(r.ctx, c, authMode)
		if err != nil {
			return nil, err
		}
		if res.Anonymous {
			r.log.Warning("No credentials found: anonymous API access is limited to 60 requests per hour (run `gh auth login` or set GH_TOKEN)")
		}
		if res.OK() {
			if opt := res.OptionalMissing(); len(opt) > 0 {
				r.log.Warning("Some features are limited:\n" + res.Message())
			}
			return c, nil
		}
		r.log.Error("The token cannot perform the requested operation on " + repo.String() + ":")
		r.log.Info(res.Message())
		if attempt > 0 || !r.canOfferRefresh(res) {
			return nil, errAlreadyReported
		}
		var scopes []string
		for _, m := range res.Missing {
			if m.Refreshable {
				scopes = append(scopes, m.Scope)
			}
		}
		p := r.env.Prompter
		if p == nil {
			p = processPrompter(r.jsonMode)
		}
		ok, perr := p.Confirm(fmt.Sprintf("Run `%s` now?", res.RefreshCommand), true)
		if perr != nil || !ok {
			return nil, errAlreadyReported
		}
		if err := r.env.RefreshAuth(r.ctx, res.Host, scopes); err != nil {
			r.log.Error("gh auth refresh failed: " + err.Error())
			return nil, errAlreadyReported
		}
	}
}

var errAlreadyReported = errors.New("authentication check failed")

func (r *run) canOfferRefresh(res *api.AuthResult) bool {
	if !r.env.IsTTY || r.mode != remediation.Interactive || r.env.Getenv("GITHUB_ACTIONS") == "true" {
		return false
	}
	if res.TokenKind != api.TokenClassic || !strings.HasPrefix(res.RefreshCommand, "gh auth refresh") {
		return false
	}
	// Only tokens stored by gh (config file or keyring) are changed by `gh auth refresh`.
	return r.tokenSource == "oauth_token" || r.tokenSource == "gh"
}

func (r *run) authMode() api.Mode {
	if r.mode == remediation.AutoFix || r.mode == remediation.Interactive {
		return api.ModeFix
	}
	return api.ModeCheck
}

// buildConfig resolves the effective configuration: the policy (built-in defaults plus the
// repository's action-version.yml) overridden by the flags given on the command line.
func (r *run) buildConfig(p *policy.File) error {
	f := r.f
	cfg := options.DefaultConfig()
	cfg.AutoFix = r.cfg.AutoFix
	cfg.AllowDestructiveTagDeletion = f.allowTagDeletion
	overrides, err := parseLevelOverrides(f.levels, f.ruleLevels, f.findingLevels)
	if err != nil {
		return err
	}
	p = policy.Merge(p, overrides)
	pin := policy.Pinned{}
	if pin.IgnorePreviewReleases = f.changed["preview-releases"]; pin.IgnorePreviewReleases {
		switch strings.ToLower(strings.TrimSpace(f.previewReleases)) {
		case "included":
			cfg.IgnorePreviewReleases = false
		case "excluded":
			cfg.IgnorePreviewReleases = true
		default:
			return fmt.Errorf("--preview-releases must be either 'included' or 'excluded'")
		}
	}
	if pin.FloatingVersionsUse = f.changed["floating-versions-use"]; pin.FloatingVersionsUse {
		cfg.FloatingVersionsUse = strings.ToLower(strings.TrimSpace(f.floatingUse))
	}
	if pin.IgnoreVersions = f.changed["ignore-versions"]; pin.IgnoreVersions {
		cfg.IgnoreVersions = options.ParseIgnoreVersions(f.ignoreVersions, r.log)
	}
	p.Apply(&cfg, pin)
	if cfg.Sources == nil {
		cfg.Sources = map[string]string{}
	}
	cfg.Sources["allow-destructive-tag-deletion"] = "default"
	if f.allowTagDeletion {
		cfg.Sources["allow-destructive-tag-deletion"] = "flag"
	}
	if errs := options.Validate(cfg); len(errs) > 0 {
		return errors.New(strings.Join(errs, "\n"))
	}
	r.cfg = cfg
	return nil
}

// loadPolicy reads the repository's action-version.yml through read and rebuilds the configuration.
func (r *run) loadPolicy(read policy.Reader) error {
	var (
		p    *policy.File
		path string
		err  error
	)
	switch {
	case r.f.noConfig:
		p = policy.Default()
	case r.f.changed["config"]:
		p, path, err = policy.LoadFrom(r.ctx, read, r.f.config)
	default:
		p, path, err = policy.Load(r.ctx, read)
	}
	if err != nil {
		return err
	}
	if path != "" {
		r.log.Infof("Using the version policy from %s", path)
	} else if r.f.noConfig {
		r.log.Infof("Ignoring repository policy files")
	}
	return r.buildConfig(p)
}

func (r *run) loadRemote() (*state.State, state.Client, *api.Client, error) {
	resolveStage := r.progress.Start("Resolve repository, token and policy")
	resolveDone := false
	defer func() {
		if !resolveDone {
			resolveStage.Done("failed")
		}
	}()
	repo, err := r.resolveRepo()
	if err != nil {
		return nil, nil, nil, err
	}
	c, err := r.connect(repo, r.authMode())
	if err != nil {
		return nil, nil, nil, err
	}
	if (r.mode == remediation.AutoFix || r.mode == remediation.Interactive) && r.token == "" {
		return nil, nil, nil, errors.New("fixing requires a GitHub token: run `gh auth login` or set GH_TOKEN/GITHUB_TOKEN, or use `verify`")
	}
	if err := r.loadPolicy(func(ctx context.Context, path string) (string, bool, error) {
		return c.GetFileContents(ctx, path, "")
	}); err != nil {
		return nil, nil, nil, err
	}
	resolveStage.Done("repository resolved and policy loaded")
	resolveDone = true
	st, err := api.Load(r.ctx, c, r.cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	return st, c, c, nil
}

func (r *run) loadLocal() (*state.State, state.Client, *api.Client, error) {
	dir := r.env.Dir
	if dir == "" {
		dir = "."
	}
	policyStage := r.progress.Start("Load repository policy")
	policyDone := false
	defer func() {
		if !policyDone {
			policyStage.Done("failed")
		}
	}()
	if err := r.loadPolicy(func(_ context.Context, path string) (string, bool, error) {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		return string(b), err == nil, err
	}); err != nil {
		return nil, nil, nil, err
	}
	policyStage.Done("policy loaded")
	policyDone = true
	localStage := r.progress.Start("Loading local refs")
	st, warnings, err := localgit.Load(r.ctx, dir, r.cfg)
	if err != nil {
		localStage.Done("failed")
		return nil, nil, nil, err
	}
	for _, w := range warnings {
		r.log.Warning(w)
	}
	localStage.Done(fmt.Sprintf("%d tags, %d branches", len(st.Tags), len(st.Branches)))
	var apiC *api.Client
	if r.f.fetchReleases {
		repo := api.Repo{Host: st.Host, Owner: st.Owner, Name: st.Repo}
		if r.f.repo != "" || repo.Owner == "" {
			if repo, err = r.resolveRepo(); err != nil {
				return nil, nil, nil, err
			}
		}
		if repo.Host == "" {
			repo.Host = "github.com"
		}
		if apiC, err = r.connect(repo, api.ModeCheck); err != nil {
			return nil, nil, nil, err
		}
		if st.Releases, err = apiC.GetReleases(r.ctx); err != nil {
			return nil, nil, nil, err
		}
		st.Owner, st.Repo, st.Host = repo.Owner, repo.Name, repo.Host
		st.Config.CheckReleases, st.Config.CheckImmutability = r.cfg.CheckReleases, r.cfg.CheckImmutability
	}
	return st, localgit.New(dir), apiC, nil
}

// scopeIssues keeps the issues of the requested versions. A plain version also covers its
// children (v1 covers v1.0 and v1.0.0); wildcard patterns match as documented for ignore-versions.
func scopeIssues(st *state.State, versions []string) {
	var kept []*state.Issue
	for _, i := range st.Issues {
		for _, p := range versions {
			if version.Matches(i.Version, p) || (!strings.Contains(p, "*") && strings.HasPrefix(i.Version, p+".")) {
				kept = append(kept, i)
				break
			}
		}
	}
	st.Issues = kept
}

// printPushCommands lists the git push commands needed to publish locally applied fixes.
func printPushCommands(st *state.State, log *output.Logger) {
	var lines []string
	for _, i := range st.Issues {
		if i.Status != state.StatusFixed || i.Action == nil {
			continue
		}
		for _, c := range i.Action.ManualCommands(st) {
			if strings.HasPrefix(c, "git push") {
				lines = append(lines, c)
			}
		}
	}
	if len(lines) == 0 {
		return
	}
	log.Info("")
	log.Group("Local fixes applied; review and push them yourself")
	log.SafeLines(lines)
	log.EndGroup()
}
