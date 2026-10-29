package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// Mode is the kind of run the token has to support.
type Mode string

const (
	// ModeCheck is read-only (--check).
	ModeCheck Mode = "check"
	// ModeFix writes refs and releases (auto-fix and interactive).
	ModeFix Mode = "fix"
)

// TokenKind describes what we learned about the credential.
type TokenKind string

const (
	// TokenClassic tokens (gh auth login, classic PATs) expose X-OAuth-Scopes.
	TokenClassic TokenKind = "classic"
	// TokenOpaque covers fine-grained PATs, GitHub App and GITHUB_TOKEN credentials, which expose no scopes.
	TokenOpaque    TokenKind = "fine-grained/app"
	TokenAnonymous TokenKind = "anonymous"
)

// MissingScope is a missing OAuth scope or repository permission.
type MissingScope struct {
	Scope    string // e.g. "repo", "workflow", "contents: write", "admin"
	Reason   string
	Optional bool // the run can continue in a degraded way
	// Refreshable is true for real OAuth scopes that `gh auth refresh -s` can add.
	Refreshable bool
}

// RepoPermissions is the permissions object of GET /repos/{owner}/{repo}.
type RepoPermissions struct {
	Admin, Maintain, Push, Triage, Pull bool
}

// AuthResult is the outcome of CheckAuth.
type AuthResult struct {
	Host      string
	Mode      Mode
	TokenKind TokenKind
	Scopes    []string // granted scopes (classic tokens only)
	Private   bool
	// Permissions is nil when the API did not report any (anonymous access).
	Permissions *RepoPermissions
	Missing     []MissingScope
	// RefreshCommand is the suggested `gh auth refresh` command (classic tokens), empty if none.
	RefreshCommand string
	// CIGuidance is the equivalent advice for CI and non-classic tokens.
	CIGuidance string
	// Anonymous requests are limited to 60 requests per hour.
	Anonymous bool
}

// OK is true when nothing blocking is missing.
func (r *AuthResult) OK() bool { return len(r.BlockingMissing()) == 0 }

// BlockingMissing returns the missing items that prevent the run (exit code 2).
func (r *AuthResult) BlockingMissing() []MissingScope { return r.filter(false) }

// OptionalMissing returns missing items that only degrade functionality.
func (r *AuthResult) OptionalMissing() []MissingScope { return r.filter(true) }

func (r *AuthResult) filter(optional bool) []MissingScope {
	var out []MissingScope
	for _, m := range r.Missing {
		if m.Optional == optional {
			out = append(out, m)
		}
	}
	return out
}

// Message formats the missing items and suggestions for display.
func (r *AuthResult) Message() string {
	if len(r.Missing) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, m := range r.Missing {
		kind := "missing"
		if m.Optional {
			kind = "optional, missing"
		}
		fmt.Fprintf(&sb, "- %s (%s): %s\n", m.Scope, kind, m.Reason)
	}
	if r.RefreshCommand != "" {
		fmt.Fprintf(&sb, "Run: %s\n", r.RefreshCommand)
	}
	if r.CIGuidance != "" {
		fmt.Fprintf(&sb, "%s\n", r.CIGuidance)
	}
	return strings.TrimRight(sb.String(), "\n")
}

// AuthError is returned by CheckAuth when the credential cannot be used at all (401) or the
// repository is not reachable (404).
type AuthError struct {
	Status  int
	Message string
	Refresh string
}

func (e *AuthError) Error() string { return e.Message }

// CheckAuth verifies the token against the repository before any API work, using one cheap
// call (GET /repos/{owner}/{repo}), which returns both X-OAuth-Scopes and the permissions
// object. Classic/OAuth tokens are checked by scope; fine-grained, App and GITHUB_TOKEN
// credentials do not expose scopes, so their repository permissions are probed instead.
func CheckAuth(ctx context.Context, c *Client, mode Mode) (*AuthResult, error) {
	res := &AuthResult{Host: c.Repo.Host, Mode: mode}
	if res.Host == "" {
		res.Host = "github.com"
	}
	refreshCmd := func(scopes ...string) string {
		return fmt.Sprintf("gh auth refresh -h %s -s %s", res.Host, strings.Join(scopes, ","))
	}

	resp, err := c.request(ctx, http.MethodGet, c.repoPath(""), nil)
	if err != nil {
		switch StatusCode(err) {
		case 401:
			return nil, &AuthError{Status: 401, Message: "authentication failed (HTTP 401): the token is invalid or expired", Refresh: "gh auth login -h " + res.Host}
		case 404:
			return nil, &AuthError{Status: 404, Message: fmt.Sprintf("repository %s not found on %s, or the token cannot access it (private repositories need the 'repo' scope)", c.Repo, res.Host), Refresh: refreshCmd("repo")}
		}
		return nil, err
	}
	defer resp.Body.Close()
	var raw struct {
		Private     bool `json:"private"`
		Permissions *struct {
			Admin    bool `json:"admin"`
			Maintain bool `json:"maintain"`
			Push     bool `json:"push"`
			Triage   bool `json:"triage"`
			Pull     bool `json:"pull"`
		} `json:"permissions"`
	}
	if err := decodeJSON(resp.Body, &raw); err != nil {
		return nil, wrap("reading repository", err)
	}
	res.Private = raw.Private
	if raw.Permissions != nil {
		p := RepoPermissions(*raw.Permissions)
		res.Permissions = &p
	}

	scopeHeader, hasScopes := resp.Header[http.CanonicalHeaderKey("X-OAuth-Scopes")]
	switch {
	case hasScopes:
		res.TokenKind = TokenClassic
		for _, s := range strings.Split(strings.Join(scopeHeader, ","), ",") {
			if s = strings.TrimSpace(s); s != "" {
				res.Scopes = append(res.Scopes, s)
			}
		}
		sort.Strings(res.Scopes)
	case res.Permissions == nil:
		res.TokenKind = TokenAnonymous
		res.Anonymous = true
	default:
		res.TokenKind = TokenOpaque
	}

	has := func(names ...string) bool {
		for _, s := range res.Scopes {
			for _, n := range names {
				if s == n {
					return true
				}
			}
		}
		return false
	}
	var refresh []string

	switch res.TokenKind {
	case TokenClassic:
		// 'repo' covers private repositories; public_repo is enough for public ones.
		hasRepo := has("repo")
		if mode == ModeFix && !hasRepo && (res.Private || !has("public_repo")) {
			res.Missing = append(res.Missing, MissingScope{Scope: "repo", Reason: "needed to push tags/branches and create, publish or delete releases", Refreshable: true})
			refresh = append(refresh, "repo")
		}
		if mode == ModeCheck && !hasRepo && res.Private {
			res.Missing = append(res.Missing, MissingScope{Scope: "repo", Reason: "needed to read a private repository", Refreshable: true})
			refresh = append(refresh, "repo")
		}
		if mode == ModeFix && !has("workflow") {
			res.Missing = append(res.Missing, MissingScope{Scope: "workflow", Reason: "needed to update tags that point to commits changing .github/workflows; those fixes become manual", Optional: true, Refreshable: true})
			refresh = append(refresh, "workflow")
		}
		if res.Permissions != nil && !res.Permissions.Admin {
			res.Missing = append(res.Missing, MissingScope{Scope: "admin", Reason: "repository admin permission is needed to enable immutable releases and read rulesets", Optional: true})
		}
	case TokenOpaque:
		if mode == ModeFix && !(res.Permissions.Push || res.Permissions.Maintain || res.Permissions.Admin) {
			res.Missing = append(res.Missing, MissingScope{Scope: "contents: write", Reason: "the token has no write access to the repository"})
			res.CIGuidance = "Grant 'permissions: contents: write' to the workflow, or provide a PAT / GitHub App token with write access (GH_TOKEN)."
		}
		if !res.Permissions.Admin {
			res.Missing = append(res.Missing, MissingScope{Scope: "admin", Reason: "repository admin permission is needed to enable immutable releases and read rulesets", Optional: true})
		}
	case TokenAnonymous:
		if mode == ModeFix {
			res.Missing = append(res.Missing, MissingScope{Scope: "authentication", Reason: "no credentials were sent; anonymous access is read-only and limited to 60 requests/hour"})
			res.RefreshCommand = "gh auth login -h " + res.Host
			res.CIGuidance = "Set GH_TOKEN (or GITHUB_TOKEN) to a token with write access."
		}
	}
	if len(refresh) > 0 {
		res.RefreshCommand = refreshCmd(refresh...)
		res.CIGuidance = fmt.Sprintf("In CI, use a PAT with the scopes %s (GITHUB_TOKEN cannot update workflow files) and grant 'permissions: contents: write'.", strings.Join(refresh, ", "))
	}
	return res, nil
}
