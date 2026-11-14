# Rule: patch_release_required

## What This Rule Checks

Validates that every patch version tag (e.g., `v1.0.0`) has a corresponding GitHub Release. This ensures that GitHub Actions users can depend on immutable releases for patch versions.

The rule also detects when floating versions (e.g., `v1`, `v1.0`) exist but their expected patch versions (e.g., `v1.0.0`) don't have releases yet.

## Why This Is An Issue

- **Impact:** Without releases, patch versions are just mutable tags that can be force-pushed, breaking the immutability contract that GitHub Actions expects.
- **Best Practice:** GitHub's [immutable release strategy](https://docs.github.com/en/actions/how-tos/create-and-publish-actions/using-immutable-releases-and-tags-to-manage-your-actions-releases) requires that patch versions use published releases to prevent tag movement.

## When This Rule Applies

This rule runs when:
- `--level releases=error` or `--level releases=warning`

## Findings

| Finding | Summary |
|---|---|
| `release_missing` | A patch version does not have a GitHub Release |
| `patch_tag_locked_by_deleted_release` | A patch tag is locked by a deleted immutable release |

## Configuration

This rule belongs to the `releases` concern. Configure it and its findings in `action-version.yml`:

```yaml
concerns:
  releases:
    level: warning
rules:
  patch_release_required:
    level: error
    findings:
      release_missing:
        level: warning
        autofix: false
      patch_tag_locked_by_deleted_release:
        level: warning
        autofix: false
```

### Level (`error`, `warning`, `none`)

- CLI: `--level releases=<level>` sets this concern; `--rule-level patch_release_required=<level>` overrides this rule.
- Config file: `concerns.releases.level` (the flag wins over it).
- Finding override: use `--finding-level FINDING=<level>` or the finding-specific YAML keys above.
- Config file, this rule only: `rules.patch_release_required.level` (wins over the concern; `none` disables just this rule).

Default: `error`.

### Auto-fix

- CLI: `fix --mode auto` applies the fix; use `fix --mode interactive` to review changes one by one. `verify` never changes anything.
- Config file: `concerns.releases.autofix: false` or `rules.patch_release_required.autofix: false` turns the fix into a manual instruction, even when `fix --mode auto` is selected.

### Other settings that influence this rule

- `--ignore-versions v1.2.3,v2.*` / `ignore-versions` in the config file: matching versions are ignored by all rules. The flag wins.

## Required Permissions

**Important:** Draft releases are NOT visible via the GitHub API with `contents: read` permission. If you have draft releases but the token only has read access, this rule will incorrectly report them as missing.

To see draft releases, use a token with write access to repository contents (in GitHub Actions: `permissions: contents: write`).

Without write access, use `--level releases=none` to disable this rule, or ensure all releases are published (not draft).

## Automatic Remediation

When you run `fix --mode auto`, this rule uses `CreateReleaseAction` which:

1. Creates a GitHub Release for the patch version
2. If the immutability level is not `none`, publishes the release immediately (making it immutable)
3. If the tag doesn't exist yet, GitHub's API will create it automatically

**Note:** If release creation returns HTTP 422 because this tag was used by a deleted immutable release, the issue is reclassified as `patch_tag_locked_by_deleted_release`. The default fix does not delete the tag. Deleting it is irreversible and only happens when `--dangerously-allow-destructive-tag-deletion` is explicitly enabled. Without that option, the finding requires manual attention.

Add the affected version to `ignore-versions` so floating versions and `latest` fall back to the previous version that still has a valid release. The finding message includes the ready-to-paste updated value; publish a new patch (or minor) release so floating versions and `latest` can advance to a valid release.

## Manual Remediation

If auto-fix is not enabled or fails, you can manually fix this issue:

### Using GitHub CLI

```bash
# Create a published release (immutable)
gh release create v1.0.0 --title "v1.0.0" --notes "Release v1.0.0"

# Or create a draft release (can be edited later)
gh release create v1.0.0 --title "v1.0.0" --notes "Release v1.0.0" --draft
```

### Using GitHub Web UI

1. Navigate to **Releases** in your repository
2. Click **Draft a new release**
3. Enter the tag name (e.g., `v1.0.0`)
4. Add a title and description
5. Click **Publish release** (for immutable) or **Save draft**

## Related Rules

- [`release_should_be_published`](./release_should_be_published.md) - Ensures draft releases are published
- [`release_should_be_immutable`](./release_should_be_immutable.md) - Validates that published releases are truly immutable
- [`patch_tag_missing`](./patch_tag_missing.md) - Handles tag creation when releases are not required

## Coordination with patch_tag_missing Rule

When `--level releases` is not `none`, the `patch_tag_missing` rule is automatically skipped. This is because:

1. GitHub's release API creates tags implicitly if they don't exist
2. Avoiding duplicate issues for the same problem
3. Ensuring the proper order: Release creation → Tag creation (implicit)

## Examples

### Failing Scenario

```
Repository state:
- Tag: v1 → abc123
- No tag: v1.0.0
- No release: v1.0.0

Configuration:
- --level releases=error
- --level immutability=error

Issue: missing_release for v1.0.0
Action: CreateReleaseAction (will create both release and tag)
```

### Passing Scenario

```
Repository state:
- Tag: v1.0.0 → abc123
- Release: v1.0.0 (published, immutable)

Result: No issues
```
