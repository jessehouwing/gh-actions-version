package actions

import (
	"fmt"

	"github.com/jessehouwing/gh-actions-version/internal/state"
)

// SerializedAction contains the parameters required to reconstruct a remediation.
type SerializedAction struct {
	Kind           string `json:"kind"`
	Name           string `json:"name,omitempty"`
	SHA            string `json:"sha,omitempty"`
	Force          bool   `json:"force,omitempty"`
	ReleaseID      int64  `json:"releaseId,omitempty"`
	IsDraft        bool   `json:"isDraft,omitempty"`
	AutoPublish    bool   `json:"autoPublish,omitempty"`
	MakeLatest     *bool  `json:"makeLatest,omitempty"`
	RequiresReload string `json:"requiresReload,omitempty"`
}

// Serialize captures an action and resolves release IDs from the verified state.
func Serialize(st *state.State, action state.Action) (SerializedAction, error) {
	var out SerializedAction
	switch a := action.(type) {
	case *CreateTagAction:
		out = SerializedAction{Kind: "create-tag", Name: a.TagName, SHA: a.Sha}
	case *UpdateTagAction:
		out = SerializedAction{Kind: "update-tag", Name: a.TagName, SHA: a.Sha, Force: a.Force}
	case *DeleteTagAction:
		out = SerializedAction{Kind: "delete-tag", Name: a.TagName}
	case *CreateBranchAction:
		out = SerializedAction{Kind: "create-branch", Name: a.BranchName, SHA: a.Sha}
	case *UpdateBranchAction:
		out = SerializedAction{Kind: "update-branch", Name: a.BranchName, SHA: a.Sha, Force: a.Force}
	case *DeleteBranchAction:
		out = SerializedAction{Kind: "delete-branch", Name: a.BranchName}
	case *ConvertBranchToTagAction:
		out = SerializedAction{Kind: "convert-branch-to-tag", Name: a.Name, SHA: a.Sha}
	case *ConvertTagToBranchAction:
		out = SerializedAction{Kind: "convert-tag-to-branch", Name: a.Name, SHA: a.Sha}
	case *CreateReleaseAction:
		out = SerializedAction{Kind: "create-release", Name: a.TagName, SHA: a.TargetSha, IsDraft: a.IsDraft, AutoPublish: a.AutoPublish, MakeLatest: a.MakeLatest}
	case *DeleteReleaseAction:
		out = SerializedAction{Kind: "delete-release", Name: a.TagName, ReleaseID: a.releaseID(st, a.ReleaseID)}
	case *PublishReleaseAction:
		out = SerializedAction{Kind: "publish-release", Name: a.TagName, ReleaseID: a.releaseID(st, a.ReleaseID), MakeLatest: a.MakeLatest}
	case *SetLatestReleaseAction:
		out = SerializedAction{Kind: "set-latest-release", Name: a.TagName, ReleaseID: a.releaseID(st, a.ReleaseID)}
	case *EnableImmutableReleasesAction:
		out = SerializedAction{Kind: "enable-immutable-releases"}
	case *RepublishReleaseAction:
		out = SerializedAction{Kind: "republish-release", Name: a.TagName, MakeLatest: a.MakeLatest}
	default:
		return SerializedAction{}, fmt.Errorf("unsupported remediation action %T", action)
	}
	switch out.Kind {
	case "delete-release", "publish-release", "set-latest-release":
		if out.ReleaseID == 0 {
			out.RequiresReload = "release ID unavailable; rerun verify"
		}
	}
	return out, nil
}

// Deserialize reconstructs an action without consulting the network.
func Deserialize(data SerializedAction) (state.Action, error) {
	if data.RequiresReload != "" {
		return nil, fmt.Errorf("%s: %s", data.Kind, data.RequiresReload)
	}
	if data.Kind != "enable-immutable-releases" && data.Name == "" {
		return nil, fmt.Errorf("%s: missing name", data.Kind)
	}
	switch data.Kind {
	case "create-tag", "update-tag", "create-branch", "update-branch", "convert-branch-to-tag", "convert-tag-to-branch":
		if data.SHA == "" {
			return nil, fmt.Errorf("%s: missing target SHA", data.Kind)
		}
	case "delete-release", "publish-release", "set-latest-release":
		if data.ReleaseID <= 0 {
			return nil, fmt.Errorf("%s: missing release ID; rerun verify", data.Kind)
		}
	}
	switch data.Kind {
	case "create-tag":
		return NewCreateTagAction(data.Name, data.SHA), nil
	case "update-tag":
		return NewUpdateTagAction(data.Name, data.SHA, data.Force), nil
	case "delete-tag":
		return NewDeleteTagAction(data.Name), nil
	case "create-branch":
		return NewCreateBranchAction(data.Name, data.SHA), nil
	case "update-branch":
		return NewUpdateBranchAction(data.Name, data.SHA, data.Force), nil
	case "delete-branch":
		return NewDeleteBranchAction(data.Name), nil
	case "convert-branch-to-tag":
		return NewConvertBranchToTagAction(data.Name, data.SHA), nil
	case "convert-tag-to-branch":
		return NewConvertTagToBranchAction(data.Name, data.SHA), nil
	case "create-release":
		a := NewCreateReleaseAction(data.Name, data.IsDraft, data.AutoPublish, data.SHA)
		a.MakeLatest = data.MakeLatest
		return a, nil
	case "delete-release":
		return NewDeleteReleaseAction(data.Name, data.ReleaseID), nil
	case "publish-release":
		a := NewPublishReleaseAction(data.Name, data.ReleaseID)
		a.MakeLatest = data.MakeLatest
		return a, nil
	case "set-latest-release":
		return NewSetLatestReleaseAction(data.Name, data.ReleaseID), nil
	case "enable-immutable-releases":
		return NewEnableImmutableReleasesAction(), nil
	case "republish-release":
		a := NewRepublishReleaseAction(data.Name)
		a.MakeLatest = data.MakeLatest
		return a, nil
	default:
		return nil, fmt.Errorf("unknown remediation action kind %q", data.Kind)
	}
}
