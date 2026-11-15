# Rule: tag_should_be_branch

## What This Rule Checks

This rule validates that floating version references (vX, vX.Y) exist as **branches** rather than tags when the action is configured to use branches for floating versions.

**Important:** Patch versions (vX.Y.Z) must always remain as tags regardless of this setting, as they need to be immutable and linked to GitHub Releases.

**Note:** This rule only triggers when a tag exists but the corresponding branch does not. If both a tag and branch exist for the same version, a separate duplicate detection rule will handle that scenario.

## Why This Is An Issue

- **Impact:** When using branches mode, floating versions need to be branches to allow easy updates without force-pushing tags
- **Best Practice:** Branches are mutable by default and can be updated normally. Some teams prefer this approach for floating versions while keeping patch versions as immutable tags

## When This Rule Applies

This rule runs when:
- `floating-versions-use` is set to `branches`
- Tags exist for major (vX) or minor (vX.Y) versions that should be branches

## Findings

| Finding | Summary |
|---|---|
| `floating_ref_should_be_branch` | A floating version should be a branch |

## Configuration

This rule belongs to the `refs` concern. Configure it and its findings in `action-version.yml`:

```yaml
concerns:
  refs:
    level: warning
rules:
  tag_should_be_branch:
    level: error
    findings:
      floating_ref_should_be_branch:
        level: warning
        fix: manual
```

### Level (`error`, `warning`, `none`)

- CLI: `--level refs=<level>` sets this concern; `--rule-level tag_should_be_branch=<level>` overrides this rule.
- Config file: `concerns.refs.level` for all rules of the concern, or `rules.tag_should_be_branch.level` for this rule only (wins over the concern). `none` skips the check.
- Finding override: use `--finding-level FINDING=<level>` or the finding-specific YAML keys above.

Default: `error`.

### Auto-fix

- CLI: `fix --mode auto` applies the fix; use `fix --mode interactive` to review changes one by one. `verify` never changes anything.
- Config file: `concerns.refs.fix: manual` or `rules.tag_should_be_branch.fix: manual` turns the fix into a manual instruction, even when `fix --mode auto` is selected.

### Other settings that influence this rule

- `--floating-versions-use <tags|branches>` / `floating-versions-use` in the config file: whether floating refs are tags or branches (default `tags`). The flag wins.
- `--ignore-version v1.2.3 --ignore-version 'v2.*'` adds one-off ignores to the config; `--no-ignore` clears configured ignores first.

## Manual Remediation

If auto-fix is not enabled or fails, you can manually convert tags to branches:

### Using Git

```bash
# Convert tag to branch (preserves the same commit SHA)
git fetch origin refs/tags/v1:refs/heads/v1
git push origin :refs/tags/v1
git push origin refs/heads/v1
```

### Using GitHub CLI

```bash
# Get the SHA from the tag
SHA=$(gh api repos/:owner/:repo/git/refs/tags/v1 --jq '.object.sha')

# Create the branch
gh api repos/:owner/:repo/git/refs -f ref='refs/heads/v1' -f sha="$SHA"

# Delete the tag
gh api repos/:owner/:repo/git/refs/tags/v1 -X DELETE
```

### Using GitHub Web UI

1. Navigate to the repository's **Code** tab
2. Click on the branch dropdown
3. Type the version name (e.g., `v1`) in the search box
4. Click **Create branch: v1 from 'main'** (or select the appropriate base)
5. Navigate to **Tags** and delete the tag version

Note: You'll need to ensure the branch points to the same commit as the tag before deleting the tag.

## Related Rules

- [`branch_should_be_tag`](./branch_should_be_tag.md) - Opposite rule when using tags mode (default)
- [`duplicate_floating_version_ref`](./duplicate_floating_version_ref.md) - Handles cases where both tag and branch exist

## Examples

### Failing Scenario

Repository has:
- Tag `v1` → abc123
- Tag `v1.0` → abc123
- Tag `v1.0.0` → abc123

Configuration:
```yaml
floating-versions-use: branches
```

**Result:** 2 issues created for v1 and v1.0 (NOT v1.0.0, as patches must remain tags)

### Passing Scenario

Repository has:
- Branch `v1` → abc123
- Branch `v1.0` → abc123
- Tag `v1.0.0` → abc123 (with release)

Configuration:
```yaml
floating-versions-use: branches
```

**Result:** No issues (floating versions are branches, patch is a tag)
