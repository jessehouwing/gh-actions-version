# gh-actions-version

A [`gh`](https://cli.github.com) extension that validates and repairs the version tags, branches and releases of a
GitHub Action repository, following GitHub's
[immutable release strategy](https://docs.github.com/en/actions/how-tos/create-and-publish-actions/using-immutable-releases-and-tags-to-manage-your-actions-releases).
It is the Go successor of the PowerShell `jessehouwing/actions-semver-checker` Action.

## Install

```sh
gh extension install jessehouwing/gh-actions-version
gh extension upgrade actions-version
```

## Quickstart

```sh
cd my-action-repo
gh actions-version setup           # configure and write action-version.yml
gh actions-version verify          # report problems (read-only)
gh actions-version fix --mode auto      # fix them
gh actions-version verify -R owner/repo # run against another repository
```

## What it checks

- Patch versions (`v1.0.0`) have a published, immutable GitHub Release.
- Floating versions (`v1`, `v1.0`) exist and point at the latest compatible patch.
- Floating versions are tags or branches, as configured; no duplicates.
- Marketplace metadata (`action.yaml` name, description, branding, README).

Prerelease status comes only from the GitHub Release API, never from tag suffixes.

## Commands and output

| Command or flag | Behavior |
|---|---|
| `setup` | Configure and write a local policy; `--yes` accepts defaults, `--print` writes YAML to stdout. |
| `verify [versions...]` | Report only; exit 1 when errors meet `--fail-on` (default: `error`). |
| `fix --mode auto` | Apply fixes (needs write access); exit 3 if fixes were applied but qualifying findings remain. |
| `fix --mode interactive` / `fix -i` | Review and confirm each fix on a terminal; `fix --yes` accepts every prompt. |
| `fix --mode manual` | Print remediation instructions without changing anything. |
| `verify --save FILE` / `fix --from FILE` | Save findings and apply them later without a preliminary reload. |
| `--local` / `--local-only` | Read refs from the local git clone instead of the API; `--fetch-releases` also fetches releases. |
| `-o, --output console\|json\|actions` | Select output format; defaults to `actions` in GitHub Actions and `console` elsewhere. |
| `--group-by version\|rule\|concern` | Group console findings by version (default), rule, or concern. |
| `--json-fields a,b,c`, `--jq` / `-q`, `--template` / `-t` | Select or format JSON output; `--jq` and `--template` imply JSON. |
| `--no-progress` | Use plain progress lines instead of a terminal spinner. |
| `--quiet` | Suppress progress and informational output; findings, warnings, and errors remain visible. `-q` is reserved for `--jq`. |

## Output formats

### Console

The default outside GitHub Actions. Findings are grouped by version by default; `--group-by` selects version,
rule, or concern grouping. The summary includes finding counts, skipped checks, and documentation links.

### JSON

JSON emits a `schemaVersion: 1` report with repository details, effective configuration and its sources,
filters, generation time, refs, serialized actions, findings, groups, skipped checks, fixes, summary, timings,
and exit code. Use `--json-fields` to select top-level fields, `--jq` to filter, or `--template` to format the
report; each implies JSON output.

### GitHub Actions

Use `--output=actions` to emit workflow-command annotations to stdout. Unresolved findings are grouped by rule,
with annotation titles in `rule/finding` form; each finding group lists its versions. Annotations are limited to
10 per severity, while the step summary retains all findings.

When `GITHUB_ACTIONS=true`, the renderer writes the report to `$GITHUB_STEP_SUMMARY` and counts to
`$GITHUB_OUTPUT` when those paths are provided. The summary includes findings, skipped checks, fixes, and
effective configuration; manual commands are included only for `fix --mode manual`, and long summaries are
capped at 1 MiB. Step outputs are `errors`, `warnings`, `fixed`, `remaining`, and `result` (`success` or
`failure`). The GitHub token is registered with `::add-mask::`.

## Verify now, fix later

```sh
gh actions-version verify -R owner/repo --save findings.json
gh actions-version fix --from findings.json --mode manual
gh actions-version fix -R owner/repo --from findings.json --mode auto
```

`--save` writes the complete findings file regardless of output format or JSON field selection.
An unfiltered `verify -o json > findings.json` produces the same kind of file. `fix --from -` reads
it from stdin. Remote automatic or interactive replay requires the independently selected repository,
including its host, to match the recorded repository before credentials are accessed. The target is
resolved from `-R`, `GH_REPO`, `GITHUB_REPOSITORY`, or the local Git remote; use `-R` when it cannot
be resolved. If `-R` is supplied in any mode, it must match the recorded repository. A file created
with `--local` must be replayed with `--local` in the intended local clone.

The file records the effective policy, filters, refs, and action parameters. `--rule`, `--skip-rule`,
`--finding`, and version arguments can narrow the saved findings but cannot add findings that were
not saved. Explicit policy flags can override recorded settings; `--config` and `--no-config` cannot
be used with `--from`. Actions missing required release IDs are marked as requiring a new `verify`
and are not automatically applied.

Manual mode makes no API calls. Auto and interactive modes make no API calls until an action runs.
There is **no staleness check**: updates and deletions act on the saved data. Review the file and
re-run `verify` if the repository may have changed. Re-verification after fixing is disabled by default
for `--from`; pass `--no-verify-after=false` to request it.

Policy options: `--level CONCERN=LEVEL`, `--rule-level RULE=LEVEL`, `--finding-level FINDING=LEVEL`
(`error`, `warning`, `none`); filters: `--rule`, `--skip-rule`, `--finding`; `--fail-on error|warning|never`.
Other options include `--preview-releases=included|excluded`, `--floating-versions-use` (`tags`, `branches`),
`--ignore-version` (repeatable; supports `v1.*`), `--no-ignore`, and `--max-wait`. See `gh actions-version --help`.

Deleting a tag used by a deleted immutable release is irreversible. It is denied by default; explicitly set
`permanently-delete-immutable-tags: dangerously-allow` in the policy or pass
`--dangerously-allow-destructive-tag-deletion` to permit it. Other tag deletions are not affected.

Exit codes: `0` success, `1` qualifying findings remain, `2` usage or runtime error, `3` fixes applied but qualifying findings remain.

## Authentication

Uses your `gh` login. Reading needs `repo` (or public access). Fixing needs write access to contents:

```sh
gh auth refresh -s repo
```

Tokens that modify `.github/workflows/*` need the `workflow` scope (`gh auth refresh -s workflow`).
In Actions, `GITHUB_TOKEN` with `contents: write` can push tags and create releases but cannot trigger workflows.

## Version policy (`action-version.yml`)

Defaults for the versioning strategy (tags or branches, major/minor/patch, releases, immutability) and
for each concern's `error`/`warning`/`none` level and `fix` policy come from a built-in policy. Commit an
`action-version.yml` at the repository root (or `.github/`; the first found wins) to override it, per
concern or per rule:

```yaml
version: 1
floating-versions-use: tags
preview: include
permanently-delete-immutable-tags: deny
concerns:
  minor: { level: warning }
  releases: { fix: manual }
rules:
  immutable_releases_enabled: { level: none }
```

Precedence: command-line flag > `action-version.yml` > built-in default. The built-in file is
[`internal/policy/action-version.yml`](../internal/policy/action-version.yml) and documents every key.

### Setup

`gh actions-version setup` asks about floating versions, releases, Marketplace checks, preview releases,
immutable-tag deletion, ignored versions and per-concern fix policies. It is local and needs no token.
Questions are grouped into sections with explanations of the choices. Fix policies can be set to all
manual, all auto, or fine-tuned by concern, with major, minor and patch versions kept together.
Auto makes supported fixes eligible when you later run `fix`; manual provides instructions instead.
Bulk choices apply only to unanswered concerns and preserve explicit `--fix-policy` flags.
Previews default to excluded. Deleting a tag used by a deleted immutable release is irreversible and
defaults to denied; enabling it requires an explicit prompt or `--dangerously-allow-destructive-tag-deletion`.

```sh
gh actions-version setup --yes --print
gh actions-version setup --yes --path .github/action-version.yml
gh actions-version setup --yes --preview-releases=included --fix-policy releases=manual
```

`--yes` accepts defaults for unanswered questions. `--floating-versions-use`, `--preview-releases`,
`--level`, `--rule-level`, `--finding-level`, repeatable `--fix-policy CONCERN=auto|manual`,
`--ignore-version`, and `--no-ignore` pre-answer their questions. Without a terminal, use `--yes`
or provide all answers; irreversible deletion remains denied unless explicitly enabled. `--path` or
`--config` selects the destination; the default is `action-version.yml` at the root, and the interactive
wizard can select `.github/`. `--print` emits YAML only and cannot be combined with a path or `--force`.

An existing file requires terminal confirmation or `--force`; even with `--yes`, overwrite is not
automatic. Updates preserve comments and unrelated settings. Existing ignores remain unless explicitly
cleared with `--no-ignore`; new ignores are added or updated. The command reports the path and changed
line counts. Invalid existing policies and concurrent changes are rejected without overwriting.

### Ignored versions

```sh
gh actions-version ignore 'v1.2.3' --scope selection --reason "broken release"
gh actions-version ignore --list
gh actions-version ignore --remove 'v1.2.3'
gh actions-version verify --no-ignore --ignore-version 'v0.*'
```

`ignore` edits the local root or `.github/` policy offline; `--config PATH` selects a file.
Scopes are `all` (hide entirely), `selection` (exclude as a highest-version candidate),
`validation` (suppress findings), and `fix` (manual-only). `--rule` can be repeated to restrict
an entry to rule or finding IDs; `--until YYYY-MM-DD` sets an inclusive UTC expiry. Reasons are
optional. Expired and unused entries generate warnings. One-off `--ignore-version` flags are
additive, after `--no-ignore` removes configured ignores. With `fix --from`, absent saved findings
cannot be restored; recorded manual fix policies remain in force.

The JSON `ignored` array reports matching versions and active scopes, reasons, expiry and configured
rule/finding restrictions (`matchedRules`; empty means unrestricted). Console and Actions output also
show ignored versions; verbose console output lists generated findings suppressed by ignores.

## Rate limiting

Requests are retried with backoff on transient and rate-limit errors. Authenticated calls have much higher limits;
use `--local` to avoid API reads of refs.

## GitHub Enterprise

Set `GH_HOST` or log in with `gh auth login --hostname HOST`; API and server URLs are derived from the host.
Immutable releases and some GraphQL fields may not exist on older GHES versions; related checks degrade to warnings
or can be set to `none`.

## Limitations

- A tag used by a deleted immutable release cannot be reused; use a `selection` ignore or a new patch version.
- Tag rulesets can block tag deletion; the tool names the ruleset and prints manual commands.
- Auto-fix never rewrites published immutable releases.

## Use in GitHub Actions

`gh` is preinstalled on GitHub-hosted runners:

```yaml
permissions:
  contents: write
steps:
  - uses: actions/checkout@v7
  - run: |
      gh extension install jessehouwing/gh-actions-version
      gh actions-version fix --mode auto
    env:
      GH_TOKEN: ${{ github.token }}
```

## Compared with the old Action

| | Old PowerShell Action | This extension |
|---|---|---|
| Runtime | `pwsh` on a runner | single Go binary via `gh` |
| Local use | no | yes (`--local`, interactive) |
| Output | log annotations | console, JSON, or GitHub Actions workflow commands (`--output`, `--jq`, `--template`) |
| Inputs | action inputs | command-line flags |

See [migration](migration.md), [troubleshooting](troubleshooting.md) and the [rule reference](rules/README.md).

## License

MIT, see [LICENSE](../LICENSE).
