# Rule: major_branch_missing

## What This Rule Checks

This rule verifies that major version branches (e.g., `v1`, `v2`) exist for all major versions that have at least one patch version.

## Why This Is An Issue

Major version branches provide a convenient way for users to always get the latest version within a major series without needing to track specific patch numbers.

- **Impact:** Users cannot use major version aliases (e.g., `uses: owner/repo@v1`) to get the latest patch in that major version
- **Best Practice:** When using branches for floating versions, major branches should exist to track the latest patches
- **Continuous Delivery:** Branches enable automatic updates via CI/CD after each release

## When This Rule Applies

This rule runs when:
- `floating-versions-use: branches`
- At least one patch version exists for a major version (e.g., `v1.0.0`)
- No major version branch exists (e.g., `v1`)

## Findings

| Finding | Summary |
|---|---|
| `major_branch_missing` | A major version branch is missing |

## Configuration

This rule belongs to the `major` concern. Configure it and its findings in `action-version.yml`:

```yaml
concerns:
  major:
    level: warning
rules:
  major_branch_missing:
    level: error
    findings:
      major_branch_missing:
        level: warning
        fix: manual
```

### Level (`error`, `warning`, `none`)

- CLI: `--level major=<level>` sets this concern; `--rule-level major_branch_missing=<level>` overrides this rule.
- Config file: `concerns.major.level` for all rules of the concern, or `rules.major_branch_missing.level` for this rule only (wins over the concern). `none` skips the check.
- Finding override: use `--finding-level FINDING=<level>` or the finding-specific YAML keys above.

Default: `error`.

### Auto-fix

- CLI: `fix --mode auto` applies the fix; use `fix --mode interactive` to review changes one by one. `verify` never changes anything.
- Config file: `concerns.major.fix: manual` or `rules.major_branch_missing.fix: manual` turns the fix into a manual instruction, even when `fix --mode auto` is selected.

### Other settings that influence this rule

- `--floating-versions-use <tags|branches>` / `floating-versions-use` in the config file: whether floating refs are tags or branches (default `tags`). The flag wins.
- `--preview-releases=included|excluded` / `preview: include|exclude` in the config file controls prerelease selection when picking the latest patch (default: exclude). The flag wins.
- `--ignore-version v1.2.3 --ignore-version 'v2.*'` adds one-off ignores to the config; `--no-ignore` clears configured ignores first.

## Manual Remediation

If auto-fix is not enabled or fails, you can manually create the missing major branch:

### Using GitHub CLI

```bash
# Find the highest patch for the major series (e.g., v1.x.x)
gh api repos/{owner}/{repo}/git/refs/tags | jq -r '.[] | select(.ref | contains("v1.")) | .ref'

# Get the SHA of the highest patch
SHA=$(gh api repos/{owner}/{repo}/git/ref/tags/v1.2.3 --jq '.object.sha')

# Create the major branch
gh api repos/{owner}/{repo}/git/refs \
  -f ref="refs/heads/v1" \
  -f sha="$SHA"
```

### Using Git

```bash
# Checkout the highest patch version
git checkout v1.2.3

# Create and push the major branch
git checkout -b v1
git push origin v1
```

## Related Rules

- [`major_branch_tracks_highest_patch`](./major_branch_tracks_highest_patch.md) - Validates that existing major branches point to the correct SHA
- [`minor_branch_missing`](./minor_branch_missing.md) - Similar validation for minor version branches
- [`major_tag_missing`](./major_tag_missing.md) - Tag equivalent for `floating-versions-use: tags`

## Examples

### Failing Scenario

```
Repository state:
- v1.0.0 → abc123 (tag)
- v1.1.0 → def456 (tag)
- v1.2.0 → ghi789 (tag)
- v1 branch is missing

Issue: Major version branch v1 is missing but patch versions exist
Remediation: Create v1 branch pointing to ghi789 (same as v1.2.0)
```

### Passing Scenario

```
Repository state:
- v1.0.0 → abc123 (tag)
- v1.1.0 → def456 (tag)
- v1.2.0 → ghi789 (tag)
- v1 → ghi789 (branch)

All major version branches exist and point to the correct SHA ✓
```
