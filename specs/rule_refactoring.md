# Rule → finding refactoring

Status: implemented in Phase 1 (Explicit Findings).

## Problem

The project started with a clear 1 rule → 1 finding mapping. The Go port drifted into rule → many findings:
some rules emit several issue types, some issue types are shared by several rules, and the issue type
strings are not documented or configurable. As a result, `action-version.yml` can only tune a rule as a
whole, and the docs describe rules but not what they report.

Examples (from `internal/rules`):

- `branch_should_be_tag` and `tag_should_be_branch` both emit `wrong_ref_type`.
- `major_tag_missing` and `major_branch_missing` both emit `missing_major_version` (same for minor).
- `floating_version_no_release` emits `immutable_floating_release` and `mutable_floating_release`.
- `duplicate_floating_version_ref` and `duplicate_latest_ref` each have two call sites for one type.
- The `*_tracks_highest_patch` rules pass the type in from a shared helper.

## Detecting a burned tag

There is no GraphQL or REST API that exposes an immutable tag whose release was deleted. Checked against
the live GraphQL schema and the REST OpenAPI description:

- GraphQL: `Release.immutable` is the only immutability field; `Ref` and `Tag` have none.
- REST: `release.immutable`, `GET /repos/{owner}/{repo}/immutable-releases` (repository setting) and
  `/orgs/{org}/settings/immutable-releases*` (organization setting). Nothing about tags.
- GitHub docs: a tag of a deleted immutable release can be deleted but its name can never be reused.

The only signal is the HTTP 422 when creating a release on the tag. Detect it only after release creation
fails; never probe preemptively. The audit log is not used because it may require organization access.

## Finding catalog

Rule | Current type | Proposed finding
--- | --- | ---
`immutable_releases_enabled` | `immutable_releases_disabled` | `immutable_releases_disabled`
`release_should_be_immutable` | `non_immutable_release` | `release_not_immutable`
`release_should_be_published` | `draft_release` | `release_is_draft`
`patch_release_required` | `missing_release` | `release_missing`, new `patch_tag_locked_by_deleted_release`
`highest_patch_release_should_be_latest` | `wrong_latest_release` | `latest_release_wrong`
`duplicate_release` | `duplicate_release` | `release_duplicate`
`floating_version_no_release` | `immutable_floating_release`, `mutable_floating_release` | same two, configurable separately
`duplicate_patch_version_ref` | `duplicate_patch_ref` | `patch_ref_duplicate`
`duplicate_floating_version_ref` | `duplicate_ref` | `floating_tag_and_branch_both_exist`
`duplicate_latest_ref` | `duplicate_latest_ref` | `latest_tag_and_branch_both_exist`
`branch_should_be_tag` | `wrong_ref_type` | `floating_ref_should_be_tag`
`tag_should_be_branch` | `wrong_ref_type` | `floating_ref_should_be_branch`
`patch_tag_missing` | `missing_patch_version` | `patch_tag_missing`
`major_tag_missing` / `major_branch_missing` | `missing_major_version` | `major_tag_missing` / `major_branch_missing`
`minor_tag_missing` / `minor_branch_missing` | `missing_minor_version` | `minor_tag_missing` / `minor_branch_missing`
`major_tag_tracks_highest_patch` | `incorrect_version` | `major_tag_wrong_target`
`major_branch_tracks_highest_patch` | `incorrect_version` | `major_branch_wrong_target`
`minor_tag_tracks_highest_patch` | `incorrect_minor_version` | `minor_tag_wrong_target`
`minor_branch_tracks_highest_patch` | `incorrect_minor_version` | `minor_branch_wrong_target`
`latest_tag_tracks_global_highest` | `incorrect_latest_tag` | `latest_tag_wrong_target`
`latest_branch_tracks_global_highest` | `incorrect_latest_branch` | `latest_branch_wrong_target`
`action_metadata_required` | `missing_marketplace_metadata` | `marketplace_metadata_missing`
`marketplace_publication_required` | `marketplace_not_published` | `marketplace_not_published`

New: `patch_tag_locked_by_deleted_release` under `patch_release_required`. Detected only after a 422 on release
creation; its fix deletes the tag and requires `permanently-delete-immutable-tags: dangerously-allow` or `--dangerously-allow-destructive-tag-deletion`.

## Options

### Option A: keep rules, make findings explicit

Each rule declares a closed list of finding IDs and summaries; severity and auto-fix are configured by concern, rule or finding.

- Docs: each rule page gets a findings table.
- Config: `rules.<rule>.findings.<id>.level` and `.fix` (`auto` or `manual`), in addition to concern and rule level.
- Tests: every finding ID is unique and belongs to exactly one rule; the concern/rule test is extended.
- Pros: small change, no rename of rules or concerns. Cons: two levels of config (rule, finding).

### Option B: split rules until 1 rule = 1 finding

- Pros: the existing rule-level config and docs stay accurate, rule and finding are the same thing.
- Cons: more rules and pages (for example `floating_version_no_release` becomes two), and rule names change.

### Option C: do nothing structurally, only document

Add the findings table to each rule page and keep config at rule level.

- Pros: no code change. Cons: findings still cannot be tuned individually and duplicated types remain ambiguous.

## Rules for any option

- Finding IDs are unique across rules and each belongs to exactly one rule.
- Finding IDs are renamed without aliases; the issue type strings are not yet a public compatibility contract.
- `action-version.yml` keeps rejecting unknown keys so typos do not silently weaken the policy.

## Decisions

- **Finding model:** Option A. Keep the existing rules and declare their findings explicitly.
- **Finding identity:** Every issue carries its owning rule and finding; JSON emits `rule` and `finding`, with no legacy `type`.
- **Configuration:** Finding overrides win over rule settings, which win over concern settings. Unknown finding IDs are errors.
- **Burned tags:** Detect only after release creation returns the matching HTTP 422. Deleting the tag requires `permanently-delete-immutable-tags: dangerously-allow` or `--dangerously-allow-destructive-tag-deletion`.
- **Audit log:** No audit-log lookup.
- **Compatibility:** Finding IDs are renamed without legacy aliases.
