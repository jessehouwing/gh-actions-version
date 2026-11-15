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

This rule belongs to the `major` concern. Configure it and its findings in `action-version.yml`:

```yaml
concerns:
  major:
    level: warning
rules:
  major_branch_tracks_highest_patch:
    level: error
    findings:
      major_branch_wrong_target:
        level: warning
        fix: manual
```

### Level (`error`, `warning`, `none`)

- CLI: `--level major=<level>` sets this concern; `--rule-level major_branch_tracks_highest_patch=<level>` overrides this rule.
- Config file: `concerns.major.level` for all rules of the concern, or `rules.major_branch_tracks_highest_patch.level` for this rule only (wins over the concern). `none` skips the check.
- Finding override: use `--finding-level FINDING=<level>` or the finding-specific YAML keys above.

Default: `error`.

### Auto-fix

- CLI: `fix --mode auto` applies the fix; use `fix --mode interactive` to review changes one by one. `verify` never changes anything.
- Config file: `concerns.major.fix: manual` or `rules.major_branch_tracks_highest_patch.fix: manual` turns the fix into a manual instruction, even when `fix --mode auto` is selected.

### Other settings that influence this rule

- `--floating-versions-use <tags|branches>` / `floating-versions-use` in the config file: whether floating refs are tags or branches (default `tags`). The flag wins.
- `--preview-releases=included|excluded` / `preview: include|exclude` in the config file controls prerelease selection when picking the latest patch (default: exclude). The flag wins.
- `--ignore-version v1.2.3 --ignore-version 'v2.*'` adds one-off ignores to the config; `--no-ignore` clears configured ignores first.

## Automatic Remediation

When you run `fix --mode auto`, this rule uses `UpdateBranchAction` which:

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
