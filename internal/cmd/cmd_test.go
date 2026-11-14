package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jessehouwing/gh-actions-version/internal/api"
	"github.com/jessehouwing/gh-actions-version/internal/testutil"
)

type result struct {
	code     int
	out, err string
}

func (r result) json(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(r.out), &m); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, r.out)
	}
	return m
}

func (r result) all() string { return r.out + r.err }

type fakePrompter struct {
	picks   []int // Select answers in order; missing answers pick Quit
	confirm bool
	asked   []string
}

func (p *fakePrompter) Select(msg string, opts []string) (int, error) {
	p.asked = append(p.asked, msg)
	if len(p.picks) == 0 {
		return len(opts) - 1, nil
	}
	i := p.picks[0]
	p.picks = p.picks[1:]
	return i, nil
}

func (p *fakePrompter) Confirm(msg string, def bool) (bool, error) {
	p.asked = append(p.asked, msg)
	return p.confirm, nil
}

// testEnv builds an Env wired to the fake server with a fixed process environment.
type testEnv struct {
	gh       *testutil.GitHub
	vars     map[string]string
	tty      bool
	prompter Prompter
	dir      string
	cfgToken string // token known to gh's config (returned when no variable is set)
	refresh  func(ctx context.Context, host string, scopes []string) error
	stdout   strings.Builder
	stderr   strings.Builder
}

func newEnv(t *testing.T, gh *testutil.GitHub) *testEnv {
	t.Setenv("GITHUB_STEP_SUMMARY", "")
	return &testEnv{gh: gh, vars: map[string]string{"GH_TOKEN": "tok"}}
}

func (e *testEnv) run(args ...string) result {
	command := ""
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		switch arg {
		case "verify", "fix":
			command = arg
		case "--check", "--no-fix":
			if command == "" {
				command = "verify"
			}
		case "--auto-fix":
			if command == "" {
				command = "fix"
			}
		case "--interactive", "-i", "--yes":
			if command == "" {
				command = "fix"
			}
			filtered = append(filtered, arg)
		default:
			filtered = append(filtered, arg)
		}
	}
	if command == "" {
		if e.tty {
			command = "fix"
		} else {
			command = "verify"
		}
	}
	return e.runRaw(append([]string{command}, filtered...)...)
}

func (e *testEnv) runRaw(args ...string) result {
	env := Env{
		Stdout: &e.stdout, Stderr: &e.stderr, IsTTY: e.tty, Dir: e.dir, Prompter: e.prompter,
		Getenv: func(k string) string { return e.vars[k] },
		Token: func(string) (string, string) {
			for _, k := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
				if v := e.vars[k]; v != "" {
					return v, k
				}
			}
			return e.cfgToken, "oauth_token"
		},
		ResolveRepo: func(flag string) (api.Repo, error) {
			owner, name, ok := strings.Cut(flag, "/")
			if flag == "" || !ok || owner == "" || name == "" {
				return api.Repo{}, errors.New("expected the [HOST/]OWNER/REPO format")
			}
			return api.Repo{Host: "github.com", Owner: owner, Name: name}, nil
		},
		RefreshAuth: e.refresh,
	}
	if e.gh != nil {
		env.ClientOptions = func(o *api.Options) {
			o.Transport.Base = e.gh.Transport()
			o.Transport.WritePacing = -1
			o.Transport.Sleep = func(context.Context, time.Duration) error { return nil }
			o.ImmutablePollDelay = time.Millisecond
		}
	}
	code := Run(context.Background(), args, env)
	return result{code, e.stdout.String(), e.stderr.String()}
}

// Common flags: validate tags only unless a test asks for releases or marketplace metadata.
var tagsOnly = []string{"-R", "o/r", "--check-releases=none", "--check-release-immutability=none", "--check-marketplace=none"}

func args(extra ...string) []string { return append(append([]string(nil), tagsOnly...), extra...) }

func commandArgs(command string, args ...string) []string {
	return append([]string{command}, args...)
}

var c1, c2 = testutil.SHA('1'), testutil.SHA('2')

// ghWithPatch: v1.0.0 exists; v1 and v1.0 are missing.
func ghWithPatch(t *testing.T) *testutil.GitHub {
	return testutil.NewGitHub(t).Tag(c1, "v1.0.0")
}

func TestParameterValidation(t *testing.T) {
	t.Run("Should accept the documented check levels", func(t *testing.T) {
		for _, flag := range []string{"--check-minor-version", "--check-releases", "--check-release-immutability", "--check-marketplace"} {
			for _, v := range []string{"error", "warning", "none", "true", "false"} {
				gh := testutil.NewGitHub(t).MarketplaceFiles()
				r := newEnv(t, gh).run("-R", "o/r", "--check", flag+"="+v)
				if r.code != 0 && r.code != 1 {
					t.Errorf("%s=%s: exit %d\n%s", flag, v, r.code, r.all())
				}
			}
		}
	})
	t.Run("Should reject invalid check levels with exit code 2", func(t *testing.T) {
		for _, flag := range []string{"--check-minor-version", "--check-releases", "--check-release-immutability", "--check-marketplace"} {
			r := newEnv(t, testutil.NewGitHub(t)).run("-R", "o/r", flag+"=sometimes")
			if r.code != 2 || !strings.Contains(r.all(), flag[2:]+" must be") {
				t.Errorf("%s: exit %d\n%s", flag, r.code, r.all())
			}
		}
	})
	t.Run("Should error on invalid floating-versions-use value", func(t *testing.T) {
		r := newEnv(t, testutil.NewGitHub(t)).run(args("--floating-versions-use=both")...)
		if r.code != 2 || !strings.Contains(r.all(), "floating-versions-use must be either 'tags' or 'branches'") {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
	})
	t.Run("Should expose every flag in --help", func(t *testing.T) {
		r := newEnv(t, nil).run("--help")
		if r.code != 0 {
			t.Fatalf("exit %d", r.code)
		}
		for _, f := range []string{"--repo", "--interactive", "--yes", "--local", "--local-only",
			"--fetch-releases", "--check-minor-version", "--check-releases", "--check-release-immutability", "--check-marketplace",
			"--include-preview-releases", "--dangerously-allow-destructive-tag-deletion", "--floating-versions-use", "--ignore-versions", "--max-wait", "--json", "--jq", "--template"} {
			if !strings.Contains(r.out, f) {
				t.Errorf("--help does not list %s", f)
			}
		}
	})
	t.Run("Should reject unknown flags and invalid version arguments with exit code 2", func(t *testing.T) {
		if r := newEnv(t, nil).run("--bogus"); r.code != 2 {
			t.Errorf("unknown flag: exit %d", r.code)
		}
		r := newEnv(t, testutil.NewGitHub(t)).run(args("not-a-version")...)
		if r.code != 2 || !strings.Contains(r.all(), "invalid version argument") {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
	})
	t.Run("Should reject a negative --max-wait", func(t *testing.T) {
		if r := newEnv(t, testutil.NewGitHub(t)).run(args("--max-wait=-1s")...); r.code != 2 {
			t.Errorf("exit %d", r.code)
		}
	})
}

func TestSubcommands(t *testing.T) {
	t.Run("bare invocation prints help and exits 2", func(t *testing.T) {
		r := newEnv(t, nil).runRaw()
		if r.code != ExitUsage || !strings.Contains(r.out, "Usage:") || !strings.Contains(r.out, "verify") || !strings.Contains(r.out, "fix") {
			t.Errorf("exit %d\n%s", r.code, r.out)
		}
	})
	t.Run("verify is read-only", func(t *testing.T) {
		gh := ghWithPatch(t)
		r := newEnv(t, gh).run(commandArgs("verify", args()...)...)
		if r.code != ExitValidation || len(gh.Writes()) != 0 {
			t.Errorf("exit %d writes %v\n%s", r.code, gh.Writes(), r.all())
		}
	})
	t.Run("fix applies remediations by default", func(t *testing.T) {
		gh := ghWithPatch(t)
		r := newEnv(t, gh).run("fix", "-R", "o/r", "--check-releases=none", "--check-release-immutability=none", "--check-marketplace=none")
		if r.code != ExitOK || gh.TagSha("v1") != c1 || gh.TagSha("v1.0") != c1 {
			t.Errorf("exit %d tags %v\n%s", r.code, gh.Tags(), r.all())
		}
	})
	t.Run("legacy mode flags are not accepted", func(t *testing.T) {
		for _, args := range [][]string{{"verify", "--check"}, {"verify", "--no-fix"}, {"fix", "--auto-fix"}} {
			if r := newEnv(t, nil).runRaw(args...); r.code != ExitUsage {
				t.Errorf("%v: exit %d\n%s", args, r.code, r.all())
			}
		}
	})
}

func TestRepositoryParameterHandling(t *testing.T) {
	t.Run("Should return error when Repository is not provided and GITHUB_REPOSITORY is not set", func(t *testing.T) {
		r := newEnv(t, testutil.NewGitHub(t)).run("--check", "--check-marketplace=none")
		if r.code != 2 || !strings.Contains(r.all(), "could not determine the repository") {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
	})
	t.Run("Should use GITHUB_REPOSITORY environment variable when Repository not provided", func(t *testing.T) {
		e := newEnv(t, testutil.NewGitHub(t))
		e.vars["GITHUB_REPOSITORY"] = "test-owner/test-repo"
		r := e.run("--check", "--check-marketplace=none", "--json=repository")
		if r.code != 0 {
			t.Fatalf("exit %d\n%s", r.code, r.all())
		}
		repo := r.json(t)["repository"].(map[string]any)
		if repo["nameWithOwner"] != "test-owner/test-repo" || repo["host"] != "github.com" {
			t.Errorf("repository = %v", repo)
		}
	})
	t.Run("Should return error for invalid repository format", func(t *testing.T) {
		r := newEnv(t, testutil.NewGitHub(t)).run("-R", "invalid-format", "--check")
		if r.code != 2 {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
		e := newEnv(t, testutil.NewGitHub(t))
		e.vars["GITHUB_REPOSITORY"] = "invalid-format"
		if r := e.run("--check"); r.code != 2 {
			t.Errorf("GITHUB_REPOSITORY: exit %d\n%s", r.code, r.all())
		}
	})
	t.Run("Should derive the host from GITHUB_SERVER_URL for GitHub Enterprise Server", func(t *testing.T) {
		e := newEnv(t, testutil.NewGitHub(t))
		e.vars["GITHUB_REPOSITORY"] = "o/r"
		e.vars["GITHUB_SERVER_URL"] = "https://ghe.example.com"
		e.vars["GITHUB_API_URL"] = "https://ghe.example.com/api/v3"
		r := e.run("--check", "--check-marketplace=none", "--json=repository")
		if r.code != 0 {
			t.Fatalf("exit %d\n%s", r.code, r.all())
		}
		if h := r.json(t)["repository"].(map[string]any)["host"]; h != "ghe.example.com" {
			t.Errorf("host = %v", h)
		}
		if h := hostFromEnv(func(k string) string { return map[string]string{"GITHUB_API_URL": "https://api.octo.ghe.com"}[k] }); h != "octo.ghe.com" {
			t.Errorf("GHE.com host = %s", h)
		}
	})
}

func TestTokenResolution(t *testing.T) {
	t.Run("Should warn when no token is available", func(t *testing.T) {
		gh := testutil.NewGitHub(t)
		gh.NoScopeHeader()
		gh.SetPermissions(nil)
		e := newEnv(t, gh)
		delete(e.vars, "GH_TOKEN")
		r := e.run(args("--check")...)
		if r.code != 0 || !strings.Contains(r.all(), "anonymous API access") {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
	})
	t.Run("Should error when auto-fix is enabled but no token available", func(t *testing.T) {
		gh := ghWithPatch(t)
		e := newEnv(t, gh)
		delete(e.vars, "GH_TOKEN")
		r := e.run(args("--auto-fix")...)
		if r.code != 2 {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
		if len(gh.Writes()) != 0 {
			t.Errorf("writes: %v", gh.Writes())
		}
	})
	t.Run("Should read the token from GITHUB_TOKEN and mask it in Actions", func(t *testing.T) {
		gh := testutil.NewGitHub(t)
		e := newEnv(t, gh)
		delete(e.vars, "GH_TOKEN")
		e.vars["GITHUB_TOKEN"] = "secret-token"
		e.vars["GITHUB_ACTIONS"] = "true"
		r := e.run(args("--check")...)
		if r.code != 0 || !strings.Contains(r.all(), "::add-mask::secret-token") {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
	})
}

func TestEmptyRepository(t *testing.T) {
	t.Run("Should work without tags (new repository scenario)", func(t *testing.T) {
		r := newEnv(t, testutil.NewGitHub(t)).run(args("--check")...)
		if r.code != 0 || !strings.Contains(r.all(), "No issues found") {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
	})
}

func TestModes(t *testing.T) {
	t.Run("Should not auto-fix when auto-fix is false", func(t *testing.T) {
		gh := ghWithPatch(t)
		r := newEnv(t, gh).run(args("--check")...)
		if r.code != 1 || len(gh.Writes()) != 0 {
			t.Errorf("exit %d writes %v\n%s", r.code, gh.Writes(), r.all())
		}
		if !strings.Contains(r.all(), "git push origin") {
			t.Errorf("manual instructions missing:\n%s", r.all())
		}
	})
	t.Run("Should report only without a terminal when no mode flag is given", func(t *testing.T) {
		gh := ghWithPatch(t)
		r := newEnv(t, gh).run(args()...)
		if r.code != 1 || len(gh.Writes()) != 0 {
			t.Errorf("exit %d writes %v", r.code, gh.Writes())
		}
	})
	t.Run("verify never writes", func(t *testing.T) {
		gh := ghWithPatch(t)
		r := newEnv(t, gh).run(commandArgs("verify", args()...)...)
		if r.code != 1 || len(gh.Writes()) != 0 {
			t.Errorf("exit %d writes %v", r.code, gh.Writes())
		}
	})
	t.Run("Should auto-fix multiple missing versions when auto-fix is enabled", func(t *testing.T) {
		gh := ghWithPatch(t)
		r := newEnv(t, gh).run(args("--auto-fix")...)
		if r.code != 0 {
			t.Fatalf("exit %d\n%s", r.code, r.all())
		}
		if gh.TagSha("v1") != c1 || gh.TagSha("v1.0") != c1 {
			t.Errorf("tags = %v", gh.Tags())
		}
		again := newEnv(t, gh).run(args("--check")...)
		if again.code != 0 {
			t.Errorf("not clean after fix: exit %d\n%s", again.code, again.all())
		}
	})
	t.Run("Should suggest force updating when major version points to wrong commit", func(t *testing.T) {
		gh := testutil.NewGitHub(t).Tag(c1, "v1.0.0", "v1.0").Tag(c2, "v1")
		r := newEnv(t, gh).run(args("--auto-fix")...)
		if r.code != 0 || gh.TagSha("v1") != c1 {
			t.Errorf("exit %d v1=%s\n%s", r.code, gh.TagSha("v1"), r.all())
		}
	})
	t.Run("Should not check minor version tags when check-minor-version is none", func(t *testing.T) {
		gh := testutil.NewGitHub(t).Tag(c1, "v1.0.0", "v1")
		if r := newEnv(t, gh).run(args("--check", "--check-minor-version=none")...); r.code != 0 {
			t.Errorf("none: exit %d\n%s", r.code, r.all())
		}
		if r := newEnv(t, gh).run(args("--check", "--check-minor-version=false")...); r.code != 0 {
			t.Errorf("false: exit %d", r.code)
		}
		if r := newEnv(t, gh).run(args("--check")...); r.code != 1 {
			t.Errorf("error: exit %d", r.code)
		}
	})
	t.Run("Should report warnings without failing", func(t *testing.T) {
		gh := testutil.NewGitHub(t).Tag(c1, "v1.0.0", "v1")
		r := newEnv(t, gh).run(args("--check", "--check-minor-version=warning", "--json=issues,summary")...)
		if r.code != 0 {
			t.Fatalf("exit %d\n%s", r.code, r.all())
		}
		sum := r.json(t)["summary"].(map[string]any)
		if sum["warnings"].(float64) != 1 || sum["errors"].(float64) != 0 {
			t.Errorf("summary = %v", sum)
		}
	})
	t.Run("Should use floating-versions-use branches", func(t *testing.T) {
		gh := ghWithPatch(t)
		r := newEnv(t, gh).run(args("--auto-fix", "--floating-versions-use=branches")...)
		if r.code != 0 || gh.BranchSha("v1") != c1 || gh.TagSha("v1") != "" {
			t.Errorf("exit %d branches=%v tags=%v\n%s", r.code, gh.Branches(), gh.Tags(), r.all())
		}
	})

	t.Run("--interactive requires a terminal", func(t *testing.T) {
		r := newEnv(t, testutil.NewGitHub(t)).run(args("-i")...)
		if r.code != 2 || !strings.Contains(r.all(), "requires a terminal") {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
	})
	t.Run("--yes turns interactive into auto-fix", func(t *testing.T) {
		gh := ghWithPatch(t)
		r := newEnv(t, gh).run(args("--yes")...)
		if r.code != 0 || gh.TagSha("v1") != c1 {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
	})
	t.Run("interactive mode applies approved fixes", func(t *testing.T) {
		gh := ghWithPatch(t)
		e := newEnv(t, gh)
		e.tty = true
		p := &fakePrompter{picks: []int{0, 1, 4}} // apply, skip, ... quit
		e.prompter = p
		r := e.run(args("--interactive")...)
		if len(p.asked) == 0 {
			t.Fatalf("no prompt shown\n%s", r.all())
		}
		if len(gh.Writes()) != 1 {
			t.Errorf("writes = %v", gh.Writes())
		}
		if r.code != 1 {
			t.Errorf("skipped issues must keep exit code 1, got %d\n%s", r.code, r.all())
		}
	})
	t.Run("interactive apply all remaining fixes everything", func(t *testing.T) {
		gh := ghWithPatch(t)
		e := newEnv(t, gh)
		e.tty = true
		e.prompter = &fakePrompter{picks: []int{2}}
		r := e.run(args("--interactive")...)
		if r.code != 0 || gh.TagSha("v1") != c1 || gh.TagSha("v1.0") != c1 {
			t.Errorf("exit %d tags %v\n%s", r.code, gh.Tags(), r.all())
		}
	})
	t.Run("verify on a terminal never prompts", func(t *testing.T) {
		gh := ghWithPatch(t)
		e := newEnv(t, gh)
		e.tty = true
		p := &fakePrompter{}
		e.prompter = p
		if r := e.run(commandArgs("verify", args()...)...); r.code != 1 || len(p.asked) != 0 || len(gh.Writes()) != 0 {
			t.Errorf("exit %d asked %v writes %v", r.code, p.asked, gh.Writes())
		}
	})
	t.Run("--json on a terminal does not prompt unless -i is given", func(t *testing.T) {
		gh := ghWithPatch(t)
		e := newEnv(t, gh)
		e.tty = true
		p := &fakePrompter{}
		e.prompter = p
		if r := e.run(commandArgs("verify", args("--json")...)...); r.code != 1 || len(p.asked) != 0 {
			t.Errorf("exit %d asked %v\n%s", r.code, p.asked, r.all())
		}
	})
}

func TestReleaseChecks(t *testing.T) {
	seed := func(t *testing.T) *testutil.GitHub {
		return testutil.NewGitHub(t).Tag(c1, "v1.0.0", "v1.0", "v1")
	}
	rel := []string{"-R", "o/r", "--check-marketplace=none"}
	t.Run("Should not check releases when check-releases is none", func(t *testing.T) {
		r := newEnv(t, seed(t)).run("-R", "o/r", "--check", "--check-marketplace=none", "--check-releases=none", "--check-release-immutability=none")
		if r.code != 0 {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
	})
	t.Run("Should suggest creating a release when tag exists but release doesn't", func(t *testing.T) {
		r := newEnv(t, seed(t)).run(append(rel, "--check")...)
		if r.code != 1 || !strings.Contains(r.all(), "v1.0.0") {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
	})
	t.Run("Should create the release during auto-fix", func(t *testing.T) {
		gh := seed(t)
		gh.ImmutableEnabled = true
		r := newEnv(t, gh).run(append(rel, "--auto-fix")...)
		if r.code != 0 || gh.FindRelease("v1.0.0") == nil {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
	})
	t.Run("Should allow checking releases but not immutability separately", func(t *testing.T) {
		gh := seed(t)
		gh.Release("v1.0.0", testutil.Latest)
		r := newEnv(t, gh).run("-R", "o/r", "--check", "--check-marketplace=none", "--check-release-immutability=none")
		if r.code != 0 {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
		r = newEnv(t, gh).run("-R", "o/r", "--check", "--check-marketplace=none", "--check-release-immutability=warning")
		if r.code != 0 {
			t.Errorf("warning: exit %d\n%s", r.code, r.all())
		}
		r = newEnv(t, gh).run("-R", "o/r", "--check", "--check-marketplace=none")
		if r.code != 1 {
			t.Errorf("error: exit %d\n%s", r.code, r.all())
		}
	})
	t.Run("Should read action-version.yml and let flags override it", func(t *testing.T) {
		gh := seed(t)
		gh.Release("v1.0.0", testutil.Latest)
		gh.File("action-version.yml", "concerns:\n  immutability:\n    level: none\n")
		args := []string{"-R", "o/r", "--check", "--check-marketplace=none"}
		if r := newEnv(t, gh).run(args...); r.code != 0 {
			t.Errorf("file: exit %d\n%s", r.code, r.all())
		}
		if r := newEnv(t, gh).run(append(args, "--check-release-immutability=error")...); r.code != 1 {
			t.Errorf("flag: exit %d\n%s", r.code, r.all())
		}
	})
	t.Run("Should reject an invalid action-version.yml", func(t *testing.T) {
		gh := seed(t)
		gh.File("action-version.yml", "bogus: 1\n")
		if r := newEnv(t, gh).run("-R", "o/r", "--check", "--check-marketplace=none"); r.code == 0 {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
	})
	t.Run("Should filter preview releases when ignore-preview-releases is true", func(t *testing.T) {
		gh := testutil.NewGitHub(t).Tag(c1, "v1.0.0", "v1.0", "v1").Tag(c2, "v1.1.0")
		gh.Release("v1.0.0", testutil.Immutable, testutil.Latest)
		gh.Release("v1.1.0", testutil.Prerelease, testutil.Immutable)
		base := []string{"-R", "o/r", "--check", "--check-marketplace=none", "--check-minor-version=none"}
		if r := newEnv(t, gh).run(base...); r.code != 0 {
			t.Errorf("ignore (default): exit %d\n%s", r.code, r.all())
		}
		if r := newEnv(t, gh).run(append(base, "--include-preview-releases")...); r.code != 1 {
			t.Errorf("not ignored: exit %d\n%s", r.code, r.all())
		}
	})
	t.Run("Should validate marketplace metadata", func(t *testing.T) {
		gh := testutil.NewGitHub(t)
		if r := newEnv(t, gh).run("-R", "o/r", "--check", "--check-releases=none", "--check-release-immutability=none"); r.code != 1 {
			t.Errorf("missing metadata: exit %d\n%s", r.code, r.all())
		}
		gh.MarketplaceFiles()
		if r := newEnv(t, gh).run("-R", "o/r", "--check", "--check-releases=none", "--check-release-immutability=none"); r.code != 0 {
			t.Errorf("valid metadata: exit %d\n%s", r.code, r.all())
		}
	})
}

func TestIgnoreVersions(t *testing.T) {
	cases := map[string]struct {
		value string
		code  int
	}{
		"Should skip validation for versions in ignore-versions list":                  {"v1.0.0", 0},
		"Should support wildcard patterns in ignore-versions":                          {"v1.*", 0},
		"Should handle multiple comma-separated versions in ignore-versions":           {"v1.0.0,v1.0,v1", 0},
		"Should silently skip invalid ignore-versions patterns but process valid ones": {"invalid,v1.0.0", 0},
		"Should handle empty ignore-versions gracefully":                               {"", 1},
		"Should handle ignore-versions with extra whitespace and commas":               {" v1.0.0 , , ", 0},
		"Should support newline-separated ignore-versions":                             {"v1.0.0\nv1.1.0", 0},
		"Should support mixed newline and comma separators":                            {"v2.0.0,\nv1.0.0", 0},
		"Should not ignore versions that do not match":                                 {"v2.0.0", 1},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			gh := ghWithPatch(t)
			r := newEnv(t, gh).run(args("--check", "--ignore-versions="+c.value)...)
			if r.code != c.code {
				t.Errorf("exit %d, want %d\n%s", r.code, c.code, r.all())
			}
		})
	}
}

func TestVersionScoping(t *testing.T) {
	seed := func(t *testing.T) *testutil.GitHub {
		return testutil.NewGitHub(t).Tag(c1, "v1.0.0").Tag(c2, "v2.0.0")
	}
	versions := func(r result, t *testing.T) []string {
		var out []string
		for _, i := range r.json(t)["issues"].([]any) {
			out = append(out, i.(map[string]any)["version"].(string))
		}
		return out
	}
	t.Run("scopes the report to the given versions", func(t *testing.T) {
		r := newEnv(t, seed(t)).run(args("--check", "--json=issues", "v2")...)
		for _, v := range versions(r, t) {
			if !strings.HasPrefix(v, "v2") {
				t.Errorf("unexpected issue for %s", v)
			}
		}
		if len(versions(r, t)) == 0 {
			t.Error("expected issues for v2")
		}
	})
	t.Run("scoped auto-fix only touches the given versions", func(t *testing.T) {
		gh := seed(t)
		r := newEnv(t, gh).run(args("--auto-fix", "v1")...)
		if gh.TagSha("v1") != c1 || gh.TagSha("v2") != "" {
			t.Errorf("tags %v\n%s", gh.Tags(), r.all())
		}
		if r.code != 0 {
			t.Errorf("exit %d", r.code)
		}
	})
	t.Run("supports wildcards", func(t *testing.T) {
		r := newEnv(t, seed(t)).run(args("--check", "--json=issues", "v1.*")...)
		for _, v := range versions(r, t) {
			if !strings.HasPrefix(v, "v1.") {
				t.Errorf("unexpected issue for %s", v)
			}
		}
	})
}

func TestJSONOutput(t *testing.T) {
	t.Run("Should return the documented schema when --json is used", func(t *testing.T) {
		gh := ghWithPatch(t)
		r := newEnv(t, gh).run(args("--check", "--json")...)
		if r.code != 1 {
			t.Fatalf("exit %d\n%s", r.code, r.all())
		}
		m := r.json(t)
		for _, k := range []string{"repository", "mode", "local", "config", "issues", "summary", "exitCode"} {
			if _, ok := m[k]; !ok {
				t.Errorf("missing field %s", k)
			}
		}
		if m["mode"] != "report-only" || m["exitCode"].(float64) != 1 {
			t.Errorf("mode/exitCode = %v/%v", m["mode"], m["exitCode"])
		}
		cfg := m["config"].(map[string]any)
		if cfg["floatingVersionsUse"] != "tags" || cfg["checkReleases"] != "none" || cfg["ignorePreviewReleases"] != true {
			t.Errorf("config = %v", cfg)
		}
		issues := m["issues"].([]any)
		if len(issues) == 0 {
			t.Fatal("no issues")
		}
		i := issues[0].(map[string]any)
		for _, k := range []string{"rule", "finding", "severity", "message", "version", "currentSha", "expectedSha", "status", "autoFixable", "action"} {
			if _, ok := i[k]; !ok {
				t.Errorf("issue is missing %s: %v", k, i)
			}
		}
		if _, ok := i["type"]; ok {
			t.Errorf("issue contains removed type field: %v", i)
		}
		act := i["action"].(map[string]any)
		if act["description"] == "" || len(act["manualCommands"].([]any)) == 0 {
			t.Errorf("action = %v", act)
		}
		sum := m["summary"].(map[string]any)
		if sum["total"].(float64) != float64(len(issues)) || sum["manualFixRequired"].(float64) == 0 {
			t.Errorf("summary = %v", sum)
		}
		if !strings.Contains(r.err, "git push") && !strings.Contains(r.err, "ERROR") {
			t.Errorf("human logs should go to stderr:\n%s", r.err)
		}
	})
	t.Run("keeps stdout machine readable", func(t *testing.T) {
		r := newEnv(t, ghWithPatch(t)).run(args("--auto-fix", "--json")...)
		if r.code != 0 {
			t.Fatalf("exit %d\n%s", r.code, r.all())
		}
		m := r.json(t)
		if m["mode"] != "auto-fix" || m["summary"].(map[string]any)["fixed"].(float64) == 0 {
			t.Errorf("report = %v", m["summary"])
		}
	})
	t.Run("selects fields", func(t *testing.T) {
		r := newEnv(t, ghWithPatch(t)).run(args("--check", "--json=summary,exitCode")...)
		m := r.json(t)
		if len(m) != 2 || m["summary"] == nil || m["exitCode"] == nil {
			t.Errorf("fields = %v", m)
		}
	})
	t.Run("rejects unknown fields", func(t *testing.T) {
		r := newEnv(t, ghWithPatch(t)).run(args("--check", "--json=bogus")...)
		if r.code != 2 || !strings.Contains(r.all(), "unknown JSON field") {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
	})
	t.Run("--jq filters the output", func(t *testing.T) {
		r := newEnv(t, ghWithPatch(t)).run(args("--check", "--jq", ".issues | map(.version) | length")...)
		if strings.TrimSpace(r.out) == "" || strings.TrimSpace(r.out) == "0" || r.code != 1 {
			t.Errorf("exit %d out %q", r.code, r.out)
		}
		r = newEnv(t, ghWithPatch(t)).run(args("--check", "--jq", ".exitCode")...)
		if strings.TrimSpace(r.out) != "1" {
			t.Errorf("out %q", r.out)
		}
	})
	t.Run("--template formats the output", func(t *testing.T) {
		r := newEnv(t, ghWithPatch(t)).run(args("--check", "--template", `{{range .issues}}{{.version}} {{.status}}{{"\n"}}{{end}}`)...)
		if !strings.Contains(r.out, "v1 manual_fix_required") {
			t.Errorf("out %q", r.out)
		}
	})
	t.Run("--jq and --template are mutually exclusive", func(t *testing.T) {
		if r := newEnv(t, ghWithPatch(t)).run(args("--jq", ".", "--template", "x")...); r.code != 2 {
			t.Errorf("exit %d", r.code)
		}
	})
	t.Run("prints nothing on stdout for usage errors", func(t *testing.T) {
		r := newEnv(t, ghWithPatch(t)).run(commandArgs("verify", args("--json", "--bogus")...)...)
		if r.code != 2 || r.out != "" {
			t.Errorf("exit %d out %q", r.code, r.out)
		}
	})
}

func TestAuthScopes(t *testing.T) {
	t.Run("Should exit 2 with a gh auth refresh suggestion when scopes are missing", func(t *testing.T) {
		gh := ghWithPatch(t)
		gh.SetScopes("read:org")
		gh.Private = true
		r := newEnv(t, gh).run(args("--auto-fix")...)
		if r.code != 2 || !strings.Contains(r.all(), "gh auth refresh -h github.com -s repo,workflow") {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
		if len(gh.Writes()) != 0 {
			t.Errorf("writes: %v", gh.Writes())
		}
	})
	t.Run("Should not require write scopes for --check on public repositories", func(t *testing.T) {
		gh := ghWithPatch(t)
		gh.SetScopes("read:org")
		if r := newEnv(t, gh).run(args("--check")...); r.code != 1 {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
	})
	t.Run("Should continue with only the workflow scope missing", func(t *testing.T) {
		gh := ghWithPatch(t)
		gh.SetScopes("repo")
		r := newEnv(t, gh).run(args("--auto-fix")...)
		if r.code != 0 || !strings.Contains(r.all(), "workflow") {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
	})
	t.Run("Should accept fine-grained tokens without scope headers", func(t *testing.T) {
		gh := ghWithPatch(t)
		gh.NoScopeHeader()
		if r := newEnv(t, gh).run(args("--auto-fix")...); r.code != 0 {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
	})
	t.Run("Should explain missing write permission for fine-grained tokens", func(t *testing.T) {
		gh := ghWithPatch(t)
		gh.NoScopeHeader()
		gh.SetPermissions(&testutil.Permissions{Pull: true})
		r := newEnv(t, gh).run(args("--auto-fix")...)
		if r.code != 2 || !strings.Contains(r.all(), "contents: write") {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
	})
	t.Run("Should offer to run gh auth refresh on a terminal and continue afterwards", func(t *testing.T) {
		gh := ghWithPatch(t)
		gh.SetScopes("read:org")
		gh.Private = true
		e := newEnv(t, gh)
		delete(e.vars, "GH_TOKEN")
		e.tty = true
		e.cfgToken = "cfg-token"
		p := &fakePrompter{confirm: true, picks: []int{2}}
		e.prompter = p
		var refreshed []string
		e.refresh = func(_ context.Context, host string, scopes []string) error {
			refreshed = append([]string{host}, scopes...)
			gh.SetScopes("repo", "workflow")
			return nil
		}
		r := e.run(args("--interactive")...)
		if r.code != 0 || strings.Join(refreshed, ",") != "github.com,repo,workflow" {
			t.Errorf("exit %d refreshed %v\n%s", r.code, refreshed, r.all())
		}
		if len(p.asked) == 0 || !strings.Contains(p.asked[0], "gh auth refresh") {
			t.Errorf("asked %v", p.asked)
		}
	})
	t.Run("Should stop with exit 2 when the refresh is declined", func(t *testing.T) {
		gh := ghWithPatch(t)
		gh.SetScopes("read:org")
		gh.Private = true
		e := newEnv(t, gh)
		delete(e.vars, "GH_TOKEN")
		e.tty = true
		e.prompter = &fakePrompter{confirm: false}
		if r := e.run(args("--interactive")...); r.code != 2 {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
	})
	t.Run("Should not offer the refresh when the token comes from the environment", func(t *testing.T) {
		gh := ghWithPatch(t)
		gh.SetScopes("read:org")
		gh.Private = true
		e := newEnv(t, gh)
		e.tty = true
		p := &fakePrompter{confirm: true}
		e.prompter = p
		if r := e.run(args()...); r.code != 2 || len(p.asked) != 0 {
			t.Errorf("exit %d asked %v", r.code, p.asked)
		}
	})
	t.Run("Should never offer the refresh in CI", func(t *testing.T) {
		gh := ghWithPatch(t)
		gh.SetScopes("read:org")
		gh.Private = true
		e := newEnv(t, gh)
		p := &fakePrompter{confirm: true}
		e.prompter = p
		e.vars["GITHUB_ACTIONS"] = "true"
		if r := e.run(args("--auto-fix")...); r.code != 2 || len(p.asked) != 0 {
			t.Errorf("exit %d asked %v", r.code, p.asked)
		}
	})
	t.Run("Should exit 2 when the repository cannot be accessed", func(t *testing.T) {
		gh := testutil.NewGitHub(t)
		gh.Fail("GET", "/repos/o/r", 404, `{"message":"Not Found"}`, 1)
		if r := newEnv(t, gh).run(args("--check")...); r.code != 2 || !strings.Contains(r.all(), "not found") {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
		gh = testutil.NewGitHub(t)
		gh.Fail("GET", "/repos/o/r", 401, `{"message":"Bad credentials"}`, 0)
		if r := newEnv(t, gh).run(args("--check")...); r.code != 2 || !strings.Contains(r.all(), "401") {
			t.Errorf("401: exit %d\n%s", r.code, r.all())
		}
	})
}

func TestAPIErrors(t *testing.T) {
	t.Run("Should handle 500 server errors from GitHub API", func(t *testing.T) {
		gh := ghWithPatch(t)
		gh.Fail("GET", "/branches", 500, `{"message":"boom"}`, 0)
		if r := newEnv(t, gh).run(args("--check")...); r.code != 2 {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
	})
	t.Run("Should exit 2 when the rate limit wait exceeds --max-wait", func(t *testing.T) {
		gh := ghWithPatch(t)
		reset := time.Now().Add(time.Hour)
		gh.Fail("GET", "/git/refs/tags", 403, `{"message":"API rate limit exceeded"}`, 0,
			"X-RateLimit-Remaining", "0", "X-RateLimit-Reset", itoa(reset.Unix()))
		r := newEnv(t, gh).run(args("--check", "--max-wait=1s")...)
		if r.code != 2 || !strings.Contains(strings.ToLower(r.all()), "rate limit") {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
	})
	t.Run("Should handle 422 errors when creating references", func(t *testing.T) {
		gh := ghWithPatch(t)
		gh.Fail("POST", "/git/refs", 422, `{"message":"Validation Failed"}`, 0)
		r := newEnv(t, gh).run(args("--auto-fix")...)
		if r.code != 1 {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
	})
	t.Run("Should require manual fixes when the token lacks the workflows permission", func(t *testing.T) {
		gh := ghWithPatch(t)
		gh.Fail("POST", "/git/refs", 403, `{"message":"Resource not accessible by integration"}`, 0)
		r := newEnv(t, gh).run(args("--auto-fix", "--json=summary")...)
		if r.code != 1 || r.json(t)["summary"].(map[string]any)["manualFixRequired"].(float64) == 0 {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
	})
}

func itoa(i int64) string { return strconv.FormatInt(i, 10) }

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestGitHubActions(t *testing.T) {
	t.Run("Should write annotations when running in GitHub Actions", func(t *testing.T) {
		e := newEnv(t, ghWithPatch(t))
		e.vars["GITHUB_ACTIONS"] = "true"
		r := e.run(args("--check")...)
		if r.code != 1 || !strings.Contains(r.all(), "::error") {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
	})
	t.Run("Should write summary to GITHUB_STEP_SUMMARY when available", func(t *testing.T) {
		e := newEnv(t, ghWithPatch(t))
		path := filepath.Join(t.TempDir(), "summary.md")
		t.Setenv("GITHUB_STEP_SUMMARY", path)
		e.vars["GITHUB_ACTIONS"] = "true"
		e.vars["GITHUB_STEP_SUMMARY"] = path
		e.run(args("--check")...)
		b := readFile(t, path)
		if !strings.Contains(b, "Version validation: o/r") || !strings.Contains(b, "Manual Remediation Required") {
			t.Errorf("summary:\n%s", b)
		}
	})
	t.Run("Should protect against workflow command injection in output", func(t *testing.T) {
		gh := testutil.NewGitHub(t).Tag(c1, "v1.0.0")
		e := newEnv(t, gh)
		e.vars["GITHUB_ACTIONS"] = "true"
		r := e.run(args("--check", "--ignore-versions=::set-env name=X::y")...)
		if strings.Contains(r.all(), "\n::set-env") {
			t.Errorf("unescaped workflow command:\n%s", r.all())
		}
	})
}

func TestLocalMode(t *testing.T) {
	git := func(t *testing.T, dir string, a ...string) string {
		t.Helper()
		c := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, a...)...)
		c.Dir = dir
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	setup := func(t *testing.T) string {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git not available")
		}
		dir := t.TempDir()
		git(t, dir, "init", "-q")
		git(t, dir, "commit", "-q", "--allow-empty", "-m", "one")
		git(t, dir, "tag", "v1.0.0")
		return dir
	}
	t.Run("--local --check is an offline lint", func(t *testing.T) {
		dir := setup(t)
		e := newEnv(t, nil)
		e.dir = dir
		r := e.run("--local", "--check", "--json=summary")
		if r.code != 1 || r.json(t)["summary"].(map[string]any)["errors"].(float64) == 0 {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
		if git(t, dir, "tag", "-l", "v1") != "" {
			t.Error("--check changed the repository")
		}
	})
	t.Run("--local --auto-fix fixes refs locally and prints the push commands", func(t *testing.T) {
		dir := setup(t)
		head := git(t, dir, "rev-parse", "HEAD")
		e := newEnv(t, nil)
		e.dir = dir
		r := e.run("--local-only", "--auto-fix")
		if r.code != 0 {
			t.Fatalf("exit %d\n%s", r.code, r.all())
		}
		if got := git(t, dir, "rev-parse", "v1^{commit}"); got != head {
			t.Errorf("v1 = %s, want %s", got, head)
		}
		if !strings.Contains(r.out, "git push origin "+head+":refs/tags/v1") {
			t.Errorf("push commands missing:\n%s", r.out)
		}
	})
	t.Run("--fetch-releases requires --local", func(t *testing.T) {
		if r := newEnv(t, nil).run("--fetch-releases", "--check"); r.code != 2 {
			t.Errorf("exit %d", r.code)
		}
	})
	t.Run("--local skips release checks with a notice", func(t *testing.T) {
		dir := setup(t)
		git(t, dir, "tag", "v1")
		git(t, dir, "tag", "v1.0")
		e := newEnv(t, nil)
		e.dir = dir
		r := e.run("--local", "--check")
		if r.code != 0 || !strings.Contains(r.all(), "skipped in local mode") {
			t.Errorf("exit %d\n%s", r.code, r.all())
		}
	})
}
