# Rule: action_metadata_required

## What This Rule Checks

Validates that all required metadata for GitHub Marketplace publication exists in the repository. This includes the action manifest file (`action.yaml` or `action.yml`) with required fields and a `README.md` file.

## Why This Is An Issue

- **Impact:** Without proper marketplace metadata, your action cannot be published to the GitHub Marketplace and users won't be able to discover it through search.
- **Best Practice:** GitHub requires specific metadata fields for marketplace listing, including action name, description, and branding (icon and color).

## Requirements Checked

GitHub Marketplace requires the following for an action to be published:

1. **action.yaml or action.yml** must exist with:
   - `name` - The action's name (displayed in marketplace)
   - `description` - A brief description of what the action does
   - `branding.icon` - A Feather icon name (see https://feathericons.com/)
   - `branding.color` - One of: white, yellow, blue, green, orange, red, purple, gray-dark

2. **README.md** must exist in the repository root

## When This Rule Applies

This rule runs when:

- `--level marketplace=error` or `--level marketplace=warning`
- The repository is being validated for marketplace readiness

**Note:** This rule checks metadata availability, not content quality. It verifies that required fields exist but does not validate their values beyond basic format checks.

## Findings

| Finding | Summary |
|---|---|
| `marketplace_metadata_missing` | Required GitHub Marketplace metadata is missing |

## Configuration

This rule belongs to the `marketplace` concern. Configure it and its findings in `action-version.yml`:

```yaml
concerns:
  marketplace:
    level: warning
rules:
  action_metadata_required:
    level: error
    findings:
      marketplace_metadata_missing:
        level: warning
        autofix: false
```

### Level (`error`, `warning`, `none`)

- CLI: `--level marketplace=<level>` sets this concern; `--rule-level action_metadata_required=<level>` overrides this rule.
- Config file: `concerns.marketplace.level` (the flag wins over it).
- Finding override: use `--finding-level FINDING=<level>` or the finding-specific YAML keys above.
- Config file, this rule only: `rules.action_metadata_required.level` (wins over the concern; `none` disables just this rule).

Default: `error`.

### Auto-fix

- CLI: `fix --mode auto` applies the fix; use `fix --mode interactive` to review changes one by one. `verify` never changes anything.
- Config file: `concerns.marketplace.autofix: false` or `rules.action_metadata_required.autofix: false` turns the fix into a manual instruction, even when `fix --mode auto` is selected.

### Other settings that influence this rule

- `--ignore-versions v1.2.3,v2.*` / `ignore-versions` in the config file: matching versions are ignored by all rules. The flag wins.

## Automatic Remediation

This rule **cannot be auto-fixed**. The metadata must be manually added by the action author because:

- The action name and description are creative decisions
- The icon and color choices are branding decisions
- The README content requires documentation effort

When this rule detects missing metadata, it provides a detailed message indicating which specific fields are missing.

## Manual Remediation

### Step 1: Create or Update action.yaml

Ensure your `action.yaml` (or `action.yml`) contains all required fields:

```yaml
name: 'My Awesome Action'
description: 'Performs awesome tasks in your workflow'
branding:
  icon: 'zap'        # See https://feathericons.com/
  color: 'yellow'    # white, yellow, blue, green, orange, red, purple, gray-dark

inputs:
  # ... your inputs

runs:
  # ... your runs configuration
```

### Step 2: Create README.md

Ensure a `README.md` file exists in the repository root with:

- Description of what the action does
- Usage examples with YAML snippets
- Input/output documentation
- Any prerequisites or requirements

## Related Rules

- [`marketplace_publication_required`](./marketplace_publication_required.md) - Verifies marketplace publication status
- [`release_should_be_immutable`](./release_should_be_immutable.md) - Checks release immutability status

## Rule Priority and Coordination

- **Priority 5** - Runs first, before `patch_release_required` (Priority 10)
- If metadata is invalid, the `marketplace_publication_required` rule will skip checking publication status
- This ensures users fix metadata issues before worrying about publication

## Examples

### Failing Scenario - Missing Branding

```yaml
# action.yaml
name: 'My Action'
description: 'Does something useful'
# Missing: branding section

runs:
  using: 'node20'
  main: 'dist/index.js'
```

**Issue:** `missing_marketplace_metadata`  
**Message:** Missing required marketplace metadata: branding.icon, branding.color

### Failing Scenario - Missing README

```
Repository structure:
├── action.yaml (complete)
├── src/
│   └── index.js
└── (no README.md)
```

**Issue:** `missing_marketplace_metadata`  
**Message:** Missing required marketplace metadata: README.md

### Passing Scenario

```yaml
# action.yaml
name: 'My Awesome Action'
description: 'Performs awesome tasks in your workflow'
branding:
  icon: 'zap'
  color: 'yellow'
runs:
  using: 'node20'
  main: 'dist/index.js'
```

```
Repository structure:
├── action.yaml
├── README.md
└── src/
    └── index.js
```

**Result:** No issues

## Valid Branding Colors

The `branding.color` field must be one of:

| Color | Preview |
|-------|---------|
| `white` | ![white](https://img.shields.io/badge/-white-white) |
| `yellow` | ![yellow](https://img.shields.io/badge/-yellow-yellow) |
| `blue` | ![blue](https://img.shields.io/badge/-blue-blue) |
| `green` | ![green](https://img.shields.io/badge/-green-green) |
| `orange` | ![orange](https://img.shields.io/badge/-orange-orange) |
| `red` | ![red](https://img.shields.io/badge/-red-red) |
| `purple` | ![purple](https://img.shields.io/badge/-purple-purple) |
| `gray-dark` | ![gray-dark](https://img.shields.io/badge/-gray--dark-gray) |

## Valid Branding Icons

The `branding.icon` field must be a valid [Feather icon](https://feathericons.com/) name. Common choices include:

- `activity`, `airplay`, `alert-circle`, `alert-triangle`
- `check`, `check-circle`, `code`, `command`
- `git-branch`, `git-commit`, `git-merge`, `git-pull-request`
- `package`, `play`, `plus`, `refresh-cw`
- `shield`, `star`, `terminal`, `upload`, `zap`

See the full list at https://feathericons.com/
