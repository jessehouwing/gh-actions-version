# Rule: major_tag_tracks_highest_patch

## What This Rule Checks

Validates that major version tags (e.g., `v1`, `v2`) point to the same commit SHA as the highest patch version within that major version series (e.g., `v1.2.3`).

## Why This Is An Issue

- **Impact:** Users referencing `v1` expect to get the latest patch (`v1.x.x`). If the major tag points to an old commit, they get outdated code.
- **Best Practice:** GitHub Actions' [versioning strategy](https://docs.github.com/en/actions/how-tos/create-and-publish-actions/using-immutable-releases-and-tags-to-manage-your-actions-releases) requires floating versions (major tags) to track the latest patch.

## When This Rule Applies

This rule runs when:
- `floating-versions-use` is set to `tags` (default)
- A major version tag exists (vX)

## Configuration

This rule belongs to the `major` concern.

### Level (`error`, `warning`, `none`)

- CLI: no flag; configure it in `action-version.yml`.
- Config file: `concerns.major.level` for all rules of the concern, or `rules.major_tag_tracks_highest_patch.level` for this rule only (wins over the concern). `none` skips the check.

Default: `error`.

### Auto-fix

- CLI: `--auto-fix` (or `--interactive`, `--yes`) applies the fix; `--check` / `--no-fix` never changes anything. Without a mode flag the tool prompts on a terminal and only reports otherwise.
- Config file: `concerns.major.autofix: false` or `rules.major_tag_tracks_highest_patch.autofix: false` turns the fix into a manual instruction, even with `--auto-fix`.

### Other settings that influence this rule

- `--floating-versions-use <tags|branches>` / `floating-versions-use` in the config file: whether floating refs are tags or branches (default `tags`). The flag wins.
- `--include-preview-releases` / `ignore-preview-releases: false` in the config file: releases marked as prerelease are skipped when picking the latest patch by default. The flag wins.
- `--ignore-versions v1.2.3,v2.*` / `ignore-versions` in the config file: matching versions are ignored by all rules. The flag wins.

## Automatic Remediation

When `--auto-fix` is enabled, this rule uses `UpdateTagAction` which:

1. Force-pushes the major tag to point to the highest patch version's SHA
2. Preserves all other tags (only updates the major tag)

## Manual Remediation

Using Git:

```bash
# Update v1 to point to the same commit as v1.2.3
git tag -f v1 <sha-of-v1.2.3>
git push origin v1 --force
```

Using GitHub CLI:

```bash
# Get the SHA of the highest patch
SHA=$(git rev-parse v1.2.3)
# Force-update the major tag
git push origin $SHA:refs/tags/v1 --force
```

## Related Rules

- [`major_tag_missing`](./major_tag_missing.md) - Creates missing major tags
- [`major_branch_tracks_highest_patch`](./major_branch_tracks_highest_patch.md) - Branch equivalent for `floating-versions-use: branches`

## Examples

### Failing Scenario

```
Repository state:
- Tag: v1 → abc123 (old commit)
- Tag: v1.0.0 → abc123
- Tag: v1.1.0 → def456 (latest)

Issue: v1 points to abc123 but should point to def456
Action: UpdateTagAction (force-push v1 to def456)
```

### Passing Scenario

```
Repository state:
- Tag: v1 → def456
- Tag: v1.0.0 → abc123
- Tag: v1.1.0 → def456 (latest)

Result: No issues - v1 correctly tracks latest patch
```

### Not Applicable (No Patches)

```
Repository state:
- Tag: v1 → abc123

Result: No issues - no patches exist to track
```
