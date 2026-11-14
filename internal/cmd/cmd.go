// Package cmd is the `gh version-action` command line: flag parsing, mode selection, startup auth
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

	"github.com/jessehouwing/gh-version-action/internal/api"
	"github.com/jessehouwing/gh-version-action/internal/localgit"
	"github.com/jessehouwing/gh-version-action/internal/options"
	"github.com/jessehouwing/gh-version-action/internal/output"
	"github.com/jessehouwing/gh-version-action/internal/policy"
	"github.com/jessehouwing/gh-version-action/internal/remediation"
	"github.com/jessehouwing/gh-version-action/internal/rules" // importing registers the validation rules
	"github.com/jessehouwing/gh-version-action/internal/state"
	"github.com/jessehouwing/gh-version-action/internal/version"
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
	repo                                               string
	check, noFix, autoFix, interactive, yes            bool
	local, fetchReleases                               bool
	checkMinor, checkReleases, checkImmut, checkMarket string
	includePreview                                     bool
	allowTagDeletion                                   bool
	floatingUse                                        string
	ignoreVersions                                     string
	maxWait                                            time.Duration
	json                                               []string
	jq, template                                       string
	jsonSet                                            bool
	changed                                            map[string]bool // flags given on the command line
}

// Run executes the command line (args exclude the program name) and returns the exit code.
func Run(ctx context.Context, args []string, env Env) int {
	env = env.withDefaults()
	var f flags
	code := ExitOK
	root := &cobra.Command{
		Use:   "version-action [versions...]",
		Short: "Validate and fix the version tags, branches and releases of an action repository",
		Long: `Validate the semantic version tags, branches and releases of a GitHub Action repository
against GitHub's immutable release strategy: patch versions (v1.0.0) need an immutable release,
floating versions (v1, v1.0, latest) must point at the latest compatible patch.

Without flags the command is interactive on a terminal and report-only elsewhere.
Optional version arguments (v1, v1.0, v1.*) limit the report and fixes to those versions.

Exit codes: 0 ok, 1 validation errors remaining, 2 usage, authentication or rate limit errors.`,
		Version:           Version,
		SilenceUsage:      true,
		SilenceErrors:     true,
		Args:              cobra.ArbitraryArgs,
		DisableAutoGenTag: true,
		Run: func(c *cobra.Command, versions []string) {
			f.jsonSet = c.Flags().Changed("json")
			f.changed = map[string]bool{}
			c.Flags().Visit(func(fl *pflag.Flag) { f.changed[fl.Name] = true })
			code = execute(ctx, &f, versions, env)
		},
	}
	root.SetOut(env.Stdout)
	root.SetErr(env.Stderr)
	root.SetIn(env.Stdin)
	root.SetArgs(args)

	fl := root.Flags()
	fl.StringVarP(&f.repo, "repo", "R", "", "Target repository as [HOST/]OWNER/REPO (default: GH_REPO, GITHUB_REPOSITORY or the git remote)")
	fl.BoolVar(&f.check, "check", false, "Report only, never write; exit 1 when errors are found")
	fl.BoolVar(&f.noFix, "no-fix", false, "Alias of --check")
	fl.BoolVar(&f.autoFix, "auto-fix", false, "Apply every fixable remediation without prompting")
	fl.BoolVarP(&f.interactive, "interactive", "i", false, "Review and apply fixes one by one (requires a terminal)")
	fl.BoolVar(&f.yes, "yes", false, "Answer yes to every prompt (interactive becomes auto-fix)")
	fl.BoolVar(&f.local, "local", false, "Read and fix refs in the local git repository; never pushes")
	fl.BoolVar(&f.local, "local-only", false, "Alias of --local")
	fl.BoolVar(&f.fetchReleases, "fetch-releases", false, "With --local: also read releases from GitHub (read-only)")
	fl.StringVar(&f.checkMinor, "check-minor-version", "error", "Minor version (v1.0) validation: error, warning or none")
	fl.StringVar(&f.checkReleases, "check-releases", "error", "Require a release for every patch version: error, warning or none")
	fl.StringVar(&f.checkImmut, "check-release-immutability", "error", "Require releases to be published and immutable: error, warning or none")
	fl.StringVar(&f.checkMarket, "check-marketplace", "error", "Validate Marketplace metadata: error, warning or none")
	fl.BoolVar(&f.includePreview, "include-preview-releases", false, "Include prereleases (from the Releases API) when picking the latest patch")
	fl.BoolVar(&f.allowTagDeletion, "dangerously-allow-destructive-tag-deletion", false, "Allow deleting a tag whose immutable release was deleted; this cannot be undone")
	fl.StringVar(&f.floatingUse, "floating-versions-use", "tags", "Use tags or branches for floating versions")
	fl.StringVar(&f.ignoreVersions, "ignore-versions", "", "Comma separated versions to skip; wildcards like v1.* are supported")
	fl.DurationVar(&f.maxWait, "max-wait", 60*time.Second, "Longest rate limit wait tolerated before failing with exit code 2")
	fl.StringSliceVar(&f.json, "json", nil, "Print machine-readable JSON to stdout (fields: "+strings.Join(jsonFields, ", ")+"); human logs go to stderr")
	fl.Lookup("json").NoOptDefVal = "all"
	fl.StringVarP(&f.jq, "jq", "q", "", "Filter the JSON output with a jq expression")
	fl.StringVarP(&f.template, "template", "t", "", "Format the JSON output with a Go template")

	if err := root.Execute(); err != nil {
		fmt.Fprintln(env.Stderr, "Error:", err)
		return ExitUsage
	}
	return code
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
	token    string
	// tokenSource is where go-gh found the token: an environment variable name, "oauth_token" or "gh".
	tokenSource string
}

func execute(ctx context.Context, f *flags, versions []string, env Env) int {
	jsonMode := f.jsonSet || f.jq != "" || f.template != ""
	human := env.Stdout
	if jsonMode {
		human = env.Stderr
	}
	log := &output.Logger{
		Out:     human,
		Actions: env.Getenv("GITHUB_ACTIONS") == "true",
		Color:   (env.IsTTY || term.IsColorForced()) && !term.IsColorDisabled(),
	}
	r := &run{ctx: ctx, f: f, env: env, log: log, jsonMode: jsonMode}

	// Flag validation.
	if f.jq != "" && f.template != "" {
		return usageError(log, "--jq and --template are mutually exclusive")
	}
	fields, err := parseJSONFields(f.json, f.jsonSet)
	if err != nil {
		return usageError(log, err.Error())
	}
	for _, v := range versions {
		if !version.IsValidPattern(v) {
			return usageError(log, fmt.Sprintf("invalid version argument %q (expected vX, vX.Y, vX.Y.Z or a wildcard like v1.*; use --json=FIELDS to select JSON fields)", v))
		}
	}
	if f.fetchReleases && !f.local {
		return usageError(log, "--fetch-releases requires --local")
	}
	if f.maxWait < 0 {
		return usageError(log, "--max-wait must not be negative")
	}
	if err := r.buildConfig(policy.Default()); err != nil {
		return usageError(log, err.Error())
	}

	// Machine output must never block on a prompt unless the user explicitly asked for one.
	tty := env.IsTTY && (!jsonMode || f.interactive)
	mode, err := options.ResolveMode(options.ModeFlags{
		AutoFix: f.autoFix, Check: f.check || f.noFix, Interactive: f.interactive, Yes: f.yes, IsTTY: tty,
	})
	if err != nil {
		return usageError(log, err.Error())
	}
	r.mode = mode
	r.cfg.AutoFix = mode != remediation.ReportOnly

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

	rules.Run(st, &st.Config, nil)
	if len(versions) > 0 {
		scopeIssues(st, versions)
	}
	if len(st.Issues) == 0 && mode == remediation.ReportOnly {
		log.Notice("No issues found!")
	}

	opt := remediation.Options{Mode: mode, Client: client, Log: log}
	if mode == remediation.Interactive {
		opt.Prompter = env.Prompter
		if opt.Prompter == nil {
			opt.Prompter = processPrompter(jsonMode)
		}
	}
	if err := remediation.Fix(ctx, st, opt); err != nil {
		return r.fail(err)
	}
	code := remediation.Finish(st, log, mode)
	if f.local {
		printPushCommands(st, log)
	}
	if apiC != nil {
		if ferr := apiC.Fatal(); ferr != nil {
			return r.fail(ferr)
		}
	}

	if env.Getenv("GITHUB_ACTIONS") == "true" {
		writeStepSummary(env, st, mode, code)
	}
	if jsonMode {
		if err := writeJSON(env, f, fields, buildReport(st, r, code)); err != nil {
			return usageError(log, err.Error())
		}
	}
	return code
}

// fail reports an operational error (authentication, rate limit, API failure) with exit code 2.
func (r *run) fail(err error) int {
	var ae *api.AuthError
	switch {
	case errors.Is(err, errAlreadyReported):
	case errors.As(err, &ae):
		r.log.Error(ae.Message)
		if ae.Refresh != "" {
			r.log.Infof("Run: %s", ae.Refresh)
		}
	case api.IsRateLimit(err):
		r.log.Error("GitHub API rate limit: " + err.Error())
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
		Transport: api.TransportOptions{MaxWait: r.f.maxWait, Warn: r.log.Warning},
		Debug:     r.log.Debug,
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
	if r.mode == remediation.ReportOnly {
		return api.ModeCheck
	}
	return api.ModeFix
}

// buildConfig resolves the effective configuration: the policy (built-in defaults plus the
// repository's action-version.yml) overridden by the flags given on the command line.
func (r *run) buildConfig(p *policy.File) error {
	f := r.f
	cfg := options.DefaultConfig()
	cfg.AutoFix = r.cfg.AutoFix
	cfg.AllowDestructiveTagDeletion = f.allowTagDeletion
	pin := policy.Pinned{Levels: map[string]bool{}}
	for flag, set := range map[string]struct {
		concern string
		value   string
		field   *string
	}{
		"check-minor-version":        {"minor", f.checkMinor, &cfg.CheckMinorVersion},
		"check-releases":             {"releases", f.checkReleases, &cfg.CheckReleases},
		"check-release-immutability": {"immutability", f.checkImmut, &cfg.CheckImmutability},
		"check-marketplace":          {"marketplace", f.checkMarket, &cfg.CheckMarketplace},
	} {
		if f.changed[flag] {
			*set.field = options.ConvertToCheckLevel(set.value)
			pin.Levels[set.concern] = true
		}
	}
	if pin.IgnorePreviewReleases = f.changed["include-preview-releases"]; pin.IgnorePreviewReleases {
		cfg.IgnorePreviewReleases = !f.includePreview
	}
	if pin.FloatingVersionsUse = f.changed["floating-versions-use"]; pin.FloatingVersionsUse {
		cfg.FloatingVersionsUse = strings.ToLower(strings.TrimSpace(f.floatingUse))
	}
	if pin.IgnoreVersions = f.changed["ignore-versions"]; pin.IgnoreVersions {
		cfg.IgnoreVersions = options.ParseIgnoreVersions(f.ignoreVersions, r.log)
	}
	p.Apply(&cfg, pin)
	if errs := options.Validate(cfg); len(errs) > 0 {
		return errors.New(strings.Join(errs, "\n"))
	}
	r.cfg = cfg
	return nil
}

// loadPolicy reads the repository's action-version.yml through read and rebuilds the configuration.
func (r *run) loadPolicy(read policy.Reader) error {
	p, path, err := policy.Load(r.ctx, read)
	if err != nil {
		return err
	}
	if path != "" {
		r.log.Infof("Using the version policy from %s", path)
	}
	return r.buildConfig(p)
}

func (r *run) loadRemote() (*state.State, state.Client, *api.Client, error) {
	repo, err := r.resolveRepo()
	if err != nil {
		return nil, nil, nil, err
	}
	c, err := r.connect(repo, r.authMode())
	if err != nil {
		return nil, nil, nil, err
	}
	if r.mode != remediation.ReportOnly && r.token == "" {
		return nil, nil, nil, errors.New("fixing requires a GitHub token: run `gh auth login` or set GH_TOKEN/GITHUB_TOKEN, or use --check")
	}
	if err := r.loadPolicy(func(ctx context.Context, path string) (string, bool, error) {
		return c.GetFileContents(ctx, path, "")
	}); err != nil {
		return nil, nil, nil, err
	}
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
	if err := r.loadPolicy(func(_ context.Context, path string) (string, bool, error) {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		return string(b), err == nil, err
	}); err != nil {
		return nil, nil, nil, err
	}
	st, warnings, err := localgit.Load(r.ctx, dir, r.cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, w := range warnings {
		r.log.Warning(w)
	}
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
