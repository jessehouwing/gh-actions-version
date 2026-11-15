package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jessehouwing/gh-actions-version/internal/api"
	"github.com/jessehouwing/gh-actions-version/internal/output"
	"github.com/jessehouwing/gh-actions-version/internal/remediation"
	"github.com/jessehouwing/gh-actions-version/internal/testutil"
)

func savedPatchFindings(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "findings.json")
	r := newEnv(t, ghWithPatch(t)).runRaw("verify", "-R", "o/r", "--save", path, "--level=marketplace=none", "--quiet")
	if r.code != ExitValidation {
		t.Fatalf("verify exit %d: %s", r.code, r.all())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSavedReportRejectsInvalidInputBeforeCredentials(t *testing.T) {
	data := savedPatchFindings(t)
	tests := []struct {
		name string
		edit func(map[string]any)
		raw  func([]byte) []byte
		args []string
	}{
		{name: "unknown finding", edit: func(m map[string]any) { m["findings"].([]any)[0].(map[string]any)["finding"] = "unknown" }},
		{name: "unknown action", edit: func(m map[string]any) {
			m["findings"].([]any)[0].(map[string]any)["action"].(map[string]any)["kind"] = "unknown"
		}},
		{name: "action missing target", edit: func(m map[string]any) {
			for _, raw := range m["findings"].([]any) {
				finding := raw.(map[string]any)
				if finding["finding"] == "major_tag_missing" {
					finding["action"].(map[string]any)["sha"] = ""
					return
				}
			}
			t.Fatal("major tag finding missing from fixture")
		}},
		{name: "invalid fix policy", edit: func(m map[string]any) {
			m["config"].(map[string]any)["rules"].(map[string]any)["major_tag_missing"].(map[string]any)["fix"] = "unsafe"
		}},
		{name: "concatenated JSON", raw: func(b []byte) []byte { return append(b, []byte(` {}`)...) }},
		{name: "remote report in local mode", args: []string{"--local"}},
		{name: "local report in remote mode", edit: func(m map[string]any) { m["local"] = true }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var saved map[string]any
			if err := json.Unmarshal(data, &saved); err != nil {
				t.Fatal(err)
			}
			if tc.edit != nil {
				tc.edit(saved)
			}
			content, err := json.Marshal(saved)
			if err != nil {
				t.Fatal(err)
			}
			if tc.raw != nil {
				content = tc.raw(content)
			}
			gh := ghWithPatch(t)
			e := newEnv(t, gh)
			e.stdin = string(content)
			args := append([]string{"fix", "-R", "o/r", "--from=-", "--mode=auto"}, tc.args...)
			r := e.runRaw(args...)
			if r.code != ExitUsage || e.tokenCalls != 0 || len(gh.Requests()) != 0 {
				t.Fatalf("exit %d token calls=%d requests=%v: %s", r.code, e.tokenCalls, gh.Requests(), r.all())
			}
		})
	}
}

type captureAuthorization struct {
	base http.RoundTripper
	auth []string
}

func (c *captureAuthorization) RoundTrip(req *http.Request) (*http.Response, error) {
	c.auth = append(c.auth, req.Header.Get("Authorization"))
	return c.base.RoundTrip(req)
}

func TestSavedReportRealEnterpriseHostResolution(t *testing.T) {
	data := savedPatchFindings(t)
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "GITHUB_REPOSITORY", "GITHUB_SERVER_URL", "GITHUB_API_URL", "GITHUB_ACTIONS"} {
		t.Setenv(key, "")
	}
	t.Setenv("GH_HOST", "enterprise.example")
	t.Setenv("GH_ENTERPRISE_TOKEN", "synthetic-enterprise-token")
	t.Setenv("GH_REPO", "o/r")
	t.Setenv("GH_CONFIG_DIR", t.TempDir())

	for _, tc := range []struct {
		name, host string
		wantCode   int
	}{
		{"mismatch prevents token use", "github.com", ExitUsage},
		{"matching host uses enterprise token", "enterprise.example", ExitOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var saved map[string]any
			if err := json.Unmarshal(data, &saved); err != nil {
				t.Fatal(err)
			}
			saved["repository"].(map[string]any)["host"] = tc.host
			content, err := json.Marshal(saved)
			if err != nil {
				t.Fatal(err)
			}
			gh := ghWithPatch(t)
			transport := &captureAuthorization{base: gh.Transport()}
			var out, errOut bytes.Buffer
			env := Env{Stdin: bytes.NewReader(content), Stdout: &out, Stderr: &errOut,
				ClientOptions: func(o *api.Options) {
					o.Transport.Base = transport
					o.Transport.WritePacing = -1
					o.Transport.Sleep = func(context.Context, time.Duration) error { return nil }
				}}
			code := Run(context.Background(), []string{"fix", "--from=-", "--mode=auto", "--rule=major_tag_missing"}, env)
			if code != tc.wantCode {
				t.Fatalf("exit %d: %s%s", code, out.String(), errOut.String())
			}
			if tc.wantCode == ExitUsage {
				if len(transport.auth) != 0 || len(gh.Requests()) != 0 {
					t.Fatalf("mismatch made requests: %v", gh.Requests())
				}
			} else if len(transport.auth) == 0 || slices.ContainsFunc(transport.auth, func(a string) bool { return a != "token synthetic-enterprise-token" }) || gh.TagSha("v1") != c1 {
				t.Fatalf("credentials or replay incorrect: auth=%v writes=%v", transport.auth, gh.Writes())
			}
		})
	}
}

func TestBurnedTagSavedReportReplay(t *testing.T) {
	const error422 = `{"message":"Validation Failed","errors":[{"resource":"Release","code":"custom","field":"tag_name","message":"tag_name was used by an immutable release"}]}`
	path := filepath.Join(t.TempDir(), "findings.json")
	verify := testutil.NewGitHub(t).Tag(c1, "v1.0.0", "v1.0", "v1")
	r := newEnv(t, verify).runRaw("verify", "-R", "o/r", "--save", path, "--rule=patch_release_required", "--level=marketplace=none", "--quiet")
	if r.code != ExitValidation {
		t.Fatalf("verify exit %d: %s", r.code, r.all())
	}
	for _, allow := range []bool{false, true} {
		t.Run(map[bool]string{false: "denied", true: "explicitly allowed"}[allow], func(t *testing.T) {
			gh := testutil.NewGitHub(t).Tag(c1, "v1.0.0", "v1.0", "v1")
			gh.Fail("POST", "/releases", 422, error422, 1)
			args := []string{"fix", "-R", "o/r", "--from", path, "--mode=auto", "--output=json"}
			if allow {
				args = append(args, "--dangerously-allow-destructive-tag-deletion")
			}
			r := newEnv(t, gh).runRaw(args...)
			if deleted := gh.TagSha("v1.0.0") == ""; deleted != allow {
				t.Fatalf("deleted=%t allow=%t exit=%d writes=%v: %s", deleted, allow, r.code, gh.Writes(), r.all())
			}
			if want := map[bool]int{false: ExitValidation, true: ExitOK}[allow]; r.code != want {
				t.Fatalf("exit %d, want %d: %s", r.code, want, r.all())
			}
			if !strings.Contains(r.all(), "patch_tag_locked_by_deleted_release") {
				t.Fatalf("burned tag finding missing: %s", r.all())
			}
		})
	}
}

func TestSavedReportRestoresReplayStateAndPolicies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "findings.json")
	verify := testutil.NewGitHub(t).Tag(c1, "v1.0.0").Tag(c2, "v1.1.0").Branch(c1, "v1")
	verify.Release("v1.0.0", testutil.Immutable)
	verify.Release("v1.1.0", testutil.Prerelease, testutil.Immutable)
	res := newEnv(t, verify).runRaw("verify", "-R", "o/r", "--save", path, "--floating-versions-use=branches",
		"--preview-releases=included", "--rule=major_branch_tracks_highest_patch", "--finding-level=major_branch_wrong_target=warning", "--level=marketplace=none")
	if res.code != ExitValidation && res.code != ExitOK {
		t.Fatalf("verify exit %d: %s", res.code, res.all())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r := &run{
		f:    &flags{from: "-", changed: map[string]bool{}},
		env:  (Env{Stdin: bytes.NewReader(data)}).withDefaults(),
		log:  &output.Logger{Out: io.Discard},
		mode: remediation.Manual,
	}
	st, _, _, err := r.loadFindings()
	if err != nil {
		t.Fatal(err)
	}
	if st.Config.IgnorePreviewReleases || st.Config.FloatingVersionsUse != "branches" ||
		len(st.Branches) != 1 || st.Branches[0].Version != "v1" || st.Branches[0].Sha != c1 ||
		len(st.Releases) != 2 || !st.Releases[1].IsPrerelease {
		t.Fatalf("restored state: config=%+v branches=%+v releases=%+v", st.Config, st.Branches, st.Releases)
	}
	if len(st.Issues) != 1 || st.Issues[0].Severity != "warning" || st.Issues[0].ExpectedSha != c2 ||
		st.Config.Rules["major_branch_tracks_highest_patch"].Findings["major_branch_wrong_target"].Level != "warning" {
		t.Fatalf("restored finding and policy: %+v", st.Issues)
	}
	r.env.Stdin = bytes.NewReader(data)
	r.f.findingLevels = []string{"major_branch_wrong_target=error"}
	st, _, _, err = r.loadFindings()
	if err != nil || len(st.Issues) != 1 || st.Issues[0].Severity != "error" {
		t.Fatalf("finding override: state=%+v error=%v", st, err)
	}
	replay := testutil.NewGitHub(t).Tag(c1, "v1.0.0").Tag(c2, "v1.1.0").Branch(c1, "v1")
	res = newEnv(t, replay).runRaw("fix", "-R", "o/r", "--from", path, "--mode=auto", "--finding-level=major_branch_wrong_target=none")
	if res.code != ExitOK || len(replay.Writes()) != 0 || replay.BranchSha("v1") != c1 {
		t.Fatalf("override failed: exit %d requests=%v: %s", res.code, replay.Requests(), res.all())
	}
	replay = testutil.NewGitHub(t).Tag(c1, "v1.0.0").Tag(c2, "v1.1.0").Branch(c1, "v1")
	res = newEnv(t, replay).runRaw("fix", "-R", "o/r", "--from", path, "--mode=auto")
	if res.code != ExitOK || replay.BranchSha("v1") != c2 {
		t.Fatalf("preview target not replayed: exit %d writes=%v: %s", res.code, replay.Writes(), res.all())
	}
}
