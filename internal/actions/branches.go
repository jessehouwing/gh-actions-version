package actions

import (
	"context"
	"fmt"

	"github.com/jessehouwing/gh-actions-version/internal/state"
)

// CreateBranchAction creates a new Git branch.
type CreateBranchAction struct {
	Base
	BranchName, Sha string
}

func NewCreateBranchAction(branch, sha string) *CreateBranchAction {
	return &CreateBranchAction{Base: newBase("Create branch", branch, 20), BranchName: branch, Sha: sha}
}

func (a *CreateBranchAction) Execute(ctx context.Context, st *state.State, c state.Client) bool {
	outf("Auto-fix: Create branch %s", a.BranchName)
	res := c.CreateRef(ctx, "refs/heads/"+a.BranchName, a.Sha, false)
	if res.Success {
		outf("✓ Success: Created branch %s", a.BranchName)
		return true
	}
	failRef(st, a, res, "create", "branch", a.BranchName, fmt.Sprintf("Version %s cannot be created by GitHub Actions because it contains workflow file changes and requires the 'workflows' permission. Please create manually.", a.BranchName))
	return false
}

func (a *CreateBranchAction) ManualCommands(*state.State) []string {
	return []string{fmt.Sprintf("git push origin %s:refs/heads/%s", a.Sha, a.BranchName)}
}

// UpdateBranchAction moves an existing Git branch.
type UpdateBranchAction struct {
	Base
	BranchName, Sha string
	Force           bool
}

func NewUpdateBranchAction(branch, sha string, force bool) *UpdateBranchAction {
	return &UpdateBranchAction{Base: newBase("Update branch", branch, 20), BranchName: branch, Sha: sha, Force: force}
}

func (a *UpdateBranchAction) Execute(ctx context.Context, st *state.State, c state.Client) bool {
	forceStr := ""
	if a.Force {
		forceStr = " (force)"
	}
	outf("Auto-fix: Update branch %s%s", a.BranchName, forceStr)
	res := c.CreateRef(ctx, "refs/heads/"+a.BranchName, a.Sha, a.Force)
	if res.Success {
		outf("✓ Success: Updated branch %s", a.BranchName)
		return true
	}
	failRef(st, a, res, "update", "branch", a.BranchName, fmt.Sprintf("Version %s cannot be updated by GitHub Actions because it contains workflow file changes and requires the 'workflows' permission. Please update manually.", a.BranchName))
	return false
}

func (a *UpdateBranchAction) ManualCommands(*state.State) []string {
	force := ""
	if a.Force {
		force = " --force"
	}
	return []string{fmt.Sprintf("git push origin %s:refs/heads/%s%s", a.Sha, a.BranchName, force)}
}

// DeleteBranchAction deletes an existing Git branch.
type DeleteBranchAction struct {
	Base
	BranchName string
}

func NewDeleteBranchAction(branch string) *DeleteBranchAction {
	return &DeleteBranchAction{Base: newBase("Delete branch", branch, 10), BranchName: branch}
}

func (a *DeleteBranchAction) Execute(ctx context.Context, st *state.State, c state.Client) bool {
	outf("Auto-fix: Delete branch %s", a.BranchName)
	if c.DeleteRef(ctx, "refs/heads/"+a.BranchName).Success {
		outf("✓ Success: Deleted branch %s", a.BranchName)
		return true
	}
	outf("✗ Failed: Delete branch %s", a.BranchName)
	return false
}

func (a *DeleteBranchAction) ManualCommands(*state.State) []string {
	return []string{"git branch -d " + a.BranchName, "git push origin :refs/heads/" + a.BranchName}
}
