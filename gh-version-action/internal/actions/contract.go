package actions

import (
	"context"

	"github.com/jessehouwing/gh-version-action/internal/state"
)

// This file is the constructor contract consumed by the rules. The actions implementer
// moves each type into its own file and implements Execute/ManualCommands (see the PowerShell originals).

type CreateTagAction struct {
	Base
	TagName, Sha string
}

func NewCreateTagAction(tag, sha string) *CreateTagAction {
	return &CreateTagAction{Base: newBase("Create tag", tag, 20), TagName: tag, Sha: sha}
}

type UpdateTagAction struct {
	Base
	TagName, Sha string
	Force        bool
}

func NewUpdateTagAction(tag, sha string, force bool) *UpdateTagAction {
	return &UpdateTagAction{Base: newBase("Update tag", tag, 20), TagName: tag, Sha: sha, Force: force}
}

type DeleteTagAction struct {
	Base
	TagName string
}

func NewDeleteTagAction(tag string) *DeleteTagAction {
	return &DeleteTagAction{Base: newBase("Delete tag", tag, 10), TagName: tag}
}

type CreateBranchAction struct {
	Base
	BranchName, Sha string
}

func NewCreateBranchAction(branch, sha string) *CreateBranchAction {
	return &CreateBranchAction{Base: newBase("Create branch", branch, 20), BranchName: branch, Sha: sha}
}

type UpdateBranchAction struct {
	Base
	BranchName, Sha string
	Force           bool
}

func NewUpdateBranchAction(branch, sha string, force bool) *UpdateBranchAction {
	return &UpdateBranchAction{Base: newBase("Update branch", branch, 20), BranchName: branch, Sha: sha, Force: force}
}

type DeleteBranchAction struct {
	Base
	BranchName string
}

func NewDeleteBranchAction(branch string) *DeleteBranchAction {
	return &DeleteBranchAction{Base: newBase("Delete branch", branch, 10), BranchName: branch}
}

type ConvertBranchToTagAction struct {
	Base
	Name, Sha string
}

func NewConvertBranchToTagAction(name, sha string) *ConvertBranchToTagAction {
	return &ConvertBranchToTagAction{Base: newBase("Convert branch to tag", name, 25), Name: name, Sha: sha}
}

type ConvertTagToBranchAction struct {
	Base
	Name, Sha string
}

func NewConvertTagToBranchAction(name, sha string) *ConvertTagToBranchAction {
	return &ConvertTagToBranchAction{Base: newBase("Convert tag to branch", name, 25), Name: name, Sha: sha}
}

type CreateReleaseAction struct {
	Base
	TagName     string
	IsDraft     bool
	AutoPublish bool
	TargetSha   string
}

// NewCreateReleaseAction: autoPublish=true creates the release published (immutable) immediately.
func NewCreateReleaseAction(tag string, isDraft, autoPublish bool, targetSha string) *CreateReleaseAction {
	return &CreateReleaseAction{Base: newBase("Create release", tag, 30), TagName: tag, IsDraft: isDraft, AutoPublish: autoPublish, TargetSha: targetSha}
}

type DeleteReleaseAction struct {
	Base
	TagName   string
	ReleaseID int64
}

func NewDeleteReleaseAction(tag string, id int64) *DeleteReleaseAction {
	return &DeleteReleaseAction{Base: newBase("Delete release", tag, 10), TagName: tag, ReleaseID: id}
}

type PublishReleaseAction struct {
	Base
	TagName   string
	ReleaseID int64 // 0 = look up from state
}

func NewPublishReleaseAction(tag string, id int64) *PublishReleaseAction {
	return &PublishReleaseAction{Base: newBase("Publish release", tag, 40), TagName: tag, ReleaseID: id}
}

type RepublishReleaseAction struct {
	Base
	TagName string
}

func NewRepublishReleaseAction(tag string) *RepublishReleaseAction {
	return &RepublishReleaseAction{Base: newBase("Republish release for immutability", tag, 45), TagName: tag}
}

type SetLatestReleaseAction struct {
	Base
	TagName   string
	ReleaseID int64 // 0 = look up from state
}

func NewSetLatestReleaseAction(tag string, id int64) *SetLatestReleaseAction {
	return &SetLatestReleaseAction{Base: newBase("Set release as latest", tag, 50), TagName: tag, ReleaseID: id}
}

// Temporary stubs; replaced by the real implementations.
func stubExec(context.Context, *state.State, state.Client) bool { return false }

func (a *CreateTagAction) Execute(c context.Context, s *state.State, cl state.Client) bool {
	return stubExec(c, s, cl)
}
func (a *CreateTagAction) ManualCommands(*state.State) []string { return nil }
func (a *UpdateTagAction) Execute(c context.Context, s *state.State, cl state.Client) bool {
	return stubExec(c, s, cl)
}
func (a *UpdateTagAction) ManualCommands(*state.State) []string { return nil }
func (a *DeleteTagAction) Execute(c context.Context, s *state.State, cl state.Client) bool {
	return stubExec(c, s, cl)
}
func (a *DeleteTagAction) ManualCommands(*state.State) []string { return nil }
func (a *CreateBranchAction) Execute(c context.Context, s *state.State, cl state.Client) bool {
	return stubExec(c, s, cl)
}
func (a *CreateBranchAction) ManualCommands(*state.State) []string { return nil }
func (a *UpdateBranchAction) Execute(c context.Context, s *state.State, cl state.Client) bool {
	return stubExec(c, s, cl)
}
func (a *UpdateBranchAction) ManualCommands(*state.State) []string { return nil }
func (a *DeleteBranchAction) Execute(c context.Context, s *state.State, cl state.Client) bool {
	return stubExec(c, s, cl)
}
func (a *DeleteBranchAction) ManualCommands(*state.State) []string { return nil }
func (a *ConvertBranchToTagAction) Execute(c context.Context, s *state.State, cl state.Client) bool {
	return stubExec(c, s, cl)
}
func (a *ConvertBranchToTagAction) ManualCommands(*state.State) []string { return nil }
func (a *ConvertTagToBranchAction) Execute(c context.Context, s *state.State, cl state.Client) bool {
	return stubExec(c, s, cl)
}
func (a *ConvertTagToBranchAction) ManualCommands(*state.State) []string { return nil }
func (a *CreateReleaseAction) Execute(c context.Context, s *state.State, cl state.Client) bool {
	return stubExec(c, s, cl)
}
func (a *CreateReleaseAction) ManualCommands(*state.State) []string { return nil }
func (a *DeleteReleaseAction) Execute(c context.Context, s *state.State, cl state.Client) bool {
	return stubExec(c, s, cl)
}
func (a *DeleteReleaseAction) ManualCommands(*state.State) []string { return nil }
func (a *PublishReleaseAction) Execute(c context.Context, s *state.State, cl state.Client) bool {
	return stubExec(c, s, cl)
}
func (a *PublishReleaseAction) ManualCommands(*state.State) []string { return nil }
func (a *RepublishReleaseAction) Execute(c context.Context, s *state.State, cl state.Client) bool {
	return stubExec(c, s, cl)
}
func (a *RepublishReleaseAction) ManualCommands(*state.State) []string { return nil }
func (a *SetLatestReleaseAction) Execute(c context.Context, s *state.State, cl state.Client) bool {
	return stubExec(c, s, cl)
}
func (a *SetLatestReleaseAction) ManualCommands(*state.State) []string { return nil }
