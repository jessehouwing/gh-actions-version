# Copilot Instructions for actions-semver-checker

## Project Overview

A Go `gh` CLI extension (`gh version-action`) plus a composite GitHub Action (`action.yaml`) that installs and runs it. It validates semantic version tags/branches/releases: floating versions (`v1`, `v1.0`) must point to the latest patch, and patch releases must be published and immutable ([GitHub's immutable release strategy](https://docs.github.com/en/actions/how-tos/create-and-publish-actions/using-immutable-releases-and-tags-to-manage-your-actions-releases)).

**Prerelease status comes only from the GitHub Release API (`prerelease` field)**, never from tag suffixes. Tags such as `v1.0.0-beta` are not supported.

## Layout

| Path | Purpose |
|------|---------|
| `main.go` | Entry point |
| `internal/cmd` | Cobra command, flags, config building, interactive flow, JSON output |
| `internal/state` | Domain model (`State`, `VersionRef`, `ReleaseInfo`, `Issue`, `Config`, `Client` interface) |
| `internal/api` | GitHub REST/GraphQL client (go-gh), retry and rate limiting |
| `internal/localgit` | Local git implementation of `state.Client` |
| `internal/rules` | Validation rules; self-register via `init()`/`Register` |
| `internal/actions` | Remediation actions (create/update/delete tag, releases, ...) |
| `internal/remediation` | Fix coordination, manual instructions |
| `internal/policy` | `action-version.yml` parsing/merging; embedded default in `internal/policy/action-version.yml` |
| `internal/scenarios`, `internal/testutil` | Scenario tests and the fake GitHub server |

## Conventions

- All state lives in `state.State`; rules read it and never call the network.
- Config precedence: CLI flag > `action-version.yml` > built-in default. Boolean flags are opt-in (no `=false` to change a default).
- Tokens come from go-gh (`auth.TokenForHost`); there is no `--token` flag. Reuse go-gh (`term`, `auth`, REST client) instead of reimplementing.
- Every rule must belong to exactly one concern in `internal/policy/policy.go` (`concernRules`); a test enforces this.
- Deleting a tag whose immutable release was deleted requires `--dangerously-allow-destructive-tag-deletion`.
- GraphQL queries must have a test verifying that all fields the code relies on are queried.
- Action inputs that override `action-version.yml` default to empty in `action.yaml`; only set inputs become flags.

## Build and test

```bash
gofmt -l .
go vet ./...
go test ./...
```
