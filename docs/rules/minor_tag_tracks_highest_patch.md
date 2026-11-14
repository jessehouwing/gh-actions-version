# Rule: minor_tag_tracks_highest_patch

## What This Rule Checks

Validates that minor version tags (e.g., `v1.0`, `v1.1`) point to the same commit SHA as the highest patch version within that minor version series (e.g., `v1.0.3`).

## Why This Is An Issue

- **Impact:** Users referencing `v1.0` expect to get the latest patch in that minor series (`v1.0.x`). If the minor tag points to an old commit, they get outdated code with potential bugs or missing features.
- **Best Practice:** GitHub Actions' [versioning strategy](https://docs.github.com/en/actions/how-tos/create-and-publish-actions/using-immutable-releases-and-tags-to-manage-your-actions-releases) requires floating versions (minor tags) to track the latest patch in their series.

## When This Rule Applies

This rule runs when:
- `floating-versions-use` is set to `tags` (default)
- `--level minor=error` or `--level minor=warning`
- A minor version tag exists (vX.Y)

## Findings

| Finding | Summary |
|---|---|
| `minor_tag_wrong_target` | A minor version tag points to the wrong patch version |

## Configuration

This rule belongs to the `minor` concern. Configure it and its findings in `action-version.yml`:

```yaml
concerns:
  minor:
    level: warning
rules:
  minor_tag_tracks_highest_patch:
    level: error
    findings:
      minor_tag_wrong_target:
        level: warning
        autofix: false
```

### Level (`error`, `warning`, `none`)

- CLI: `--level minor=<level>` sets this concern; `--rule-level minor_tag_tracks_highest_patch=<level>` overrides this rule.
- Config file: `concerns.minor.level` (the flag wins over it).
- Finding override: use `--finding-level FINDING=<level>` or the finding-specific YAML keys above.
- Config file, this rule only: `rules.minor_tag_tracks_highest_patch.level` (wins over the concern; `none` disables just this rule).

Default: `error`.

### Auto-fix

- CLI: `fix --mode auto` applies the fix; use `fix --mode interactive` to review changes one by one. `verify` never changes anything.
- Config file: `concerns.minor.autofix: false` or `rules.minor_tag_tracks_highest_patch.autofix: false` turns the fix into a manual instruction, even when `fix --mode auto` is selected.

### Other settings that influence this rule

- `--floating-versions-use <tags|branches>` / `floating-versions-use` in the config file: whether floating refs are tags or branches (default `tags`). The flag wins.
- `--include-preview-releases` / `ignore-preview-releases: false` in the config file: releases marked as prerelease are skipped when picking the latest patch by default. The flag wins.
- `--ignore-versions v1.2.3,v2.*` / `ignore-versions` in the config file: matching versions are ignored by all rules. The flag wins.

## Automatic Remediation

When you run `fix --mode auto`, this rule uses `UpdateTagAction` which:

1. Force-pushes the minor tag to point to the highest patch version's SHA within that minor series
2. Preserves all other tags (only updates the minor tag)

## Manual Remediation

Using Git:

```bash
# Update v1.0 to point to the same commit as v1.0.3 (highest patch in v1.0.x series)
git tag -f v1.0 <sha-of-v1.0.3>
git push origin v1.0 --force
```

Using GitHub CLI:

```bash
# Get the SHA of the highest patch in the minor series
SHA=$(git rev-parse v1.0.3)
# Force-update the minor tag
git push origin $SHA:refs/tags/v1.0 --force
```

## Related Rules

- [`minor_tag_missing`](./minor_tag_missing.md) - Creates missing minor tags
- [`major_tag_tracks_highest_patch`](./major_tag_tracks_highest_patch.md) - Similar validation for major version tags
- [`minor_branch_tracks_highest_patch`](./minor_branch_tracks_highest_patch.md) - Branch equivalent for `floating-versions-use: branches`

## Examples

### Failing Scenario

```
Repository state:
- Tag: v1.0 → abc123 (old commit)
- Tag: v1.0.0 → abc123
- Tag: v1.0.1 → def456
- Tag: v1.0.2 → ghi789 (latest in v1.0.x series)

Issue: v1.0 points to abc123 but should point to ghi789
Action: UpdateTagAction (force-push v1.0 to ghi789)
```

### Passing Scenario

```
Repository state:
- Tag: v1.0 → ghi789
- Tag: v1.0.0 → abc123
- Tag: v1.0.1 → def456
- Tag: v1.0.2 → ghi789 (latest in v1.0.x series)

Result: No issues - v1.0 correctly tracks latest patch
```

### Not Applicable (No Patches in Series)

```
Repository state:
- Tag: v1.0 → abc123

Result: No issues - no patches exist in this minor series to track
```

### Severity: Warning

When `--level minor=warning` is set, this rule reports issues with warning severity instead of error:

```bash
gh actions-version verify --level minor=warning
```

This allows minor version mismatches to be flagged without failing the run.
