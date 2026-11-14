# Rule: immutable_releases_enabled

## What This Rule Checks

Validates that the repository setting **Release immutability** is enabled, so that published releases become immutable.

## Why This Is An Issue

- **Impact:** Without the setting, published releases stay mutable and can be modified or deleted.
- **Best Practice:** GitHub's [immutable release strategy](https://docs.github.com/en/actions/how-tos/create-and-publish-actions/using-immutable-releases-and-tags-to-manage-your-actions-releases) relies on this setting.

## When This Rule Applies

- `--check-release-immutability` is `error` or `warning`
- The setting could be read (`GET /repos/{owner}/{repo}/immutable-releases`). When it can't be read (no API access, `--local`, older GHES), the rule is skipped.

## Configuration

This rule belongs to the `immutability` concern.

### Level (`error`, `warning`, `none`)

- CLI: `--check-release-immutability=<level>` sets the level of every rule in this concern; `none` disables the rule.
- Config file: `concerns.immutability.level` (the flag wins over it).
- Config file, this rule only: `rules.immutable_releases_enabled.level` (wins over the concern; `none` disables just this rule).

Default: `error`.

### Auto-fix

- CLI: `--auto-fix` (or `--interactive`, `--yes`) applies the fix; `--check` / `--no-fix` never changes anything. Without a mode flag the tool prompts on a terminal and only reports otherwise.
- Config file: `concerns.immutability.autofix: false` or `rules.immutable_releases_enabled.autofix: false` turns the fix into a manual instruction, even with `--auto-fix`.

### Other settings that influence this rule

- `--ignore-versions v1.2.3,v2.*` / `ignore-versions` in the config file: matching versions are ignored by all rules. The flag wins.

## Automatic Remediation

With `--auto-fix` the setting is enabled (`PUT /repos/{owner}/{repo}/immutable-releases`). This needs repository admin permissions; if the token lacks them, the issue is reported as a manual fix.

## Manual Remediation

```bash
gh api -X PUT "repos/{owner}/{repo}/immutable-releases"
```

Or in the web UI: **Settings** > **General** > **Releases** > enable **Release immutability**.

## Related Rules

- [`release_should_be_immutable`](./release_should_be_immutable.md)
- [`release_should_be_published`](./release_should_be_published.md)
- [`patch_release_required`](./patch_release_required.md)
