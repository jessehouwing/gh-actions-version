package api

import (
	"context"
	"regexp"

	"github.com/jessehouwing/gh-actions-version/internal/rules"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

var (
	reLatest  = regexp.MustCompile(`^latest$`)
	reVersion = regexp.MustCompile(`^v\d+(\.\d+){0,2}$`)
)

var _ rules.MarketplaceSource = (*Client)(nil)

// FileContents implements rules.MarketplaceSource ("" when the file does not exist).
func (c *Client) FileContents(ctx context.Context, path, ref string) (string, error) {
	content, found, err := c.GetFileContents(ctx, path, ref)
	if err != nil || !found {
		return "", err
	}
	return content, nil
}

// RootEntries implements rules.MarketplaceSource.
func (c *Client) RootEntries(ctx context.Context, ref string) ([]rules.MarketplaceDirEntry, error) {
	entries, err := c.GetDirectoryContents(ctx, "", ref)
	if err != nil {
		return nil, err
	}
	out := make([]rules.MarketplaceDirEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, rules.MarketplaceDirEntry{Name: e.Name, Type: e.Type})
	}
	return out, nil
}

// GetMarketplaceMetadata loads action metadata via the rules package parser.
func (c *Client) GetMarketplaceMetadata(ctx context.Context, ref string) (*state.MarketplaceMetadata, error) {
	return rules.LoadActionMarketplaceMetadata(ctx, c, ref)
}

// Load fetches tags, branches, releases and (unless cfg.CheckMarketplace is "none" or empty)
// marketplace metadata into a new State (Initialize-RepositoryData). Tags and branches are
// listed once; version refs and the 'latest' alias are picked out locally.
func Load(ctx context.Context, c *Client, cfg state.Config) (*state.State, error) {
	c.SetIgnoreVersions(cfg.IgnoreVersions)
	st := state.New()
	st.Owner, st.Repo, st.Host, st.Config = c.Repo.Owner, c.Repo.Name, c.Repo.Host, cfg

	tags, err := c.GetTags(ctx, "")
	if err != nil {
		return nil, err
	}
	branches, err := c.GetBranches(ctx, "")
	if err != nil {
		return nil, err
	}
	st.Tags, st.Branches = splitVersionRefs(tags), splitVersionRefs(branches)
	c.TrackRefs(tags...)
	c.TrackRefs(branches...)

	if st.Releases, err = c.GetReleases(ctx); err != nil {
		return nil, err
	}
	if cfg.CheckImmutability != "" && cfg.CheckImmutability != "none" {
		st.ImmutableReleasesEnabled = c.ImmutableReleasesEnabled(ctx)
	}
	if cfg.CheckMarketplace != "" && cfg.CheckMarketplace != "none" {
		if st.Marketplace, err = c.GetMarketplaceMetadata(ctx, ""); err != nil {
			return nil, err
		}
	}
	return st, nil
}

// splitVersionRefs keeps version refs (vN, vN.N, vN.N.N) and appends at most one 'latest'.
func splitVersionRefs(all []*state.VersionRef) []*state.VersionRef {
	var out []*state.VersionRef
	var latest *state.VersionRef
	for _, r := range all {
		switch {
		case reVersion.MatchString(r.Version):
			out = append(out, r)
		case latest == nil && reLatest.MatchString(r.Version):
			latest = r
		}
	}
	if latest != nil {
		out = append(out, latest)
	}
	return out
}
