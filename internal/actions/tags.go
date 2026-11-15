package actions

import (
	"context"
	"fmt"

	"github.com/jessehouwing/gh-actions-version/internal/state"
)

// CreateTagAction creates a new Git tag.
type CreateTagAction struct {
	Base
	TagName, Sha string
}

func NewCreateTagAction(tag, sha string) *CreateTagAction {
	return &CreateTagAction{Base: newBase("Create tag", tag, 20), TagName: tag, Sha: sha}
}

func (a *CreateTagAction) Execute(ctx context.Context, st *state.State, c state.Client) bool {
	outf("Auto-fix: Create tag %s", a.TagName)
	res := c.CreateRef(ctx, "refs/tags/"+a.TagName, a.Sha, false)
	if res.Success {
		outf("✓ Success: Created tag %s", a.TagName)
		return true
	}
	failRef(st, a, res, "create", "tag", a.TagName, fmt.Sprintf("Version %s cannot be created by GitHub Actions because it contains workflow file changes and requires the 'workflows' permission. Please create manually.", a.TagName))
	return false
}

func (a *CreateTagAction) ManualCommands(*state.State) []string {
	return []string{fmt.Sprintf("git push origin %s:refs/tags/%s", a.Sha, a.TagName)}
}

// UpdateTagAction moves an existing Git tag.
type UpdateTagAction struct {
	Base
	TagName, Sha string
	Force        bool
}

func NewUpdateTagAction(tag, sha string, force bool) *UpdateTagAction {
	return &UpdateTagAction{Base: newBase("Update tag", tag, 20), TagName: tag, Sha: sha, Force: force}
}

func (a *UpdateTagAction) Execute(ctx context.Context, st *state.State, c state.Client) bool {
	forceStr := ""
	if a.Force {
		forceStr = " (force)"
	}
	outf("Auto-fix: Update tag %s%s", a.TagName, forceStr)
	res := c.CreateRef(ctx, "refs/tags/"+a.TagName, a.Sha, a.Force)
	if res.Success {
		outf("✓ Success: Updated tag %s", a.TagName)
		return true
	}
	failRef(st, a, res, "update", "tag", a.TagName, fmt.Sprintf("Version %s cannot be updated by GitHub Actions because it contains workflow file changes and requires the 'workflows' permission. Please update manually.", a.TagName))
	return false
}

func (a *UpdateTagAction) ManualCommands(*state.State) []string {
	force := ""
	if a.Force {
		force = " --force"
	}
	return []string{fmt.Sprintf("git push origin %s:refs/tags/%s%s", a.Sha, a.TagName, force)}
}

// DeleteTagAction deletes an existing Git tag.
type DeleteTagAction struct {
	Base
	TagName string
}

func NewDeleteTagAction(tag string) *DeleteTagAction {
	return &DeleteTagAction{Base: newBase("Delete tag", tag, 10), TagName: tag}
}

func (a *DeleteTagAction) Execute(ctx context.Context, st *state.State, c state.Client) bool {
	outf("Auto-fix: Delete tag %s", a.TagName)
	if c.DeleteRef(ctx, "refs/tags/"+a.TagName).Success {
		outf("✓ Success: Deleted tag %s", a.TagName)
		return true
	}
	outf("✗ Failed: Delete tag %s", a.TagName)
	return false
}

func (a *DeleteTagAction) ManualCommands(*state.State) []string {
	return []string{"git tag -d " + a.TagName, "git push origin :refs/tags/" + a.TagName}
}
