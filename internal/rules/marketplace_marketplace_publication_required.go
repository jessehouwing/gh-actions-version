package rules

import (
	"context"
	"fmt"

	"github.com/jessehouwing/gh-actions-version/internal/state"
)

func init() {
	Register(&Rule{
		Name:        "marketplace_publication_required",
		Description: "The latest release should be published to GitHub Marketplace",
		Findings:    []FindingDef{{ID: "marketplace_not_published", Summary: "The latest release is not published to GitHub Marketplace"}},
		Priority:    50,
		Category:    "marketplace",
		Condition: func(st *state.State, cfg *state.Config) []any {
			if !checkEnabled(cfg.CheckMarketplace) {
				return nil
			}
			if st.Marketplace == nil || !st.Marketplace.IsValid() {
				return nil
			}
			for _, r := range st.Releases {
				if r.IsLatest && !r.IsIgnored {
					return []any{r}
				}
			}
			return nil
		},
		Check: func(item any, st *state.State, cfg *state.Config) bool {
			rel := item.(*state.ReleaseInfo)
			if st.Marketplace == nil || !st.Marketplace.IsValid() || st.Marketplace.Name == "" {
				return true
			}
			res, err := MarketplaceVersionPublished(context.Background(), serverURLFor(st), st.Marketplace.Name, rel.TagName)
			if err != nil {
				MarketplaceWarn(fmt.Sprintf("Could not verify marketplace publication for %s: %v", rel.TagName, err))
				return true
			}
			if res.Published == nil {
				// Unknown result: avoid false positives
				MarketplaceWarn(fmt.Sprintf("Could not verify marketplace publication for %s: %s", rel.TagName, res.Error))
				return true
			}
			return *res.Published
		},
		CreateIssue: func(item any, st *state.State, cfg *state.Config) *state.Issue {
			rel := item.(*state.ReleaseInfo)
			v := rel.TagName
			issue := state.NewIssue("marketplace_not_published", releaseSeverity(cfg.CheckMarketplace),
				fmt.Sprintf("Release %s is not published to GitHub Marketplace", v))
			issue.Version = v
			issue.Status = state.StatusManualFixRequired

			server := serverURLFor(st)
			repoURL := fmt.Sprintf("%s/%s/%s", server, st.Owner, st.Repo)
			marketURL := ""
			if st.Marketplace != nil && st.Marketplace.HasName {
				marketURL = server + "/marketplace/actions/" + MarketplaceSlug(st.Marketplace.Name)
			}
			issue.ManualFixCommand = fmt.Sprintf("# To publish %s to GitHub Marketplace:\n# \n# 1. Go to: %s/releases/tag/%s\n# 2. Click 'Edit' on the release\n# 3. Check 'Publish this Action to the GitHub Marketplace'\n# 4. Review and accept the GitHub Marketplace Developer Agreement (if not already done)\n# 5. Select the appropriate categories for your action\n# 6. Click 'Update release'\n#\n# After publishing, the action will be available at:\n# %s?version=%s\n#\n# Note: You must have verified your email and have a published README.md\n# with action.yaml containing name, description, and branding fields.",
				v, repoURL, v, marketURL, v)
			return issue
		},
	})
}
