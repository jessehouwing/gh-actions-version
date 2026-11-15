# Subcommand-based CLI (`setup`, `verify`, `fix`)

Status: implemented.

## Goals

- Select the mode with subcommands instead of mode flags.
- Add `setup` that asks the main questions and writes `action-version.yml`.
- Three output formats: console, json, actions (GitHub Actions workflow commands plus a job summary).
- Keep precedence: command-line flag > `action-version.yml` > built-in default.
- Keep the extension binary name `gh actions-version` (repository `gh-actions-version`).

## Command overview

```
gh actions-version setup  [flags]     Ask questions and write action-version.yml
gh actions-version verify [flags]     Read-only validation
gh actions-version fix    [flags]     Validate, then repair
gh actions-version ignore PATTERN [flags]  Add, list or remove ignored versions in the config file
```

Exit codes (all commands): `0` success, `1` findings at level `error` remain, `2` usage or runtime error,
`3` (fix only) some fixes were applied but findings remain.

Invoking the binary without a subcommand prints root help and exits `2`.

## Global flags (all commands)

| Flag | Default | Description |
|---|---|---|
| `-R, --repo OWNER/REPO` | current repository | Target repository `[HOST/]OWNER/REPO`. |
| `--local` | false | Read refs from the local git clone; never pushes. |
| `--fetch-releases` | false | With `--local`: also read releases from GitHub (read-only). |
| `--config PATH` | auto | Use this policy file instead of searching the root and `.github/`. |
| `--no-config` | false | Ignore any `action-version.yml`; use built-in defaults and flags. |
| `-o, --output console\|json\|actions` | auto | Output format, see below. |
| `--max-wait DURATION` | 60s | Longest rate-limit wait tolerated before exiting with code 2. |
| `-v, --verbose` | false | Debug logging (to stderr). |
| `--no-color` | auto | Disable colors (also honors `NO_COLOR`). |

`--output` default: `actions` when `GITHUB_ACTIONS=true`, otherwise `console`.

### Policy flags (verify and fix)

These override `action-version.yml`. Value flags take one value; boolean flags are opt-in (no `=false`).

| Flag | Values | Config key |
|---|---|---|
| `--floating-versions-use` | `tags`, `branches` | `floating-versions-use` |
| `--preview-releases` | `included`, `excluded` | `preview: include`, `exclude` |
| `--ignore-version` | repeatable pattern; quote wildcards (`'v1.*'`) | `ignore` / `ignore-versions` |
| `--level CONCERN=LEVEL` | repeatable; `LEVEL` is `error`, `warning`, `none` | `concerns.<concern>.level` |
| `--rule-level RULE=LEVEL` | repeatable | `rules.<rule>.level` |
| `--finding-level FINDING=LEVEL` | repeatable | `rules.<rule>.findings.<finding>.level` |

Level flags are validated against the registered concerns, rules and findings. The four legacy
`--check-*` flags are removed.

## `setup`

Interactive wizard (requires a terminal unless every answer is given as a flag).

Questions, in order:

1. Floating versions: tags or branches.
2. Floating levels to maintain: major only, major and minor.
3. Releases: require a release for every patch? Require immutable releases?
4. Marketplace checks: on or off.
5. Prerelease handling: ignore previews when choosing the latest patch.
6. Permanently delete tags whose immutable release was deleted? Default deny; deletion is irreversible.
7. Versions to ignore.
8. Per-concern fix policy: automatic or manual only.
9. Where to write: repository root or `.github/`.

Flags:

| Flag | Description |
|---|---|
| `--yes` | Accept defaults for every unanswered question (non-interactive). |
| `--path PATH` | Destination (default `action-version.yml`; `.github/action-version.yml` also accepted). |
| `--force` | Update an existing file without confirmation, preserving comments and unrelated settings. |
| `--print` | Write to stdout instead of a file. |
| `--dangerously-allow-destructive-tag-deletion` | Opt into irreversible deletion of tags used by deleted immutable releases. |
| `--fix-policy CONCERN=auto\|manual` | Pre-answer a concern's fix policy; repeatable. |
| `--no-ignore` | Pre-answer with no ignored versions and clear existing ignores. |
| Policy flags above | Pre-answer the matching question. |

Output: the written path and a diff summary. Exit `2` when a file exists and neither `--force` nor an
interactive confirmation was given. Existing ignores are retained unless `--no-ignore` is specified; added
patterns are updated or added to the existing policy. `--config` selects the destination as an alternative
to `--path`. `--print` cannot be combined with either destination flag or `--force`.

## `ignore`

Edits `action-version.yml` offline; see `ignored_versions.md` for flags, scopes and behavior.

## `verify`

Read-only. Never writes, never prompts. Positional version patterns (for example `v1` or `v2.*`)
limit the findings to report.

| Flag | Description |
|---|---|
| Global and policy flags | See above. |
| `--fail-on error\|warning\|never` | Exit `1` on this severity or worse (default `error`). |
| `--rule RULE` / `--skip-rule RULE` | Repeatable; run or skip individual rules. |
| `--finding FINDING` | Repeatable; report only these findings (see the rule/finding spec). |
| `--save FILE` | Also write the findings as a findings file (see below), whatever `--output` is. |

`verify` reports findings only. It never prints remediation commands or says how to fix; that is `fix`'s job
(`fix --mode manual`). The `json` output still carries `manualCommands` per finding for tooling.

### Findings file (verify → fix)

`verify --output json` (or `--save FILE`) produces a self-contained findings file:
`schemaVersion`, `repository`, `config` (effective policy), `filters` (`rule`, `skipRule`, `finding`, ignored
versions as given to `verify`), `generatedAt`, `refs` (name → SHA of every tag, branch and release considered),
`findings[]`. Filters are applied when `verify` runs, so the file only contains matching findings and `fix --from`
never widens them; the recorded `filters` are informational and printed by `fix`. Filters given to `fix --from`
can only narrow the set further.

```bash
gh actions-version verify --save findings.json
gh actions-version fix --from findings.json --mode auto
gh actions-version verify -o json > findings.json   # equivalent to --save
```

`fix --from FILE|-` (`-` reads stdin) skips loading state and validation and acts on the file's findings:

- Same filters (`--rule`, `--finding`) and modes apply. The file's effective policy is used unless policy flags
  are given.
- **Manual instructions from a file:** `fix --from FILE --mode manual` prints the commands for the file's
  findings, grouped as in the console report, with no API calls at all. `--output actions` puts them in the job
  summary, and `--output json` emits them as `manualCommands`. `--rule` and `--finding` filter as usual.
- **No staleness guard.** `fix --from` trusts the file: it makes no API calls until it applies an action, then
  only the calls the action needs. It prints the file's `generatedAt`. Creates still fail naturally if the ref
  now exists; updates and deletes act on the file's data, so re-run `verify` when in doubt.
- For remote automatic or interactive replay, the independently resolved target repository and host
  must match the file's `repository` before credentials are accessed; resolution uses `--repo`,
  `GH_REPO`, `GITHUB_REPOSITORY`, or the local Git remote, and fails closed if no target is available.
  An explicit `--repo` must match in every mode; unknown `schemaVersion` exits `2`.
- `--no-verify-after` is implied unless given explicitly to re-verify.

## `fix`

Validates (or reads `--from`), then applies remediations. Positional version patterns limit the
findings to fix. Mode is a required choice with a default:

| Form | Behavior |
|---|---|
| `fix --mode interactive` or `--interactive` (`-i`) | Show each fix and ask; terminal required. Default on a terminal. |
| `fix --mode auto` or `--auto` | Apply every auto-fixable remediation without prompting. Default when not on a terminal. |
| `fix --mode manual` | Apply nothing; print manual instructions (commands) for every finding. |

`--manual` selects manual mode. Only one mode may be given; conflicting modes exit `2`.

| Flag | Description |
|---|---|
| Global, policy and `verify` filter flags | See above. |
| `--dry-run` | Show what `auto` would do; change nothing (like `manual`, but names the exact actions). |
| `--yes` | In `interactive` mode, answer yes to every prompt. |
| `--dangerously-allow-destructive-tag-deletion` | Allow deleting tags of deleted immutable releases; irreversible. |
| `--no-verify-after` | Skip the re-validation pass after fixing (default: re-run and report what remains). |

Per-concern and per-rule `fix: manual` in the policy file still turns that finding into a manual instruction,
in every mode.

## Output formats

Selected with `-o/--output`; human logs always go to stderr when stdout carries machine output.

### `console`

Colored, grouped by rule and version, with a final summary table. Interactive prompts use this format.

### `json`

Stable document on stdout (schema versioned with `"schemaVersion": 1`): `repository`, `config` (effective
settings and where each came from), `findings[]` (rule, finding, severity, version, message, autoFixable,
manualCommands), `fixes[]` (action, status, error), `summary`. Additional flags:

| Flag | Description |
|---|---|
| `--json-fields a,b,c` | Select the fields to print. |
| `--jq EXPR` (`-q`), `--template TMPL` (`-t`) | As in other `gh` commands; imply `json`. These flags are mutually exclusive. |

### `actions`

- Findings and fixes are logged with workflow commands: `::error`, `::warning`, `::notice` (with `title=` set
  to the finding ID and `file=`/`line=` when a policy file location is known), plus `::group::`/`::endgroup::`
  per rule and `::add-mask::` for tokens.
- A job summary is appended to `$GITHUB_STEP_SUMMARY`: result badge, counts per severity, a findings table
  (rule, finding, version, severity, auto-fixable), executed fixes, manual commands in fenced blocks, and the
  effective configuration. Summary size is capped (1 MiB per step); long tables are truncated with a note.
- Outputs are written to `$GITHUB_OUTPUT`: `errors`, `warnings`, `fixed`, `remaining`, `result`
  (`success`/`failed`).
- `--output actions` outside Actions (no `GITHUB_STEP_SUMMARY`) logs the commands only and skips the summary.

## Decisions

- Bare invocation prints root help and exits `2`.
- `fix` defaults to `interactive` on a terminal and `auto` without a terminal.
- Exit code `3` is retained when fixes were applied but error-level findings remain.
- Level overrides use only repeatable `--level`, `--rule-level` and `--finding-level` `KEY=LEVEL` flags;
  the legacy `--check-*` flags are removed.
- Positional version patterns remain supported on both `verify` and `fix`.
- `--jq`/`-q`, `--template`/`-t` and `--json-fields` remain supported; jq and template are mutually exclusive.
