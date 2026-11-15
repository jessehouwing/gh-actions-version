package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jessehouwing/gh-actions-version/internal/progress"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

type rewrite struct{ target *url.URL }

func (r rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = r.target.Scheme
	req.URL.Host = r.target.Host
	return http.DefaultTransport.RoundTrip(req)
}

type harness struct {
	c      *Client
	sleeps []time.Duration
	now    time.Time
	mu     sync.Mutex
	warns  []string
}

func newHarness(t *testing.T, h http.HandlerFunc, mut ...func(*Options)) *harness {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	hn := &harness{now: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}
	o := Options{
		Repo:  Repo{Host: "github.com", Owner: "owner", Name: "repo"},
		Token: "tok",
		Transport: TransportOptions{
			Base:        rewrite{u},
			WritePacing: -1,
			Now:         func() time.Time { return hn.now },
			Sleep: func(_ context.Context, d time.Duration) error {
				hn.mu.Lock()
				defer hn.mu.Unlock()
				hn.sleeps = append(hn.sleeps, d)
				hn.now = hn.now.Add(d)
				return nil
			},
			Jitter: func(time.Duration) time.Duration { return 0 },
			Warn:   func(m string) { hn.warns = append(hn.warns, m) },
		},
		ImmutablePollAttempts: 3,
		ImmutablePollDelay:    time.Second,
	}
	for _, m := range mut {
		m(&o)
	}
	c, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	hn.c = c
	return hn
}

func jsonResp(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if s, ok := v.(string); ok {
		_, _ = io.WriteString(w, s)
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

func ctx() context.Context { return context.Background() }

func TestImmutableReleaseStatus(t *testing.T) {
	t.Run("Should return the enabled flag", func(t *testing.T) {
		h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
			jsonResp(w, 200, map[string]any{"enabled": true})
		})
		got := h.c.ImmutableReleasesEnabled(ctx())
		if got == nil || !*got {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("Should return nil when the API fails", func(t *testing.T) {
		h := newHarness(t, func(w http.ResponseWriter, r *http.Request) { jsonResp(w, 404, map[string]any{"message": "Not Found"}) })
		if got := h.c.ImmutableReleasesEnabled(ctx()); got != nil {
			t.Fatalf("got %v", *got)
		}
	})
	t.Run("Should enable and confirm without sleeping", func(t *testing.T) {
		var puts int
		h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "PUT" {
				puts++
				w.WriteHeader(204)
				return
			}
			jsonResp(w, 200, map[string]any{"enabled": true})
		})
		if !h.c.EnableImmutableReleases(ctx()) || puts != 1 || len(h.sleeps) != 0 {
			t.Fatalf("puts=%d sleeps=%v", puts, h.sleeps)
		}
	})
	t.Run("Should poll with exponential backoff until enabled", func(t *testing.T) {
		var gets int
		h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "PUT" {
				w.WriteHeader(204)
				return
			}
			gets++
			jsonResp(w, 200, map[string]any{"enabled": gets >= 4})
		}, func(o *Options) { o.ImmutablePollAttempts = 5 })
		if !h.c.EnableImmutableReleases(ctx()) {
			t.Fatal("expected enabled")
		}
		want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
		if fmt.Sprint(h.sleeps) != fmt.Sprint(want) || gets != 4 {
			t.Fatalf("sleeps=%v gets=%d", h.sleeps, gets)
		}
	})
	t.Run("Should return false after max attempts", func(t *testing.T) {
		var gets int
		h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "PUT" {
				w.WriteHeader(204)
				return
			}
			gets++
			jsonResp(w, 200, map[string]any{"enabled": false})
		})
		if h.c.EnableImmutableReleases(ctx()) || gets != 3 || len(h.sleeps) != 2 {
			t.Fatalf("gets=%d sleeps=%v", gets, h.sleeps)
		}
	})
	t.Run("Should return false when the PUT fails", func(t *testing.T) {
		h := newHarness(t, func(w http.ResponseWriter, r *http.Request) { jsonResp(w, 403, map[string]any{"message": "forbidden"}) })
		if h.c.EnableImmutableReleases(ctx()) {
			t.Fatal("expected false")
		}
	})
}

func TestTagRulesets(t *testing.T) {
	rs := map[int]map[string]any{
		1: {"id": 1, "name": "protect", "target": "tag", "enforcement": "active", "source_type": "Repository", "source": "owner/repo",
			"rules": []any{map[string]any{"type": "deletion"}}, "conditions": map[string]any{"ref_name": map[string]any{"include": []string{"refs/tags/v*"}, "exclude": []string{}}}},
		2: {"id": 2, "name": "nodel", "target": "tag", "enforcement": "active", "rules": []any{map[string]any{"type": "update"}},
			"conditions": map[string]any{"ref_name": map[string]any{"include": []string{"~ALL"}, "exclude": []string{}}}},
		3: {"id": 3, "name": "excluded", "target": "tag", "enforcement": "active", "rules": []any{map[string]any{"type": "deletion"}},
			"conditions": map[string]any{"ref_name": map[string]any{"include": []string{"~ALL"}, "exclude": []string{"refs/tags/v1.*"}}}},
		4: {"id": 4, "name": "org", "target": "tag", "enforcement": "evaluate", "source_type": "Organization", "source": "myorg",
			"rules": []any{map[string]any{"type": "deletion"}}, "conditions": map[string]any{"ref_name": map[string]any{"include": []string{"~ALL"}, "exclude": []string{}}}},
		5: {"id": 5, "name": "branch", "target": "branch", "enforcement": "active", "rules": []any{map[string]any{"type": "deletion"}},
			"conditions": map[string]any{"ref_name": map[string]any{"include": []string{"~ALL"}, "exclude": []string{}}}},
		6: {"id": 6, "name": "off", "target": "tag", "enforcement": "disabled", "rules": []any{map[string]any{"type": "deletion"}},
			"conditions": map[string]any{"ref_name": map[string]any{"include": []string{"~ALL"}, "exclude": []string{}}}},
	}
	t.Run("Should find active deletion rulesets matching the tag", func(t *testing.T) {
		h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/rulesets") {
				var l []any
				for i := 1; i <= 6; i++ {
					l = append(l, map[string]any{"id": i, "target": rs[i]["target"], "enforcement": rs[i]["enforcement"]})
				}
				jsonResp(w, 200, l)
				return
			}
			var id int
			fmt.Sscanf(r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:], "%d", &id)
			jsonResp(w, 200, rs[id])
		})
		got, err := h.c.TagRulesets(ctx(), "v1.0.0")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0].ID != 1 || got[0].Url != "https://github.com/owner/repo/rules/1" {
			t.Fatalf("%+v", got)
		}
		if got[1].ID != 4 || got[1].Source != "organization myorg" || got[1].Url != "https://github.com/organizations/myorg/settings/rules/4" {
			t.Fatalf("%+v", got[1])
		}
		names, _ := h.c.TagProtection(ctx(), "v1.0.0")
		if len(names) != 2 || names[0] != "protect" {
			t.Fatalf("%v", names)
		}
	})
	t.Run("Should return empty when rulesets cannot be read", func(t *testing.T) {
		h := newHarness(t, func(w http.ResponseWriter, r *http.Request) { jsonResp(w, 403, map[string]any{"message": "no"}) })
		names, err := h.c.TagProtection(ctx(), "v1.0.0")
		if err != nil || len(names) != 0 {
			t.Fatalf("%v %v", names, err)
		}
	})
}

func TestImmutableReleaseError(t *testing.T) {
	h := func(status int, body string) error {
		hn := newHarness(t, func(w http.ResponseWriter, r *http.Request) { jsonResp(w, status, body) })
		_, err := hn.c.CreateReleaseWithLatest(ctx(), "v1.0.0", false, nil)
		return err
	}
	immut := `{"message":"Validation Failed","errors":[{"resource":"Release","code":"custom","field":"tag_name","message":"tag_name was used by an immutable release"}]}`
	t.Run("Should detect a structured 422", func(t *testing.T) {
		if !IsImmutableReleaseError(h(422, immut)) {
			t.Fatal("expected true")
		}
	})
	t.Run("Should be false for a 422 with another message", func(t *testing.T) {
		if IsImmutableReleaseError(h(422, `{"message":"Validation Failed","errors":[{"field":"tag_name","message":"other"}]}`)) {
			t.Fatal("expected false")
		}
	})
	t.Run("Should be false for non-422 statuses", func(t *testing.T) {
		for _, s := range []int{400, 403, 404, 500} {
			if IsImmutableReleaseError(h(s, immut)) {
				t.Fatalf("status %d", s)
			}
		}
	})
	t.Run("Should fall back to string matching", func(t *testing.T) {
		if !IsImmutableReleaseError(errors.New("422 - tag_name was used by an immutable release")) {
			t.Fatal("expected true")
		}
	})
	t.Run("Should mark create release result unfixable", func(t *testing.T) {
		hn := newHarness(t, func(w http.ResponseWriter, r *http.Request) { jsonResp(w, 422, immut) })
		res, err := hn.c.CreateReleaseWithLatest(ctx(), "v1.0.0", false, nil)
		if err == nil || !errors.Is(err, state.ErrUnfixable) || res.Success || !res.Unfixable {
			t.Fatalf("%+v %v", res, err)
		}
	})
}

func TestCreateRef(t *testing.T) {
	type call struct{ m, p string }
	run := func(t *testing.T, known bool, patch func(w http.ResponseWriter), post int) (state.RefResult, []call) {
		var calls []call
		h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, call{r.Method, r.URL.Path})
			switch r.Method {
			case "PATCH":
				patch(w)
			case "POST":
				jsonResp(w, post, map[string]any{"ref": "refs/tags/v1"})
			}
		})
		if known {
			h.c.TrackRefs(state.NewVersionRef("v1", "refs/tags/v1", "aaa", state.TypeTag))
		}
		return h.c.CreateRef(ctx(), "refs/tags/v1", "bbb", true), calls
	}
	t.Run("Should require a manual fix on 403", func(t *testing.T) {
		res, _ := run(t, false, nil, 403)
		if res.Success || !res.RequiresManualFix {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("Should POST when a known ref PATCH returns 422 Reference does not exist", func(t *testing.T) {
		res, calls := run(t, true, func(w http.ResponseWriter) {
			jsonResp(w, 422, map[string]any{"message": "Reference does not exist"})
		}, 201)
		if !res.Success || len(calls) != 2 || calls[1].m != "POST" {
			t.Fatalf("%+v %v", res, calls)
		}
	})
	t.Run("Should fail when PATCH returns another 422", func(t *testing.T) {
		res, calls := run(t, true, func(w http.ResponseWriter) { jsonResp(w, 422, map[string]any{"message": "Something else"}) }, 201)
		if res.Success || len(calls) != 1 {
			t.Fatalf("%+v %v", res, calls)
		}
	})
	t.Run("Should POST when PATCH returns 404", func(t *testing.T) {
		res, calls := run(t, true, func(w http.ResponseWriter) { jsonResp(w, 404, map[string]any{"message": "Not Found"}) }, 201)
		if !res.Success || len(calls) != 2 {
			t.Fatalf("%+v %v", res, calls)
		}
	})
	t.Run("Should POST directly for an unknown ref", func(t *testing.T) {
		res, calls := run(t, false, nil, 201)
		if !res.Success || len(calls) != 1 || calls[0].m != "POST" {
			t.Fatalf("%+v %v", res, calls)
		}
	})
}

func releasesPage(nodes []map[string]any, more bool, cursor string) map[string]any {
	return map[string]any{"data": map[string]any{
		"rateLimit": map[string]any{"cost": 1, "remaining": 4999, "resetAt": "2030-01-01T00:00:00Z"},
		"repository": map[string]any{"releases": map[string]any{
			"nodes": nodes, "pageInfo": map[string]any{"hasNextPage": more, "endCursor": cursor}}}}}
}

func TestGetReleases(t *testing.T) {
	t.Run("Should query all required GraphQL fields", func(t *testing.T) {
		for _, f := range []string{"databaseId", "tagName", "isPrerelease", "isDraft", "immutable", "isLatest", "url", "tagCommit", "oid", "rateLimit", "cost", "remaining", "resetAt"} {
			if !strings.Contains(ReleasesQuery, f) {
				t.Errorf("query is missing %s", f)
			}
		}
		for _, f := range []string{"tagName", "isDraft", "immutable"} {
			if !strings.Contains(ReleaseImmutabilityQuery, f) {
				t.Errorf("immutability query is missing %s", f)
			}
		}
	})
	t.Run("Should map GraphQL nodes, paginate and honour ignore-versions", func(t *testing.T) {
		var pages int
		var progressOutput strings.Builder
		h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
			pages++
			if pages == 1 {
				jsonResp(w, 200, releasesPage([]map[string]any{
					{"databaseId": 1, "tagName": "v1.0.0", "isPrerelease": false, "isDraft": false, "immutable": true, "isLatest": true},
				}, true, "c1"))
				return
			}
			jsonResp(w, 200, releasesPage([]map[string]any{
				{"databaseId": 2, "tagName": "v2.0.0", "isPrerelease": true, "isDraft": true, "immutable": false, "isLatest": false},
			}, false, ""))
		}, func(o *Options) {
			o.IgnoreVersions = []string{"v2.*"}
			o.Progress = progress.New(progress.Options{Writer: &progressOutput})
		})
		rels, err := h.c.GetReleases(ctx())
		if err != nil || len(rels) != 2 || pages != 2 {
			t.Fatalf("%v %d %v", rels, pages, err)
		}
		a, b := rels[0], rels[1]
		if a.ID != 1 || !a.IsImmutable || !a.IsLatest || a.IsIgnored || b.ID != 2 || !b.IsPrerelease || !b.IsDraft || !b.IsIgnored {
			t.Fatalf("%+v %+v", a, b)
		}
		if rl, ok := h.c.LastGraphQLRateLimit(); !ok || rl.Remaining != 4999 {
			t.Fatalf("%+v", rl)
		}
		for _, want := range []string{"page 1, 1 releases loaded", "page 2, 2 releases loaded"} {
			if !strings.Contains(progressOutput.String(), want) {
				t.Errorf("progress output %q does not contain %q", progressOutput.String(), want)
			}
		}
	})
}

func TestAPIFailureHandling(t *testing.T) {
	status := func(s int) *harness {
		return newHarness(t, func(w http.ResponseWriter, r *http.Request) { jsonResp(w, s, map[string]any{"message": "x"}) },
			func(o *Options) { o.Transport.MaxAttempts = 1 })
	}
	t.Run("Should return an error when GetTags gets a 500", func(t *testing.T) {
		if _, err := status(500).c.GetTags(ctx(), ""); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("Should return empty when GetTags gets a 404", func(t *testing.T) {
		got, err := status(404).c.GetTags(ctx(), "")
		if err != nil || len(got) != 0 {
			t.Fatalf("%v %v", got, err)
		}
	})
	t.Run("Should return an error when GetBranches gets a 500", func(t *testing.T) {
		if _, err := status(500).c.GetBranches(ctx(), ""); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("Should return an error when GetReleases gets a 500", func(t *testing.T) {
		if _, err := status(500).c.GetReleases(ctx()); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("Should return empty for a 404 ref and an error otherwise", func(t *testing.T) {
		if sha, err := status(404).c.GetRef(ctx(), "v1", "tags"); sha != "" || err != nil {
			t.Fatalf("%q %v", sha, err)
		}
		if _, err := status(500).c.GetRef(ctx(), "v1", "tags"); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("Should return an error when ReleaseImmutable gets a 500", func(t *testing.T) {
		if _, err := status(500).c.ReleaseImmutable(ctx(), "v1"); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestTagsBranchesPagination(t *testing.T) {
	t.Run("Should follow Link headers and dereference annotated tags", func(t *testing.T) {
		var base string
		h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/git/tags/ann"):
				jsonResp(w, 200, map[string]any{"object": map[string]any{"sha": "commit1", "type": "commit"}})
			case strings.Contains(r.URL.Path, "/git/matching-refs/tags") || strings.Contains(r.URL.Path, "/git/refs/tags"):
				if r.URL.Query().Get("page") == "2" {
					jsonResp(w, 200, []any{map[string]any{"ref": "refs/tags/v1.0.0", "object": map[string]any{"sha": "ann", "type": "tag"}}})
					return
				}
				w.Header().Set("Link", fmt.Sprintf(`<%s%s?page=2>; rel="next"`, "https://api.github.com", r.URL.Path))
				jsonResp(w, 200, []any{map[string]any{"ref": "refs/tags/v1", "object": map[string]any{"sha": "s1", "type": "commit"}}})
			default:
				jsonResp(w, 404, map[string]any{"message": "nf"})
			}
		})
		_ = base
		tags, err := h.c.GetTags(ctx(), "")
		if err != nil || len(tags) != 2 {
			t.Fatalf("%v %v", tags, err)
		}
		if tags[1].Sha != "commit1" || !tags[1].IsPatch {
			t.Fatalf("%+v", tags[1])
		}
	})
}

func TestLoad(t *testing.T) {
	action := "name: 'My Action'\ndescription: Does things\nbranding:\n  icon: tag\n  color: blue\n"
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/git/refs/tags") || strings.Contains(p, "matching-refs/tags"):
			jsonResp(w, 200, []any{
				map[string]any{"ref": "refs/tags/v1", "object": map[string]any{"sha": "a", "type": "commit"}},
				map[string]any{"ref": "refs/tags/v1.0.0", "object": map[string]any{"sha": "a", "type": "commit"}},
				map[string]any{"ref": "refs/tags/latest", "object": map[string]any{"sha": "a", "type": "commit"}},
				map[string]any{"ref": "refs/tags/other", "object": map[string]any{"sha": "a", "type": "commit"}},
			})
		case strings.HasSuffix(p, "/branches"):
			jsonResp(w, 200, []any{map[string]any{"name": "main", "commit": map[string]any{"sha": "m"}}, map[string]any{"name": "v1", "commit": map[string]any{"sha": "b"}}})
		case strings.HasSuffix(p, "/graphql"):
			jsonResp(w, 200, releasesPage([]map[string]any{{"databaseId": 1, "tagName": "v1.0.0", "isLatest": true}}, false, ""))
		case strings.HasSuffix(p, "/contents/action.yaml"):
			jsonResp(w, 200, map[string]any{"type": "file", "encoding": "base64", "content": b64(action)})
		case strings.HasSuffix(p, "/contents/") || strings.HasSuffix(p, "/contents"):
			jsonResp(w, 200, []any{map[string]any{"name": "README.md", "type": "file", "path": "README.md"}})
		default:
			jsonResp(w, 404, map[string]any{"message": "nf"})
		}

	})
	cfg := state.Config{CheckMarketplace: "error", IgnoreVersions: []string{"v1.0.0"}}
	s, err := Load(ctx(), h.c, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("Should split version tags, latest and branches", func(t *testing.T) {
		names := []string{}
		for _, v := range s.Tags {
			names = append(names, v.Version)
		}
		if fmt.Sprint(names) != "[v1 v1.0.0 latest]" || len(s.Branches) != 1 || s.Branches[0].Version != "v1" {
			t.Fatalf("%v %v", names, s.Branches)
		}
	})
	t.Run("Should flag ignored versions", func(t *testing.T) {
		if !s.Tags[1].IsIgnored || s.Tags[0].IsIgnored || !s.Releases[0].IsIgnored {
			t.Fatal("ignore flags wrong")
		}
	})
	t.Run("Should read marketplace metadata", func(t *testing.T) {
		m := s.Marketplace
		if m == nil || !m.IsValid() || m.Name != "My Action" || m.BrandingIcon != "tag" || m.BrandingColor != "blue" {
			t.Fatalf("%+v", m)
		}
	})
	t.Run("Should skip marketplace when check-marketplace is none", func(t *testing.T) {
		s2, err := Load(ctx(), h.c, state.Config{CheckMarketplace: "none"})
		if err != nil || (s2.Marketplace != nil && s2.Marketplace.ActionFileExists) {
			t.Fatalf("%v %v", s2.Marketplace, err)
		}
	})
}

func TestLoadFailsOnMarketplaceMetadataError(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/git/") || strings.HasSuffix(r.URL.Path, "/branches"):
			jsonResp(w, 200, []any{})
		case strings.HasSuffix(r.URL.Path, "/graphql"):
			jsonResp(w, 200, releasesPage(nil, false, ""))
		case strings.HasSuffix(r.URL.Path, "/contents/action.yaml"):
			jsonResp(w, 403, map[string]any{"message": "metadata denied"})
		default:
			jsonResp(w, 404, map[string]any{"message": "unexpected request"})
		}
	})
	s, err := Load(ctx(), h.c, state.Config{CheckMarketplace: "error"})
	if err == nil || s != nil || !strings.Contains(err.Error(), "metadata denied") {
		t.Fatalf("state=%+v error=%v", s, err)
	}
}

func b64(s string) string {
	const a = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var out []byte
	for i := 0; i < len(s); i += 3 {
		var n uint32
		k := 0
		for j := 0; j < 3; j++ {
			n <<= 8
			if i+j < len(s) {
				n |= uint32(s[i+j])
				k++
			}
		}
		for j := 0; j < 4; j++ {
			if j <= k {
				out = append(out, a[(n>>(18-6*uint(j)))&63])
			} else {
				out = append(out, '=')
			}
		}
	}
	return string(out)
}

func TestMatchRefNamePattern(t *testing.T) {
	cases := []struct {
		p, r string
		want bool
	}{
		{"~ALL", "refs/tags/v1", true},
		{"refs/tags/v*", "refs/tags/v1.0", true},
		{"refs/tags/*", "refs/tags/a/b", false},
		{"refs/tags/**", "refs/tags/a/b", true},
		{"refs/tags/v1", "refs/tags/v2", false},
	}
	for _, c := range cases {
		t.Run(c.p+" vs "+c.r, func(t *testing.T) {
			if MatchRefNamePattern(c.p, c.r) != c.want {
				t.Fatal("mismatch")
			}
		})
	}
}

func TestRateLimit(t *testing.T) {
	reset := func(h *harness, d time.Duration) string { return fmt.Sprint(h.now.Add(d).Unix()) }
	t.Run("Should wait for a short primary reset and then succeed", func(t *testing.T) {
		var n int
		var hn *harness
		hn = newHarness(t, func(w http.ResponseWriter, r *http.Request) {
			n++
			if n == 1 {
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("X-RateLimit-Reset", reset(hn, 10*time.Second))
				jsonResp(w, 403, map[string]any{"message": "API rate limit exceeded for user"})
				return
			}
			jsonResp(w, 200, []any{})
		})
		if _, err := hn.c.GetBranches(ctx(), ""); err != nil || n != 2 || len(hn.sleeps) != 1 || hn.sleeps[0] < 10*time.Second {
			t.Fatalf("n=%d sleeps=%v err=%v", n, hn.sleeps, err)
		}
	})
	t.Run("Should fail fast when the primary reset exceeds MaxWait", func(t *testing.T) {
		var hn *harness
		hn = newHarness(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", reset(hn, time.Hour))
			jsonResp(w, 403, map[string]any{"message": "API rate limit exceeded for user"})
		})
		_, err := hn.c.GetBranches(ctx(), "")
		var rl *RateLimitError
		if !errors.Is(err, ErrRateLimited) || !errors.As(err, &rl) || rl.Wait < 59*time.Minute || len(hn.sleeps) != 0 {
			t.Fatalf("%v", err)
		}
		if hn.c.Fatal() == nil {
			t.Fatal("expected sticky fatal error")
		}
	})
	t.Run("Should honour Retry-After", func(t *testing.T) {
		var n int
		hn := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
			n++
			if n == 1 {
				w.Header().Set("Retry-After", "7")
				jsonResp(w, 429, map[string]any{"message": "slow down"})
				return
			}
			jsonResp(w, 200, []any{})
		})
		if _, err := hn.c.GetBranches(ctx(), ""); err != nil || len(hn.sleeps) != 1 || hn.sleeps[0] != 7*time.Second {
			t.Fatalf("%v %v", hn.sleeps, err)
		}
	})
	t.Run("Should back off for a secondary limit without headers", func(t *testing.T) {
		var n int
		hn := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
			n++
			if n == 1 {
				jsonResp(w, 403, map[string]any{"message": "You have exceeded a secondary rate limit."})
				return
			}
			jsonResp(w, 200, []any{})
		}, func(o *Options) { o.Transport.MaxWait = 2 * time.Minute })
		if _, err := hn.c.GetBranches(ctx(), ""); err != nil || len(hn.sleeps) != 1 || hn.sleeps[0] != time.Minute {
			t.Fatalf("%v %v", hn.sleeps, err)
		}
	})
	t.Run("Should wait and retry on a GraphQL RATE_LIMITED error", func(t *testing.T) {
		var n int
		var hn *harness
		hn = newHarness(t, func(w http.ResponseWriter, r *http.Request) {
			n++
			if n == 1 {
				w.Header().Set("X-RateLimit-Reset", reset(hn, 5*time.Second))
				jsonResp(w, 200, map[string]any{"errors": []any{map[string]any{"type": "RATE_LIMITED", "message": "API rate limit exceeded"}}})
				return
			}
			jsonResp(w, 200, releasesPage(nil, false, ""))
		})
		if _, err := hn.c.GetReleases(ctx()); err != nil || n != 2 || len(hn.sleeps) != 1 {
			t.Fatalf("n=%d %v %v", n, hn.sleeps, err)
		}
	})
	t.Run("Should reuse cached bodies for 304 responses", func(t *testing.T) {
		var n int
		hn := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
			n++
			if r.Header.Get("If-None-Match") == `"e1"` {
				w.WriteHeader(304)
				return
			}
			w.Header().Set("ETag", `"e1"`)
			jsonResp(w, 200, []any{map[string]any{"name": "main", "commit": map[string]any{"sha": "m"}}})
		})
		for i := 0; i < 2; i++ {
			b, err := hn.c.GetBranches(ctx(), "")
			if err != nil || len(b) != 1 {
				t.Fatalf("%v %v", b, err)
			}
		}
		if n != 2 || hn.c.Transport.Stats.NotModified != 1 {
			t.Fatalf("n=%d %+v", n, hn.c.Transport.Stats)
		}
	})
	t.Run("Should not retry a write after a 5xx", func(t *testing.T) {
		var n int
		hn := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
			n++
			jsonResp(w, 502, map[string]any{"message": "bad gateway"})
		})
		res := hn.c.DeleteRef(ctx(), "refs/tags/v1")
		if res.Success || n != 1 {
			t.Fatalf("n=%d %+v", n, res)
		}
	})
	t.Run("Should retry a read after a 5xx", func(t *testing.T) {
		var n int
		hn := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
			n++
			if n == 1 {
				jsonResp(w, 502, map[string]any{"message": "bad gateway"})
				return
			}
			jsonResp(w, 200, []any{})
		})
		if _, err := hn.c.GetBranches(ctx(), ""); err != nil || n != 2 {
			t.Fatalf("n=%d %v", n, err)
		}
	})
	t.Run("Should retry a write on a secondary rate limit", func(t *testing.T) {
		var n int
		hn := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
			n++
			if n == 1 {
				w.Header().Set("Retry-After", "3")
				jsonResp(w, 403, map[string]any{"message": "secondary rate limit"})
				return
			}
			w.WriteHeader(204)
		})
		if res := hn.c.DeleteRef(ctx(), "refs/tags/v1"); !res.Success || n != 2 {
			t.Fatalf("n=%d %+v", n, res)
		}
	})
	t.Run("Should pace writes", func(t *testing.T) {
		hn := newHarness(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }, func(o *Options) {
			o.Transport.WritePacing = time.Second
			o.Transport.Sleep = nil
		})
		start := time.Now()
		hn.c.DeleteRef(ctx(), "refs/tags/a")
		hn.c.DeleteRef(ctx(), "refs/tags/b")
		if time.Since(start) < 900*time.Millisecond {
			t.Fatalf("writes not paced: %v", time.Since(start))
		}
	})
	t.Run("Should stop waiting when the context is cancelled", func(t *testing.T) {
		var hn *harness
		hn = newHarness(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "5")
			jsonResp(w, 429, map[string]any{"message": "x"})
		}, func(o *Options) {
			o.Transport.Sleep = func(c context.Context, d time.Duration) error { return context.Canceled }
		})
		if _, err := hn.c.GetBranches(ctx(), ""); !errors.Is(err, context.Canceled) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("Should warn when quota is low", func(t *testing.T) {
		hn := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-RateLimit-Limit", "5000")
			w.Header().Set("X-RateLimit-Remaining", "100")
			w.Header().Set("X-RateLimit-Resource", "core")
			jsonResp(w, 200, []any{})
		})
		hn.c.GetBranches(ctx(), "")
		hn.c.GetBranches(ctx(), "")
		if len(hn.warns) != 1 {
			t.Fatalf("%v", hn.warns)
		}
	})
}

func TestCheckAuth(t *testing.T) {
	repoResp := func(scopes *string, perms map[string]bool, private bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if scopes != nil {
				w.Header().Set("X-OAuth-Scopes", *scopes)
			}
			body := map[string]any{"private": private}
			if perms != nil {
				body["permissions"] = perms
			}
			jsonResp(w, 200, body)
		}
	}
	sp := func(s string) *string { return &s }
	missing := func(r *AuthResult) []string {
		var o []string
		for _, m := range r.BlockingMissing() {
			o = append(o, m.Scope)
		}
		return o
	}
	t.Run("Should accept repo scope in fix mode and note missing workflow", func(t *testing.T) {
		h := newHarness(t, repoResp(sp("repo"), map[string]bool{"push": true}, true))
		r, err := CheckAuth(ctx(), h.c, ModeFix)
		if err != nil || !r.OK() || !hasOptional(r, "workflow") {
			t.Fatalf("%+v %v", r, err)
		}
		if r.RefreshCommand != "gh auth refresh -h github.com -s workflow" {
			t.Fatalf("refresh: %q", r.RefreshCommand)
		}
	})
	t.Run("Should accept repo and workflow", func(t *testing.T) {
		h := newHarness(t, repoResp(sp("repo, workflow"), map[string]bool{"push": true}, true))
		r, err := CheckAuth(ctx(), h.c, ModeFix)
		if err != nil || !r.OK() || hasOptional(r, "workflow") {
			t.Fatalf("%+v %v", r, err)
		}
	})
	t.Run("Should require repo for a private repo in check mode", func(t *testing.T) {
		h := newHarness(t, repoResp(sp("read:org"), nil, true))
		r, _ := CheckAuth(ctx(), h.c, ModeCheck)
		if r.OK() || fmt.Sprint(missing(r)) != "[repo]" {
			t.Fatalf("%v", missing(r))
		}
		if r.RefreshCommand != "gh auth refresh -h github.com -s repo" {
			t.Fatalf("refresh: %q", r.RefreshCommand)
		}
	})
	t.Run("Should accept public_repo for a public repo in fix mode", func(t *testing.T) {
		h := newHarness(t, repoResp(sp("public_repo"), map[string]bool{"push": true}, false))
		r, _ := CheckAuth(ctx(), h.c, ModeFix)
		if !r.OK() {
			t.Fatalf("%v", missing(r))
		}
	})
	t.Run("Should not require scopes for a public repo in check mode", func(t *testing.T) {
		h := newHarness(t, repoResp(sp(""), nil, false))
		if r, _ := CheckAuth(ctx(), h.c, ModeCheck); !r.OK() {
			t.Fatalf("%v", missing(r))
		}
	})
	t.Run("Should probe permissions for fine-grained tokens", func(t *testing.T) {
		h := newHarness(t, repoResp(nil, map[string]bool{"pull": true}, true))
		r, _ := CheckAuth(ctx(), h.c, ModeFix)
		if r.OK() || r.TokenKind != TokenOpaque || len(r.BlockingMissing()) != 1 || !strings.Contains(r.BlockingMissing()[0].Scope, "contents") {
			t.Fatalf("%+v", r)
		}
		h2 := newHarness(t, repoResp(nil, map[string]bool{"push": true}, true))
		if r, _ := CheckAuth(ctx(), h2.c, ModeFix); !r.OK() {
			t.Fatal("push permission should suffice")
		}
	})
	t.Run("Should return an AuthError for 401 and 404", func(t *testing.T) {
		for _, s := range []int{401, 404} {
			h := newHarness(t, func(w http.ResponseWriter, r *http.Request) { jsonResp(w, s, map[string]any{"message": "x"}) })
			_, err := CheckAuth(ctx(), h.c, ModeFix)
			var ae *AuthError
			if !errors.As(err, &ae) || ae.Status != s {
				t.Fatalf("status %d: %v", s, err)
			}
		}
	})
	t.Run("Should suggest the refresh command for a GHES host", func(t *testing.T) {
		h := newHarness(t, repoResp(sp("read:org"), nil, true), func(o *Options) { o.Repo.Host = "ghe.example.com" })
		r, _ := CheckAuth(ctx(), h.c, ModeFix)
		if r.RefreshCommand != "gh auth refresh -h ghe.example.com -s repo,workflow" {
			t.Fatalf("%q", r.RefreshCommand)
		}
	})
}

func hasOptional(r *AuthResult, scope string) bool {
	for _, m := range r.OptionalMissing() {
		if m.Scope == scope {
			return true
		}
	}
	return false
}
