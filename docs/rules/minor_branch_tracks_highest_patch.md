# Rule: minor_branch_tracks_highest_patch

## What This Rule Checks

Validates that minor version branches (e.g., `v1.0`, `v1.1`) point to the same commit SHA as the highest patch version within that minor version series (e.g., `v1.0.3`).

## Why This Is An Issue

- **Impact:** Users referencing `v1.0` expect to get the latest patch in that minor series (`v1.0.x`). If the minor branch points to an old commit, they get outdated code with potential bugs or missing features.
- **Best Practice:** When using branches for floating versions, they should track the latest stable patch to ensure users get the most up-to-date code.
- **Continuous Updates:** Branches enable automatic SHA updates via CI/CD after each patch release.

## When This Rule Applies

This rule runs when:
- `floating-versions-use` is set to `branches`
- `--level minor=error` or `--level minor=warning`
- A minor version branch exists (vX.Y)

## Findings

| Finding | Summary |
|---|---|
| `minor_branch_wrong_target` | A minor version branch points to the wrong patch version |

## Configuration

This rule belongs to the `minor` concern. Configure it and its findings in `action-version.yml`:

```yaml
concerns:
  minor:
    level: warning
rules:
  minor_branch_tracks_highest_patch:
    level: error
    findings:
      minor_branch_wrong_target:
        level: warning
        fix: manual
```

### Level (`error`, `warning`, `none`)

- CLI: `--level minor=<level>` sets this concern; `--rule-level minor_branch_tracks_highest_patch=<level>` overrides this rule.
- Config file: `concerns.minor.level` (the flag wins over it).
- Finding override: use `--finding-level FINDING=<level>` or the finding-specific YAML keys above.
- Config file, this rule only: `rules.minor_branch_tracks_highest_patch.level` (wins over the concern; `none` disables just this rule).

Default: `error`.

### Auto-fix

- CLI: `fix --mode auto` applies the fix; use `fix --mode interactive` to review changes one by one. `verify` never changes anything.
- Config file: `concerns.minor.fix: manual` or `rules.minor_branch_tracks_highest_patch.fix: manual` turns the fix into a manual instruction, even when `fix --mode auto` is selected.

### Other settings that influence this rule

- `--floating-versions-use <tags|branches>` / `floating-versions-use` in the config file: whether floating refs are tags or branches (default `tags`). The flag wins.
- `--preview-releases=included|excluded` / `preview: include|exclude` in the config file controls prerelease selection when picking the latest patch (default: exclude). The flag wins.
- `--ignore-version v1.2.3 --ignore-version 'v2.*'` adds one-off ignores to the config; `--no-ignore` clears configured ignores first.

## Automatic Remediation

When you run `fix --mode auto`, this rule uses `UpdateBranchAction` which:

1. Updates the minor branch to point to the highest patch version's SHA within that minor series
2. Force-pushes the branch reference
3. Preserves all other branches (only updates the minor branch)

## Manual Remediation

Using Git:

```bash
# Update v1.0 to point to the same commit as v1.0.3 (highest patch in v1.0.x series)
git checkout v1.0.3
git branch -f v1.0
git push origin v1.0 --force
```

Using GitHub CLI:

```bash
# Get the SHA of the highest patch in the minor series
SHA=$(git rev-parse v1.0.3)
# Force-update the minor branch
gh api repos/{owner}/{repo}/git/refs/heads/v1.0 -X PATCH -f sha="$SHA" -F force=true
```

## Related Rules

- [`minor_branch_missing`](./minor_branch_missing.md) - Creates missing minor branches
- [`major_branch_tracks_highest_patch`](./major_branch_tracks_highest_patch.md) - Similar validation for major version branches
- [`minor_tag_tracks_highest_patch`](./minor_tag_tracks_highest_patch.md) - Tag equivalent for `floating-versions-use: tags`

## Examples

### Failing Scenario

```
Repository state:
- Branch: v1.0 → abc123 (old commit)
- Tag: v1.0.0 → abc123
- Tag: v1.0.1 → def456
- Tag: v1.0.2 → ghi789 (latest in v1.0.x series)

Issue: v1.0 points to abc123 but should point to ghi789
Action: UpdateBranchAction (force-push v1.0 to ghi789)
```

### Passing Scenario

```
Repository state:
- Branch: v1.0 → ghi789
- Tag: v1.0.0 → abc123
- Tag: v1.0.1 → def456
- Tag: v1.0.2 → ghi789 (latest in v1.0.x series)

Result: No issues - v1.0 correctly tracks latest patch
```

### Severity: Warning

When `--level minor=warning` is set, this rule reports issues with warning severity instead of error:

```bash
gh actions-version verify --level minor=warning
```

This allows minor version mismatches to be flagged without failing the run.
