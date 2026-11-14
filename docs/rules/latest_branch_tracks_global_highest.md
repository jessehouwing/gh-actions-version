# Rule: latest_branch_tracks_global_highest

## What This Rule Checks

This rule verifies that the "latest" branch points to the same commit SHA as the global highest patch version across all major versions.

## Why This Is An Issue

When using branches for floating versions, the "latest" branch provides users with a convenient way to always get the newest version.

- **Impact:** Users referencing `latest` expect the newest patch version. If it points to an old commit, they get outdated code
- **Best Practice:** The "latest" alias should always track the absolute highest patch version in the repository
- **User Experience:** Ensures `uses: owner/repo@latest` gets the most recent release

## When This Rule Applies

This rule runs when:
- `floating-versions-use: branches`
- A "latest" branch exists

## Findings

| Finding | Summary |
|---|---|
| `latest_branch_wrong_target` | The latest branch points to the wrong patch version |

## Configuration

This rule belongs to the `latest` concern. Configure it and its findings in `action-version.yml`:

```yaml
concerns:
  latest:
    level: warning
rules:
  latest_branch_tracks_global_highest:
    level: error
    findings:
      latest_branch_wrong_target:
        level: warning
        fix: manual
```

### Level (`error`, `warning`, `none`)

- CLI: `--level latest=<level>` sets this concern; `--rule-level latest_branch_tracks_global_highest=<level>` overrides this rule.
- Config file: `concerns.latest.level` for all rules of the concern, or `rules.latest_branch_tracks_global_highest.level` for this rule only (wins over the concern). `none` skips the check.
- Finding override: use `--finding-level FINDING=<level>` or the finding-specific YAML keys above.

Default: `error`.

### Auto-fix

- CLI: `fix --mode auto` applies the fix; use `fix --mode interactive` to review changes one by one. `verify` never changes anything.
- Config file: `concerns.latest.fix: manual` or `rules.latest_branch_tracks_global_highest.fix: manual` turns the fix into a manual instruction, even when `fix --mode auto` is selected.

### Other settings that influence this rule

- `--floating-versions-use <tags|branches>` / `floating-versions-use` in the config file: whether floating refs are tags or branches (default `tags`). The flag wins.
- `--preview-releases=included|excluded` / `preview: include|exclude` in the config file controls prerelease selection when picking the latest patch (default: exclude). The flag wins.
- `--ignore-versions v1.2.3,v2.*` / `ignore-versions` in the config file: matching versions are ignored by all rules. The flag wins.

## Manual Remediation

If auto-fix is not enabled or fails, you can manually update the latest branch:

### Using GitHub CLI

```bash
# Find the highest patch version
HIGHEST_TAG=$(gh api repos/{owner}/{repo}/git/refs/tags | jq -r '.[] | select(.ref | contains("v")) | .ref' | sort -V | tail -1)

# Get the SHA
SHA=$(gh api repos/{owner}/{repo}/git/ref/${HIGHEST_TAG#refs/} --jq '.object.sha')

# Force-update the latest branch
gh api repos/{owner}/{repo}/git/refs/heads/latest \
  -X PATCH \
  -f sha="$SHA" \
  -F force=true
```

### Using Git

```bash
# Checkout the highest patch version
git checkout v2.5.3

# Force-update and push the latest branch
git branch -f latest
git push origin latest --force
```

### Using GitHub Web UI

1. Navigate to your repository's **Branches** page
2. Find the "latest" branch
3. Use the "..." menu to delete it
4. Create a new branch named "latest" from the highest patch tag

## Related Rules

- [`latest_branch_missing`](./latest_branch_missing.md) - Creates the latest branch if it doesn't exist
- [`latest_tag_tracks_global_highest`](./latest_tag_tracks_global_highest.md) - Tag equivalent for `floating-versions-use: tags`

## Examples

### Failing Scenario

```
Repository state:
- v1.0.0 → abc123 (tag)
- v2.0.0 → def456 (tag)
- v2.5.3 → ghi789 (tag, highest)
- latest → def456 (branch, pointing to old v2.0.0)

Issue: latest points to def456 but should point to ghi789
Action: UpdateBranchAction (force-push latest to ghi789)
```

### Passing Scenario

```
Repository state:
- v1.0.0 → abc123 (tag)
- v2.0.0 → def456 (tag)
- v2.5.3 → ghi789 (tag, highest)
- latest → ghi789 (branch)

Result: No issues - latest correctly tracks highest patch ✓
```

### Not Applicable (No Patches)

```
Repository state:
- latest → abc123 (branch)
- No patch versions exist

Result: No issues - no patches to track
```
