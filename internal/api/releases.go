package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/jessehouwing/gh-actions-version/internal/state"
)

// ReleasesPageSize is the GraphQL page size used when listing releases.
const ReleasesPageSize = 100

// ReleasesQuery lists releases with immutability/latest metadata. The rateLimit block lets the
// client observe query cost.
const ReleasesQuery = `query($owner: String!, $name: String!, $first: Int!, $after: String) {
  rateLimit {
    cost
    remaining
    resetAt
  }
  repository(owner: $owner, name: $name) {
    releases(first: $first, after: $after, orderBy: {field: CREATED_AT, direction: DESC}) {
      pageInfo {
        hasNextPage
        endCursor
      }
      nodes {
        databaseId
        tagName
        isPrerelease
        isDraft
        immutable
        isLatest
      }
    }
  }
}`

// ReleaseImmutabilityQuery checks a single release.
const ReleaseImmutabilityQuery = `query($owner: String!, $name: String!, $tag: String!) {
  rateLimit {
    cost
    remaining
    resetAt
  }
  repository(owner: $owner, name: $name) {
    release(tagName: $tag) {
      tagName
      isDraft
      immutable
    }
  }
}`

type releaseNode struct {
	DatabaseID   int64  `json:"databaseId"`
	TagName      string `json:"tagName"`
	IsPrerelease bool   `json:"isPrerelease"`
	IsDraft      bool   `json:"isDraft"`
	Immutable    bool   `json:"immutable"`
	IsLatest     bool   `json:"isLatest"`
}

// toReleaseInfo maps a GraphQL node to the domain object.
func toReleaseInfo(n releaseNode) *state.ReleaseInfo {
	return &state.ReleaseInfo{
		TagName: n.TagName, ID: n.DatabaseID, IsDraft: n.IsDraft, IsPrerelease: n.IsPrerelease,
		IsImmutable: n.Immutable, IsLatest: n.IsLatest,
	}
}

// GetReleases lists all releases via GraphQL (Get-GitHubRelease).
func (c *Client) GetReleases(ctx context.Context) ([]*state.ReleaseInfo, error) {
	stage := c.startProgress("Loading releases")
	result := "failed"
	defer func() { stage.Done(result) }()
	var out []*state.ReleaseInfo
	var after *string
	pages := 0
	for {
		vars := map[string]any{"owner": c.Repo.Owner, "name": c.Repo.Name, "first": ReleasesPageSize, "after": after}
		var resp struct {
			RateLimit  *GraphQLRateLimit `json:"rateLimit"`
			Repository struct {
				Releases struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []releaseNode `json:"nodes"`
				} `json:"releases"`
			} `json:"repository"`
		}
		if err := c.graphql(ctx, ReleasesQuery, vars, &resp); err != nil {
			return nil, wrapOnce("fetching releases", err)
		}
		c.setRate(resp.RateLimit)
		rel := resp.Repository.Releases
		for _, n := range rel.Nodes {
			ri := toReleaseInfo(n)
			ri.IsIgnored = c.ignored(ri.TagName)
			out = append(out, ri)
		}
		pages++
		stage.Update(fmt.Sprintf("page %d, %d releases loaded", pages, len(out)))
		if len(rel.Nodes) == 0 || !rel.PageInfo.HasNextPage {
			result = fmt.Sprintf("%d releases", len(out))
			return out, nil
		}
		cur := rel.PageInfo.EndCursor
		after = &cur
	}
}

// TestReleaseImmutability reports whether the release for tag is immutable
// (Test-ReleaseImmutability). A missing release yields false.
func (c *Client) ReleaseImmutable(ctx context.Context, tag string) (bool, error) {
	var resp struct {
		RateLimit  *GraphQLRateLimit `json:"rateLimit"`
		Repository struct {
			Release *struct {
				TagName   string `json:"tagName"`
				IsDraft   bool   `json:"isDraft"`
				Immutable bool   `json:"immutable"`
			} `json:"release"`
		} `json:"repository"`
	}
	vars := map[string]any{"owner": c.Repo.Owner, "name": c.Repo.Name, "tag": tag}
	if err := c.graphql(ctx, ReleaseImmutabilityQuery, vars, &resp); err != nil {
		return false, wrapOnce("release immutability check for "+tag, err)
	}
	c.setRate(resp.RateLimit)
	return resp.Repository.Release != nil && resp.Repository.Release.Immutable, nil
}

type restRelease struct {
	ID      int64  `json:"id"`
	TagName string `json:"tag_name"`
	Draft   bool   `json:"draft"`
	HtmlUrl string `json:"html_url"`
}

// GetReleaseByTag fetches a release via REST; nil when there is none.
func (c *Client) GetReleaseByTag(ctx context.Context, tag string) (*state.ReleaseInfo, error) {
	var r restRelease
	err := c.do(ctx, http.MethodGet, c.repoPath("/releases/tags/"+url.PathEscape(tag)), nil, &r)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, wrap("fetching release "+tag, err)
	}
	return &state.ReleaseInfo{TagName: r.TagName, ID: r.ID, IsDraft: r.Draft, HtmlUrl: r.HtmlUrl}, nil
}

func releaseFailure(err error) state.ReleaseResult {
	res := state.ReleaseResult{Error: ErrorDetail(err)}
	if IsRateLimit(err) {
		res.Error = err.Error()
	}
	res.Unfixable = IsImmutableReleaseError(err)
	return res
}

func makeLatestValue(b *bool) string {
	if *b {
		return "true"
	}
	return "false"
}

// CreateRelease creates a release. The release is a draft when draft is set and autoPublish is
// not; autoPublish creates it published (immutable) immediately.
func (c *Client) CreateRelease(ctx context.Context, tag string, draft bool, autoPublish bool) (state.ReleaseResult, error) {
	return c.CreateReleaseWithLatest(ctx, tag, draft && !autoPublish, nil)
}

// CreateReleaseWithOptions is New-GitHubRelease including make_latest.
func (c *Client) CreateReleaseWithLatest(ctx context.Context, tag string, draft bool, makeLatest *bool) (state.ReleaseResult, error) {
	body := map[string]any{"tag_name": tag, "name": tag, "body": "Release " + tag, "draft": draft}
	if makeLatest != nil {
		body["make_latest"] = makeLatestValue(makeLatest)
	}
	var r restRelease
	if err := c.do(ctx, http.MethodPost, c.repoPath("/releases"), body, &r); err != nil {
		c.logf("Failed to create release for %s: %s", tag, ErrorDetail(err))
		res := releaseFailure(err)
		if res.Unfixable {
			return res, fmt.Errorf("%w: %w", state.ErrUnfixable, err)
		}
		return res, err
	}
	return state.ReleaseResult{Success: true, ID: r.ID, HtmlUrl: r.HtmlUrl}, nil
}

// PublishRelease publishes a draft release (Publish-GitHubRelease).
func (c *Client) PublishRelease(ctx context.Context, id int64, makeLatest *bool) (state.ReleaseResult, error) {
	body := map[string]any{"draft": false}
	if makeLatest != nil {
		body["make_latest"] = makeLatestValue(makeLatest)
	}
	var r restRelease
	if err := c.do(ctx, http.MethodPatch, c.repoPath("/releases/"+itoa(id)), body, &r); err != nil {
		c.logf("Failed to publish release %d: %s", id, ErrorDetail(err))
		return releaseFailure(err), err
	}
	return state.ReleaseResult{Success: true, ID: id, HtmlUrl: r.HtmlUrl}, nil
}

// DeleteRelease deletes a release by id (Remove-GitHubRelease).
func (c *Client) DeleteRelease(ctx context.Context, id int64) error {
	if err := c.do(ctx, http.MethodDelete, c.repoPath("/releases/"+itoa(id)), nil, nil); err != nil {
		return wrap("deleting release "+itoa(id), err)
	}
	return nil
}

// SetReleaseLatest marks a release as latest (Set-GitHubReleaseLatest).
func (c *Client) SetReleaseLatest(ctx context.Context, id int64) error {
	t := true
	if err := c.do(ctx, http.MethodPatch, c.repoPath("/releases/"+itoa(id)), map[string]any{"make_latest": makeLatestValue(&t)}, nil); err != nil {
		return wrap("setting release "+itoa(id)+" as latest", err)
	}
	return nil
}

// RepublishRelease turns a mutable release into an immutable one by converting it to a draft and
// publishing it again (Republish-GitHubRelease).
func (c *Client) RepublishRelease(ctx context.Context, tag string, makeLatest *bool) state.RepublishResult {
	var rel restRelease
	if err := c.do(ctx, http.MethodGet, c.repoPath("/releases/tags/"+url.PathEscape(tag)), nil, &rel); err != nil {
		return state.RepublishResult{Reason: ErrorDetail(err)}
	}
	immutable, err := c.ReleaseImmutable(ctx, tag)
	if err != nil {
		return state.RepublishResult{Reason: err.Error()}
	}
	if immutable {
		return state.RepublishResult{Success: true, Reason: "Already immutable"}
	}
	if !rel.Draft {
		if err := c.do(ctx, http.MethodPatch, c.repoPath("/releases/"+itoa(rel.ID)), map[string]any{"draft": true}, nil); err != nil {
			return state.RepublishResult{Reason: ErrorDetail(err), Unfixable: IsImmutableReleaseError(err)}
		}
	}
	res, err := c.PublishRelease(ctx, rel.ID, makeLatest)
	if err != nil || !res.Success {
		return state.RepublishResult{Reason: "Failed to publish", Unfixable: res.Unfixable}
	}
	return state.RepublishResult{Success: true, Reason: "Republished successfully"}
}

// ImmutableReleasesEnabled queries GET /immutable-releases.
func (c *Client) immutableEnabled(ctx context.Context) (bool, error) {
	var r struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.do(ctx, http.MethodGet, c.repoPath("/immutable-releases"), nil, &r); err != nil {
		return false, wrap("querying immutable releases status", err)
	}
	if r.Enabled == nil {
		return false, errors.New("immutable releases status not reported")
	}
	return *r.Enabled, nil
}

// EnableImmutableReleases enables the setting (needs admin) and polls until it is reported
// enabled, since the setting is eventually consistent. Polling uses exponential backoff
// (capped at 30s) via the injectable sleeper.
func (c *Client) enableImmutable(ctx context.Context) (bool, error) {
	if err := c.do(ctx, http.MethodPut, c.repoPath("/immutable-releases"), nil, nil); err != nil {
		return false, wrap("enabling immutable releases", err)
	}
	delay := c.pollD
	for attempt := 1; attempt <= c.pollN; attempt++ {
		if on, err := c.immutableEnabled(ctx); err == nil && on {
			return true, nil
		} else if err != nil && IsRateLimit(err) {
			return false, err
		}
		if attempt < c.pollN {
			c.logf("Immutable releases not yet reported as enabled (attempt %d of %d); retrying in %s", attempt, c.pollN, delay)
			if err := c.sleepCtx(ctx, delay); err != nil {
				return false, err
			}
			delay = min(delay*2, 30*time.Second)
		}
	}
	return false, nil
}

// ImmutableReleasesEnabled returns the repo setting, or nil when it could not be determined.
func (c *Client) ImmutableReleasesEnabled(ctx context.Context) *bool {
	on, err := c.immutableEnabled(ctx)
	if err != nil {
		return nil
	}
	return &on
}

// EnableImmutableReleases enables the setting and returns true once confirmed.
func (c *Client) EnableImmutableReleases(ctx context.Context) bool {
	ok, err := c.enableImmutable(ctx)
	return err == nil && ok
}
