package rules

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/jessehouwing/gh-actions-version/internal/state"
)

var (
	slugSpaceRe   = regexp.MustCompile(`\s+`)
	slugInvalidRe = regexp.MustCompile(`[^a-z0-9\-]`)
	slugDashRe    = regexp.MustCompile(`-+`)
	slugTrimRe    = regexp.MustCompile(`^-|-$`)
)

// MarketplaceSlug converts an action name to its GitHub Marketplace URL slug (port of ConvertTo-MarketplaceSlug).
func MarketplaceSlug(actionName string) string {
	s := strings.ToLower(actionName)
	s = slugSpaceRe.ReplaceAllString(s, "-")
	s = slugInvalidRe.ReplaceAllString(s, "")
	s = slugDashRe.ReplaceAllString(s, "-")
	return slugTrimRe.ReplaceAllString(s, "")
}

// MarketplaceCheck is the outcome of a marketplace publication check.
type MarketplaceCheck struct {
	Published *bool // nil when unknown
	URL       string
	Error     string
}

// MarketplaceVersionPublished checks whether a version is published to the GitHub Marketplace.
// It is a variable so tests can replace it.
var MarketplaceVersionPublished = func(ctx context.Context, serverURL, actionName, version string) (MarketplaceCheck, error) {
	return checkMarketplaceVersionPublished(ctx, &http.Client{Timeout: 10 * time.Second}, serverURL, actionName, version)
}

// MarketplaceWarn reports a non-fatal marketplace problem; replaceable in tests.
var MarketplaceWarn = func(msg string) {
	msg = strings.NewReplacer("\r", " ", "\n", " ").Replace(msg)
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		fmt.Fprintf(os.Stderr, "::warning::%s\n", msg)
		return
	}
	fmt.Fprintf(os.Stderr, "warning: %s\n", msg)
}

var embeddedDataRe = regexp.MustCompile(`<script[^>]+data-target="react-app\.embeddedData"[^>]*>([^<]+)</script>`)

func boolPtr(b bool) *bool { return &b }

// checkMarketplaceVersionPublished fetches {server}/marketplace/actions/{slug}?version={version} and looks the
// version up in the embedded React app data (port of Test-MarketplaceVersionPublished).
func checkMarketplaceVersionPublished(ctx context.Context, client *http.Client, serverURL, actionName, version string) (MarketplaceCheck, error) {
	if serverURL == "" {
		serverURL = "https://github.com"
	}
	marketplaceURL := serverURL + "/marketplace/actions/" + MarketplaceSlug(actionName)
	versionURL := marketplaceURL + "?version=" + url.QueryEscape(version)

	if strings.EqualFold(os.Getenv("GITHUB_API_DISABLE_API"), "true") {
		return MarketplaceCheck{}, fmt.Errorf("API calls are disabled (GITHUB_API_DISABLE_API=true): Marketplace version check %s", version)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, versionURL, nil)
	if err != nil {
		return MarketplaceCheck{URL: marketplaceURL, Error: "Failed to check marketplace: " + err.Error()}, nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return MarketplaceCheck{URL: marketplaceURL, Error: "Failed to check marketplace: " + err.Error()}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return MarketplaceCheck{Published: boolPtr(false), URL: marketplaceURL, Error: "Action is not published to the GitHub Marketplace"}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return MarketplaceCheck{URL: marketplaceURL, Error: fmt.Sprintf("Failed to check marketplace: unexpected status %s", resp.Status)}, nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return MarketplaceCheck{URL: marketplaceURL, Error: "Failed to check marketplace: " + err.Error()}, nil
	}

	published := false
	if m := embeddedDataRe.FindSubmatch(body); m != nil {
		var data struct {
			Payload struct {
				ReleaseData struct {
					Releases []struct {
						TagName string `json:"tagName"`
					} `json:"releases"`
				} `json:"releaseData"`
			} `json:"payload"`
		}
		if json.Unmarshal(m[1], &data) == nil {
			for _, r := range data.Payload.ReleaseData.Releases {
				if r.TagName == version {
					published = true
					break
				}
			}
		}
	}
	return MarketplaceCheck{Published: boolPtr(published), URL: versionURL}, nil
}

// MarketplaceDirEntry is an entry of a repository directory listing.
type MarketplaceDirEntry struct {
	Name string
	Type string // "file" or "dir"
}

// MarketplaceSource reads the repository files needed to derive marketplace metadata.
type MarketplaceSource interface {
	// FileContents returns the file content, or "" when the file does not exist.
	FileContents(ctx context.Context, path, ref string) (string, error)
	RootEntries(ctx context.Context, ref string) ([]MarketplaceDirEntry, error)
}

var (
	metaNameRe        = regexp.MustCompile(`(?mi)^name:\s*['"]?([^'"#\r\n]+)['"]?`)
	metaDescRe        = regexp.MustCompile(`(?mi)^description:\s*['"]?([^'"#\r\n]+)['"]?`)
	metaDescMultiRe   = regexp.MustCompile(`(?mi)^description:\s*['"]\|`)
	metaBrandingRe    = regexp.MustCompile(`(?i)^\s*branding:\s*$`)
	metaIconRe        = regexp.MustCompile(`(?i)^\s{2,}icon:\s*['"]?([^'"#]+)['"]?\s*$`)
	metaColorRe       = regexp.MustCompile(`(?i)^\s{2,}color:\s*['"]?([^'"#]+)['"]?\s*$`)
	metaTopLevelRe    = regexp.MustCompile(`^\S`)
	metaBlankRe       = regexp.MustCompile(`^\s*$`)
	metaLineSplitRe   = regexp.MustCompile(`[\r\n]+`)
	metaReadmeNameRe  = regexp.MustCompile(`(?i)^readme(\.md)?$`)
	metaActionFileSet = []string{"action.yaml", "action.yml"}
)

// ParseActionMarketplaceMetadata derives metadata from action file content using the same
// regex-based parsing as the PowerShell implementation. actionContent == "" means no action file.
func ParseActionMarketplaceMetadata(actionPath, actionContent string, rootEntries []MarketplaceDirEntry) *state.MarketplaceMetadata {
	md := &state.MarketplaceMetadata{}

	if actionContent != "" && actionPath != "" {
		md.ActionFileExists = true
		md.ActionFilePath = actionPath

		if m := metaNameRe.FindStringSubmatch(actionContent); m != nil {
			md.Name = strings.TrimSpace(m[1])
			md.HasName = len(md.Name) > 0
		}

		if m := metaDescRe.FindStringSubmatch(actionContent); m != nil {
			md.Description = strings.TrimSpace(m[1])
			md.HasDescription = len(md.Description) > 0
		} else if metaDescMultiRe.MatchString(actionContent) {
			md.HasDescription = true
			md.Description = "(multi-line)"
		}

		inBranding := false
		for _, line := range metaLineSplitRe.Split(actionContent, -1) {
			if metaBrandingRe.MatchString(line) {
				inBranding = true
				continue
			}
			if !inBranding {
				continue
			}
			if m := metaIconRe.FindStringSubmatch(line); m != nil {
				md.BrandingIcon = strings.TrimSpace(m[1])
				md.HasBrandingIcon = len(md.BrandingIcon) > 0
			} else if m := metaColorRe.FindStringSubmatch(line); m != nil {
				md.BrandingColor = strings.TrimSpace(m[1])
				md.HasBrandingColor = len(md.BrandingColor) > 0
			} else if metaTopLevelRe.MatchString(line) && !metaBlankRe.MatchString(line) {
				break
			}
		}
	}

	for _, e := range rootEntries {
		if e.Type == "file" && metaReadmeNameRe.MatchString(e.Name) {
			md.ReadmeExists = true
			break
		}
	}
	return md
}

// LoadActionMarketplaceMetadata fetches action.yaml (then action.yml) and the root listing and parses them
// (port of Get-ActionMarketplaceMetadata). ref may be empty for the default branch.
func LoadActionMarketplaceMetadata(ctx context.Context, src MarketplaceSource, ref string) (*state.MarketplaceMetadata, error) {
	var content, path string
	for _, p := range metaActionFileSet {
		c, err := src.FileContents(ctx, p, ref)
		if err != nil {
			return nil, err
		}
		if c != "" {
			content, path = c, p
			break
		}
	}
	entries, err := src.RootEntries(ctx, ref)
	if err != nil {
		return nil, err
	}
	return ParseActionMarketplaceMetadata(path, content, entries), nil
}

// serverURLFor returns the web server URL for the state's host.
func serverURLFor(st *state.State) string {
	h := st.Host
	switch {
	case h == "":
		return "https://github.com"
	case strings.Contains(h, "://"):
		return strings.TrimRight(h, "/")
	}
	return "https://" + h
}
