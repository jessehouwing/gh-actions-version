// Package testutil provides a fake GitHub API server for tests of the extension.
//
// GitHub is an httptest server speaking the slice of the GitHub REST and GraphQL API the
// extension uses: tags, branches, releases (GraphQL list + REST writes), refs, contents,
// immutable-releases and rulesets. Seed it with Tag, Branch, Release, File and the other
// setters, point an API client at it through Transport (which rewrites every request to the
// server regardless of host), run the code under test and inspect the recorded Writes or the
// resulting refs and releases.
//
//	gh := testutil.NewGitHub(t)
//	gh.Tag(testutil.SHA('1'), "v1.0.0", "v1")
//	gh.Release("v1.0.0")
//	... run ...
//	gh.Writes()            // ["POST /git/refs refs/tags/v1.0.1 1111...", ...]
//	gh.TagSha("v1.0.1")    // state after the run
//
// Responses carry the headers the extension inspects: X-OAuth-Scopes (see SetScopes) and the
// X-RateLimit-* family (see SetRateLimit). Failures can be injected per request with Fail.
package testutil

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Owner and Repo are the repository served by the fake.
const (
	Owner = "o"
	Repo  = "r"
)

var repoPath = regexp.MustCompile(`^(?:.*/api/v3)?/repos/[^/]+/[^/]+`)

// SHA returns a 40 character SHA made of c.
func SHA(c byte) string { return strings.Repeat(string(c), 40) }

// Release is a seeded or created release.
type Release struct {
	ID         int64
	Tag        string
	Draft      bool
	Prerelease bool
	Immutable  bool
	Latest     bool
}

// Permissions is the permissions object of GET /repos/{owner}/{repo}.
type Permissions struct{ Admin, Maintain, Push, Triage, Pull bool }

type failure struct {
	method, contains string
	status           int
	body             string
	header           http.Header
	times            int // 0 = always
}

// GitHub is the fake server. Exported fields may be changed before the code under test runs;
// use the methods once requests may be in flight.
type GitHub struct {
	// URL is the server address.
	URL string
	// Private reports the repository as private.
	Private bool
	// ImmutableEnabled is the immutable releases repository setting; new published releases are
	// immutable while it is on.
	ImmutableEnabled bool

	t   testing.TB
	mu  sync.Mutex
	srv *httptest.Server

	tags, branches map[string]string
	releases       []*Release
	files          map[string]string
	nextID         int64
	scopes         *string // nil: no X-OAuth-Scopes header (fine-grained / app / GITHUB_TOKEN)
	perms          *Permissions
	rateRemaining  int
	rateReset      time.Time
	failures       []*failure
	writes         []string
	requests       []string
	rulesets       []string // JSON of full rulesets
}

// NewGitHub starts a fake server for the test. By default the token looks like a classic OAuth
// token with the `repo` and `workflow` scopes and admin permission on a public repository.
func NewGitHub(t testing.TB) *GitHub {
	g := &GitHub{
		t: t, tags: map[string]string{}, branches: map[string]string{}, files: map[string]string{},
		nextID: 1000, rateRemaining: 5000, rateReset: time.Now().Add(time.Hour),
		perms: &Permissions{Admin: true, Maintain: true, Push: true, Triage: true, Pull: true},
	}
	g.SetScopes("repo", "workflow")
	g.srv = httptest.NewServer(http.HandlerFunc(g.serve))
	g.URL = g.srv.URL
	t.Cleanup(g.srv.Close)
	return g
}

// Transport returns a RoundTripper that sends every request to the fake server, whatever its host.
func (g *GitHub) Transport() http.RoundTripper { return rewrite{g.srv.URL} }

type rewrite struct{ target string }

func (r rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	u, _ := url.Parse(r.target)
	req = req.Clone(req.Context())
	req.URL.Scheme, req.URL.Host = u.Scheme, u.Host
	return http.DefaultTransport.RoundTrip(req)
}

// Tag seeds tags pointing at sha.
func (g *GitHub) Tag(sha string, names ...string) *GitHub {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, n := range names {
		g.tags[n] = sha
	}
	return g
}

// Branch seeds branches pointing at sha.
func (g *GitHub) Branch(sha string, names ...string) *GitHub {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, n := range names {
		g.branches[n] = sha
	}
	return g
}

// File seeds a repository file (served by the contents API at any ref).
func (g *GitHub) File(path, content string) *GitHub {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.files[path] = content
	return g
}

// MarketplaceFiles seeds a valid action.yaml and README.md.
func (g *GitHub) MarketplaceFiles() *GitHub {
	return g.File("action.yaml", "name: x\ndescription: y\nbranding:\n  icon: check\n  color: blue\nruns:\n  using: node20\n  main: i.js\n").File("README.md", "# x\n")
}

// Release seeds a release for tag; opts mutate it (Draft, Prerelease, Immutable, Latest).
func (g *GitHub) Release(tag string, opts ...func(*Release)) *Release {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.nextID++
	r := &Release{ID: g.nextID, Tag: tag}
	for _, o := range opts {
		o(r)
	}
	g.releases = append(g.releases, r)
	return r
}

// Release options.
func Draft(r *Release)      { r.Draft = true }
func Prerelease(r *Release) { r.Prerelease = true }
func Immutable(r *Release)  { r.Immutable = true }
func Latest(r *Release)     { r.Latest = true }

// SetScopes sets the X-OAuth-Scopes header (classic token). Calling it without arguments sends an empty header.
func (g *GitHub) SetScopes(scopes ...string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	s := strings.Join(scopes, ", ")
	g.scopes = &s
}

// NoScopeHeader makes the token look like a fine-grained PAT, GitHub App or GITHUB_TOKEN credential.
func (g *GitHub) NoScopeHeader() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.scopes = nil
}

// SetPermissions sets the permissions reported for the repository; nil omits the object (anonymous).
func (g *GitHub) SetPermissions(p *Permissions) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.perms = p
}

// SetRateLimit sets the X-RateLimit-Remaining/-Reset values returned on every response.
func (g *GitHub) SetRateLimit(remaining int, reset time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.rateRemaining, g.rateReset = remaining, reset
}

// Fail makes requests whose method matches (empty: any) and whose path+query contains substr
// answer with status and body. times limits how often it triggers (0: always).
func (g *GitHub) Fail(method, substr string, status int, body string, times int, header ...string) {
	h := http.Header{}
	for i := 0; i+1 < len(header); i += 2 {
		h.Set(header[i], header[i+1])
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.failures = append(g.failures, &failure{method: method, contains: substr, status: status, body: body, header: h, times: times})
}

// Ruleset seeds a tag ruleset (raw JSON of the GET /rulesets/{id} response) that is also listed by GET /rulesets.
func (g *GitHub) Ruleset(rawJSON string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.rulesets = append(g.rulesets, rawJSON)
}

// Writes returns the mutating requests received so far, e.g. "POST /git/refs refs/tags/v1 <sha>",
// "PATCH /git/refs/tags/v1 <sha>", "DELETE /git/refs/tags/v1", "POST /releases v1.0.0 draft=false".
func (g *GitHub) Writes() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.writes...)
}

// Requests returns every request received ("METHOD /path?query").
func (g *GitHub) Requests() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.requests...)
}

// TagSha returns the SHA of a tag, "" when it does not exist.
func (g *GitHub) TagSha(name string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.tags[name]
}

// BranchSha returns the SHA of a branch, "" when it does not exist.
func (g *GitHub) BranchSha(name string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.branches[name]
}

// Tags returns the sorted tag names.
func (g *GitHub) Tags() []string { return g.names(g.tags) }

// Branches returns the sorted branch names.
func (g *GitHub) Branches() []string { return g.names(g.branches) }

func (g *GitHub) names(m map[string]string) []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// FindRelease returns a copy of the release for tag, or nil.
func (g *GitHub) FindRelease(tag string) *Release {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, r := range g.releases {
		if r.Tag == tag {
			c := *r
			return &c
		}
	}
	return nil
}

func (g *GitHub) serve(w http.ResponseWriter, req *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	body, _ := io.ReadAll(req.Body)
	path := req.URL.Path
	g.requests = append(g.requests, req.Method+" "+req.URL.RequestURI())

	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("X-RateLimit-Limit", "5000")
	h.Set("X-RateLimit-Remaining", strconv.Itoa(g.rateRemaining))
	h.Set("X-RateLimit-Reset", strconv.FormatInt(g.rateReset.Unix(), 10))
	h.Set("X-RateLimit-Resource", "core")
	if g.scopes != nil {
		h.Set("X-OAuth-Scopes", *g.scopes)
	}

	for _, f := range g.failures {
		if (f.method == "" || f.method == req.Method) && strings.Contains(req.URL.RequestURI(), f.contains) && (f.times >= 0) {
			if f.times > 0 {
				f.times--
				if f.times == 0 {
					f.times = -1
				}
			}
			for k, v := range f.header {
				h[k] = v
			}
			w.WriteHeader(f.status)
			io.WriteString(w, f.body)
			return
		}
	}

	if path == "/graphql" || strings.HasSuffix(path, "/api/graphql") {
		g.graphql(w, body)
		return
	}
	if m := repoPath.FindStringSubmatchIndex(path); m != nil {
		path = path[m[1]:]
	} else {
		g.reply(w, 404, map[string]string{"message": "Not Found"})
		return
	}
	g.rest(w, req, path, body)
}

func (g *GitHub) reply(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func (g *GitHub) rest(w http.ResponseWriter, req *http.Request, path string, body []byte) {
	method := req.Method
	notFound := func() { g.reply(w, 404, map[string]string{"message": "Not Found"}) }
	record := func(s string) { g.writes = append(g.writes, s) }
	var in map[string]any
	if len(body) > 0 {
		_ = json.Unmarshal(body, &in)
	}
	str := func(k string) string { s, _ := in[k].(string); return s }

	switch {
	case path == "" && method == "GET":
		r := map[string]any{"private": g.Private, "full_name": Owner + "/" + Repo}
		if g.perms != nil {
			r["permissions"] = map[string]bool{"admin": g.perms.Admin, "maintain": g.perms.Maintain, "push": g.perms.Push, "triage": g.perms.Triage, "pull": g.perms.Pull}
		}
		g.reply(w, 200, r)
	case path == "/git/refs/tags" && method == "GET":
		g.reply(w, 200, g.refList(g.tags, "refs/tags/"))
	case path == "/branches" && method == "GET":
		out := []map[string]any{}
		for _, n := range sorted(g.branches) {
			out = append(out, map[string]any{"name": n, "commit": map[string]string{"sha": g.branches[n]}})
		}
		g.reply(w, 200, out)
	case path == "/git/refs" && method == "POST":
		ref, sha := str("ref"), str("sha")
		record(fmt.Sprintf("POST /git/refs %s %s", ref, sha))
		m, name := g.refMap(ref)
		if _, ok := m[name]; ok {
			g.reply(w, 422, map[string]string{"message": "Reference already exists"})
			return
		}
		m[name] = sha
		g.reply(w, 201, map[string]any{"ref": ref})
	case strings.HasPrefix(path, "/git/refs/") && (method == "PATCH" || method == "DELETE"):
		ref := strings.TrimPrefix(path, "/git/")
		m, name := g.refMap(ref)
		if _, ok := m[name]; !ok {
			g.reply(w, 422, map[string]string{"message": "Reference does not exist"})
			return
		}
		if method == "DELETE" {
			record("DELETE /git/" + ref)
			delete(m, name)
			w.WriteHeader(204)
			return
		}
		record(fmt.Sprintf("PATCH /git/%s %s", ref, str("sha")))
		m[name] = str("sha")
		g.reply(w, 200, map[string]any{"ref": ref})
	case path == "/immutable-releases" && method == "GET":
		g.reply(w, 200, map[string]any{"enabled": g.ImmutableEnabled})
	case path == "/immutable-releases" && method == "PUT":
		record("PUT /immutable-releases")
		g.ImmutableEnabled = true
		w.WriteHeader(204)
	case path == "/rulesets" && method == "GET":
		out := []json.RawMessage{}
		for _, r := range g.rulesets {
			out = append(out, json.RawMessage(r))
		}
		g.reply(w, 200, out)
	case strings.HasPrefix(path, "/rulesets/") && method == "GET":
		id := strings.TrimPrefix(path, "/rulesets/")
		for _, r := range g.rulesets {
			var m struct {
				ID int64 `json:"id"`
			}
			_ = json.Unmarshal([]byte(r), &m)
			if strconv.FormatInt(m.ID, 10) == id {
				w.WriteHeader(200)
				io.WriteString(w, r)
				return
			}
		}
		notFound()
	case path == "/releases" && method == "POST":
		tag := str("tag_name")
		draft, _ := in["draft"].(bool)
		record(fmt.Sprintf("POST /releases %s draft=%v", tag, draft))
		g.nextID++
		r := &Release{ID: g.nextID, Tag: tag, Draft: draft, Immutable: !draft && g.ImmutableEnabled}
		if ml := str("make_latest"); ml == "true" {
			g.setLatest(r)
		}
		g.releases = append(g.releases, r)
		g.reply(w, 201, g.restRelease(r))
	case strings.HasPrefix(path, "/releases/tags/") && method == "GET":
		tag, _ := url.PathUnescape(strings.TrimPrefix(path, "/releases/tags/"))
		for _, r := range g.releases {
			if r.Tag == tag {
				g.reply(w, 200, g.restRelease(r))
				return
			}
		}
		notFound()
	case strings.HasPrefix(path, "/releases/"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(path, "/releases/"), 10, 64)
		idx := -1
		for i, r := range g.releases {
			if r.ID == id {
				idx = i
			}
		}
		if idx < 0 {
			notFound()
			return
		}
		r := g.releases[idx]
		switch method {
		case "DELETE":
			record("DELETE /releases " + r.Tag)
			g.releases = append(g.releases[:idx], g.releases[idx+1:]...)
			w.WriteHeader(204)
		case "PATCH":
			if d, ok := in["draft"].(bool); ok {
				record(fmt.Sprintf("PATCH /releases %s draft=%v", r.Tag, d))
				r.Draft = d
				r.Immutable = !d && g.ImmutableEnabled
			}
			if str("make_latest") == "true" {
				record("PATCH /releases " + r.Tag + " make_latest")
				g.setLatest(r)
			}
			g.reply(w, 200, g.restRelease(r))
		default:
			g.reply(w, 200, g.restRelease(r))
		}
	case strings.HasPrefix(path, "/contents") && (method == "GET" || method == "HEAD"):
		g.contents(w, strings.Trim(strings.TrimPrefix(path, "/contents"), "/"), method)
	default:
		g.reply(w, 404, map[string]string{"message": "Not Found: " + method + " " + path})
	}
}

func (g *GitHub) setLatest(r *Release) {
	for _, o := range g.releases {
		o.Latest = false
	}
	r.Latest = true
}

func (g *GitHub) restRelease(r *Release) map[string]any {
	return map[string]any{
		"id": r.ID, "tag_name": r.Tag, "draft": r.Draft, "prerelease": r.Prerelease,
		"html_url": fmt.Sprintf("%s/%s/%s/releases/tag/%s", g.URL, Owner, Repo, r.Tag),
	}
}

func (g *GitHub) contents(w http.ResponseWriter, p, method string) {
	if p == "" {
		seen := map[string]bool{}
		out := []map[string]string{}
		for _, name := range sorted(g.files) {
			top, rest, _ := strings.Cut(name, "/")
			if seen[top] {
				continue
			}
			seen[top] = true
			typ := "file"
			if rest != "" {
				typ = "dir"
			}
			out = append(out, map[string]string{"name": top, "path": top, "type": typ, "sha": "abc"})
		}
		g.reply(w, 200, out)
		return
	}
	content, ok := g.files[p]
	if !ok {
		g.reply(w, 404, map[string]string{"message": "Not Found"})
		return
	}
	if method == "HEAD" {
		w.WriteHeader(200)
		return
	}
	g.reply(w, 200, map[string]any{"type": "file", "name": p, "path": p, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(content))})
}

func (g *GitHub) graphql(w http.ResponseWriter, body []byte) {
	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	_ = json.Unmarshal(body, &req)
	rate := map[string]any{"cost": 1, "remaining": 4999, "resetAt": g.rateReset.UTC().Format(time.RFC3339)}
	if strings.Contains(req.Query, "releases(") {
		nodes := []map[string]any{}
		for _, r := range g.releases {
			nodes = append(nodes, map[string]any{"databaseId": r.ID, "tagName": r.Tag, "isPrerelease": r.Prerelease, "isDraft": r.Draft, "immutable": r.Immutable, "isLatest": r.Latest})
		}
		g.reply(w, 200, map[string]any{"data": map[string]any{"rateLimit": rate, "repository": map[string]any{"releases": map[string]any{
			"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""}, "nodes": nodes}}}})
		return
	}
	tag, _ := req.Variables["tag"].(string)
	var rel any
	for _, r := range g.releases {
		if r.Tag == tag {
			rel = map[string]any{"tagName": r.Tag, "isDraft": r.Draft, "immutable": r.Immutable}
		}
	}
	g.reply(w, 200, map[string]any{"data": map[string]any{"rateLimit": rate, "repository": map[string]any{"release": rel}}})
}

func (g *GitHub) refMap(ref string) (map[string]string, string) {
	if n, ok := strings.CutPrefix(ref, "refs/tags/"); ok {
		return g.tags, n
	}
	return g.branches, strings.TrimPrefix(ref, "refs/heads/")
}

func (g *GitHub) refList(m map[string]string, prefix string) []map[string]any {
	out := []map[string]any{}
	for _, n := range sorted(m) {
		out = append(out, map[string]any{"ref": prefix + n, "object": map[string]string{"type": "commit", "sha": m[n]}})
	}
	return out
}

func sorted(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
