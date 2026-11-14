# Migrating from actions-semver-checker

## Workflows

Replace

```yaml
- uses: jessehouwing/actions-semver-checker@v2
  with:
    auto-fix: true
```

with

```yaml
- run: |
    gh extension install jessehouwing/gh-actions-version
    gh actions-version fix --mode auto
  env:
    GH_TOKEN: ${{ github.token }}
```

Former inputs map to the new CLI options. For example, `check-releases: warning` becomes
`gh actions-version verify --level releases=warning`, and `auto-fix: true` becomes
`gh actions-version fix --mode auto`. Use `--level CONCERN=LEVEL` for concern levels and
`--rule-level RULE=LEVEL` or `--finding-level FINDING=LEVEL` for more specific overrides.

## Locally

```sh
gh extension install jessehouwing/gh-actions-version
gh actions-version verify
```

## Deprecating the PowerShell code

1. Publish the first `gh-actions-version` release.
2. Add a deprecation notice to the old README pointing here.
3. Keep the PowerShell Action receiving only security fixes for one major version, then archive it.
4. Behavior differences are tracked in [test-parity.md](test-parity.md).
