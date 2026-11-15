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

## Findings

| Finding | Summary |
|---|---|
| `major_tag_wrong_target` | A major version tag points to the wrong patch version |

## Configuration

This rule belongs to the `major` concern. Configure it and its findings in `action-version.yml`:

```yaml
concerns:
  major:
    level: warning
rules:
  major_tag_tracks_highest_patch:
    level: error
    findings:
      major_tag_wrong_target:
        level: warning
        fix: manual
```

### Level (`error`, `warning`, `none`)

- CLI: `--level major=<level>` sets this concern; `--rule-level major_tag_tracks_highest_patch=<level>` overrides this rule.
- Config file: `concerns.major.level` for all rules of the concern, or `rules.major_tag_tracks_highest_patch.level` for this rule only (wins over the concern). `none` skips the check.
- Finding override: use `--finding-level FINDING=<level>` or the finding-specific YAML keys above.

Default: `error`.

### Auto-fix

- CLI: `fix --mode auto` applies the fix; use `fix --mode interactive` to review changes one by one. `verify` never changes anything.
- Config file: `concerns.major.fix: manual` or `rules.major_tag_tracks_highest_patch.fix: manual` turns the fix into a manual instruction, even when `fix --mode auto` is selected.

### Other settings that influence this rule

- `--floating-versions-use <tags|branches>` / `floating-versions-use` in the config file: whether floating refs are tags or branches (default `tags`). The flag wins.
- `--preview-releases=included|excluded` / `preview: include|exclude` in the config file controls prerelease selection when picking the latest patch (default: exclude). The flag wins.
- `--ignore-version v1.2.3 --ignore-version 'v2.*'` adds one-off ignores to the config; `--no-ignore` clears configured ignores first.

## Automatic Remediation

When you run `fix --mode auto`, this rule uses `UpdateTagAction` which:

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
