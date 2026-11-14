# Rule: release_should_be_published

## What This Rule Checks

Validates that draft releases for patch versions (e.g., `v1.0.0`) are published (not draft) when release checking or release immutability checking is enabled. This ensures releases are complete and become immutable as required by GitHub Actions best practices.

## Why This Is An Issue

- **Impact:** Draft releases are mutable and can be edited. When `--check-releases` is enabled, a draft release indicates an incomplete release process. When `--check-release-immutability` is enabled, draft releases violate GitHub's immutable release strategy for Actions.
- **Best Practice:** GitHub's [immutable release strategy](https://docs.github.com/en/actions/how-tos/create-and-publish-actions/using-immutable-releases-and-tags-to-manage-your-actions-releases) requires that patch versions use **published** (non-draft) releases to prevent content changes.

## When This Rule Applies

This rule runs when:
- `--check-releases` is set to `error` or `warning`, OR
- `--check-release-immutability` is set to `error` or `warning`
- AND a draft release exists for a patch version (vX.Y.Z)

**Note:** This rule only checks patch versions. Floating versions (vX, vX.Y) should not have releases at all - they are checked by the `floating_version_no_release` rule instead.

## Findings

| Finding | Summary |
|---|---|
| `release_is_draft` | A patch release is still a draft |

## Configuration

Finding-specific settings can be configured with `rules.<rule>.findings.<finding>.level` and `.autofix`; precedence is finding, then rule, then concern.


This rule belongs to the `immutability` concern.

### Level (`error`, `warning`, `none`)

- CLI: `--check-release-immutability=<level>` sets the level of every rule in this concern; `none` disables the rule.
- Config file: `concerns.immutability.level` (the flag wins over it).
- Config file, this rule only: `rules.release_should_be_published.level` (wins over the concern; `none` disables just this rule).

Default: `error`.

### Auto-fix

- CLI: `--auto-fix` (or `--interactive`, `--yes`) applies the fix; `--check` / `--no-fix` never changes anything. Without a mode flag the tool prompts on a terminal and only reports otherwise.
- Config file: `concerns.immutability.autofix: false` or `rules.release_should_be_published.autofix: false` turns the fix into a manual instruction, even with `--auto-fix`.

### Other settings that influence this rule

- `--ignore-versions v1.2.3,v2.*` / `ignore-versions` in the config file: matching versions are ignored by all rules. The flag wins.

## Required Permissions

**Important:** Draft releases are NOT visible via the GitHub API with `contents: read` permission. This rule can only detect draft releases if the token has write access to repository contents.

(in GitHub Actions: `permissions: contents: write`).

Without write access, draft releases will not be detected by this rule. Instead, the `patch_release_required` rule will incorrectly report them as missing releases.

## Automatic Remediation

When `--auto-fix` is enabled, this rule uses `PublishReleaseAction` which:

1. Publishes the draft release, making it immutable
2. Preserves all release metadata (notes, assets, etc.)

**Note:** If the tag was previously used by a deleted immutable release, GitHub will return HTTP 422 and the issue will be marked as **unfixable**. In this case, create a new patch version or add the version to `ignore-versions`.

## Manual Remediation

If auto-fix is not enabled or fails, you can manually fix this issue:

### Using GitHub CLI

```bash
# Publish the draft release
gh release edit v1.0.0 --draft=false
```

### Using GitHub Web UI

1. Navigate to **Releases** in your repository
2. Find the draft release (marked with "Draft")
3. Click **Edit**
4. Uncheck **Save as draft**
5. Click **Publish release**

## Related Rules

- [`patch_release_required`](./patch_release_required.md) - Ensures releases exist for patch versions
- [`release_should_be_immutable`](./release_should_be_immutable.md) - Validates that published releases have the immutable flag
- [`floating_version_no_release`](./floating_version_no_release.md) - Ensures floating versions don't have releases

## Rule Coordination

This rule complements `patch_release_required`:

1. `patch_release_required` (Priority 10) creates releases (either draft or published based on config)
2. `release_should_be_published` (Priority 11) publishes any remaining drafts

When `--check-release-immutability` is enabled:
- `patch_release_required` creates releases directly as published (AutoPublish=true)
- This rule handles any pre-existing drafts that need publishing

## Examples

### Failing Scenario

```
Repository state:
- Tag: v1.0.0 → abc123
- Release: v1.0.0 (draft)

Configuration:
- --check-release-immutability=error

Issue: draft_release for v1.0.0
Action: PublishReleaseAction (will publish the draft)
```

### Passing Scenario

```
Repository state:
- Tag: v1.0.0 → abc123
- Release: v1.0.0 (published, immutable)

Result: No issues
```

### Not Applicable (Floating Version)

```
Repository state:
- Tag: v1 → abc123
- Release: v1 (draft)

Result: This rule does NOT apply to floating versions
Note: The floating_version_no_release rule will flag this
```
