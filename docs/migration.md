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
    gh actions-version --auto-fix
  env:
    GH_TOKEN: ${{ github.token }}
```

Former inputs map to flags of the same name (for example `check-releases: warning` becomes `--check-releases=warning`).

## Locally

```sh
gh extension install jessehouwing/gh-actions-version
gh actions-version --check
```

## Deprecating the PowerShell code

1. Publish the first `gh-actions-version` release.
2. Add a deprecation notice to the old README pointing here.
3. Keep the PowerShell Action receiving only security fixes for one major version, then archive it.
4. Behavior differences are tracked in [test-parity.md](test-parity.md).
