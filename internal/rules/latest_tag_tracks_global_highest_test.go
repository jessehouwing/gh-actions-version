package rules

import "testing"

func TestLatestTagTracksGlobalHighest(t *testing.T) {
	runLatestRuleTests(t, latestKind{"latest_tag_tracks_global_highest", false, "tags", "branches", "incorrect_latest_tag"})
}
