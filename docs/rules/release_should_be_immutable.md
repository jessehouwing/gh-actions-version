# Rule: release_should_be_immutable

## What This Rule Checks

Validates that published (non-draft) releases for patch versions are truly immutable according to GitHub's internal immutability flag. This requires the "Release immutability" feature to be enabled in the repository settings.

## Why This Is An Issue

- **Impact:** Even published releases can remain mutable if the repository doesn't have the "Release immutability" setting enabled. This violates GitHub's immutable release strategy and allows releases to be deleted or modified.
- **Best Practice:** GitHub's [immutable release strategy](https://docs.github.com/en/actions/how-tos/create-and-publish-actions/using-immutable-releases-and-tags-to-manage-your-actions-releases) requires that releases have the `immutable: true` flag, which can only be set by enabling "Release immutability" in repository settings.

## When This Rule Applies

This rule runs when:
- `--level immutability=error` or `--level immutability=warning`
- A published (non-draft) release exists for a patch version (vX.Y.Z)
- The release does not have the `immutable: true` flag (checked via GraphQL API)

**Note:** This rule only checks patch versions. Floating versions (vX, vX.Y) should not have releases at all.

## Findings

| Finding | Summary |
|---|---|
| `release_not_immutable` | A published patch release is not immutable |

## Configuration

This rule belongs to the `immutability` concern. Configure it and its findings in `action-version.yml`:

```yaml
concerns:
  immutability:
    level: warning
rules:
  release_should_be_immutable:
    level: error
    findings:
      release_not_immutable:
        level: warning
        fix: manual
```

### Level (`error`, `warning`, `none`)

- CLI: `--level immutability=<level>` sets this concern; `--rule-level release_should_be_immutable=<level>` overrides this rule.
- Config file: `concerns.immutability.level` (the flag wins over it).
- Finding override: use `--finding-level FINDING=<level>` or the finding-specific YAML keys above.
- Config file, this rule only: `rules.release_should_be_immutable.level` (wins over the concern; `none` disables just this rule).

Default: `error`.

### Auto-fix

- CLI: `fix --mode auto` applies the fix; use `fix --mode interactive` to review changes one by one. `verify` never changes anything.
- Config file: `concerns.immutability.fix: manual` or `rules.release_should_be_immutable.fix: manual` turns the fix into a manual instruction, even when `fix --mode auto` is selected.

### Other settings that influence this rule

- `--ignore-version v1.2.3 --ignore-version 'v2.*'` adds one-off ignores to the config; `--no-ignore` clears configured ignores first.
- `--dangerously-allow-destructive-tag-deletion`: required for the fix to delete a tag whose immutable release was deleted; there is no config file equivalent.

## Automatic Remediation

When you run `fix --mode auto`, this rule uses `RepublishReleaseAction` which:

1. Queries whether "Release immutability" is enabled (`GET /repos/{owner}/{repo}/immutable-releases`) and enables it (`PUT`) if it is off
2. Converts the release to draft
3. Republishes it
4. Verifies the `immutable: true` flag is set

**Important:** Enabling the setting requires repository admin permissions, which the default Actions `GITHUB_TOKEN` usually lacks. If it cannot be enabled, the issue is marked as "manual_fix_required" with the `gh api -X PUT` command to run, and the release is not republished (it would stay mutable).

**Note:** If the tag was previously used by a deleted immutable release, GitHub will return HTTP 422. Deleting the tag is irreversible and requires `permanently-delete-immutable-tags: dangerously-allow` in the policy or `--dangerously-allow-destructive-tag-deletion`. Otherwise the issue is **manual_fix_required** with a command to delete the tag yourself. If a tag ruleset blocks deletion, the issue is also **manual_fix_required**; the message and manual instructions name the ruleset(s) and give the remote deletion command (`git push origin :refs/tags/<tag>`).

## Manual Remediation

### Step 1: Enable Release Immutability in Repository Settings

Requires repository admin permissions:

```bash
gh api -X PUT "repos/{owner}/{repo}/immutable-releases"
```

Or in the web UI: **Settings** > **General** > **Releases** > enable **Release immutability**.

### Step 2: Republish the Release

Using GitHub CLI:

```bash
# Convert to draft then republish
gh release edit v1.0.0 --draft=true
gh release edit v1.0.0 --draft=false
```

Using GitHub Web UI:

1. Navigate to **Releases** in your repository
2. Find the release and click **Edit**
3. Check **Save as draft** and save
4. Edit again, uncheck **Save as draft**, and publish

## Related Rules

- [`patch_release_required`](./patch_release_required.md) - Ensures releases exist for patch versions
- [`release_should_be_published`](./release_should_be_published.md) - Ensures draft releases are published
- [`floating_version_no_release`](./floating_version_no_release.md) - Ensures floating versions don't have releases

## How Immutability Is Checked

This rule uses GitHub's GraphQL API to check the `immutable` field:

```graphql
query($owner: String!, $name: String!, $tag: String!) {
  repository(owner: $owner, name: $name) {
    release(tagName: $tag) {
      immutable
    }
  }
}
```

The `immutable` field is only `true` when:
1. The release is published (not draft)
2. The repository has "Release immutability" enabled in settings
3. The release was published after the setting was enabled

## Rule Priority and Coordination

- **Priority 12** - Runs after `release_should_be_published` (Priority 11)
- Only checks published releases (drafts are handled by the previous rule)

## Examples

### Failing Scenario

```
Repository state:
- Tag: v1.0.0 → abc123
- Release: v1.0.0 (published, immutable=false)

Repository settings:
- Release immutability: Disabled

Issue: non_immutable_release for v1.0.0
Action: RepublishReleaseAction (will detect setting is disabled and require manual fix)
```

### Passing Scenario

```
Repository state:
- Tag: v1.0.0 → abc123
- Release: v1.0.0 (published, immutable=true)

Repository settings:
- Release immutability: Enabled

Result: No issues
```

### Not Applicable (Draft Release)

```
Repository state:
- Tag: v1.0.0 → abc123
- Release: v1.0.0 (draft)

Result: This rule does NOT apply to drafts
Note: The release_should_be_published rule will flag this
```
