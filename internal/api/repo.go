package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/jessehouwing/gh-actions-version/internal/state"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

func itoa(i int64) string { return strconv.FormatInt(i, 10) }

func pathEscape(s string) string { return url.PathEscape(s) }

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

func asAPIError(err error, target **APIError) bool {
	for err != nil {
		if ae, ok := err.(*APIError); ok {
			*target = ae
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func withRef(path, ref string) string {
	if ref == "" {
		return path
	}
	return path + "?ref=" + url.QueryEscape(ref)
}

// GetFileContents returns a file's decoded content (Get-GitHubFileContents). found is false
// when the file does not exist (404) or is not a base64 file.
func (c *Client) GetFileContents(ctx context.Context, path, ref string) (content string, found bool, err error) {
	var f struct {
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	err = c.do(ctx, http.MethodGet, withRef(c.repoPath("/contents/"+escapePath(path)), ref), nil, &f)
	if IsNotFound(err) {
		c.logf("File %s not found in repository", path)
		return "", false, nil
	}
	if err != nil {
		return "", false, wrap("fetching file "+path, err)
	}
	if f.Encoding != "base64" || f.Content == "" {
		return "", false, nil
	}
	b, derr := base64.StdEncoding.DecodeString(strings.NewReplacer("\n", "", "\r", "").Replace(f.Content))
	if derr != nil {
		return "", false, wrap("decoding file "+path, derr)
	}
	return string(b), true, nil
}

// FileExists checks existence with a HEAD request (Test-GitHubFileExists). Non-404 errors other
// than rate limiting report false.
func (c *Client) FileExists(ctx context.Context, path, ref string) (bool, error) {
	resp, err := c.request(ctx, http.MethodHead, withRef(c.repoPath("/contents/"+escapePath(path)), ref), nil)
	if err != nil {
		if IsRateLimit(err) {
			return false, err
		}
		return false, nil
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK, nil
}

// DirEntry is an item of a directory listing.
type DirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
	Sha  string `json:"sha"`
}

// GetDirectoryContents lists a directory ("" or "/" for the root) (Get-GitHubDirectoryContents).
// A missing directory or a path that is a file yields an empty list.
func (c *Client) GetDirectoryContents(ctx context.Context, path, ref string) ([]DirEntry, error) {
	p := c.repoPath("/contents")
	if path != "" && path != "/" && path != "." {
		p += "/" + escapePath(strings.Trim(path, "/"))
	}
	var raw json.RawMessage
	err := c.do(ctx, http.MethodGet, withRef(p, ref), nil, &raw)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, wrap("listing directory "+path, err)
	}
	if t := strings.TrimSpace(string(raw)); !strings.HasPrefix(t, "[") {
		return nil, nil // a single file object
	}
	var out []DirEntry
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// MatchRefNamePattern is Test-RefNamePattern: fnmatch-style where ** crosses '/', * and ? do not;
// "~ALL" matches everything.
func MatchRefNamePattern(pattern, ref string) bool {
	if pattern == "~ALL" {
		return true
	}
	e := regexp.QuoteMeta(pattern)
	e = strings.ReplaceAll(e, `\*\*`, ".*")
	e = strings.ReplaceAll(e, `\*`, "[^/]*")
	e = strings.ReplaceAll(e, `\?`, "[^/]")
	re, err := regexp.Compile("(?i)^" + e + "$")
	return err == nil && re.MatchString(ref)
}

// TagProtectionRulesets finds the active tag rulesets that block deleting tag
// (Get-GitHubTagProtection). It needs permission to read rulesets; failures are returned.
func (c *Client) TagRulesets(ctx context.Context, tag string) ([]state.Ruleset, error) {
	full := "refs/tags/" + tag
	var summaries []struct {
		ID          int64  `json:"id"`
		Target      string `json:"target"`
		Enforcement string `json:"enforcement"`
	}
	err := c.getPages(ctx, c.repoPath("/rulesets?includes_parents=true&per_page=100"), func(b []byte) error {
		var page []struct {
			ID          int64  `json:"id"`
			Target      string `json:"target"`
			Enforcement string `json:"enforcement"`
		}
		if err := json.Unmarshal(b, &page); err != nil {
			return err
		}
		summaries = append(summaries, page...)
		return nil
	})
	if err != nil {
		return nil, wrap("listing rulesets", err)
	}
	var out []state.Ruleset
	for _, s := range summaries {
		if s.Target != "tag" || s.Enforcement == "disabled" {
			continue
		}
		var rs struct {
			ID          int64  `json:"id"`
			Name        string `json:"name"`
			Enforcement string `json:"enforcement"`
			SourceType  string `json:"source_type"`
			Source      string `json:"source"`
			Rules       []struct {
				Type string `json:"type"`
			} `json:"rules"`
			Conditions struct {
				RefName struct {
					Include []string `json:"include"`
					Exclude []string `json:"exclude"`
				} `json:"ref_name"`
			} `json:"conditions"`
		}
		if err := c.do(ctx, http.MethodGet, c.repoPath("/rulesets/"+itoa(s.ID)), nil, &rs); err != nil {
			return nil, wrap("getting ruleset "+itoa(s.ID), err)
		}
		hasDeletion := false
		for _, r := range rs.Rules {
			if r.Type == "deletion" {
				hasDeletion = true
			}
		}
		if !hasDeletion {
			continue
		}
		matches := func(ps []string) bool {
			for _, p := range ps {
				if p != "" && MatchRefNamePattern(p, full) {
					return true
				}
			}
			return false
		}
		if !matches(rs.Conditions.RefName.Include) || matches(rs.Conditions.RefName.Exclude) {
			continue
		}
		r := state.Ruleset{ID: rs.ID, Name: rs.Name, Enforcement: rs.Enforcement}
		if rs.SourceType == "Organization" {
			r.Source = "organization " + rs.Source
			r.Url = c.serverURL + "/organizations/" + rs.Source + "/settings/rules/" + itoa(rs.ID)
		} else {
			r.Source = "repository " + c.Repo.String()
			r.Url = c.serverURL + "/" + c.Repo.String() + "/rules/" + itoa(rs.ID)
		}
		out = append(out, r)
	}
	return out, nil
}

// TagProtection implements state.Client: names of rulesets blocking deletion of tag. Failures to
// read rulesets (e.g. missing permission) yield an empty list, like the PowerShell version;
// rate limit errors are retained in Fatal().
func (c *Client) TagProtection(ctx context.Context, tag string) ([]string, error) {
	rs, err := c.TagRulesets(ctx, tag)
	if err != nil {
		c.logf("Failed to query tag protection for %s: %v", tag, err)
		return nil, nil
	}
	names := make([]string, 0, len(rs))
	for _, r := range rs {
		names = append(names, r.Name)
	}
	return names, nil
}

func decodeJSON(r io.Reader, v any) error { return json.NewDecoder(r).Decode(v) }
