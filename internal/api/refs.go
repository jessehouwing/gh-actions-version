package api

import (
	"context"
	"net/http"
	"regexp"
	"strings"

	"github.com/jessehouwing/gh-actions-version/internal/state"
)

type refObject struct {
	Type string `json:"type"`
	Sha  string `json:"sha"`
}

type refItem struct {
	Ref    string    `json:"ref"`
	Object refObject `json:"object"`
}

// deref resolves annotated tag objects to the commit they point to.
func (c *Client) deref(ctx context.Context, obj refObject, name string) (string, error) {
	for i := 0; obj.Type == "tag" && i < 5; i++ {
		var t struct {
			Object refObject `json:"object"`
		}
		if err := c.do(ctx, http.MethodGet, c.repoPath("/git/tags/"+obj.Sha), nil, &t); err != nil {
			return "", wrap("dereferencing annotated tag "+name, err)
		}
		obj = t.Object
	}
	return obj.Sha, nil
}

func compile(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, nil
	}
	return regexp.Compile(pattern)
}

// GetTags fetches tags (Get-GitHubTag). pattern is an optional regex on the tag name. A 404
// (repository without tags) yields an empty result.
func (c *Client) GetTags(ctx context.Context, pattern string) ([]*state.VersionRef, error) {
	re, err := compile(pattern)
	if err != nil {
		return nil, err
	}
	var out []*state.VersionRef
	err = c.getPages(ctx, c.repoPath("/git/refs/tags?per_page=100"), func(b []byte) error {
		refs, err := decodeRefs(b)
		if err != nil {
			return err
		}
		for _, r := range refs {
			name := strings.TrimPrefix(r.Ref, "refs/tags/")
			if re != nil && !re.MatchString(name) {
				continue
			}
			sha, err := c.deref(ctx, r.Object, name)
			if err != nil {
				return err
			}
			vr := state.NewVersionRef(name, "refs/tags/"+name, sha, state.TypeTag)
			vr.IsIgnored = c.ignored(name)
			out = append(out, vr)
		}
		return nil
	})
	if IsNotFound(err) {
		c.logf("No tags found in repository (404 response - this is normal for repos with no tags)")
		return nil, nil
	}
	if err != nil {
		return nil, wrapOnce("fetching tags", err)
	}
	return out, nil
}

// wrapOnce wraps err unless it is already an *APIError or a rate limit error.
func wrapOnce(op string, err error) error {
	var ae *APIError
	if asAPIError(err, &ae) || IsRateLimit(err) {
		return err
	}
	return wrap(op, err)
}

// GetBranches fetches branches (Get-GitHubBranch).
func (c *Client) GetBranches(ctx context.Context, pattern string) ([]*state.VersionRef, error) {
	re, err := compile(pattern)
	if err != nil {
		return nil, err
	}
	var out []*state.VersionRef
	err = c.getPages(ctx, c.repoPath("/branches?per_page=100"), func(b []byte) error {
		var items []struct {
			Name   string `json:"name"`
			Commit struct {
				Sha string `json:"sha"`
			} `json:"commit"`
		}
		if err := jsonUnmarshal(b, &items); err != nil {
			return err
		}
		for _, br := range items {
			if re != nil && !re.MatchString(br.Name) {
				continue
			}
			vr := state.NewVersionRef(br.Name, "refs/heads/"+br.Name, br.Commit.Sha, state.TypeBranch)
			vr.IsIgnored = c.ignored(br.Name)
			out = append(out, vr)
		}
		return nil
	})
	if err != nil {
		return nil, wrapOnce("fetching branches", err)
	}
	return out, nil
}

// decodeRefs accepts both an array and a single object (GitHub returns an object when only
// one ref matches a prefix exactly).
func decodeRefs(b []byte) ([]refItem, error) {
	trim := strings.TrimSpace(string(b))
	if strings.HasPrefix(trim, "[") {
		var items []refItem
		return items, jsonUnmarshal(b, &items)
	}
	var one refItem
	if err := jsonUnmarshal(b, &one); err != nil {
		return nil, err
	}
	return []refItem{one}, nil
}

// GetRef returns the commit SHA of a ref (Get-GitHubRef). refType is "tags" or "heads". It
// returns "" and no error when the ref does not exist.
func (c *Client) GetRef(ctx context.Context, name, refType string) (string, error) {
	var r refItem
	err := c.do(ctx, http.MethodGet, c.repoPath("/git/refs/"+refType+"/"+escapePath(name)), nil, &r)
	if IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", wrap("fetching ref "+refType+"/"+name, err)
	}
	return c.deref(ctx, r.Object, name)
}

func (c *Client) setKnown(ref string, v bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v {
		c.known[ref] = true
	} else {
		delete(c.known, ref)
	}
}

func (c *Client) isKnown(ref string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.known[ref]
}

// TrackRefs records refs that are known to exist so CreateRef updates (PATCH) instead of
// creating (POST) them. Load calls this.
func (c *Client) TrackRefs(refs ...*state.VersionRef) {
	for _, r := range refs {
		c.setKnown(r.Ref, true)
	}
}

func refNotExist(err error) bool {
	switch StatusCode(err) {
	case 404:
		return true
	case 422:
		return strings.Contains(ErrorDetail(err), "Reference does not exist")
	}
	return false
}

// CreateRef creates or force-updates a ref (New-GitHubRef). The REST fallback to `git push` of
// the PowerShell version is intentionally not ported.
func (c *Client) CreateRef(ctx context.Context, ref, sha string, force bool) state.RefResult {
	create := func() error {
		return c.do(ctx, http.MethodPost, c.repoPath("/git/refs"), map[string]any{"ref": ref, "sha": sha}, nil)
	}
	update := func() error {
		return c.do(ctx, http.MethodPatch, c.repoPath("/git/"+escapePath(ref)), map[string]any{"sha": sha, "force": force}, nil)
	}
	var err error
	if c.isKnown(ref) {
		err = update()
		if err != nil && refNotExist(err) {
			err = create()
		}
	} else {
		err = create()
		if err != nil && StatusCode(err) == 422 && strings.Contains(ErrorDetail(err), "Reference already exists") {
			err = update()
		}
	}
	if err != nil {
		return c.refFailure("create/update ref "+ref, ref, err)
	}
	c.setKnown(ref, true)
	return state.RefResult{Success: true}
}

// DeleteRef deletes a ref (Remove-GitHubRef).
func (c *Client) DeleteRef(ctx context.Context, ref string) state.RefResult {
	if err := c.do(ctx, http.MethodDelete, c.repoPath("/git/"+escapePath(ref)), nil, nil); err != nil {
		return c.refFailure("delete ref "+ref, ref, err)
	}
	c.setKnown(ref, false)
	return state.RefResult{Success: true}
}

func (c *Client) refFailure(op, ref string, err error) state.RefResult {
	res := state.RefResult{Error: ErrorDetail(err)}
	if IsRateLimit(err) {
		res.Error = err.Error()
		return res
	}
	if StatusCode(err) == 403 {
		res.RequiresManualFix = true
		res.Error = "REST API returned 403 for " + ref + ": " + ErrorDetail(err)
	}
	c.logf("Failed to %s: %s", op, res.Error)
	return res
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = pathEscape(s)
	}
	return strings.Join(parts, "/")
}
