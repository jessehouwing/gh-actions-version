# Rule: ignore_entries_valid

Warns when an ignore entry has expired or matches no existing tag, branch, or release.
An entry remains active through its `until` date (UTC); expired entries do not suppress findings.

## Findings

| Finding | Summary |
|---|---|
| `ignore_expired` | An ignore entry has expired |
| `ignore_unused` | An ignore entry matches no version |

## Configuration

This rule belongs to the `config` concern:

```yaml
concerns:
  config:
    level: warning
rules:
  ignore_entries_valid:
    findings:
      ignore_expired:
        level: error
      ignore_unused:
        level: none
```

Review or remove the entry in `action-version.yml`; the tool does not automatically edit it.
