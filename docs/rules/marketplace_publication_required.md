# Rule: marketplace_publication_required

## What This Rule Checks

Verifies that the latest release has been published to the GitHub Marketplace. This makes your action discoverable to millions of GitHub users through the marketplace search and listings.

## Why This Is An Issue

- **Impact:** If your action isn't published to the marketplace, users searching for actions won't find it. You miss out on discoverability, usage statistics, and the trust that comes with an official marketplace listing.
- **Best Practice:** After creating a release, publish it to the GitHub Marketplace to maximize visibility and adoption.

## When This Rule Applies

This rule runs when:
- `--level marketplace=error` or `--level marketplace=warning`
- Marketplace metadata is valid (name, description, branding, README exist)
- A release marked as "latest" exists in the repository

**Note:** This rule only checks the release marked as "latest" by GitHub. It does not check every release.

## Findings

| Finding | Summary |
|---|---|
| `marketplace_not_published` | The latest release is not published to GitHub Marketplace |

## Configuration

This rule belongs to the `marketplace` concern. Configure it and its findings in `action-version.yml`:

```yaml
concerns:
  marketplace:
    level: warning
rules:
  marketplace_publication_required:
    level: error
    findings:
      marketplace_not_published:
        level: warning
        autofix: false
```

### Level (`error`, `warning`, `none`)

- CLI: `--level marketplace=<level>` sets this concern; `--rule-level marketplace_publication_required=<level>` overrides this rule.
- Config file: `concerns.marketplace.level` (the flag wins over it).
- Finding override: use `--finding-level FINDING=<level>` or the finding-specific YAML keys above.
- Config file, this rule only: `rules.marketplace_publication_required.level` (wins over the concern; `none` disables just this rule).

Default: `error`.

### Auto-fix

- CLI: `fix --mode auto` applies the fix; use `fix --mode interactive` to review changes one by one. `verify` never changes anything.
- Config file: `concerns.marketplace.autofix: false` or `rules.marketplace_publication_required.autofix: false` turns the fix into a manual instruction, even when `fix --mode auto` is selected.

### Other settings that influence this rule

- `--ignore-versions v1.2.3,v2.*` / `ignore-versions` in the config file: matching versions are ignored by all rules. The flag wins.

## How Detection Works

The rule queries the public GitHub Marketplace URL:
```
https://github.com/marketplace/actions/{action-slug}?version={version}
```

When a version is published, the marketplace page displays "Use {version}". When not published, it falls back to "Use latest version". The rule detects this difference to determine publication status.

**Note:** If the marketplace check fails due to network issues, the rule logs a warning but does not fail (to avoid false positives).

## Automatic Remediation

This rule **cannot be auto-fixed** because:

- GitHub does not provide an API for marketplace publication
- Publication requires accepting the Marketplace Developer Agreement (first time)
- Category selection is a manual decision

When this rule detects a missing publication, it provides manual fix instructions with the exact steps and URLs needed.

## Manual Remediation

### Prerequisites

Before publishing, ensure:

- Your email is verified on GitHub
- `action.yaml` exists with `name`, `description`, and `branding` fields
- `README.md` exists in the repository root
- The release is published (not draft)

### Step 1: Navigate to the Release

Go to your release page:
```
https://github.com/{owner}/{repo}/releases/tag/{version}
```

### Step 2: Publish to Marketplace

1. Click **Edit** on the release
2. Check **Publish this Action to the GitHub Marketplace**
3. Review and accept the GitHub Marketplace Developer Agreement (first time only)
4. Select appropriate categories for your action
5. Click **Update release**

### Verification

After publishing, verify the action appears at:
```
https://github.com/marketplace/actions/{action-slug}?version={version}
```

## Related Rules

- [`action_metadata_required`](./action_metadata_required.md) - Validates required marketplace metadata (runs first)
- [`release_should_be_immutable`](./release_should_be_immutable.md) - Checks release immutability
- [`highest_patch_release_should_be_latest`](./highest_patch_release_should_be_latest.md) - Ensures correct "latest" release

## Rule Priority and Coordination

- **Priority 50** - Runs after all other rules
- Only runs if `action_metadata_required` passes (metadata must be valid first)
- Only checks the release marked as "latest" by GitHub
- Skips if no "latest" release exists

## Examples

### Failing Scenario

```
Repository state:
- Tag: v1.0.0 → abc123
- Release: v1.0.0 (published, isLatest=true)
- Marketplace metadata: Valid (name, description, branding, README)

Marketplace status:
- Action NOT published to marketplace

Issue: marketplace_not_published for v1.0.0
Status: manual_fix_required
```

### Passing Scenario

```
Repository state:
- Tag: v1.0.0 → abc123
- Release: v1.0.0 (published, isLatest=true)
- Marketplace metadata: Valid

Marketplace status:
- Action published at: github.com/marketplace/actions/my-action?version=v1.0.0

Result: No issues
```

### Not Applicable (Invalid Metadata)

```
Repository state:
- Tag: v1.0.0 → abc123
- Release: v1.0.0 (published, isLatest=true)
- Marketplace metadata: Invalid (missing branding)

Result: This rule does NOT apply
Note: The action_metadata_required rule will flag this instead
```

### Not Applicable (No Latest Release)

```
Repository state:
- Tag: v1.0.0 → abc123
- No releases exist

Result: This rule does NOT apply
Note: Other rules will flag the missing release
```

## Limitations

| Limitation | Description |
|------------|-------------|
| Network required | The marketplace URL check requires network access to github.com |
| GitHub.com only | GitHub Enterprise Server may not support marketplace features |
| Network failures | If the check fails due to network issues, the rule logs a warning but does not fail |
| Latest only | Only checks the release marked as "latest", not all releases |
| Manual publication | Cannot be auto-fixed - marketplace publication requires manual steps |

## Benefits of Marketplace Publication

Publishing to the GitHub Marketplace provides:

- **Discoverability** - Users can find your action via marketplace search
- **Verified listing** - Official listing with your branding
- **Usage statistics** - Insights into how many workflows use your action
- **Trust indicators** - "Verified creator" badge (if eligible)
- **Version management** - Users can browse and select specific versions
