# Actions SemVer Checker Action

Every time you publish a new version of a GitHub Action, say `v1.2.3`, it's customary to also update the tags for `v1.2` and `v1` to point to the same commit. That way people can subscribe to  either an exact version or a floating version that's automatically updated when the action's author pushes a new version.

Unfortunately, GitHub's creative use of tags doesn't do this automatically and many actions don't auto-update their major and minor versions whenever they release a new  patch.

You can run this action for your GitHub Action's repository to ensure the correct tags have been created and point to the correct commits.

## Quick Start

```yaml
- uses: jessehouwing/actions-semver-checker@v2
  with:
    # GitHub token for API access. Falls back to GITHUB_TOKEN if not provided.
    # Required for auto-fix mode
    token: ${{ secrets.GITHUB_TOKEN }}
    
    # Check minor version tags (v1.0) in addition to major tags (v1)
    # Options: error, warning, none, true, false
    # Default: error
    check-minor-version: 'error'
    
    # Require GitHub Releases for every patch version (v1.0.0)
    # Options: error, warning, none, true, false  
    # Default: error
    check-releases: 'error'
    
    # Ensure releases are published (immutable), not drafts
    # Options: error, warning, none, true, false
    # Default: error
    check-release-immutability: 'error'
    
    # Validate GitHub Marketplace metadata and publication
    # Checks action.yaml has name, description, branding; README.md exists
    # Options: error, warning, none, true, false
    # Default: error
    check-marketplace: 'error'
    
    # Exclude preview/pre-release versions from floating version calculations
    # Options: true, false
    # Default: true
    ignore-preview-releases: 'true'
    
    # Use tags or branches for floating versions (v1, v1.0, latest)
    # Options: tags, branches
    # Default: tags
    floating-versions-use: 'tags'
    
    # Automatically fix detected issues. Requires contents: write permission
    # Options: true, false
    # Default: false
    auto-fix: 'false'
    
    # Comma-separated list of versions to skip during validation
    # Supports wildcards (e.g., 'v1.*')
    # Default: "" (empty)
    ignore-versions: ''

    # Allow auto-fix to delete a tag whose immutable release was deleted (cannot be undone)
    # Default: false
    dangerously-allow-destructive-tag-deletion: 'false'
```

The action installs the `gh` extension that does the checking (pinned to the action's own version) and runs it
with these inputs. Defaults can also be set per repository with an `action-version.yml`
(see [`docs/cli.md`](docs/cli.md#version-policy-action-versionyml));
inputs and CLI flags that are explicitly set override it (these inputs default to empty, which means "not set").

## GitHub's Immutable Release Strategy

This action implements [GitHub's recommended approach](https://docs.github.com/en/actions/how-tos/create-and-publish-actions/using-immutable-releases-and-tags-to-manage-your-actions-releases) for versioning actions:

- **Patch versions (v1.0.0, v1.0.1)** → Immutable GitHub Releases that never change
- **Floating versions (v1, v1.0, latest)** → Mutable Git tags that point to the latest compatible release

This strategy balances **stability** (pinned versions never change) with **convenience** (floating versions get updates automatically).

📖 **[Read the comprehensive versioning guidance](docs/versioning-guidance.md)** for detailed best practices, workflows, and troubleshooting.

## Features

- ✅ Validates that major (`v1`) and minor (`v1.0`) version tags point to the latest patch version
- ✅ Checks that every patch version (`v1.0.0`) has a corresponding GitHub Release (via REST API)
- ✅ Verifies that releases are immutable (not in draft status)
- ✅ Supports filtering preview/pre-release versions from major/minor tag calculations
- ✅ Can configure floating versions (major/minor/latest) to use branches or tags
- ✅ Provides suggested commands to fix any issues with direct links to GitHub release pages
- ✅ Optional auto-fix mode to automatically update version tags/branches
- ✅ **NEW in v2:** No checkout required - uses GitHub REST API exclusively
- ✅ Ignore specific versions from validation (useful for legacy versions)
- ✅ Auto-fix automatically republishes non-immutable releases to make them immutable (when `check-release-immutability` is enabled)
- ✅ Retry logic with exponential backoff for better reliability
- ✅ Validates GitHub Marketplace metadata (action name, description, branding, README)
- ✅ Checks that releases are published to the GitHub Marketplace

Example output:

> ### Annotations
>
> 🔴 Manual Remediation Required
>
> ```text
> Version: v1 ref 59499a44cd4482b68a7e989a5e7dd781414facfa must match: v1.0.6 ref 1a13fd188ebef96fb179faedfabcc8de5cb6189d
> Version: v1.0 ref 59499a44cd4482b68a7e989a5e7dd781414facfa must match: v1.0.6 ref 1a13fd188ebef96fb179faedfabcc8de5cb6189d
> Version: latest ref 59499a44cd4482b68a7e989a5e7dd781414facfa must match: v1.0.6 ref 1a13fd188ebef96fb179faedfabcc8de5cb6189d
> ```

And a set of suggested Git commands to fix this:

> ### Suggested fix
>
> ```bash
> git push origin 1a13fd188ebef96fb179faedfabcc8de5cb6189d:refs/tags/v1 --force
> git push origin 1a13fd188ebef96fb179faedfabcc8de5cb6189d:refs/tags/v1.0 --force
> git push origin 1a13fd188ebef96fb179faedfabcc8de5cb6189d:latest --force
> ```

## CLI (`gh` extension)

The same checks are available from the command line as a `gh` extension:

```bash
gh extension install jessehouwing/gh-version-action
gh version-action --check -R owner/repo
```

See the [CLI documentation](docs/cli.md) for flags, modes, authentication and the `action-version.yml` policy.

## Supported Validation Rules

The action uses a modular rule-based validation system. Each rule can be configured independently via action inputs.

### Reference Type Rules

| Rule | Description | Documentation |
| --- | --- | --- |
| `tag_should_be_branch` | Validates that floating versions use branches when configured for branches mode | [📖 Details](lib/rules/ref_type/tag_should_be_branch/README.md) |
| `branch_should_be_tag` | Validates that floating versions use tags when configured for tags mode | [📖 Details](lib/rules/ref_type/branch_should_be_tag/README.md) |
| `duplicate_floating_version_ref` | Detects when a floating version exists as both tag and branch | [📖 Details](lib/rules/ref_type/duplicate_floating_version_ref/README.md) |
| `duplicate_latest_ref` | Detects when 'latest' exists as both tag and branch | [📖 Details](lib/rules/ref_type/duplicate_latest_ref/README.md) |
| `duplicate_patch_version_ref` | Detects when a patch version exists as both tag and branch | [📖 Details](lib/rules/ref_type/duplicate_patch_version_ref/README.md) |

### Release Rules

| Rule | Description | Documentation |
| --- | --- | --- |
| `patch_release_required` | Ensures every patch version has a GitHub Release | [📖 Details](lib/rules/releases/patch_release_required/README.md) |
| `release_should_be_published` | Validates that releases are published, not drafts | [📖 Details](lib/rules/releases/release_should_be_published/README.md) |
| `release_should_be_immutable` | Ensures releases are truly immutable | [📖 Details](lib/rules/releases/release_should_be_immutable/README.md) |
| `highest_patch_release_should_be_latest` | Ensures the correct release is marked as 'latest' | [📖 Details](lib/rules/releases/highest_patch_release_should_be_latest/README.md) |
| `floating_version_no_release` | Warns when a release exists for a floating version | [📖 Details](lib/rules/releases/floating_version_no_release/README.md) |
| `duplicate_release` | Detects and removes duplicate draft releases for the same tag | [📖 Details](lib/rules/releases/duplicate_release/README.md) |

### Version Tracking Rules

| Rule | Description | Documentation |
| --- | --- | --- |
| `major_tag_missing` | Detects missing major version tags (v1, v2) | [📖 Details](lib/rules/version_tracking/major_tag_missing/README.md) |
| `major_tag_tracks_highest_patch` | Ensures major tags point to latest patch | [📖 Details](lib/rules/version_tracking/major_tag_tracks_highest_patch/README.md) |
| `major_branch_missing` | Detects missing major version branches | [📖 Details](lib/rules/version_tracking/major_branch_missing/README.md) |
| `major_branch_tracks_highest_patch` | Ensures major branches point to latest patch | [📖 Details](lib/rules/version_tracking/major_branch_tracks_highest_patch/README.md) |
| `minor_tag_missing` | Detects missing minor version tags (v1.0, v1.1) | [📖 Details](lib/rules/version_tracking/minor_tag_missing/README.md) |
| `minor_tag_tracks_highest_patch` | Ensures minor tags point to latest patch | [📖 Details](lib/rules/version_tracking/minor_tag_tracks_highest_patch/README.md) |
| `minor_branch_missing` | Detects missing minor version branches | [📖 Details](lib/rules/version_tracking/minor_branch_missing/README.md) |
| `minor_branch_tracks_highest_patch` | Ensures minor branches point to latest patch | [📖 Details](lib/rules/version_tracking/minor_branch_tracks_highest_patch/README.md) |
| `patch_tag_missing` | Detects missing patch version tags (v1.0.0) | [📖 Details](lib/rules/version_tracking/patch_tag_missing/README.md) |

### Latest Version Rules

| Rule | Description | Documentation |
| --- | --- | --- |
| `latest_tag_tracks_global_highest` | Ensures 'latest' tag points to the globally highest patch | [📖 Details](lib/rules/latest/latest_tag_tracks_global_highest/README.md) |
| `latest_branch_tracks_global_highest` | Ensures 'latest' branch points to the globally highest patch | [📖 Details](lib/rules/latest/latest_branch_tracks_global_highest/README.md) |

### Marketplace Rules

| Rule | Description | Documentation |
| --- | --- | --- |
| `action_metadata_required` | Validates action.yaml has required marketplace metadata (name, description, branding) and README.md exists | [📖 Details](lib/rules/marketplace/action_metadata_required/README.md) |
| `marketplace_publication_required` | Verifies the latest release has been published to the GitHub Marketplace | [📖 Details](lib/rules/marketplace/marketplace_publication_required/README.md) |

## Supported Auto-Fix Actions

When `auto-fix: true` is enabled, the action can automatically remediate issues using these actions:

### Tag Actions

| Action | Description | Documentation |
| --- | --- | --- |
| `CreateTagAction` | Creates a new Git tag at a specified commit | [📖 Details](lib/actions/tags/CreateTagAction/README.md) |
| `UpdateTagAction` | Updates an existing tag to point to a different commit | [📖 Details](lib/actions/tags/UpdateTagAction/README.md) |
| `DeleteTagAction` | Deletes an existing tag | [📖 Details](lib/actions/tags/DeleteTagAction/README.md) |

### Branch Actions

| Action | Description | Documentation |
| --- | --- | --- |
| `CreateBranchAction` | Creates a new branch at a specified commit | [📖 Details](lib/actions/branches/CreateBranchAction/README.md) |
| `UpdateBranchAction` | Updates an existing branch to point to a different commit | [📖 Details](lib/actions/branches/UpdateBranchAction/README.md) |
| `DeleteBranchAction` | Deletes an existing branch | [📖 Details](lib/actions/branches/DeleteBranchAction/README.md) |

### Release Actions

| Action | Description | Documentation |
| --- | --- | --- |
| `CreateReleaseAction` | Creates a new GitHub Release for a tag | [📖 Details](lib/actions/releases/CreateReleaseAction/README.md) |
| `PublishReleaseAction` | Publishes a draft release | [📖 Details](lib/actions/releases/PublishReleaseAction/README.md) |
| `RepublishReleaseAction` | Republishes a release to make it immutable | [📖 Details](lib/actions/releases/RepublishReleaseAction/README.md) |
| `SetLatestReleaseAction` | Sets a release as the 'latest' release | [📖 Details](lib/actions/releases/SetLatestReleaseAction/README.md) |
| `DeleteReleaseAction` | Deletes an existing release | [📖 Details](lib/actions/releases/DeleteReleaseAction/README.md) |

### Conversion Actions

| Action | Description | Documentation |
| --- | --- | --- |
| `ConvertTagToBranchAction` | Converts a tag to a branch | [📖 Details](lib/actions/conversions/ConvertTagToBranchAction/README.md) |
| `ConvertBranchToTagAction` | Converts a branch to a tag | [📖 Details](lib/actions/conversions/ConvertBranchToTagAction/README.md) |

## Prerequisites

## v2 (Current)

**No checkout required!** Version 2 uses the GitHub REST API exclusively, so you don't need to checkout the repository:

```yaml
- uses: jessehouwing/actions-semver-checker@v2
```

This is a significant improvement over v1, making the action faster and simpler to use.

## v1 (Legacy)

Version 1 requires full git history and tags:

```yaml
- uses: actions/checkout@v6
  with:
    fetch-depth: 0      # Required for v1: Fetches full git history
    fetch-tags: true    # Required for v1: Fetches all tags
```

## Auto-fix Mode Prerequisites

If you're using the `auto-fix` feature to automatically update version tags/branches:

```yaml
jobs:
  check-semver:
    permissions:
      contents: write  # Required for auto-fix to push tags/branches
    steps:
      - uses: jessehouwing/actions-semver-checker@v2
        with:
          auto-fix: true
          token: ${{ secrets.GITHUB_TOKEN }}  # Required for auto-fix API calls
```

**Requirements:**

- **`contents: write` permission** - Required to push tag/branch updates via REST API
- **`token`** - GitHub token for API calls (create releases, update refs)

## Using Custom Tokens for Workflow File Changes

The default `GITHUB_TOKEN` **cannot** push tags or branches that would modify files in `.github/workflows/`. This is a security feature of GitHub Actions. If your action repository has workflow files that change between versions, auto-fix will fail with a permission error.

To work around this limitation, you can use either:

1. **GitHub App Token** (Recommended for organizations)
2. **Fine-grained Personal Access Token** (Simpler for personal repositories)

### Option 1: GitHub App Token (Recommended)

Using a GitHub App provides the most secure and manageable approach, especially for organizations.

**Step 1:** Create a GitHub App with the following permissions:

- **Repository permissions:**
  - `Contents`: Read and write
  - `Workflows`: Read and write *(this is the key permission)*
  - `Metadata`: Read-only (automatically selected)

**Step 2:** Install the App on your repository

**Step 3:** Use the [actions/create-github-app-token](https://github.com/marketplace/actions/create-github-app-token) action:

```yaml
name: Auto-fix SemVer

on:
  push:
    tags:
      - 'v*.*.*'

jobs:
  fix-semver:
    runs-on: ubuntu-latest
    steps:
      - name: Generate GitHub App Token
        id: app-token
        uses: actions/create-github-app-token@v1
        with:
          app-id: ${{ vars.APP_ID }}
          private-key: ${{ secrets.APP_PRIVATE_KEY }}
          # Scope the token to only this repository
          owner: ${{ github.repository_owner }}
          repositories: ${{ github.event.repository.name }}

      - uses: jessehouwing/actions-semver-checker@v2
        with:
          auto-fix: true
          token: ${{ steps.app-token.outputs.token }}
```

**Benefits:**

- Fine-grained permissions scoped to specific repositories
- Can be managed at organization level
- Token automatically expires (more secure than PATs)
- Audit logging for all actions taken


### Option 2: Fine-grained Personal Access Token

For personal repositories or simpler setups, a fine-grained PAT works well.

**Step 1:** Create a Fine-grained PAT at [GitHub Settings → Developer Settings → Personal Access Tokens → Fine-grained tokens](https://github.com/settings/tokens?type=beta)

**Step 2:** Configure the token with these permissions:

- **Repository access:** Select your action repository
- **Repository permissions:**
  - `Contents`: Read and write
  - `Workflows`: Read and write *(this is the key permission)*
  - `Metadata`: Read-only (automatically selected)

**Step 3:** Add the token as a repository secret (e.g., `SEMVER_TOKEN`)

**Step 4:** Use the token in your workflow:

```yaml
name: Auto-fix SemVer

on:
  push:
    tags:
      - 'v*.*.*'

jobs:
  fix-semver:
    runs-on: ubuntu-latest
    steps:
      - uses: jessehouwing/actions-semver-checker@v2
        with:
          auto-fix: true
          token: ${{ secrets.SEMVER_TOKEN }}
```

> **⚠️ Security Note:** PATs are tied to a user account. If the user leaves the organization or their account is compromised, the token may need to be rotated. For organizations, GitHub App tokens are preferred.

### When Do You Need a Custom Token?

You need a custom token with `workflows` permission if:

- Your action repository contains `.github/workflows/` files
- These workflow files change between versions
- You want to use `auto-fix: true` to push tags/branches

If your repository has no workflow files, or workflow files don't change between versions, the default `GITHUB_TOKEN` with `contents: write` permission is sufficient.

### Troubleshooting Token Issues

If auto-fix fails with a permission error like:

```text
refusing to allow a GitHub App to create or update workflow
```

This indicates the token lacks `workflows` permission. Follow one of the options above to use a properly configured token.

## Usage

## Basic Usage

```yaml
- uses: jessehouwing/actions-semver-checker@v2
```

## Configuration Options

### `token`

**Default:** `""` (uses GITHUB_TOKEN)

GitHub token for API access. If not provided, falls back to the GITHUB_TOKEN environment variable.

```yaml
- uses: jessehouwing/actions-semver-checker@v2
  with:
    token: ${{ secrets.GITHUB_TOKEN }}
```

### `check-minor-version`

**Default:** `error`

Configures whether to check minor versions (e.g., `v1.0`) in addition to major versions.

**Options:** `error`, `warning`, or `none`

- `error` or `true`: Report as error and fail the action
- `warning`: Report as warning but don't fail
- `none` or `false`: Skip this check entirely

```yaml
- uses: jessehouwing/actions-semver-checker@v2
  with:
    check-minor-version: 'error'
```

### `check-releases`

**Default:** `error`

Check that every build version (e.g., `v1.0.0`) has a corresponding GitHub Release.

**Options:** `error`, `warning`, or `none`

- `error` or `true`: Report as error and fail the action
- `warning`: Report as warning but don't fail
- `none` or `false`: Skip this check entirely

> **⚠️ Permission Note:** Draft releases are NOT visible with `contents: read` permission. If you have draft releases and this check is enabled, you must use `contents: write` permission or the action will incorrectly report them as missing.

```yaml
- uses: jessehouwing/actions-semver-checker@v2
  with:
    check-releases: 'error'
```

### `check-release-immutability`

**Default:** `error`

Check that releases are immutable (not in draft status). Draft releases allow tag changes, making them mutable.

**Options:** `error`, `warning`, or `none`

- `error` or `true`: Report as error and fail the action
- `warning`: Report as warning but don't fail
- `none` or `false`: Skip this check entirely

```yaml
- uses: jessehouwing/actions-semver-checker@v2
  with:
    check-release-immutability: 'error'
```

### `ignore-preview-releases`

**Default:** `true` ✅ **Recommended**

Ignore preview/pre-release versions when calculating which version major/minor tags should point to. When enabled (default):

- Preview releases are excluded from `v1` and `v1.0` tag calculations
- If `v1.1.1` (stable) exists and `v1.1.2` (preview), `v1` and `v1.1` will point to `v1.1.1`

```yaml
# Default behavior (preview releases ignored)
- uses: jessehouwing/actions-semver-checker@v2

# Or explicitly set to false to include preview releases
- uses: jessehouwing/actions-semver-checker@v2
  with:
    ignore-preview-releases: 'false'
```

### `floating-versions-use`

**Default:** `tags`

Specify whether floating versions (major like `v1`, minor like `v1.0`, and `latest`) should use tags or branches. This is useful when you want mutable major/minor versions that can be updated via branch commits.

**Options:** `tags` or `branches`

```yaml
- uses: jessehouwing/actions-semver-checker@v2
  with:
    floating-versions-use: 'branches'
```

### `auto-fix`

**Default:** `false`

Automatically fix major/minor version tags or branches when a build tag is pushed.

**⚠️ Important:** When enabling auto-fix, you must grant `contents: write` permission:

```yaml
jobs:
  check-semver:
    permissions:
      contents: write  # Required for auto-fix
    steps:
      - uses: jessehouwing/actions-semver-checker@v2
        with:
          auto-fix: 'true'
```

**Note:**

- Auto-fix handles all operations via REST API (no checkout required)
- When `check-release-immutability` is set to `error` or `warning` (default), auto-fix will also automatically republish non-immutable releases to make them immutable

**Auto-fix behavior for releases:**

When `auto-fix: true` is enabled and `check-release-immutability` is set to `error` or `warning`:

1. Creates releases for missing patch versions (vX.Y.Z)
2. Attempts to publish draft releases automatically
3. **Republishes non-immutable releases** by temporarily converting them to draft and publishing again to make them immutable

This automatic republishing helps migrate repositories to GitHub's immutable release strategy without manual intervention.

### `ignore-versions`

**Default:** `""` (empty)

List of versions to ignore during validation. This is useful for skipping legacy or problematic versions that you don't want to validate.

**Supported formats:**

```yaml
# Comma-separated
ignore-versions: 'v1.0.0,v2.0.0,v3.0.0'

# Newline-separated (using YAML literal block)
ignore-versions: |
  v1.0.0
  v2.0.0
  v3.0.0

# JSON array
ignore-versions: '["v1.0.0", "v2.0.0", "v3.0.0"]'
```

**Wildcard support:**

You can use wildcards to match multiple versions:

```yaml
# Ignore all v1.x versions
ignore-versions: 'v1.*'

# Ignore all preview releases and specific version
ignore-versions: 'v1.0.0'
```

**Use cases:**

- Skip validation for legacy versions that don't follow current standards
- Ignore problematic versions that can't be fixed
- Exclude pre-release versions from validation
- Bulk ignore version ranges using wildcards

## Examples

### Complete Workflow Example

```yaml
name: Check SemVer

on:
  push:
    tags:
      - 'v*'
  workflow_dispatch:

jobs:
  check-semver:
    permissions:
      contents: read
    concurrency:
      group: '${{ github.workflow }}'
      cancel-in-progress: true
    runs-on: ubuntu-latest
    steps:
      - uses: jessehouwing/actions-semver-checker@v2
        with:
          check-minor-version: 'true'
          check-releases: 'true'
          check-release-immutability: 'true'
```

### Auto-fix enabled

```yaml
name: Auto-fix SemVer

on:
  push:
    tags:
      - 'v*.*.*'  # Only trigger on patch versions

jobs:
  fix-semver:
    permissions:
      contents: write  # Required for auto-fix
    runs-on: ubuntu-latest
    steps:
      - uses: jessehouwing/actions-semver-checker@v2
        with:
          token: ${{ secrets.GITHUB_TOKEN }}
          auto-fix: 'true'
```

### With Preview Release Support

```yaml
name: Check SemVer (Stable Only)

on:
  push:
    tags:
      - 'v*'

jobs:
  check-semver:
    runs-on: ubuntu-latest
    steps:
      - uses: jessehouwing/actions-semver-checker@v2
        with:
          check-releases: 'error'
          ignore-preview-releases: false
```

## Suggested Fixes

When issues are detected, the action provides specific commands to fix them, including direct links to GitHub release pages:

### Creating a Release

```bash
gh release create v1.0.0 --draft --title "v1.0.0" --notes "Release v1.0.0"
gh release edit v1.0.0 --draft=false  # Or edit at: https://github.com/{owner}/{repo}/releases/edit/v1.0.0
```

### Updating Version Tags

```bash
git push origin <sha>:refs/tags/v1 --force
git push origin <sha>:refs/tags/v1.0 --force
```

### Updating Version Branches

```bash
git push origin <sha>:refs/heads/v1 --force
git push origin <sha>:refs/heads/v1.0 --force
git push origin <sha>:refs/heads/latest --force
```

## Permissions

### Read-only Mode (Default)

Requires `contents: read` permission to retrieve tags, branches and published releases.

> **Note:** Draft releases are NOT visible with `contents: read` permission. If you have draft releases and `check-releases` is enabled, the action will incorrectly report them as missing. Use `contents: write` to see draft releases.

### Auto-fix Mode

Requires `contents: write` permission to push tag/branch updates and see draft releases:

```yaml
jobs:
  check-semver:
    permissions:
      contents: write
```

## Architecture

The action is a thin wrapper around the Go `gh` extension in this repository. `main.go` starts the CLI,
`internal/cmd` parses flags and builds the configuration, `internal/api` and `internal/localgit` load tags,
branches and releases, `internal/rules` holds the validation rules, `internal/actions` the remediation
actions, and `internal/policy` the `action-version.yml` defaults. See [docs/cli.md](docs/cli.md).

## Migration from v1 to v2

v2 is backward compatible with v1. The main differences:

- **New input options:**
  - `token` - Explicit GitHub token input
  - `check-releases` - Now accepts "error" (default), "warning", or "none"
  - `check-release-immutability` - Now accepts "error" (default), "warning", or "none"
  - `ignore-versions` - Comma-separated list of versions to ignore (NEW in v2.1)

- **Configuration improvements:**
  - `floating-versions-use` replaces `use-branches` - Now accepts `tags` (default) or `branches`
  - Release suggestions include direct GitHub edit links
  - Link header-based pagination for better API performance
  - Retry logic with exponential backoff for better reliability (NEW in v2.1)

- **Opt-in features:**
  - `ignore-preview-releases: true` (default) - Set to `false` to include prereleases in floating version calculations
  - `floating-versions-use: tags` (default) - Set to `branches` to use branches for floating versions
  - `auto-fix: false` (default) - Set to `true` to automatically fix missing/incorrect tags
  - **NEW in v2.1:** When `auto-fix: true` and `check-release-immutability` is enabled, automatically republishes non-immutable releases to make them immutable

If you want warnings instead of errors for release checks:

```yaml
- uses: jessehouwing/actions-semver-checker@v2
  with:
    check-releases: 'warning'
    check-release-immutability: 'warning'
```

To disable release checks entirely (v1 behavior):

```yaml
- uses: jessehouwing/actions-semver-checker@v2
  with:
    check-releases: 'none'
    check-release-immutability: 'none'
```

To skip validation for specific versions:

```yaml
- uses: jessehouwing/actions-semver-checker@v2
  with:
    ignore-versions: 'v1.0.0,v2.0.0'
```

To automatically fix issues including republishing non-immutable releases:

```yaml
- uses: jessehouwing/actions-semver-checker@v2
  with:
    auto-fix: 'true'
    check-release-immutability: 'error'  # or 'warning' - enables automatic republishing
```
