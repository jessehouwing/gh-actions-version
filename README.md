# gh-version-action

A [`gh`](https://cli.github.com) extension that validates and repairs the version tags, branches and releases of a
GitHub Action repository, following GitHub's
[immutable release strategy](https://docs.github.com/en/actions/how-tos/create-and-publish-actions/using-immutable-releases-and-tags-to-manage-your-actions-releases).
It is the Go successor of the PowerShell `jessehouwing/actions-semver-checker` Action.

## Install

```sh
gh extension install jessehouwing/gh-version-action
gh extension upgrade version-action
```

## Quickstart

```sh
cd my-action-repo
gh version-action                 # report problems (read-only)
gh version-action --auto-fix      # fix them
gh version-action -R owner/repo   # run against another repository
```

## What it checks

- Patch versions (`v1.0.0`) have a published, immutable GitHub Release.
- Floating versions (`v1`, `v1.0`) exist and point at the latest compatible patch.
- Floating versions are tags or branches, as configured; no duplicates.
- Marketplace metadata (`action.yaml` name, description, branding, README).

Prerelease status comes only from the GitHub Release API, never from tag suffixes.

## Modes

| Flag | Behavior |
|---|---|
| `--check` (alias `--no-fix`) | Report only; exit 1 when errors are found. |
| `--auto-fix` | Apply fixes (needs write access). |
| `--interactive` / `-i` | Review and confirm each fix on a terminal; `--yes` turns it into auto-fix. |
| `--local` / `--local-only` | Read refs from the local git clone instead of the API; `--fetch-releases` also fetches releases. |
| `--json fields` (`--jq`, `--template`) | Machine-readable output, as in other `gh` commands. |

Every rule is documented in [docs/rules](docs/rules/README.md).

Rule options: `--check-minor-version`, `--check-releases`, `--check-release-immutability`, `--check-marketplace`
(`error`, `warning`, `none`), `--include-preview-releases`, `--floating-versions-use` (`tags`, `branches`, `both`),
`--ignore-versions` (supports `v1.*`), `--max-wait`. See `gh version-action --help`.

`--dangerously-allow-destructive-tag-deletion` is needed only when a tag was used by an immutable release that has since been deleted: such a tag can't be republished, and deleting it is irreversible. Without the flag the issue is reported as a manual fix with the `git push origin :refs/tags/<tag>` command. Other tag deletions are not affected.

Exit codes: `0` success, `1` validation errors remain, `2` usage error.

## Authentication

Uses your `gh` login. Reading needs `repo` (or public access). Fixing needs write access to contents:

```sh
gh auth refresh -s repo
```

Tokens that modify `.github/workflows/*` need the `workflow` scope (`gh auth refresh -s workflow`).
In Actions, `GITHUB_TOKEN` with `contents: write` can push tags and create releases but cannot trigger workflows.

## Version policy (`action-version.yml`)

Defaults for the versioning strategy (tags or branches, major/minor/patch, releases, immutability) and
for each concern's `error`/`warning`/`none` level and `autofix` come from a built-in policy. Commit an
`action-version.yml` at the repository root (or `.github/`; the first found wins) to override it, per
concern or per rule:

```yaml
version: 1
floating-versions-use: tags
concerns:
  minor: { level: warning }
  releases: { autofix: false }
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
  - uses: actions/checkout@v4
  - run: |
      gh extension install jessehouwing/gh-version-action
      gh version-action --auto-fix
    env:
      GH_TOKEN: ${{ github.token }}
```

## Compared with the old Action

| | Old PowerShell Action | This extension |
|---|---|---|
| Runtime | `pwsh` on a runner | single Go binary via `gh` |
| Local use | no | yes (`--local`, interactive) |
| Output | log annotations | annotations, `--json`, `--jq`, `--template` |
| Inputs | action inputs | command-line flags |

See [docs/migration.md](docs/migration.md) and [docs/troubleshooting.md](docs/troubleshooting.md).

## License

MIT, see [LICENSE](LICENSE).
