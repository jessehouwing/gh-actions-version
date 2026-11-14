package actions

import (
	"context"
	"fmt"

	"github.com/jessehouwing/gh-version-action/internal/state"
)

// ConvertBranchToTagAction converts a branch to a tag.
type ConvertBranchToTagAction struct {
	Base
	Name, Sha string
}

func NewConvertBranchToTagAction(name, sha string) *ConvertBranchToTagAction {
	return &ConvertBranchToTagAction{Base: newBase("Convert branch to tag", name, 25), Name: name, Sha: sha}
}

func (a *ConvertBranchToTagAction) Execute(ctx context.Context, st *state.State, c state.Client) bool {
	if st.FindVersion(a.Name, state.TypeTag) != nil {
		outf("Auto-fix: Delete branch %s (tag already exists)", a.Name)
		if c.DeleteRef(ctx, "refs/heads/"+a.Name).Success {
			outf("✓ Success: Removed branch %s", a.Name)
			return true
		}
		outf("✗ Failed: Delete branch %s", a.Name)
		return false
	}

	outf("Auto-fix: Convert branch %s to tag", a.Name)
	res := c.CreateRef(ctx, "refs/tags/"+a.Name, a.Sha, false)
	if !res.Success {
		failRef(st, a, res, "create", "tag", a.Name, fmt.Sprintf("Version %s cannot be converted to a tag by GitHub Actions because it contains workflow file changes and requires the 'workflows' permission. Please convert manually.", a.Name))
		return false
	}
	if c.DeleteRef(ctx, "refs/heads/"+a.Name).Success {
		outf("✓ Success: Converted branch %s to tag", a.Name)
		return true
	}
	outf("✗ Failed: Delete branch %s after creating tag", a.Name)
	return false
}

func (a *ConvertBranchToTagAction) ManualCommands(st *state.State) []string {
	if st.FindVersion(a.Name, state.TypeTag) != nil {
		return []string{"git push origin :refs/heads/" + a.Name}
	}
	return []string{
		fmt.Sprintf("git push origin %s:refs/tags/%s", a.Sha, a.Name),
		"git push origin :refs/heads/" + a.Name,
	}
}

// ConvertTagToBranchAction converts a tag to a branch.
type ConvertTagToBranchAction struct {
	Base
	Name, Sha string
}

func NewConvertTagToBranchAction(name, sha string) *ConvertTagToBranchAction {
	return &ConvertTagToBranchAction{Base: newBase("Convert tag to branch", name, 25), Name: name, Sha: sha}
}

func (a *ConvertTagToBranchAction) Execute(ctx context.Context, st *state.State, c state.Client) bool {
	if st.Owner != "" && st.Repo != "" {
		immutable, err := c.ReleaseImmutable(ctx, a.Name)
		if err != nil {
			outf("::debug::Failed to check release immutability for tag %s: %s", a.Name, err)
		} else if immutable {
			outf("✗ Unfixable: Tag %s is immutable and cannot be converted to a branch", a.Name)
			if i := issueFor(st, a); i != nil {
				i.Status = state.StatusUnfixable
				i.Message = fmt.Sprintf("Tag %s is immutable and cannot be converted to a branch. Consider keeping the tag or using ignore-versions.", a.Name)
			}
			return false
		}
	}

	if st.FindVersion(a.Name, state.TypeBranch) != nil {
		outf("Auto-fix: Delete tag %s (branch already exists)", a.Name)
		if c.DeleteRef(ctx, "refs/tags/"+a.Name).Success {
			outf("✓ Success: Removed tag %s", a.Name)
			return true
		}
		outf("✗ Failed: Delete tag %s", a.Name)
		return false
	}

	outf("Auto-fix: Convert tag %s to branch", a.Name)
	res := c.CreateRef(ctx, "refs/heads/"+a.Name, a.Sha, false)
	if !res.Success {
		failRef(st, a, res, "create", "branch", a.Name, fmt.Sprintf("Version %s cannot be converted to a branch by GitHub Actions because it contains workflow file changes and requires the 'workflows' permission. Please convert manually.", a.Name))
		return false
	}
	if c.DeleteRef(ctx, "refs/tags/"+a.Name).Success {
		outf("✓ Success: Converted tag %s to branch", a.Name)
		return true
	}
	outf("✗ Failed: Delete tag %s after creating branch", a.Name)
	return false
}

func (a *ConvertTagToBranchAction) ManualCommands(st *state.State) []string {
	if i := issueFor(st, a); i != nil && i.Status == state.StatusUnfixable {
		return []string{}
	}
	if st.FindVersion(a.Name, state.TypeBranch) != nil {
		return []string{"git push origin :refs/tags/" + a.Name}
	}
	return []string{
		fmt.Sprintf("git push origin %s:refs/heads/%s", a.Sha, a.Name),
		"git push origin :refs/tags/" + a.Name,
	}
}
