package rules

import (
	"strings"

	"github.com/jessehouwing/gh-version-action/internal/state"
)

func init() {
	Register(&Rule{
		Name:        "action_metadata_required",
		Description: "Action metadata (name, description, branding) and README.md are required for GitHub Marketplace",
		Priority:    5,
		Category:    "marketplace",
		Condition: func(st *state.State, cfg *state.Config) []any {
			if !checkEnabled(cfg.CheckMarketplace) {
				return nil
			}
			if st.Marketplace == nil || !st.Marketplace.IsValid() {
				return []any{st.Marketplace}
			}
			return nil
		},
		Check: func(item any, st *state.State, cfg *state.Config) bool { return false },
		CreateIssue: func(item any, st *state.State, cfg *state.Config) *state.Issue {
			md := st.Marketplace
			if md == nil {
				md = &state.MarketplaceMetadata{}
			}
			issue := state.NewIssue("missing_marketplace_metadata", releaseSeverity(cfg.CheckMarketplace),
				"GitHub Marketplace requires: "+strings.Join(md.MissingRequirements(), "; "))
			issue.Status = state.StatusManualFixRequired

			path := md.ActionFilePath
			var b strings.Builder
			b.WriteString("# Missing marketplace metadata. Please add the following to your repository:\n")
			if !md.HasName {
				b.WriteString("\n# Add 'name' property to " + path)
			}
			if !md.HasDescription {
				b.WriteString("\n# Add 'description' property to " + path)
			}
			if !md.HasBrandingIcon || !md.HasBrandingColor {
				b.WriteString("\n\n# Add branding section to " + path + ":\n# branding:\n#   icon: 'check-circle'  # See: https://feathericons.com/\n#   color: 'blue'         # Options: white, yellow, blue, green, orange, red, purple, gray-dark")
			}
			if !md.ReadmeExists {
				b.WriteString("\n\n# Create README.md in the repository root with documentation for your action")
			}
			issue.ManualFixCommand = b.String()
			return issue
		},
	})
}
