package cmd

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/cli/go-gh/v2/pkg/auth"
	"github.com/cli/go-gh/v2/pkg/prompter"
	"github.com/cli/go-gh/v2/pkg/term"

	"github.com/jessehouwing/gh-version-action/internal/api"
	"github.com/jessehouwing/gh-version-action/internal/remediation"
)

// Prompter asks questions on the terminal. It extends remediation.Prompter (the interactive
// fix menu) with a yes/no question used to offer `gh auth refresh`.
type Prompter interface {
	remediation.Prompter
	Confirm(message string, def bool) (bool, error)
}

// Env is everything Run needs from the outside world. Every field is optional; OSEnv returns the
// real process environment and tests inject fakes.
type Env struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	// IsTTY reports that stdin and stdout are terminals (enables the interactive default).
	IsTTY bool
	// Getenv reads environment variables (GH_TOKEN, GITHUB_REPOSITORY, GITHUB_ACTIONS, ...).
	Getenv func(string) string
	// Dir is the working directory used for --local and git remote detection.
	Dir string
	// Prompter is used by interactive mode; nil builds a go-gh prompter on the process terminal.
	Prompter Prompter
	// ResolveRepo resolves -R/GH_REPO values or, with an empty argument, the git remote.
	ResolveRepo func(flag string) (api.Repo, error)
	// Token returns the credential for host when no token variable is set.
	Token func(host string) string
	// ClientOptions can adjust the API client options (tests redirect the transport here).
	ClientOptions func(*api.Options)
	// RefreshAuth runs `gh auth refresh -h host -s scopes`.
	RefreshAuth func(ctx context.Context, host string, scopes []string) error
}

// OSEnv is the environment of the running process.
func OSEnv() Env {
	return Env{
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		IsTTY: term.IsTerminal(os.Stdin) && term.IsTerminal(os.Stdout) && os.Getenv("GITHUB_ACTIONS") != "true",
	}
}

func (e Env) withDefaults() Env {
	if e.Stdin == nil {
		e.Stdin = strings.NewReader("")
	}
	if e.Stdout == nil {
		e.Stdout = io.Discard
	}
	if e.Stderr == nil {
		e.Stderr = io.Discard
	}
	if e.Getenv == nil {
		e.Getenv = os.Getenv
	}
	if e.ResolveRepo == nil {
		e.ResolveRepo = api.ResolveRepo
	}
	if e.Token == nil {
		e.Token = func(host string) string { t, _ := auth.TokenForHost(host); return t }
	}
	if e.RefreshAuth == nil {
		e.RefreshAuth = runGhAuthRefresh
	}
	return e
}

func runGhAuthRefresh(ctx context.Context, host string, scopes []string) error {
	gh, err := exec.LookPath("gh")
	if err != nil {
		return fmt.Errorf("gh not found on PATH: %w", err)
	}
	c := exec.CommandContext(ctx, gh, "auth", "refresh", "-h", host, "-s", strings.Join(scopes, ","))
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stderr, os.Stderr
	return c.Run()
}

// goghPrompter adapts the go-gh prompter.
type goghPrompter struct{ p *prompter.Prompter }

func (g goghPrompter) Select(message string, options []string) (int, error) {
	return g.p.Select(message, "", options)
}

func (g goghPrompter) Confirm(message string, def bool) (bool, error) {
	return g.p.Confirm(message, def)
}

// processPrompter prompts on the real terminal; prompts go to stderr when stdout carries JSON.
func processPrompter(jsonMode bool) Prompter {
	out := os.Stdout
	if jsonMode {
		out = os.Stderr
	}
	return goghPrompter{prompter.New(os.Stdin, out, os.Stderr)}
}

// repoFromEnv builds the repository from GITHUB_REPOSITORY and the GITHUB_SERVER_URL /
// GITHUB_API_URL variables of GitHub Actions (including GHES and GHE.com).
func repoFromEnv(slug string, getenv func(string) string) (api.Repo, error) {
	owner, name, ok := strings.Cut(slug, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return api.Repo{}, fmt.Errorf("invalid repository %q: expected OWNER/REPO", slug)
	}
	return api.Repo{Host: hostFromEnv(getenv), Owner: owner, Name: name}, nil
}

func hostFromEnv(getenv func(string) string) string {
	if s := getenv("GITHUB_SERVER_URL"); s != "" {
		if u, err := url.Parse(s); err == nil && u.Host != "" {
			return u.Host
		}
	}
	if s := getenv("GITHUB_API_URL"); s != "" {
		if u, err := url.Parse(s); err == nil && u.Host != "" {
			if u.Host == "api.github.com" {
				return "github.com"
			}
			return strings.TrimPrefix(u.Host, "api.")
		}
	}
	return "github.com"
}
