package scenarios

import (
	"context"
	"reflect"
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/api"
	"github.com/jessehouwing/gh-actions-version/internal/options"
	"github.com/jessehouwing/gh-actions-version/internal/remediation"
	"github.com/jessehouwing/gh-actions-version/internal/rules"
	"github.com/jessehouwing/gh-actions-version/internal/state"
	"github.com/jessehouwing/gh-actions-version/internal/testutil"
)

func loadFixture(t *testing.T, g *testutil.GitHub, cfg state.Config) *state.State {
	t.Helper()
	c := fixtureClient(t, g)
	st, err := api.Load(context.Background(), c, cfg)
	if err != nil {
		t.Fatal(err)
	}
	rules.Run(st, &st.Config, nil)
	return st
}

func fixtureClient(t *testing.T, g *testutil.GitHub) *api.Client {
	t.Helper()
	c, err := api.New(api.Options{
		Repo:      api.Repo{Host: "github.com", Owner: testutil.Owner, Name: testutil.Repo},
		Token:     "test-token",
		Transport: api.TransportOptions{Base: g.Transport(), WritePacing: -1},
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func issueCounts(st *state.State) map[string]int {
	counts := make(map[string]int)
	for _, issue := range st.Issues {
		counts[issue.Finding]++
	}
	return counts
}

func TestCheckoutFixture(t *testing.T) {
	g := testutil.CheckoutFixture(t)
	cfg := options.DefaultConfig()
	cfg.CheckMarketplace = "error"
	st := loadFixture(t, g, cfg)

	if got := len(st.Releases); got != 55 {
		t.Fatalf("fixture has %d releases, want 55", got)
	}
	if st.ImmutableReleasesEnabled != nil {
		t.Fatalf("immutable release setting = %v, want unknown", *st.ImmutableReleasesEnabled)
	}
	if got := issueCounts(st); got["release_not_immutable"] != 55 ||
		got["release_missing"] != 2 || got["marketplace_metadata_missing"] != 1 || len(got) != 3 {
		t.Fatalf("fixture findings = %v, want 55 non-immutable releases, 2 missing releases, 1 missing marketplace metadata", got)
	}
}

func TestScenarioFixtures(t *testing.T) {
	tests := []struct {
		name string
		new  func(testing.TB) *testutil.GitHub
		cfg  func() state.Config
		want map[string]int
	}{
		{
			name: "clean",
			new:  testutil.CleanFixture,
			cfg: func() state.Config {
				c := options.DefaultConfig()
				c.CheckMarketplace = "none"
				return c
			},
			want: map[string]int{},
		},
		{
			name: "floating versions on branches",
			new:  testutil.FloatingOnBranchesFixture,
			cfg: func() state.Config {
				c := options.DefaultConfig()
				c.CheckMarketplace = "none"
				c.FloatingVersionsUse = "branches"
				return c
			},
			want: map[string]int{},
		},
		{
			name: "duplicate refs",
			new:  testutil.DuplicateRefsFixture,
			cfg: func() state.Config {
				c := options.DefaultConfig()
				c.CheckMarketplace = "none"
				return c
			},
			want: map[string]int{"floating_tag_and_branch_both_exist": 2},
		},
		{
			name: "draft release",
			new:  testutil.DraftsFixture,
			cfg: func() state.Config {
				c := options.DefaultConfig()
				c.CheckMarketplace = "none"
				return c
			},
			want: map[string]int{"release_is_draft": 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := issueCounts(loadFixture(t, tt.new(t), tt.cfg()))
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("fixture findings = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBurnedTagScenario(t *testing.T) {
	const error422 = `{"message":"Validation Failed","errors":[{"resource":"Release","code":"custom","field":"tag_name","message":"tag_name was used by an immutable release"}]}`
	for _, allowDelete := range []bool{false, true} {
		t.Run(map[bool]string{false: "deletion disabled", true: "deletion enabled"}[allowDelete], func(t *testing.T) {
			g := testutil.NewGitHub(t)
			g.Tag("abc123", "v1", "v1.0", "v1.0.0")
			g.Fail("POST", "/releases", 422, error422, 1)

			cfg := options.DefaultConfig()
			cfg.CheckMinorVersion = "none"
			cfg.CheckReleases = "error"
			cfg.CheckImmutability = "none"
			cfg.CheckMarketplace = "none"
			cfg.AllowDestructiveTagDeletion = allowDelete

			client := fixtureClient(t, g)
			st, err := api.Load(context.Background(), client, cfg)
			if err != nil {
				t.Fatal(err)
			}
			rules.Run(st, &st.Config, nil)
			if len(st.Issues) != 1 || st.Issues[0].Finding != "release_missing" {
				t.Fatalf("initial findings = %+v, want release_missing", st.Issues)
			}
			if err := remediation.Fix(context.Background(), st, remediation.Options{Mode: remediation.AutoFix, Client: client}); err != nil {
				t.Fatal(err)
			}
			issue := st.Issues[0]
			if issue.Finding != "patch_tag_locked_by_deleted_release" {
				t.Fatalf("finding = %q, want patch_tag_locked_by_deleted_release", issue.Finding)
			}
			if allowDelete {
				if issue.Status != state.StatusFixed || g.TagSha("v1.0.0") != "" {
					t.Fatalf("status=%s tag SHA=%q, want fixed and deleted", issue.Status, g.TagSha("v1.0.0"))
				}
			} else if issue.Status != state.StatusManualFixRequired || g.TagSha("v1.0.0") != "abc123" {
				t.Fatalf("status=%s tag SHA=%q, want manual fix and unchanged tag", issue.Status, g.TagSha("v1.0.0"))
			}
		})
	}
}
