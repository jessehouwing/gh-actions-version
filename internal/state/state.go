// Package state is the domain model (port of lib/StateModel.ps1).
package state

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// Issue statuses.
const (
	StatusPending           = "pending"
	StatusFixed             = "fixed"
	StatusFailed            = "failed"
	StatusManualFixRequired = "manual_fix_required"
	StatusUnfixable         = "unfixable"
)

const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

const (
	TypeTag    = "tag"
	TypeBranch = "branch"
)

var semverRe = regexp.MustCompile(`^\d+(\.\d+){0,2}$`)

// VersionRef is a tag or branch with parsed version info. Suffixes such as -beta are not supported.
type VersionRef struct {
	Version   string // e.g. v1.0.0
	Ref       string // e.g. refs/tags/v1.0.0
	Sha       string
	Type      string // TypeTag or TypeBranch
	IsPatch   bool
	IsMinor   bool
	IsMajor   bool
	IsIgnored bool
	Major     int
	Minor     int
	Patch     int
}

// NewVersionRef parses version and returns the ref.
func NewVersionRef(version, ref, sha, typ string) *VersionRef {
	v := &VersionRef{Version: version, Ref: ref, Sha: sha, Type: typ}
	s := strings.TrimPrefix(version, "v")
	if !semverRe.MatchString(s) {
		return v
	}
	parts := strings.Split(s, ".")
	v.Major, _ = strconv.Atoi(parts[0])
	switch len(parts) {
	case 3:
		v.IsPatch = true
		v.Minor, _ = strconv.Atoi(parts[1])
		v.Patch, _ = strconv.Atoi(parts[2])
	case 2:
		v.IsMinor = true
		v.Minor, _ = strconv.Atoi(parts[1])
	case 1:
		v.IsMajor = true
	}
	return v
}

func (v *VersionRef) String() string { return v.Version + " -> " + v.Sha + " (" + v.Type + ")" }

// ReleaseInfo is a GitHub release. IsPrerelease comes from the Release API only.
type ReleaseInfo struct {
	TagName      string
	Sha          string
	IsDraft      bool
	IsPrerelease bool
	IsImmutable  bool
	IsIgnored    bool
	IsLatest     bool
	ID           int64
	HtmlUrl      string
}

func (r *ReleaseInfo) String() string {
	var st []string
	if r.IsDraft {
		st = append(st, "draft")
	}
	if r.IsPrerelease {
		st = append(st, "prerelease")
	}
	if !r.IsImmutable {
		st = append(st, "mutable")
	}
	if r.IsLatest {
		st = append(st, "latest")
	}
	if len(st) == 0 {
		return r.TagName
	}
	return r.TagName + " [" + strings.Join(st, ", ") + "]"
}

// MarketplaceMetadata describes the action metadata required for Marketplace publication.
type MarketplaceMetadata struct {
	ActionFileExists bool
	ActionFilePath   string
	HasName          bool
	HasDescription   bool
	HasBrandingIcon  bool
	HasBrandingColor bool
	ReadmeExists     bool
	Name             string
	Description      string
	BrandingIcon     string
	BrandingColor    string
	ValidationErrors []string
}

func (m *MarketplaceMetadata) IsValid() bool {
	return m.ActionFileExists && m.HasName && m.HasDescription && m.HasBrandingIcon && m.HasBrandingColor && m.ReadmeExists
}

func (m *MarketplaceMetadata) MissingRequirements() []string {
	var out []string
	if !m.HasName {
		out = append(out, "name property in action.yaml")
	}
	if !m.HasDescription {
		out = append(out, "description property in action.yaml")
	}
	if !m.HasBrandingIcon {
		out = append(out, "branding.icon property in action.yaml")
	}
	if !m.HasBrandingColor {
		out = append(out, "branding.color property in action.yaml")
	}
	if !m.ReadmeExists {
		out = append(out, "README.md file in repository root")
	}
	return out
}

// Action is a remediation action (port of RemediationAction).
type Action interface {
	Description() string
	Version() string
	Priority() int
	ManualCommandsLanguage() string // "bash" or "yaml"
	// Execute applies the fix. It returns true on success and may update the matching issue status.
	Execute(ctx context.Context, st *State, c Client) bool
	ManualCommands(st *State) []string
}

// Issue is a validation problem.
type Issue struct {
	Rule             string
	Finding          string
	Severity         string
	Message          string
	Version          string
	CurrentSha       string
	ExpectedSha      string
	ManualFixCommand string
	Action           Action
	Status           string
	// ManualOnly is set by the policy (fix: manual): the issue is reported with manual instructions
	// and never fixed by the tool.
	ManualOnly bool
}

func NewIssue(finding, severity, message string) *Issue {
	return &Issue{Finding: finding, Severity: severity, Message: message, Status: StatusPending}
}

func (i *Issue) IsAutoFixable() bool { return i.Action != nil && !i.ManualOnly }

// Priority mirrors the action priority (default 100).
func (i *Issue) Priority() int {
	if i.Action != nil {
		return i.Action.Priority()
	}
	return 100
}

func (i *Issue) String() string { return strings.ToUpper(i.Severity) + ": " + i.Message }

// Config holds the options that rules read (port of $Config / action inputs).
type Config struct {
	CheckMinorVersion     string // error|warning|none
	CheckReleases         string // error|warning|none
	CheckImmutability     string // error|warning|none
	CheckMarketplace      string // error|warning|none
	IgnorePreviewReleases bool
	FloatingVersionsUse   string // tags|branches
	AutoFix               bool
	// AllowDestructiveTagDeletion permits deleting a tag whose immutable release was deleted (irreversible).
	AllowDestructiveTagDeletion bool
	IgnoreVersions              []string
	IgnoreEntries               []IgnoreEntry
	// Sources maps effective policy paths to flag, file:<path>, or default.
	Sources map[string]string
	// Rules holds the per-rule level/fix policy from action-version.yml, keyed by rule name.
	Rules map[string]RulePolicy
}

type IgnoreEntry struct {
	Versions []string `yaml:"versions" json:"versions"`
	Reason   string   `yaml:"reason,omitempty" json:"reason,omitempty"`
	Scope    string   `yaml:"scope,omitempty" json:"scope"`
	Rules    []string `yaml:"rules,omitempty" json:"rules,omitempty"`
	Until    string   `yaml:"until,omitempty" json:"until,omitempty"`
}

// RulePolicy overrides how the issues of one rule are reported and fixed. Empty fields change nothing.
type RulePolicy struct {
	Level    string // error|warning|none
	AutoFix  *bool  // false: always a manual fix
	Findings map[string]FindingPolicy
}

// FindingPolicy overrides a rule's reporting and fix settings for one finding.
type FindingPolicy struct {
	Level   string
	AutoFix *bool
}

// State is the single source of truth (port of RepositoryState).
type State struct {
	Tags          []*VersionRef
	Branches      []*VersionRef
	Releases      []*ReleaseInfo
	Marketplace   *MarketplaceMetadata
	SkippedChecks []SkippedCheck
	// ImmutableReleasesEnabled is the repository's 'Release immutability' setting; nil when unknown.
	ImmutableReleasesEnabled *bool
	Owner                    string
	Repo                     string
	Host                     string
	Config                   Config
	Issues                   []*Issue
}

type SkippedCheck struct {
	Check  string
	Reason string
}

func New() *State { return &State{Marketplace: &MarketplaceMetadata{}} }

func (s *State) AddIssue(i *Issue) { s.Issues = append(s.Issues, i) }

// AllRefs returns tags followed by branches.
func (s *State) AllRefs() []*VersionRef {
	out := make([]*VersionRef, 0, len(s.Tags)+len(s.Branches))
	out = append(out, s.Tags...)
	return append(out, s.Branches...)
}

func (s *State) PatchVersions() []*VersionRef {
	return filter(s.AllRefs(), func(v *VersionRef) bool { return v.IsPatch })
}
func (s *State) MinorVersions() []*VersionRef {
	return filter(s.AllRefs(), func(v *VersionRef) bool { return v.IsMinor })
}
func (s *State) MajorVersions() []*VersionRef {
	return filter(s.AllRefs(), func(v *VersionRef) bool { return v.IsMajor })
}

func filter(in []*VersionRef, f func(*VersionRef) bool) []*VersionRef {
	var out []*VersionRef
	for _, v := range in {
		if f(v) {
			out = append(out, v)
		}
	}
	return out
}

// FindVersion looks up by version; typ is TypeTag, TypeBranch or "" for both (tags first).
func (s *State) FindVersion(version, typ string) *VersionRef {
	find := func(list []*VersionRef) *VersionRef {
		for _, v := range list {
			if v.Version == version {
				return v
			}
		}
		return nil
	}
	switch typ {
	case TypeTag:
		return find(s.Tags)
	case TypeBranch:
		return find(s.Branches)
	}
	if v := find(s.Tags); v != nil {
		return v
	}
	return find(s.Branches)
}

func (s *State) FindRelease(tag string) *ReleaseInfo {
	for _, r := range s.Releases {
		if r.TagName == tag {
			return r
		}
	}
	return nil
}

func (s *State) CountStatus(status string) int {
	n := 0
	for _, i := range s.Issues {
		if i.Status == status {
			n++
		}
	}
	return n
}

func (s *State) FixedCount() int     { return s.CountStatus(StatusFixed) }
func (s *State) FailedCount() int    { return s.CountStatus(StatusFailed) }
func (s *State) UnfixableCount() int { return s.CountStatus(StatusUnfixable) }
func (s *State) ManualFixCount() int { return s.CountStatus(StatusManualFixRequired) }

func (s *State) ErrorIssues() []*Issue   { return s.bySeverity(SeverityError) }
func (s *State) WarningIssues() []*Issue { return s.bySeverity(SeverityWarning) }

func (s *State) bySeverity(sev string) []*Issue {
	var out []*Issue
	for _, i := range s.Issues {
		if i.Severity == sev {
			out = append(out, i)
		}
	}
	return out
}

// ReturnCode is 1 when error-severity issues remain failed, manual_fix_required or unfixable.
func (s *State) ReturnCode() int {
	for _, i := range s.Issues {
		if i.Severity == SeverityError && (i.Status == StatusFailed || i.Status == StatusManualFixRequired || i.Status == StatusUnfixable) {
			return 1
		}
	}
	return 0
}

// RefResult is the outcome of a ref write.
type RefResult struct {
	Success           bool
	RequiresManualFix bool // token lacks the 'workflows' permission
	Unfixable         bool // e.g. HTTP 422 on immutable release conflicts
	Error             string
}

// Client is the write/read surface remediation actions use. internal/api implements it.
// Extend this interface (and the implementations) when an action needs more operations.
type Client interface {
	CreateRef(ctx context.Context, ref, sha string, force bool) RefResult
	DeleteRef(ctx context.Context, ref string) RefResult
	CreateRelease(ctx context.Context, tag string, draft bool, autoPublish bool) (ReleaseResult, error)
	PublishRelease(ctx context.Context, id int64, makeLatest *bool) (ReleaseResult, error)
	DeleteRelease(ctx context.Context, id int64) error
	SetReleaseLatest(ctx context.Context, id int64) error
	TagProtection(ctx context.Context, tag string) ([]string, error) // names of rulesets blocking deletion

	// Added for the remediation actions.

	// CreateReleaseWithLatest creates a release; makeLatest nil lets GitHub decide. Supersedes CreateRelease for actions.
	CreateReleaseWithLatest(ctx context.Context, tag string, draft bool, makeLatest *bool) (ReleaseResult, error)
	// ReleaseImmutable reports whether the release for tag is immutable (Test-ReleaseImmutability).
	ReleaseImmutable(ctx context.Context, tag string) (bool, error)
	// ImmutableReleasesEnabled returns the repo setting, or nil when it could not be determined.
	ImmutableReleasesEnabled(ctx context.Context) *bool
	// EnableImmutableReleases enables the setting and returns true once confirmed.
	EnableImmutableReleases(ctx context.Context) bool
	// RepublishRelease drafts and re-publishes the release for tag (Republish-GitHubRelease).
	RepublishRelease(ctx context.Context, tag string, makeLatest *bool) RepublishResult
	// TagRulesets returns active tag rulesets that block deleting tag (Get-GitHubTagProtection).
	TagRulesets(ctx context.Context, tag string) ([]Ruleset, error)
}

// ErrUnfixable can be returned (wrapped) by Client methods that return only an error
// when the failure is an HTTP 422 on an immutable release conflict.
var ErrUnfixable = errors.New("unfixable")

// Ruleset is a tag ruleset that blocks deleting a tag.
type Ruleset struct {
	ID          int64
	Name        string
	Enforcement string
	Source      string
	Url         string
}

// RepublishResult is the outcome of RepublishRelease.
type RepublishResult struct {
	Success   bool
	Unfixable bool
	Reason    string
}

// ReleaseResult is the outcome of a release write.
type ReleaseResult struct {
	Success           bool
	ID                int64
	HtmlUrl           string
	Unfixable         bool
	RequiresManualFix bool
	Error             string
}
