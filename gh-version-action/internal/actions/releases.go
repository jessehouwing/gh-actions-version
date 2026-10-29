package actions

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jessehouwing/gh-version-action/internal/state"
)

// CreateReleaseAction creates a GitHub release.
type CreateReleaseAction struct {
	ReleaseBase
	IsDraft     bool
	AutoPublish bool
	TargetSha   string
	MakeLatest  *bool // nil lets GitHub decide
}

// NewCreateReleaseAction: autoPublish=true creates the release published (immutable) immediately.
func NewCreateReleaseAction(tag string, isDraft, autoPublish bool, targetSha string) *CreateReleaseAction {
	return &CreateReleaseAction{ReleaseBase: newReleaseBase("Create release", tag, 30), IsDraft: isDraft, AutoPublish: autoPublish, TargetSha: targetSha}
}

func (a *CreateReleaseAction) Execute(ctx context.Context, st *state.State, c state.Client) bool {
	draft := !a.AutoPublish
	desc := "Create draft release"
	if a.AutoPublish {
		desc = "Create and publish release"
	}
	outf("Auto-fix: %s for %s", desc, a.TagName)
	res, err := c.CreateReleaseWithLatest(ctx, a.TagName, draft, a.MakeLatest)
	if err == nil && res.Success {
		outf("✓ Success: %s for %s", desc, a.TagName)
		return true
	}
	if res.Unfixable || errors.Is(err, state.ErrUnfixable) {
		a.markAsUnfixable(st, "missing_release", fmt.Sprintf("Release %s cannot be created because this tag was previously used by an immutable release that was deleted. GitHub does not allow recreating a release on this tag. Add %s to the ignore-versions list so floating versions and 'latest' fall back to the previous released version (new ignore-versions value: \"%s\"), and/or publish a new patch (or minor) release so floating versions and 'latest' can advance to a version with a valid release.", a.TagName, a.TagName, a.suggestedIgnoreVersions(st)))
	} else {
		outf("✗ Failed: %s for %s", desc, a.TagName)
	}
	return false
}

func (a *CreateReleaseAction) ManualCommands(st *state.State) []string {
	if a.isIssueUnfixable(st, "missing_release") {
		return a.ignoreVersionsYamlSnippet(st)
	}
	target := ""
	if a.TargetSha != "" {
		target = " --target " + a.TargetSha
	}
	draft := ""
	if !a.AutoPublish {
		draft = " --draft"
	}
	return []string{fmt.Sprintf("gh release create %s%s%s --title \"%s\" --notes \"Release %s\"%s%s",
		a.TagName, repoArg(st), draft, a.TagName, a.TagName, target, latestArg(a.MakeLatest))}
}

// DeleteReleaseAction deletes a GitHub release.
type DeleteReleaseAction struct {
	ReleaseBase
	ReleaseID int64
}

func NewDeleteReleaseAction(tag string, id int64) *DeleteReleaseAction {
	return &DeleteReleaseAction{ReleaseBase: newReleaseBase("Delete release", tag, 10), ReleaseID: id}
}

func (a *DeleteReleaseAction) Execute(ctx context.Context, st *state.State, c state.Client) bool {
	outf("Auto-fix: Remove release for %s", a.TagName)
	id := a.releaseID(st, a.ReleaseID)
	if id != 0 && c.DeleteRelease(ctx, id) == nil {
		outf("✓ Success: Removed release for %s", a.TagName)
		return true
	}
	outf("✗ Failed: Remove release for %s", a.TagName)
	return false
}

func (a *DeleteReleaseAction) ManualCommands(st *state.State) []string {
	return []string{fmt.Sprintf("gh release delete %s%s --yes", a.TagName, repoArg(st))}
}

// PublishReleaseAction publishes a draft release.
type PublishReleaseAction struct {
	ReleaseBase
	ReleaseID  int64 // 0 = look up from state
	MakeLatest *bool // nil lets GitHub decide
}

func NewPublishReleaseAction(tag string, id int64) *PublishReleaseAction {
	return &PublishReleaseAction{ReleaseBase: newReleaseBase("Publish release", tag, 40), ReleaseID: id}
}

func (a *PublishReleaseAction) Execute(ctx context.Context, st *state.State, c state.Client) bool {
	outf("Auto-fix: Publish draft release for %s", a.TagName)
	var res state.ReleaseResult
	var err error
	if id := a.releaseID(st, a.ReleaseID); id != 0 {
		res, err = c.PublishRelease(ctx, id, a.MakeLatest)
	} else {
		err = errors.New("release ID not found")
	}
	if err == nil && res.Success {
		outf("✓ Success: Published release for %s", a.TagName)
		return true
	}
	if res.Unfixable || errors.Is(err, state.ErrUnfixable) {
		a.markAsUnfixable(st, "draft_release", fmt.Sprintf("Release %s cannot be published because this tag was previously used by an immutable release that was deleted. Add %s to the ignore-versions list so floating versions and 'latest' fall back to the previous released version (new ignore-versions value: \"%s\"), and/or publish a new patch (or minor) release so floating versions and 'latest' can advance to a version with a valid release.", a.TagName, a.TagName, a.suggestedIgnoreVersions(st)))
	} else {
		outf("✗ Failed: Publish release for %s", a.TagName)
	}
	return false
}

func (a *PublishReleaseAction) ManualCommands(st *state.State) []string {
	if a.isIssueUnfixable(st, "draft_release") {
		return a.ignoreVersionsYamlSnippet(st)
	}
	return []string{fmt.Sprintf("gh release edit %s%s --draft=false%s", a.TagName, repoArg(st), latestArg(a.MakeLatest))}
}

// SetLatestReleaseAction marks a release as the latest.
type SetLatestReleaseAction struct {
	ReleaseBase
	ReleaseID int64 // 0 = look up from state
}

func NewSetLatestReleaseAction(tag string, id int64) *SetLatestReleaseAction {
	return &SetLatestReleaseAction{ReleaseBase: newReleaseBase("Set release as latest", tag, 50), ReleaseID: id}
}

func (a *SetLatestReleaseAction) Execute(ctx context.Context, st *state.State, c state.Client) bool {
	outf("Auto-fix: Set release %s as latest", a.TagName)
	id := a.releaseID(st, a.ReleaseID)
	if id == 0 {
		outf("✗ Failed: Could not find release ID for %s", a.TagName)
		return false
	}
	err := c.SetReleaseLatest(ctx, id)
	if err == nil {
		outf("✓ Success: Set release %s as latest", a.TagName)
		return true
	}
	if errors.Is(err, state.ErrUnfixable) {
		a.markAsUnfixable(st, "wrong_latest_release", fmt.Sprintf("Cannot set %s as latest release", a.TagName))
	} else {
		outf("✗ Failed: Set release %s as latest", a.TagName)
	}
	return false
}

func (a *SetLatestReleaseAction) ManualCommands(st *state.State) []string {
	if a.isIssueUnfixable(st, "wrong_latest_release") {
		return []string{}
	}
	return []string{fmt.Sprintf("gh release edit %s%s --latest", a.TagName, repoArg(st))}
}

// RepublishReleaseAction republishes a release so it becomes immutable.
type RepublishReleaseAction struct {
	ReleaseBase
	MakeLatest               *bool // nil lets GitHub decide
	TagDeletionFailed        bool  // the release was deleted (422) and removing the tag failed
	ImmutabilityEnableFailed bool  // immutability could not be enabled or confirmed
	BlockingRulesets         []state.Ruleset
}

func NewRepublishReleaseAction(tag string) *RepublishReleaseAction {
	return &RepublishReleaseAction{ReleaseBase: newReleaseBase("Republish release for immutability", tag, 45)}
}

func immutableReleasesEndpoint(st *state.State) string {
	if st.Owner != "" && st.Repo != "" {
		return "repos/" + st.Owner + "/" + st.Repo + "/immutable-releases"
	}
	return "repos/{owner}/{repo}/immutable-releases"
}

func (a *RepublishReleaseAction) markImmutabilityManualFix(st *state.State, reason string) {
	enable := "gh api -X PUT " + immutableReleasesEndpoint(st)
	a.ImmutabilityEnableFailed = true
	a.markAsManualFixRequired(st, "non_immutable_release", fmt.Sprintf("%s Enable 'Release immutability' for the repository (requires repository admin permissions): %s", reason, enable))
	outf("::warning::Release %s is not immutable. Enable 'Release immutability' with: %s", a.TagName, enable)
}

func (a *RepublishReleaseAction) Execute(ctx context.Context, st *state.State, c state.Client) bool {
	outf("Auto-fix: Republish release %s to make it immutable", a.TagName)

	// Releases only become immutable when the setting is on while they are published, so enable it first
	if enabled := c.ImmutableReleasesEnabled(ctx); enabled != nil && !*enabled {
		outf("'Release immutability' is disabled for the repository; enabling it")
		if !c.EnableImmutableReleases(ctx) {
			a.markImmutabilityManualFix(st, fmt.Sprintf("Release %s is not immutable because 'Release immutability' is disabled for the repository and it could not be enabled automatically (the request failed or the setting was not confirmed in time).", a.TagName))
			return false
		}
	}

	res := c.RepublishRelease(ctx, a.TagName, a.MakeLatest)
	if res.Success {
		if immutable, err := c.ReleaseImmutable(ctx, a.TagName); err == nil && immutable {
			outf("✓ Success: Republished release for %s and verified immutability", a.TagName)
			return true
		}
		a.markImmutabilityManualFix(st, fmt.Sprintf("Release %s was republished but is still mutable.", a.TagName))
		return false
	}

	if !res.Unfixable {
		outf("✗ Failed: Republish release for %s - %s", a.TagName, res.Reason)
		return false
	}

	// The release was deleted (likely for security reasons); remove the tag so nobody can keep referencing it
	outf("Release %s was previously deleted; deleting tag so it can no longer be referenced", a.TagName)
	if NewDeleteTagAction(a.TagName).Execute(ctx, st, c) {
		return true
	}
	a.TagDeletionFailed = true
	a.BlockingRulesets, _ = c.TagRulesets(ctx, a.TagName)
	msg := fmt.Sprintf("Release %s cannot be republished because this tag was previously used by an immutable release that was deleted, and the tag could not be deleted automatically.", a.TagName)
	if len(a.BlockingRulesets) > 0 {
		names := make([]string, len(a.BlockingRulesets))
		for i, r := range a.BlockingRulesets {
			names[i] = fmt.Sprintf("'%s' (%s)", r.Name, r.Url)
		}
		msg += " Tag deletion is blocked by ruleset(s): " + strings.Join(names, ", ") + ". Bypass or disable them and delete the tag."
	} else {
		msg += " Delete the tag manually; check the tag rulesets and the permissions of the token."
	}
	a.markAsManualFixRequired(st, "non_immutable_release", msg)
	return false
}

func (a *RepublishReleaseAction) ManualCommands(st *state.State) []string {
	if a.TagDeletionFailed {
		cmds := []string{fmt.Sprintf("# The release for %s was deleted, so the tag should be removed to stop people from referencing it.", a.TagName)}
		for _, r := range a.BlockingRulesets {
			cmds = append(cmds, fmt.Sprintf("# Tag deletion is blocked by %s ruleset '%s' (%s): add yourself to its bypass list or disable it: %s", r.Source, r.Name, r.Enforcement, r.Url))
		}
		return append(cmds, "git push origin :refs/tags/"+a.TagName)
	}

	var cmds []string
	// Immutability is known to be enabled when any release is already immutable or an earlier immutability fix succeeded
	enabled := false
	for _, r := range st.Releases {
		if r.IsImmutable {
			enabled = true
			break
		}
	}
	for _, i := range st.Issues {
		if i.Type == "non_immutable_release" && i.Status == state.StatusFixed {
			enabled = true
			break
		}
	}
	if a.ImmutabilityEnableFailed || !enabled {
		cmds = append(cmds,
			"# Enable 'Release immutability' for the repository (requires repository admin permissions):",
			"gh api -X PUT "+immutableReleasesEndpoint(st))
	}
	repo := repoArg(st)
	return append(cmds,
		fmt.Sprintf("gh release edit %s%s --draft=true", a.TagName, repo),
		fmt.Sprintf("gh release edit %s%s --draft=false%s", a.TagName, repo, latestArg(a.MakeLatest)))
}
