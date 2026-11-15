# Rule: immutable_releases_enabled

## What This Rule Checks

Validates that the repository setting **Release immutability** is enabled, so that published releases become immutable.

## Why This Is An Issue

- **Impact:** Without the setting, published releases stay mutable and can be modified or deleted.
- **Best Practice:** GitHub's [immutable release strategy](https://docs.github.com/en/actions/how-tos/create-and-publish-actions/using-immutable-releases-and-tags-to-manage-your-actions-releases) relies on this setting.

## When This Rule Applies

- `--level immutability=error` or `--level immutability=warning`
- The setting could be read (`GET /repos/{owner}/{repo}/immutable-releases`). When it can't be read (no API access, `--local`, older GHES), the rule is skipped.

## Findings

| Finding | Summary |
|---|---|
| `immutable_releases_disabled` | Release immutability is disabled |

## Configuration

This rule belongs to the `immutability` concern. Configure it and its findings in `action-version.yml`:

```yaml
concerns:
  immutability:
    level: warning
rules:
  immutable_releases_enabled:
    level: error
    findings:
      immutable_releases_disabled:
        level: warning
        fix: manual
```

### Level (`error`, `warning`, `none`)

- CLI: `--level immutability=<level>` sets this concern; `--rule-level immutable_releases_enabled=<level>` overrides this rule.
- Config file: `concerns.immutability.level` (the flag wins over it).
- Finding override: use `--finding-level FINDING=<level>` or the finding-specific YAML keys above.
- Config file, this rule only: `rules.immutable_releases_enabled.level` (wins over the concern; `none` disables just this rule).

Default: `error`.

### Auto-fix

- CLI: `fix --mode auto` applies the fix; use `fix --mode interactive` to review changes one by one. `verify` never changes anything.
- Config file: `concerns.immutability.fix: manual` or `rules.immutable_releases_enabled.fix: manual` turns the fix into a manual instruction, even when `fix --mode auto` is selected.

### Other settings that influence this rule

- `--ignore-version v1.2.3 --ignore-version 'v2.*'` adds one-off ignores to the config; `--no-ignore` clears configured ignores first.

## Automatic Remediation

With `fix --mode auto` the setting is enabled (`PUT /repos/{owner}/{repo}/immutable-releases`). This needs repository admin permissions; if the token lacks them, the issue is reported as a manual fix.

## Manual Remediation

```bash
gh api -X PUT "repos/{owner}/{repo}/immutable-releases"
```

Or in the web UI: **Settings** > **General** > **Releases** > enable **Release immutability**.

## Related Rules

- [`release_should_be_immutable`](./release_should_be_immutable.md)
- [`release_should_be_published`](./release_should_be_published.md)
- [`patch_release_required`](./patch_release_required.md)
