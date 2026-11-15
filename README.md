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
| `verify [versions...]` | Report only; exit 1 when errors meet `--fail-on` (default: `error`). |
| `fix --mode auto` | Apply fixes (needs write access); exit 3 if fixes were applied but qualifying findings remain. |
| `fix --mode interactive` / `fix -i` | Review and confirm each fix on a terminal; `fix --yes` accepts every prompt. |
| `fix --mode manual` | Print remediation instructions without changing anything. |
| `--local` / `--local-only` | Read refs from the local git clone instead of the API; `--fetch-releases` also fetches releases. |
| `-o, --output console\|json\|actions` | Select output format; defaults to `actions` in GitHub Actions and `console` elsewhere. |
| `--json-fields a,b,c`, `--jq` / `-q`, `--template` / `-t` | Select or format JSON output; `--jq` and `--template` imply JSON. |

Every rule is documented in [docs/rules](docs/rules/README.md).
See [CLI output formats](docs/cli.md#output-formats) for details on console, JSON, and GitHub Actions reports.

Policy options: `--level CONCERN=LEVEL`, `--rule-level RULE=LEVEL`, `--finding-level FINDING=LEVEL`
(`error`, `warning`, `none`); filters: `--rule`, `--skip-rule`, `--finding`; `--fail-on error|warning|never`.
Other options include `--preview-releases=included|excluded`, `--floating-versions-use` (`tags`, `branches`, `both`),
`--ignore-versions` (supports `v1.*`), and `--max-wait`. See `gh actions-version --help`.

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
[`internal/policy/action-version.yml`](internal/policy/action-version.yml) and documents every key.

## Rate limiting

Requests are retried with backoff on transient and rate-limit errors. Authenticated calls have much higher limits;
use `--local` to avoid API reads of refs.

## GitHub Enterprise

Set `GH_HOST` or log in with `gh auth login --hostname HOST`; API and server URLs are derived from the host.
Immutable releases and some GraphQL fields may not exist on older GHES versions; related checks degrade to warnings
or can be set to `none`.

## Limitations

- A tag used by a deleted immutable release cannot be reused; use `--ignore-versions` or a new patch version.
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

See [docs/migration.md](docs/migration.md) and [docs/troubleshooting.md](docs/troubleshooting.md).

## License

MIT, see [LICENSE](LICENSE).
