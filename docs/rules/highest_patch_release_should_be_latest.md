# highest_patch_release_should_be_latest

## Overview

This rule ensures that the correct release is marked as "latest" in GitHub. The latest release should be the highest non-prerelease, non-draft patch version.

## CRITICAL: Prerelease Determination

**Prerelease status is determined ONLY from the GitHub Release API's `prerelease` field.**

GitHub Actions does NOT support semver prerelease suffixes on tags:

- ❌ `v1.0.0-beta` - Tag suffix is ignored, will cause parsing errors
- ❌ `v1.0.0-rc1` - Tag suffix is ignored
- ❌ `v1.0.0-preview` - Tag suffix is ignored
- ✅ `v1.0.0` with `prerelease: true` on the GitHub Release - This is the ONLY supported way

When determining which release should be "latest", this rule checks `ReleaseInfo.IsPrerelease` which comes from the GitHub Release API, NOT from the tag name.

## When This Rule Applies

This rule is triggered when:

- `--check-releases` is set to `error` or `warning`
- There is at least one published, non-prerelease release

## Validation Logic

1. Finds all published, non-prerelease, non-ignored patch releases
2. Determines the highest version using semantic versioning (major.minor.patch)
3. Compares against the currently marked "latest" release in GitHub
4. Creates an issue if the wrong release is marked as latest

## Remediation

When an issue is detected, the rule creates a `SetLatestReleaseAction` which:

- Calls the GitHub API to update the release's `make_latest` setting
- Sets the correct release as the latest

**Manual fix command:**

```bash
gh release edit v1.0.0 --latest
```

## Related Rules

- `patch_release_required` - Ensures releases exist for patch versions
- `release_should_be_published` - Ensures draft releases are published
- `release_should_be_immutable` - Ensures releases are immutable

## Findings

| Finding | Summary |
|---|---|
| `latest_release_wrong` | The highest patch release is not marked as latest |

## Configuration

Finding-specific settings can be configured with `rules.<rule>.findings.<finding>.level` and `.autofix`; precedence is finding, then rule, then concern.


This rule belongs to the `releases` concern.

### Level (`error`, `warning`, `none`)

- CLI: `--check-releases=<level>` sets the level of every rule in this concern; `none` disables the rule.
- Config file: `concerns.releases.level` (the flag wins over it).
- Config file, this rule only: `rules.highest_patch_release_should_be_latest.level` (wins over the concern; `none` disables just this rule).

Default: `error`.

### Auto-fix

- CLI: `--auto-fix` (or `--interactive`, `--yes`) applies the fix; `--check` / `--no-fix` never changes anything. Without a mode flag the tool prompts on a terminal and only reports otherwise.
- Config file: `concerns.releases.autofix: false` or `rules.highest_patch_release_should_be_latest.autofix: false` turns the fix into a manual instruction, even with `--auto-fix`.

### Other settings that influence this rule

- `--ignore-versions v1.2.3,v2.*` / `ignore-versions` in the config file: matching versions are ignored by all rules. The flag wins.

## Examples

### Scenario 1: Prerelease incorrectly marked as latest

**State:**

- v1.0.0 (published, `prerelease: false` in API, not marked latest)
- v2.0.0 (published, `prerelease: true` in API, **marked as latest**)

**Result:** Issue created - v1.0.0 should be latest (v2.0.0 is a prerelease per the API)

### Scenario 2: Older version incorrectly marked as latest

**State:**

- v1.0.0 (published, **marked as latest**)
- v2.0.0 (published, not marked as latest)

**Result:** Issue created - v2.0.0 should be latest

### Scenario 3: Correct state

**State:**

- v1.0.0 (published, not marked latest)
- v2.0.0 (published, **marked as latest**)

**Result:** No issue - v2.0.0 is correctly marked as latest
