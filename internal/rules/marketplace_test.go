package rules

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/state"
)

func mkFind(name string) *Rule {
	for _, r := range All() {
		if r.Name == name {
			return r
		}
	}
	return nil
}

func mkValid() *state.MarketplaceMetadata {
	return &state.MarketplaceMetadata{ActionFileExists: true, ActionFilePath: "action.yaml", HasName: true, HasDescription: true,
		HasBrandingIcon: true, HasBrandingColor: true, ReadmeExists: true, Name: "Test Action"}
}

func TestMarketplaceSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Test Action":                "test-action",
		"Actions   SemVer   Checker": "actions-semver-checker",
		"My Action! (v2.0)":          "my-action-v20",
		"my-action":                  "my-action",
		"  Test Action  ":            "test-action",
	} {
		t.Run(in, func(t *testing.T) {
			if got := MarketplaceSlug(in); got != want {
				t.Fatalf("got %q want %q", got, want)
			}
		})
	}
}

func TestCheckMarketplaceVersionPublished(t *testing.T) {
	page := `<html><script type="application/json" data-target="react-app.embeddedData">{"payload":{"releaseData":{"releases":[{"tagName":"v1.0.0"}]}}}</script></html>`
	cases := []struct {
		name      string
		status    int
		body      string
		version   string
		wantPub   *bool
		wantError string
	}{
		{"published", 200, page, "v1.0.0", boolPtr(true), ""},
		{"not published when version missing", 200, page, "v2.0.0", boolPtr(false), ""},
		{"404 not published", 404, "", "v1.0.0", boolPtr(false), "not published"},
		{"server error unknown", 500, "", "v1.0.0", nil, "Failed to check marketplace"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.RequestURI()
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()
			res, err := checkMarketplaceVersionPublished(context.Background(), srv.Client(), srv.URL, "Test Action", c.version)
			if err != nil {
				t.Fatal(err)
			}
			if (res.Published == nil) != (c.wantPub == nil) || (res.Published != nil && *res.Published != *c.wantPub) {
				t.Fatalf("published = %v", res.Published)
			}
			if !strings.Contains(res.Error, c.wantError) {
				t.Fatalf("error = %q", res.Error)
			}
			if !strings.HasPrefix(gotPath, "/marketplace/actions/test-action?version=") {
				t.Fatalf("path = %q", gotPath)
			}
		})
	}
	t.Run("network error", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		url := srv.URL
		srv.Close()
		res, _ := checkMarketplaceVersionPublished(context.Background(), http.DefaultClient, url, "x", "v1")
		if res.Published != nil || !strings.Contains(res.Error, "Failed to check marketplace") {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("API disable guard", func(t *testing.T) {
		t.Setenv("GITHUB_API_DISABLE_API", "true")
		_, err := checkMarketplaceVersionPublished(context.Background(), http.DefaultClient, "http://x", "x", "v1")
		if err == nil || !strings.Contains(err.Error(), "GITHUB_API_DISABLE_API=true") {
			t.Fatalf("err = %v", err)
		}
	})
}

type fakeSrc struct {
	files   map[string]string
	entries []MarketplaceDirEntry
}

func (f fakeSrc) FileContents(_ context.Context, p, _ string) (string, error) { return f.files[p], nil }
func (f fakeSrc) RootEntries(context.Context, string) ([]MarketplaceDirEntry, error) {
	return f.entries, nil
}

func TestLoadActionMarketplaceMetadata(t *testing.T) {
	readme := []MarketplaceDirEntry{{"README.md", "file"}}
	full := "name: 'Test Action'\ndescription: 'A test action for unit testing'\nbranding:\n  icon: 'check-circle'\n  color: 'blue'\n"
	t.Run("full action.yaml", func(t *testing.T) {
		md, _ := LoadActionMarketplaceMetadata(context.Background(), fakeSrc{map[string]string{"action.yaml": full}, readme}, "")
		if !md.IsValid() || md.Name != "Test Action" || md.BrandingIcon != "check-circle" || md.BrandingColor != "blue" {
			t.Fatalf("%+v", md)
		}
	})
	t.Run("no action file", func(t *testing.T) {
		md, _ := LoadActionMarketplaceMetadata(context.Background(), fakeSrc{}, "")
		if md.ActionFileExists || md.IsValid() {
			t.Fatalf("%+v", md)
		}
	})
	t.Run("partial metadata", func(t *testing.T) {
		md, _ := LoadActionMarketplaceMetadata(context.Background(), fakeSrc{map[string]string{"action.yaml": "name: 'Test Action'\n"}, nil}, "")
		if !md.HasName || md.HasDescription || md.HasBrandingIcon || md.HasBrandingColor || md.ReadmeExists {
			t.Fatalf("%+v", md)
		}
		j := strings.Join(md.MissingRequirements(), "|")
		for _, w := range []string{"description", "branding.icon", "branding.color", "README"} {
			if !strings.Contains(j, w) {
				t.Fatalf("missing %s in %s", w, j)
			}
		}
	})
	t.Run("action.yml fallback", func(t *testing.T) {
		md, _ := LoadActionMarketplaceMetadata(context.Background(), fakeSrc{map[string]string{"action.yml": "name: 'Test Action YML'\n"}, nil}, "")
		if md.Name != "Test Action YML" || md.ActionFilePath != "action.yml" {
			t.Fatalf("%+v", md)
		}
	})
}

func TestMarketplaceMetadataModel(t *testing.T) {
	t.Run("IsValid true when all met", func(t *testing.T) {
		if !mkValid().IsValid() {
			t.Fatal()
		}
	})
	for name, mut := range map[string]func(*state.MarketplaceMetadata){
		"action file missing": func(m *state.MarketplaceMetadata) { m.ActionFileExists = false },
		"name missing":        func(m *state.MarketplaceMetadata) { m.HasName = false },
		"readme missing":      func(m *state.MarketplaceMetadata) { m.ReadmeExists = false },
	} {
		t.Run("IsValid false when "+name, func(t *testing.T) {
			m := mkValid()
			mut(m)
			if m.IsValid() {
				t.Fatal()
			}
		})
	}
	t.Run("missing requirements", func(t *testing.T) {
		m := (&state.MarketplaceMetadata{}).MissingRequirements()
		if len(m) != 5 || !strings.Contains(strings.Join(m, "|"), "README.md file in repository root") {
			t.Fatalf("%v", m)
		}
		if len(mkValid().MissingRequirements()) != 0 {
			t.Fatal()
		}
	})
}

func TestActionMetadataRequiredRule(t *testing.T) {
	r := mkFind("action_metadata_required")
	if r == nil || r.Category != "marketplace" || r.Priority > 10 {
		t.Fatalf("bad rule %+v", r)
	}
	st := state.New()
	st.Marketplace = &state.MarketplaceMetadata{ActionFilePath: "action.yaml"}
	for _, c := range []struct {
		cfg  string
		want int
	}{{"none", 0}, {"", 0}, {"error", 1}, {"warning", 1}} {
		t.Run("condition "+c.cfg, func(t *testing.T) {
			if got := len(r.Condition(st, &state.Config{CheckMarketplace: c.cfg})); got != c.want {
				t.Fatalf("got %d", got)
			}
		})
	}
	t.Run("empty when metadata valid", func(t *testing.T) {
		s := state.New()
		s.Marketplace = mkValid()
		if len(r.Condition(s, &state.Config{CheckMarketplace: "error"})) != 0 {
			t.Fatal()
		}
	})
	t.Run("CreateIssue", func(t *testing.T) {
		i := r.CreateIssue(nil, st, &state.Config{CheckMarketplace: "error"})
		if i.Status != state.StatusManualFixRequired || i.Action != nil || i.Severity != "error" {
			t.Fatalf("%+v", i)
		}
		if !strings.Contains(i.Message, "README.md") || !strings.Contains(i.Message, "action.yaml") || !strings.Contains(i.ManualFixCommand, "action.yaml") {
			t.Fatalf("%+v", i)
		}
		w := r.CreateIssue(nil, st, &state.Config{CheckMarketplace: "warning"})
		if w.Severity != "warning" {
			t.Fatal()
		}
	})
}

func TestMarketplacePublicationRequiredRule(t *testing.T) {
	r := mkFind("marketplace_publication_required")
	if r == nil || r.Category != "marketplace" || r.Priority < 40 {
		t.Fatalf("bad rule %+v", r)
	}
	cfg := &state.Config{CheckMarketplace: "error"}
	mk := func(md *state.MarketplaceMetadata, latest bool) *state.State {
		s := state.New()
		s.Marketplace = md
		s.Releases = []*state.ReleaseInfo{{TagName: "v1.0.0", IsLatest: latest}}
		return s
	}
	t.Run("condition", func(t *testing.T) {
		if len(r.Condition(mk(mkValid(), true), &state.Config{CheckMarketplace: "none"})) != 0 {
			t.Fatal("none")
		}
		if len(r.Condition(mk(&state.MarketplaceMetadata{}, true), cfg)) != 0 {
			t.Fatal("invalid")
		}
		if len(r.Condition(mk(mkValid(), false), cfg)) != 0 {
			t.Fatal("no latest")
		}
		got := r.Condition(mk(mkValid(), true), cfg)
		if len(got) != 1 || got[0].(*state.ReleaseInfo).TagName != "v1.0.0" {
			t.Fatal("latest")
		}
	})
	orig := MarketplaceVersionPublished
	defer func() { MarketplaceVersionPublished = orig }()
	rel := &state.ReleaseInfo{TagName: "v1.0.0"}
	for _, c := range []struct {
		name string
		md   *state.MarketplaceMetadata
		res  MarketplaceCheck
		err  error
		want bool
	}{
		{"true when metadata has no name", &state.MarketplaceMetadata{}, MarketplaceCheck{Published: boolPtr(false)}, nil, true},
		{"true when published", mkValid(), MarketplaceCheck{Published: boolPtr(true)}, nil, true},
		{"false when not published", mkValid(), MarketplaceCheck{Published: boolPtr(false)}, nil, false},
		{"true on check error", mkValid(), MarketplaceCheck{}, errors.New("boom"), true},
		{"true when unknown", mkValid(), MarketplaceCheck{Error: "x"}, nil, true},
	} {
		t.Run("Check "+c.name, func(t *testing.T) {
			MarketplaceVersionPublished = func(context.Context, string, string, string) (MarketplaceCheck, error) { return c.res, c.err }
			MarketplaceWarn = func(string) {}
			if got := r.Check(rel, mk(c.md, true), cfg); got != c.want {
				t.Fatalf("got %v", got)
			}
		})
	}
	t.Run("CreateIssue", func(t *testing.T) {
		s := mk(mkValid(), true)
		s.Owner, s.Repo = "o", "r"
		i := r.CreateIssue(rel, s, cfg)
		if i.Type != "marketplace_not_published" || i.Version != "v1.0.0" || i.Status != state.StatusManualFixRequired ||
			!strings.Contains(i.ManualFixCommand, "https://github.com/o/r/releases/tag/v1.0.0") ||
			!strings.Contains(i.ManualFixCommand, "/marketplace/actions/test-action?version=v1.0.0") {
			t.Fatalf("%+v", i)
		}
	})
}
