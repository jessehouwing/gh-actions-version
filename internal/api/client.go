// Package api ports lib/GitHubApi.ps1 on top of go-gh, with centralised rate limit handling.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	ghapi "github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/repository"

	"github.com/jessehouwing/gh-actions-version/internal/progress"
	"github.com/jessehouwing/gh-actions-version/internal/state"
	"github.com/jessehouwing/gh-actions-version/internal/version"
)

var _ state.Client = (*Client)(nil)

// Repo identifies the target repository and host.
type Repo struct {
	Host  string // e.g. github.com, ghe.example.com, octo.ghe.com
	Owner string
	Name  string
}

func (r Repo) String() string { return r.Owner + "/" + r.Name }

// ResolveRepo resolves the target repository from -R (OWNER/REPO or HOST/OWNER/REPO) or, when
// empty, from the git remote of the current directory (GH_REPO is honoured by go-gh).
func ResolveRepo(flag string) (Repo, error) {
	var r repository.Repository
	var err error
	if flag != "" {
		r, err = repository.Parse(flag)
	} else {
		r, err = repository.Current()
	}
	if err != nil {
		return Repo{}, err
	}
	return Repo{Host: r.Host, Owner: r.Owner, Name: r.Name}, nil
}

// Options configure New.
type Options struct {
	Repo  Repo
	Token string // empty: go-gh resolves it from GH_TOKEN/GITHUB_TOKEN/gh config for Repo.Host
	// ServerURL is the web URL used in links (default derived from the host).
	ServerURL      string
	IgnoreVersions []string
	Transport      TransportOptions
	// Debug receives diagnostics (::debug:: lines); optional. It must treat messages as untrusted.
	Debug    func(msg string)
	Progress progress.Tracker
	// ImmutablePollAttempts/ImmutablePollDelay tune Enable-GitHubImmutableRelease polling
	// (defaults 6 and 1s).
	ImmutablePollAttempts int
	ImmutablePollDelay    time.Duration
}

// Client implements state.Client and the read APIs.
type Client struct {
	Repo      Repo
	Transport *Transport

	rest      *ghapi.RESTClient
	gql       *ghapi.GraphQLClient
	serverURL string
	debug     func(string)
	progress  progress.Tracker
	sleep     func(context.Context, time.Duration) error
	pollN     int
	pollD     time.Duration

	mu      sync.Mutex
	ignore  []string
	known   map[string]bool
	fatal   error
	rate    GraphQLRateLimit
	hasRate bool
}

// New builds a Client with the rate limit transport installed under the go-gh clients.
func New(o Options) (*Client, error) {
	t := NewTransport(o.Transport)
	co := ghapi.ClientOptions{
		Host:         o.Repo.Host,
		AuthToken:    o.Token,
		Transport:    t,
		LogIgnoreEnv: false,
	}
	if o.Token == "" {
		// go-gh looks up (and requires) a token from the process environment when none is given;
		// use a placeholder and strip it so anonymous access never depends on ambient credentials.
		co.AuthToken = "anonymous"
		co.Transport = stripAuth{t}
	}
	rest, err := ghapi.NewRESTClient(co)
	if err != nil {
		return nil, err
	}
	gql, err := ghapi.NewGraphQLClient(co)
	if err != nil {
		return nil, err
	}
	c := &Client{
		Repo: o.Repo, Transport: t, rest: rest, gql: gql, debug: o.Debug,
		progress: o.Progress, sleep: t.opts.Sleep, ignore: o.IgnoreVersions, known: map[string]bool{},
		pollN: o.ImmutablePollAttempts, pollD: o.ImmutablePollDelay, serverURL: o.ServerURL,
	}
	if c.pollN == 0 {
		c.pollN = 6
	}
	if c.pollD == 0 {
		c.pollD = time.Second
	}
	if c.serverURL == "" {
		c.serverURL = serverURLFor(o.Repo.Host)
	}
	return c, nil
}

func (c *Client) startProgress(name string) progress.Stage {
	if c.progress == nil {
		return progress.NoopStage{}
	}
	return c.progress.Start(name)
}

type stripAuth struct{ next http.RoundTripper }

func (s stripAuth) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Del("Authorization")
	return s.next.RoundTrip(r)
}

func serverURLFor(host string) string {
	if host == "" {
		host = "github.com"
	}
	if strings.HasPrefix(host, "localhost") {
		return "http://" + host
	}
	return "https://" + host
}

// SetIgnoreVersions sets the ignore-versions patterns applied to returned objects.
func (c *Client) SetIgnoreVersions(p []string) {
	c.mu.Lock()
	c.ignore = p
	c.mu.Unlock()
}

// Fatal returns the first rate limit or authentication error seen by any call. The state.Client
// methods fold errors into result structs; callers can check this to abort with exit code 2.
func (c *Client) Fatal() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fatal
}

func (c *Client) note(err error) {
	if err == nil {
		return
	}
	if IsRateLimit(err) || StatusCode(err) == 401 {
		c.mu.Lock()
		if c.fatal == nil {
			c.fatal = err
		}
		c.mu.Unlock()
	}
}

func (c *Client) logf(format string, a ...any) {
	if c.debug != nil {
		c.debug(fmt.Sprintf(format, a...))
	}
}

func (c *Client) ignored(v string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return version.ShouldBeIgnored(v, c.ignore)
}

func (c *Client) repoPath(suffix string) string {
	return "repos/" + url.PathEscape(c.Repo.Owner) + "/" + url.PathEscape(c.Repo.Name) + suffix
}

func jsonBody(v any) (io.Reader, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(b), nil
}

// do performs a REST call and decodes the JSON response into out (may be nil).
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	r, err := jsonBody(body)
	if err != nil {
		return err
	}
	if out == nil {
		out = &struct{}{}
	}
	err = c.rest.DoWithContext(ctx, method, path, r, out)
	c.note(err)
	return err
}

// request performs a REST call returning the response (for headers). Caller closes the body.
func (c *Client) request(ctx context.Context, method, path string, body any) (*http.Response, error) {
	r, err := jsonBody(body)
	if err != nil {
		return nil, err
	}
	resp, err := c.rest.RequestWithContext(ctx, method, path, r)
	c.note(err)
	return resp, err
}

var nextLink = regexp.MustCompile(`<([^>]+)>\s*;\s*rel="next"`)

// getPages follows Link rel=next, calling fn with each page's body.
func (c *Client) getPages(ctx context.Context, path string, fn func(body []byte) error) error {
	for path != "" {
		resp, err := c.request(ctx, http.MethodGet, path, nil)
		if err != nil {
			return err
		}
		b, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		if err := fn(b); err != nil {
			return err
		}
		path = ""
		if m := nextLink.FindStringSubmatch(resp.Header.Get("Link")); m != nil {
			path = m[1]
		}
	}
	return nil
}

// GraphQLRateLimit is the rateLimit object returned by GraphQL queries.
type GraphQLRateLimit struct {
	Cost      int       `json:"cost"`
	Remaining int       `json:"remaining"`
	ResetAt   time.Time `json:"resetAt"`
}

// LastGraphQLRateLimit returns the most recent GraphQL rateLimit data, if any query ran.
func (c *Client) LastGraphQLRateLimit() (GraphQLRateLimit, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rate, c.hasRate
}

func (c *Client) graphql(ctx context.Context, query string, vars map[string]any, out any) error {
	err := c.gql.DoWithContext(ctx, query, vars, out)
	c.note(err)
	return err
}

func (c *Client) setRate(r *GraphQLRateLimit) {
	if r == nil {
		return
	}
	c.mu.Lock()
	c.rate, c.hasRate = *r, true
	c.mu.Unlock()
}

// sleepCtx sleeps using the injectable sleeper.
func (c *Client) sleepCtx(ctx context.Context, d time.Duration) error { return c.sleep(ctx, d) }
