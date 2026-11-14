# Implementation plan for the proposed specs

Status: proposed. Covers `../rule_refactoring.md`, `cli_redesign.md`, `output_redesign.md` and `ignored_versions.md`.

## Principles

- **Small steps.** One PR is one reviewable concern, ideally under ~400 changed lines excluding golden files.
  A PR never mixes a refactor with a behavior change.
- **Always green.** Every commit builds and passes the full test suite. No "fix tests in the next PR".
- **Characterize before changing.** Output changes are made visible by golden files (Phase 0); every PR that
  changes output shows the exact diff in `testdata/`.
- **Tests first where behavior changes.** Write the failing test for the new behavior, then implement.
- **Specs stay the source of truth.** If implementation forces a spec change, update the spec in the same PR.
  When a phase lands, its spec moves from `specs/proposed/` to `specs/` with the decisions recorded.

## Test cadence

Go is not on `PATH` by default on the dev machine: prefix `$env:Path = "C:\Program Files\Go\bin;$env:Path"`.

| When | Command |
|---|---|
| After every edit | `go build ./...` and `go test ./internal/<changed package>/...` |
| Before every commit | `gofmt -l .` (empty), `go vet ./...`, `go test ./...` (~90s, `internal/localgit` is the slow one) |
| When output changes | `go test ./internal/cmd/... -run Golden -update`, then review the `testdata/` diff before committing |
| Before opening a PR | full suite again on the rebased branch, plus a smoke run: `go run . verify -R actions/checkout` |
| End of each phase | smoke runs against `actions/checkout` in every output format; compare with the spec's examples |

CI (`ci.yml`) runs the same checks on each PR in the stack.

## Delivery: stacked PRs

- Repository: `jessehouwing/gh-actions-version`, remote `extension`. Ultimate base: `main`.
- Native GitHub PR stack (stacks API verified available). One stack for the whole plan, extended phase by phase;
  merged layers stay members.
- Branch per layer: `stack/p<phase>-<nn>-<slug>`, e.g. `stack/p1-03-findings-releases`. Each branches from the
  tip of the layer below; each PR targets the branch below it.
- PRs are opened ready for review (not drafts).
- **Review cadence: stop after each phase.** All PRs of the phase are pushed, opened and registered in the stack;
  then work pauses for review. Landing is only done on request.
- Rebasing after review uses `git rebase --onto` and `--force-with-lease=<branch>:<recorded sha>`.
- Commit trailer: `Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>`.

## Decisions

Answers to the open questions in the specs. Each spec is updated with these in the first PR of its phase.

| Topic | Decision |
|---|---|
| Phase order | Findings first; their IDs feed output, JSON, ignores and `--finding-level`. |
| Findings model | Option A: keep rules, explicit finding IDs, config at concern, rule and finding level. |
| Old issue `type` strings | Renamed without alias (not public yet). JSON gets `rule` and `finding`. |
| Burned tag detection | Only after a failed release creation (HTTP 422). No probing. |
| Bare `gh actions-version` | Prints help, exit `2`. |
| `fix` mode default | Terminal: `interactive`. No terminal: `auto`. |
| Exit code `3` | Kept: fixes applied but findings remain. |
| Level flags | Only generic `--level CONCERN=LEVEL`, `--rule-level RULE=LEVEL`, `--finding-level FINDING=LEVEL`. The four `--check-*` flags are removed. |
| `--jq`, `--template`, `--json-fields` | Kept, including shorthands `-q` (`--jq`) and `-t` (`--template`). `--quiet` has no shorthand. Not extended in this plan. |
| Positional versions | Kept on `verify` and `fix` (`verify v1 'v2.*'`); recorded in the findings file filters. |
| Action-input parsing | Removed (`options.ReadActionInput`, `INPUTS`), `action.yaml` is gone. |
| Ignore scopes | All four: `all`, `selection`, `validation`, `fix`. |
| Ignore `rules:` | Supported (rule or finding IDs). |
| Ignore `until` | Supported. |
| Ignore `reason` | Recommended, never required. |
| `--ignore-version` | Kept: repeatable, `scope: all`. `--no-ignore` added. Comma-list `--ignore-versions` removed. |
| Console grouping | Default `version` then findings. `--group-by version\|rule\|concern`. |
| Truncating version lists | Never. |
| Docs links | GitHub URL pinned to the binary's version tag (`main` for `dev` builds). |
| Progress | `github.com/briandowns/spinner` on a terminal. |
| Ignore hygiene (assumed) | New concern `config` with rule `ignore_entries_valid`, findings `ignore_expired` and `ignore_unused`. |
| Line endings (assumed) | `.gitattributes` with `*.go text eol=lf` so `gofmt -l .` is meaningful on Windows. |
| Spec promotion | Each spec moves to `specs/` when its phase lands. |

## Phase 0: safety net and cleanup

Goal: make every later output change visible and remove dead code. No user-visible behavior change.

| PR | Branch | Content | Tests |
|---|---|---|---|
| 0.1 | `stack/p0-01-eol` | `.gitattributes` (`*.go text eol=lf`), renormalize. Verify `gofmt -l .` is empty. | Full suite. |
| 0.2 | `stack/p0-02-remove-action-input` | Remove `options.ReadActionInput`, `ActionInput`, `INPUTS`/action env handling in `internal/cmd/env.go`, JSON-array form of `ParseIgnoreVersions`. | Delete obsolete tests; full suite. |
| 0.3 | `stack/p0-03-fixture-checkout` | `testutil` fixture modeled on `actions/checkout`: ~55 published non-immutable releases, immutability setting unreadable (403), 2 missing releases, missing branding. Plus fixtures: clean repo, floating-on-branches, duplicate refs, drafts. | Scenario test asserting the fixture's current finding counts. |
| 0.4 | `stack/p0-04-golden` | Golden harness in `internal/cmd`: runs `execute` against each fixture in console, json and actions output; normalizes durations, temp paths and hosts; `-update` flag. Captures **today's** output. | The golden tests themselves. |

Exit criterion: any later change to output fails a golden test until `-update` is reviewed.

## Phase 1: explicit findings (Option A)

Spec: `../rule_refactoring.md`. Goal: every issue carries `rule` and `finding`; finding IDs are declared,
unique, documented and configurable.

| PR | Branch | Content | Tests |
|---|---|---|---|
| 1.1 | `stack/p1-01-spec` | Record decisions in `rule_refactoring.md`. Verify the type strings produced by the `*_tracks_highest_patch` helper and fill in the catalog. | none (docs) |
| 1.2 | `stack/p1-02-finding-registry` | `rules.Rule` gains `Findings []FindingDef{ID, Summary}`. `state.Issue` gains `Rule` and `Finding`; `Type` remains for now. Engine stamps `Rule` on every issue. Each rule declares its **current** types as findings (no renames yet). | Registry test: every finding unique and owned by exactly one rule; every rule declares at least one. Engine test: an undeclared type fails the test run. |
| 1.3 | `stack/p1-03-findings-releases` | Rename release findings per catalog (`release_not_immutable`, `release_is_draft`, `release_missing`, `latest_release_wrong`, `release_duplicate`, floating-release pair). Update `actions/base.go` lookups and `remediation/instructions.go`. | Rule tests, scenario tests, golden diff (JSON `type`). |
| 1.4 | `stack/p1-04-findings-reftype` | `floating_ref_should_be_tag/branch`, `*_tag_and_branch_both_exist`, `patch_ref_duplicate`. Splits the shared `wrong_ref_type`. | Same. |
| 1.5 | `stack/p1-05-findings-tracking` | `major/minor_tag/branch_missing`, `*_wrong_target` for the tracking rules, `patch_tag_missing`. | Same. |
| 1.6 | `stack/p1-06-findings-marketplace` | `marketplace_metadata_missing`, `marketplace_not_published`, `immutable_releases_disabled`. | Same. |
| 1.7 | `stack/p1-07-drop-type` | Replace `Issue.Type` by `Issue.Finding`; JSON emits `rule` and `finding`; remove `type`. | Golden diff; full suite. |
| 1.8 | `stack/p1-08-finding-policy` | `rules.<rule>.findings.<id>.level` / `.fix` (`auto` or `manual`) in `action-version.yml`; precedence finding > rule > concern; unknown finding is an error. Update the embedded default file comments. | `policy` tests (parse, unknown key, precedence); `engine` tests. |
| 1.9 | `stack/p1-09-burned-tag` | Finding `patch_tag_locked_by_deleted_release`: when release creation returns 422 for an existing immutable tag, the failed fix is reclassified to this finding. Its fix (delete tag) stays behind `--dangerously-allow-destructive-tag-deletion`. Suggestion text points to `ignore-versions` for now (Phase 5 upgrades it). | Fake server returns 422; scenario asserts the finding, no deletion without the flag, deletion with it. |
| 1.10 | `stack/p1-10-docs` | Findings table on every `docs/rules/*.md` page; `docs/rules/README.md`; Configuration sections mention `findings.<id>`. Promote `rule_refactoring.md` to `specs/`. | Test: every declared finding appears in its rule page (keeps docs in sync). |

Checkpoint: review stop. Smoke run against `actions/checkout`; JSON shows rule/finding for every issue.

## Phase 2: subcommand CLI skeleton

Spec: `cli_redesign.md`. Goal: new command surface, wired to the **existing** pipeline and renderers. Output
format redesign is Phase 3.

| PR | Branch | Content | Tests |
|---|---|---|---|
| 2.1 | `stack/p2-01-spec` | Record decisions in `cli_redesign.md`. | none |
| 2.2 | `stack/p2-02-subcommands` | Cobra `verify` and `fix` subcommands; bare root prints help, exit 2. `verify` = old `--check`; `fix` = old auto-fix for now. Positional versions on both. Remove `--check`, `--no-fix`, `--auto-fix`. Golden invocations updated; outputs unchanged. | `cmd` tests per subcommand; golden (no output diff). |
| 2.3 | `stack/p2-03-global-flags` | Persistent flags: `-R`, `--local`, `--fetch-releases`, `--max-wait`, `-v/--verbose`, `--no-color`, `--config PATH`, `--no-config` (new in the policy loader). | Policy loader tests for `--config`/`--no-config`; flag tests. |
| 2.4 | `stack/p2-04-level-flags` | `--level`, `--rule-level`, `--finding-level` (repeatable `KEY=LEVEL`, validated against the registry). Remove the four `--check-*` flags. `--floating-versions-use`, `--include-preview-releases`. | Precedence tests: flag > file > default, per concern/rule/finding; invalid key exits 2. |
| 2.5 | `stack/p2-05-fix-modes` | `fix --mode interactive\|auto\|manual`, `--interactive/-i`, `--auto`, `--manual`; conflicts exit 2; default by terminal. `--yes`, `--dry-run`, `--no-verify-after` (default re-verifies). | Mode resolution table test; dry-run makes no writes (fake server asserts no mutations). |
| 2.6 | `stack/p2-06-filters-exit` | `verify --fail-on`, `--rule`, `--skip-rule`, `--finding` on both; exit code 3 for fix. | Filter and exit code tests. |
| 2.7 | `stack/p2-07-output-flag` | `-o/--output console\|json\|actions`, default by `GITHUB_ACTIONS`; `--json-fields`; `--jq/-q`, `--template/-t` imply json. Old `--json` removed. | Golden runs per format via `-o`. |
| 2.8 | `stack/p2-08-docs` | README, `docs/cli.md`, `docs/migration.md` and every rule page's Configuration section use the new commands and flags. | Test: no doc mentions removed flags (`--check-`, `--auto-fix`, `--json `). |

Checkpoint: review stop. `cli_redesign.md` stays proposed until findings file, `ignore` and `setup` exist.

## Phase 3: output redesign

Spec: `output_redesign.md`. Goal: one report model, three renderers, progress on stderr.

| PR | Branch | Content | Tests |
|---|---|---|---|
| 3.1 | `stack/p3-01-spec` | Record decisions in `output_redesign.md` (grouping by version, no truncation, URL docs). | none |
| 3.2 | `stack/p3-02-config-source` | Policy records where each effective value came from (`flag`, `file:<path>`, `default`). | Policy tests. |
| 3.3 | `stack/p3-03-report-model` | New `internal/report`: pure `Build(state) Report` with findings, groups (by version, rule, concern), skipped checks, fixes, summary, timings, config with sources. Docs URL built from `cmd.Version` (`main` when `dev`); `release.yml` passes `-ldflags -X .../internal/cmd.Version=<tag>` to `gh-extension-precompile`. | Unit tests with hand-built states; URL test. |
| 3.4 | `stack/p3-04-skipped-cause` | State records skipped checks (e.g. immutability setting 403) with reason; report derives `cause` lines (e.g. `release_not_immutable` with setting disabled/unknown). | Scenario on the checkout fixture. |
| 3.5 | `stack/p3-05-progress` | `internal/progress`: `Stage` start/update/done; spinner on terminal stderr, plain lines otherwise, `::group::` in actions, no-op for `--quiet`. Instrument loading of refs, release pages, settings, rules, fixes; explicit rate-limit message. Flags `--no-progress`, `--quiet`. | Writer-based tests per implementation; `api` test for page events; json stdout stays a single document. |
| 3.6 | `stack/p3-06-console` | Console renderer from the report: grouped by version, `--group-by`, summary, skipped checks, wording rules, colors. `verify` prints no remediation, only the `fix` hint. | Golden diff (big, reviewed in isolation). |
| 3.7 | `stack/p3-07-manual` | `fix --mode manual` renderer: commands deduplicated per finding with `<tag>`; `-v` expands per version. | Golden. |
| 3.8 | `stack/p3-08-json` | JSON renderer: `schemaVersion: 1`, `findings[]` (incl. `manualCommands`), `groups[]`, `skipped[]`, `fixes[]`, `summary`, `timings`, `config`. `--json-fields`/`--jq`/`--template` work on it. | Golden + a schema-shape test. |
| 3.9 | `stack/p3-09-actions` | Actions renderer: annotations titled `rule/finding`, one per finding group; job summary (no manual commands unless manual mode); `$GITHUB_OUTPUT` outputs; 1 MiB cap; `::add-mask::`. | Golden with temp `GITHUB_STEP_SUMMARY`/`GITHUB_OUTPUT`. |
| 3.10 | `stack/p3-10-cleanup-docs` | Remove unused `output.Logger` paths and `jsonout.go` remnants; docs output section; promote `output_redesign.md`. | Full suite. |

Checkpoint: review stop. Smoke against `actions/checkout`: console output resembles the spec sample; json
validates; actions summary rendered in a scratch workflow run.

## Phase 4: findings file (`verify --save`, `fix --from`)

Spec: `cli_redesign.md` (findings file section).

| PR | Branch | Content | Tests |
|---|---|---|---|
| 4.1 | `stack/p4-01-action-serialization` | Each `state.Action` type gets a stable serialized form (`kind` + params needed to execute without reloading: tag, target SHA, release ID, ...). | Round-trip test for every action type; test that every registered action type is covered. |
| 4.2 | `stack/p4-02-findings-file` | Report JSON extended with `filters` (rules, skip-rules, findings, versions, ignored), `generatedAt`, `refs`, serialized actions. `verify --save FILE`; `-o json` emits the same document. | Golden; `--save` with `-o console` writes the file and prints console. |
| 4.3 | `stack/p4-03-fix-from` | `fix --from FILE\|-`: validate `schemaVersion` and `--repo`, narrow with filters, all modes, print `generatedAt` and recorded filters, `--no-verify-after` implied. | Fake server asserts **zero** requests for `--mode manual` and zero before the first action in `auto`; narrowing test; mismatch exits 2. |
| 4.4 | `stack/p4-04-docs` | README and `docs/cli.md` section "Verify now, fix later". | none |

Checkpoint: review stop.

## Phase 5: ignored versions

Spec: `ignored_versions.md`.

| PR | Branch | Content | Tests |
|---|---|---|---|
| 5.1 | `stack/p5-01-spec` | Record decisions (reason optional, `config` concern). | none |
| 5.2 | `stack/p5-02-ignore-config` | Parse structured `ignore:` (versions, reason, scope, rules, until) and simple `ignore-versions`; validation of patterns, scopes, rule/finding IDs, dates. | Policy tests incl. unknown keys. |
| 5.3 | `stack/p5-03-scopes` | Replace `VersionRef.IsIgnored` with per-version ignore matches. `all` (today), `selection` (excluded from highest-patch selection helpers), `validation` (findings dropped), `fix` (manual only), `rules:` restriction. Union when several entries match. | Scenario per scope; `releases_helper`/`versiontracking_common` selection tests. |
| 5.4 | `stack/p5-04-hygiene` | Concern `config`, rule `ignore_entries_valid`, findings `ignore_expired`, `ignore_unused`. Docs page. | Concern/rule test; scenarios. |
| 5.5 | `stack/p5-05-flags` | `--ignore-version` (repeatable), `--no-ignore`; remove `--ignore-versions`. | Precedence tests. |
| 5.6 | `stack/p5-06-yaml-edit` | `internal/policy/edit`: comment-preserving edits with `yaml.v3` `Node`: add/update/remove ignore entries; find root or `.github/` file. | Round-trip tests preserving comments, order and unrelated keys. |
| 5.7 | `stack/p5-07-ignore-cmd` | `ignore PATTERN` (`--reason`, `--scope`, `--until`, `--rule`), `--list`, `--remove`; offline; prompts for reason on a terminal (optional). | `cmd` tests with temp dirs; asserts no network. |
| 5.8 | `stack/p5-08-output-docs` | `ignored[]` in report and all renderers; burned-tag suggestion prints a structured `scope: selection` entry or the `ignore` command. Promote `ignored_versions.md`. | Golden. |

Checkpoint: review stop.

## Phase 6: `setup`

| PR | Branch | Content | Tests |
|---|---|---|---|
| 6.1 | `stack/p6-01-setup-model` | Question model producing a policy; `--yes`, flags pre-answer, `--print`. | Table tests: answers → YAML. |
| 6.2 | `stack/p6-02-setup-prompts` | Interactive prompts via go-gh `prompter`; ignored versions via the Phase 5 editor. | Fake prompter tests. |
| 6.3 | `stack/p6-03-setup-write` | Write or update (comments kept), `--path`, `--force`, diff summary, exit 2 on an existing file without force/confirmation. | Temp dir tests. |
| 6.4 | `stack/p6-04-docs` | README quick start starts with `setup`; promote `cli_redesign.md`; final docs sweep. | Doc test from 2.8 still passes. |

Final checkpoint: full smoke matrix (`setup`, `verify`, `fix --mode manual`, `verify --save` + `fix --from`,
`ignore`) against `actions/checkout` (read-only) and a scratch repository (writes).

## Risks

- **Golden churn.** Phases 1-3 change output repeatedly. Mitigation: golden updates are reviewed per PR and
  big renderer swaps (3.6-3.9) get their own PRs.
- **Stack rebases.** Review feedback on a low layer forces rebasing everything above. Mitigation: stop per
  phase, keep layers small, rebase with `--onto` and leases.
- **Action serialization (4.1)** may expose actions that depend on live lookups. Mitigation: if an action
  cannot be serialized, `fix --from` reports it as `requires-reload` and `verify` is suggested; flagged before 4.3.
- **`fix` defaults to `auto` without a terminal**: a bare `fix` in CI writes. Documented prominently in 2.8.
- **Docs URL pinning** depends on the release build setting `Version`; covered by 3.3.
