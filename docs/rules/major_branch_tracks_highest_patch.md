# Rule: major_branch_tracks_highest_patch

## What This Rule Checks

Validates that major version branches (e.g., `v1`, `v2`) point to the same commit SHA as the highest patch version within that major version series (e.g., `v1.2.3`).

## Why This Is An Issue

- **Impact:** Users referencing `v1` expect to get the latest patch (`v1.x.x`). If the major branch points to an old commit, they get outdated code.
- **Best Practice:** When using branches for floating versions, they should track the latest stable patch to ensure users get the most up-to-date code.
- **Branch Advantage:** Unlike tags, branches can be automatically updated via CI/CD workflows after each release.

## When This Rule Applies

This rule runs when:
- `floating-versions-use` is set to `branches`
- A major version branch exists (vX)

## Findings

| Finding | Summary |
|---|---|
| `major_branch_wrong_target` | A major version branch points to the wrong patch version |

## Configuration

Finding-specific settings can be configured with `rules.<rule>.findings.<finding>.level` and `.autofix`; precedence is finding, then rule, then concern.


This rule belongs to the `major` concern.

### Level (`error`, `warning`, `none`)

- CLI: no flag; configure it in `action-version.yml`.
- Config file: `concerns.major.level` for all rules of the concern, or `rules.major_branch_tracks_highest_patch.level` for this rule only (wins over the concern). `none` skips the check.

Default: `error`.

### Auto-fix

- CLI: `--auto-fix` (or `--interactive`, `--yes`) applies the fix; `--check` / `--no-fix` never changes anything. Without a mode flag the tool prompts on a terminal and only reports otherwise.
- Config file: `concerns.major.autofix: false` or `rules.major_branch_tracks_highest_patch.autofix: false` turns the fix into a manual instruction, even with `--auto-fix`.

### Other settings that influence this rule

- `--floating-versions-use <tags|branches>` / `floating-versions-use` in the config file: whether floating refs are tags or branches (default `tags`). The flag wins.
- `--include-preview-releases` / `ignore-preview-releases: false` in the config file: releases marked as prerelease are skipped when picking the latest patch by default. The flag wins.
- `--ignore-versions v1.2.3,v2.*` / `ignore-versions` in the config file: matching versions are ignored by all rules. The flag wins.

## Automatic Remediation

When `--auto-fix` is enabled, this rule uses `UpdateBranchAction` which:

1. Updates the major branch to point to the highest patch version's SHA
2. Force-pushes the branch reference
3. Preserves all other branches (only updates the major branch)

## Manual Remediation

Using Git:

```bash
# Update v1 to point to the same commit as v1.2.3
git checkout v1.2.3
git branch -f v1
git push origin v1 --force
```

Using GitHub CLI:

```bash
# Get the SHA of the highest patch
SHA=$(git rev-parse v1.2.3)
# Force-update the major branch
gh api repos/{owner}/{repo}/git/refs/heads/v1 -X PATCH -f sha="$SHA" -F force=true
```

## Related Rules

- [`major_branch_missing`](./major_branch_missing.md) - Creates missing major branches
- [`major_tag_tracks_highest_patch`](./major_tag_tracks_highest_patch.md) - Tag equivalent for `floating-versions-use: tags`
- [`minor_branch_tracks_highest_patch`](./minor_branch_tracks_highest_patch.md) - Similar validation for minor version branches

## Examples

### Failing Scenario

```
Repository state:
- Branch: v1 → abc123 (old commit)
- Tag: v1.0.0 → abc123
- Tag: v1.1.0 → def456 (latest)

Issue: v1 points to abc123 but should point to def456
Action: UpdateBranchAction (force-push v1 to def456)
```

### Passing Scenario

```
Repository state:
- Branch: v1 → def456
- Tag: v1.0.0 → abc123
- Tag: v1.1.0 → def456 (latest)

Result: No issues - v1 correctly tracks latest patch
```

### Not Applicable (Using Tags)

```
Configuration:
  floating-versions-use: tags

Result: This rule does not apply
```
