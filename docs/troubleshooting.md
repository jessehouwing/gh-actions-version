# Troubleshooting

| Symptom | Cause / fix |
|---|---|
| `HTTP 403` / `Resource not accessible by integration` | Token lacks write access. Run `gh auth refresh -s repo`, or set `permissions: contents: write` in Actions. |
| Fix fails for `.github/workflows` changes | Needs a PAT or app token with `workflow` scope. |
| `HTTP 422` on release create | Tag was used by a deleted immutable release. Use a `selection` ignore or a new patch version. |
| Tag delete blocked | A ruleset has a `deletion` rule; the output names it. Adjust the ruleset or run the printed `git push origin :refs/tags/<tag>`. |
| Rate limit errors | Authenticate, or wait; see `--max-wait`. |
| `--interactive requires a terminal` | Use `fix --auto` or `verify` in CI. |
| Prerelease ignored for `v1` | prereleases are excluded unless you pass `--preview-releases=included`; prerelease status is from the Release API only. |
| Not found on GHES | Check `GH_HOST` and that the host supports immutable releases. |

Exit code 2 means a usage or runtime error; 1 means qualifying findings remain; 3 means fixes were applied but qualifying findings remain.
