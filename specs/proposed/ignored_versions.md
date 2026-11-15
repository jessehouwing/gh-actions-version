# Proposed: specifying ignored versions and releases

Status: proposed. Companion to `cli_redesign.md` and `../rule_refactoring.md`.

## Problem

Today `ignore-versions` is a flat list of version patterns (`v1.2.3`, `v1.*`). It hides a version from every
rule, with no reason, no scope and no expiry. That is too coarse for the cases it is used for:

- A tag whose immutable release was deleted can never have a release again (see `../rule_refactoring.md`). The
  tool suggests adding it to `ignore-versions`, which also hides it from unrelated checks.
- A bad release (yanked, broken) must not be chosen as the latest patch, but should still be validated for
  immutability.
- A legacy range (`v0.*`) should be skipped entirely.

## Goals

- One clear place and syntax to say "this version/release is not subject to rule X".
- Every ignore can record why and can be reviewed and removed.
- Works in `action-version.yml` and on the command line, with the usual precedence (flag > file > default).
- Existing behavior (ignore everything about a version) stays the default for the simple form.

## Version patterns

Used everywhere below. Unchanged from today: exact `vX`, `vX.Y`, `vX.Y.Z`, and wildcards `vX.*`, `vX.Y.*`.
Prerelease status is never part of a pattern; it comes from the GitHub Release.

## Config file

### Simple form

```yaml
ignore-versions: [v1.2.3, v0.*]
```

Equivalent to an entry with `scope: all` and no reason.

### Structured form

```yaml
ignore:
  - versions: [v1.2.3]
    reason: Immutable release was deleted; tag cannot be reused (security incident #123).
    scope: selection          # see below
  - versions: [v0.*]
    reason: Pre-1.0 releases predate the policy.
    scope: all
  - versions: [v2.4.0]
    reason: Broken release, yanked.
    scope: selection
    rules: [release_should_be_immutable]  # optional: restrict to rules or findings
    until: 2027-01-01                    # optional: ignore expires, then it is reported again
```

Keys:

| Key | Required | Meaning |
|---|---|---|
| `versions` | yes | One or more version patterns. |
| `reason` | recommended | Free text, shown in the output and job summary. |
| `scope` | no, default `all` | What the ignore does, see below. |
| `rules` | no | Limit the ignore to these rule or finding IDs. Default: all rules the scope covers. |
| `until` | no | ISO date. After it, the entry is inert and an `ignore_expired` warning is reported. |

### Scopes

| Scope | Effect |
|---|---|
| `all` | The version is invisible: no rule reports it, nothing is created, moved or deleted for it, and it is not a candidate when choosing the latest patch. This is today's behavior. |
| `selection` | The version is still validated and reported, but it is **not a candidate** when a floating ref (`vN`, `vN.M`, `latest`) or the latest release picks "the highest patch". Used for broken or locked releases. |
| `validation` | The version is a candidate for selection but **no finding is reported** for it. Used for legacy versions that should keep being tracked. |
| `fix` | Findings are reported but never fixed automatically; manual instructions are printed. Narrower than `fix: manual` because it is per version. |

Rules without a version (settings such as `immutable_releases_enabled`) are not affected by `ignore`.

### Interaction with the policy

- An ignore never changes concern or rule levels; it only removes versions from consideration.
- When several entries match one version, the union of their scopes applies; `reason`s are all listed.
- Unknown keys and invalid patterns are errors, as for the rest of the file.

## Command line

The config file is the single home of ignores. The `ignore` command edits it; there is no comma-list flag.

### `gh actions-version ignore`

```
gh actions-version ignore 'v0.*' --reason "pre-1.0" [--scope all|selection|validation|fix] [--until 2027-01-01] [--rule RULE]
gh actions-version ignore --list
gh actions-version ignore --remove 'v0.*'
```

- Always quote wildcard patterns: an unquoted `v0.*` is a shell glob (bash expands it when a file such as `v0.1.0`
  exists; zsh fails with "no matches found").
- Writes to the policy file that exists (root or `.github/`), or creates `action-version.yml` in the root.
  `--config PATH` overrides.
- Preserves comments and layout; refuses to run when the file does not parse.
- Adding a pattern that already exists updates that entry instead of appending a duplicate.
- Without `--reason` it prompts on a terminal; the reason is optional, including without a terminal.
- Local and offline: needs no API access, no token and no `-R`.
- `--list` prints entries with their reason, scope and expiry; `--remove` deletes matching entries.

### Flags on `verify` and `fix`

| Flag | Description |
|---|---|
| `--ignore-version PATTERN` | Repeatable, one-off CI override, `scope: all`, no reason. Adds to the config entries. Quote wildcards: `--ignore-version 'v0.*'`. |
| `--no-ignore` | Disable all ignores from the config file for this run (audit mode). |

`--no-ignore` clears the config entries before `--ignore-version` entries are added.
## Output

- `console`: a summary line ("2 versions ignored: v1.2.3 (selection) - deleted release") and a note per
  skipped finding in verbose mode.
- `json`: top-level `ignored[]` with `version`, `scope`, `reason`, `until`, `matchedRules`.
- `actions`: a `::notice` per entry and an "Ignored versions" table in the job summary with the reason.
- Expired entries and entries that match no existing version are reported as warnings
  (`ignore_expired`, `ignore_unused`) so the list does not rot.

## Generated suggestions

When a fix fails because a tag is locked by a deleted immutable release, the tool prints a ready-to-paste
entry with `scope: selection`, the tag, and a reason prefilled from the failure:

```yaml
ignore:
  - versions: [v1.2.3]
    scope: selection
    reason: Tag was used by a deleted immutable release; no release can be created for it.
```

This replaces the current suggestion that extends `ignore-versions`, which hides the tag from every rule.

## `setup`

The `setup` wizard asks for ignored versions (versions, scope, reason) and writes them with the same code as `ignore`.

## Decisions

- Support all four scopes, expiry dates, and restriction to rule or finding IDs.
- Reasons are recommended but optional for every scope.
- Keep repeatable `--ignore-version` for one-off overrides and add `--no-ignore` for auditing.
- Expired and unused entries are warnings under the `config` concern, from the
  `ignore_entries_valid` rule (`ignore_expired` and `ignore_unused` findings).
