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
- uses: jessehouwing/gh-version-action@v1
  with:
    auto-fix: true
```

The composite `action.yaml` accepts the same inputs (`token`, `check-minor-version`, `check-releases`,
`check-release-immutability`, `check-marketplace`, `ignore-preview-releases`, `floating-versions-use`, `auto-fix`,
`ignore-versions`) and maps them to flags; `auto-fix: true` becomes `--auto-fix`, otherwise `--check`.

## Locally

```sh
gh extension install jessehouwing/gh-version-action
gh version-action --check
```

## Deprecating the PowerShell code

1. Publish the first `gh-version-action` release.
2. Add a deprecation notice to the old README pointing here.
3. Keep the PowerShell Action receiving only security fixes for one major version, then archive it.
4. Behavior differences are tracked in [test-parity.md](test-parity.md).
