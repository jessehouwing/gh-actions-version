# Proposed: output redesign

Status: proposed, nothing implemented. Companion to `cli_redesign.md`, `../rule_refactoring.md` and
`ignored_versions.md`.

## Problem

Observed by running the current CLI against `actions/checkout` with `--check` (361 lines of output, exit 1):

- No progress while loading refs and releases; large repositories look hung.
- Findings are flat `ERROR: Manual fix required issue: ...` lines with no rule or finding ID, no grouping and
  no link to documentation.
- One root cause is reported many times: with the repository setting `Release immutability` off, 55 releases
  each report "published but not immutable" and each repeats the same manual commands.
- The wording "Manual fix required issue" is shown even for read-only runs where nothing was attempted.
- No summary of counts at the end of a `verify` run.
- Features skipped for lack of permission (admin) appear once as a warning; the checks they disable are silent.

## Goals

- Show what is happening, on every output format, without polluting machine-readable output.
- Group findings by version by default, with selectable rule or concern grouping; show a summary.
- Deduplicate shared causes and shared remediation commands.
- Make every finding traceable to its rule page in `docs/rules/`.
- Keep `console`, `json` and `actions` outputs consistent: same findings, different rendering.

## Streams

| Stream | Content |
|---|---|
| stdout | The result: findings report (`console`), the JSON document (`json`). |
| stderr | Progress, warnings, debug, prompts. |

`actions` output writes workflow commands to stdout (as the runner expects) and the summary to
`$GITHUB_STEP_SUMMARY`; progress becomes `::group::` sections.

## Progress

Stages, in order, each with a start line and a result line (counts and elapsed time):

1. Resolve repository, token and policy (`Using the version policy from ...`).
2. Load refs: `Loading tags and branches... 142 tags, 6 branches (1.2s)`.
3. Load releases: `Loading releases... page 3/12 (2.1s)`; GraphQL and REST calls shown as pages or batches.
4. Read settings (immutable releases, rulesets) or report which were skipped and why.
5. Run rules: `Running 25 rules... 25/25 (0.1s)`.
6. (`fix`) Apply fixes: `[n/total] <finding> <version>` with ok/failed per item.

Rules:

- On a terminal: a single updating line (spinner or progress bar) per stage, replaced by the result line.
- Not on a terminal, or `--no-progress`: plain stage lines, no control characters.
- `actions`: each stage is a `::group::title` ... `::endgroup::`; no spinner.
- `json`: progress goes to stderr only; stdout stays a single valid document.
- Rate limiting is shown explicitly: `Rate limited, resuming in 42s (limit: --max-wait 60s)`; exceeding
  `--max-wait` exits with code 2 and says so.
- `-q, --quiet` suppresses progress and informational lines (findings and errors still print);
  `-v, --verbose` adds per-request detail.

## Console report

The example below uses `--group-by rule`; the default is grouping by version. Rule grouping shows the
documentation link and a deduplicated list of versions:

```
immutability / release_not_immutable                       55 errors
  Releases are published but not immutable.
  v7.0.1, v7.0.0, v6.1.0, v6.0.3, ... (all 55 versions are listed in actual output)
  Cause: repository setting 'Release immutability' is disabled or could not be read.
  Docs:  https://github.com/jessehouwing/gh-actions-version/blob/<version>/docs/rules/release_should_be_immutable.md
  Fix:   gh api -X PUT repos/actions/checkout/immutable-releases   (needs repository admin)
         then, per release: gh release edit <tag> --draft=true && gh release edit <tag> --draft=false

releases / release_missing                                  2 errors
  v1.1.0, v1.2.0
  ...

Summary: 58 errors, 0 warnings, 0 ignored. 3 findings can be fixed automatically (`fix --mode auto`).
Skipped checks: immutable_releases_enabled (admin permission missing).
```

Rules:

- `verify` prints no remediation commands, only findings and a hint (`Run: gh actions-version fix`). Commands
  appear in `fix --mode manual`, where they are printed once per finding with a `<tag>` placeholder, not once
  per version; `-v` expands them per version.
- A "Cause" line is shown when a finding has a known common cause (such as the repository setting).
- Findings from a lacking permission name the skipped checks in the summary, not only in a warning at the top.
- Wording reflects the mode: `verify` says "finding", `fix` says "failed fix", "manual step"; never "manual fix
  required" for something no one tried to fix.
- Severity colors on a terminal; `NO_COLOR` and `--no-color` honored.
- Ignored versions are summarized with their reasons (see `ignored_versions.md`).

## Findings in every format

Every finding carries the same fields: `rule`, `finding`, `severity`, `version`, `message`, `cause` (optional),
`docs` (URL), `autoFixable`, `manual` (commands, JSON only in `verify`), `ignored` (with reason, when applicable).
The docs URL is pinned to the CLI version, with `main` used for dev builds. Renaming finding IDs is covered by
`../rule_refactoring.md`.

## JSON

- `schemaVersion`, `repository`, `config` (effective values and their source), `findings[]`, `ignored[]`,
  `skipped[]` (checks skipped and why), `fixes[]` (in `fix`), `summary`, and `timings` per stage.
- Findings are grouped consistently with the console report: also available as `groups[]`
  (`rule`, `finding`, `versions[]`, `count`).
- Selection with `--json-fields`, filtering with `--jq`, templating with `--template` (see `cli_redesign.md`).

## Actions

- Findings: `::error` / `::warning` / `::notice` with `title=<rule>/<finding>`; grouped by `::group::` per rule.
  A finding for many versions is one annotation listing the versions (annotations are limited to 10 per step
  per type, so detail goes to the summary).
- Job summary (`$GITHUB_STEP_SUMMARY`): result line, counts, findings table grouped like the console report,
  skipped checks, ignored versions with reasons,   executed fixes, effective configuration; manual commands in fenced
    blocks only for `fix --mode manual`. Truncated with a note before the 1 MiB limit.
- Step outputs (`$GITHUB_OUTPUT`): `errors`, `warnings`, `fixed`, `remaining`, `result`.
- Secrets are never printed; tokens are registered with `::add-mask::`.

## Interactive (`fix --mode interactive`)

- Shows `[n/total]` with finding, version and the exact action.
- Offers: apply, skip, apply all remaining, quit.
- Re-validation after fixing prints a before/after summary.

## Flags

| Flag | Description |
|---|---|
| `-o, --output console\|json\|actions` | Output format (default: `actions` in Actions, otherwise `console`). |
| `--no-progress` | Plain stage lines or none; implied when stderr is not a terminal. |
| `-q, --quiet` | Only findings and errors. |
| `-v, --verbose` | Per-request detail and debug lines. |
| `--no-color` | Disable colors (`NO_COLOR` honored). |
| `--group-by version\|rule\|concern` | Console grouping (default `version`). |

## Decisions

- Group findings by version by default; `--group-by` also supports `rule` and `concern`.
- Never truncate version lists; the sample above abbreviates the list for readability only.
- Progress is human-readable on stderr; no machine-readable progress stream is emitted.
- Documentation links use `https://github.com/jessehouwing/gh-actions-version/blob/<version>/docs/rules/<rule>.md`,
  where `<version>` is the CLI version tag or `main` for dev builds.
