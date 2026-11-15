# Rules

One page per validation rule. Levels and auto-fix are configured per concern or rule in [action-version.yml](../../internal/policy/action-version.yml) or with flags; see the [README](../../README.md).

## Findings index

| Finding | Rule | Summary |
|---|---|---|
| `ignore_expired` | [`ignore_entries_valid`](./ignore_entries_valid.md) | An ignore entry has expired |
| `ignore_unused` | [`ignore_entries_valid`](./ignore_entries_valid.md) | An ignore entry matches no version |
| `latest_branch_wrong_target` | [`latest_branch_tracks_global_highest`](./latest_branch_tracks_global_highest.md) | The latest branch points to the wrong patch version |
| `latest_tag_wrong_target` | [`latest_tag_tracks_global_highest`](./latest_tag_tracks_global_highest.md) | The latest tag points to the wrong patch version |
| `marketplace_metadata_missing` | [`action_metadata_required`](./action_metadata_required.md) | Required GitHub Marketplace metadata is missing |
| `marketplace_not_published` | [`marketplace_publication_required`](./marketplace_publication_required.md) | The latest release is not published to GitHub Marketplace |
| `floating_ref_should_be_tag` | [`branch_should_be_tag`](./branch_should_be_tag.md) | A floating version should be a tag |
| `floating_tag_and_branch_both_exist` | [`duplicate_floating_version_ref`](./duplicate_floating_version_ref.md) | A floating version exists as both a tag and a branch |
| `latest_tag_and_branch_both_exist` | [`duplicate_latest_ref`](./duplicate_latest_ref.md) | The latest version exists as both a tag and a branch |
| `patch_ref_duplicate` | [`duplicate_patch_version_ref`](./duplicate_patch_version_ref.md) | A patch version exists as both a tag and a branch |
| `floating_ref_should_be_branch` | [`tag_should_be_branch`](./tag_should_be_branch.md) | A floating version should be a branch |
| `release_duplicate` | [`duplicate_release`](./duplicate_release.md) | A duplicate draft release exists |
| `immutable_floating_release` | [`floating_version_no_release`](./floating_version_no_release.md) | An immutable release exists for a floating version |
| `mutable_floating_release` | [`floating_version_no_release`](./floating_version_no_release.md) | A release exists for a floating version |
| `latest_release_wrong` | [`highest_patch_release_should_be_latest`](./highest_patch_release_should_be_latest.md) | The highest patch release is not marked as latest |
| `immutable_releases_disabled` | [`immutable_releases_enabled`](./immutable_releases_enabled.md) | Release immutability is disabled |
| `release_missing` | [`patch_release_required`](./patch_release_required.md) | A patch version does not have a GitHub Release |
| `patch_tag_locked_by_deleted_release` | [`patch_release_required`](./patch_release_required.md) | A patch tag is locked by a deleted immutable release |
| `release_not_immutable` | [`release_should_be_immutable`](./release_should_be_immutable.md) | A published patch release is not immutable |
| `release_is_draft` | [`release_should_be_published`](./release_should_be_published.md) | A patch release is still a draft |
| `major_branch_missing` | [`major_branch_missing`](./major_branch_missing.md) | A major version branch is missing |
| `major_branch_wrong_target` | [`major_branch_tracks_highest_patch`](./major_branch_tracks_highest_patch.md) | A major version branch points to the wrong patch version |
| `major_tag_missing` | [`major_tag_missing`](./major_tag_missing.md) | A major version tag is missing |
| `major_tag_wrong_target` | [`major_tag_tracks_highest_patch`](./major_tag_tracks_highest_patch.md) | A major version tag points to the wrong patch version |
| `minor_branch_missing` | [`minor_branch_missing`](./minor_branch_missing.md) | A minor version branch is missing |
| `minor_branch_wrong_target` | [`minor_branch_tracks_highest_patch`](./minor_branch_tracks_highest_patch.md) | A minor version branch points to the wrong patch version |
| `minor_tag_missing` | [`minor_tag_missing`](./minor_tag_missing.md) | A minor version tag is missing |
| `minor_tag_wrong_target` | [`minor_tag_tracks_highest_patch`](./minor_tag_tracks_highest_patch.md) | A minor version tag points to the wrong patch version |
| `patch_tag_missing` | [`patch_tag_missing`](./patch_tag_missing.md) | A patch version tag is missing |

## config

- [ignore_entries_valid](./ignore_entries_valid.md)

## latest

- [latest_branch_tracks_global_highest](./latest_branch_tracks_global_highest.md)
- [latest_tag_tracks_global_highest](./latest_tag_tracks_global_highest.md)

## marketplace

- [action_metadata_required](./action_metadata_required.md)
- [marketplace_publication_required](./marketplace_publication_required.md)

## ref_type

- [branch_should_be_tag](./branch_should_be_tag.md)
- [duplicate_floating_version_ref](./duplicate_floating_version_ref.md)
- [duplicate_latest_ref](./duplicate_latest_ref.md)
- [duplicate_patch_version_ref](./duplicate_patch_version_ref.md)
- [tag_should_be_branch](./tag_should_be_branch.md)

## releases

- [duplicate_release](./duplicate_release.md)
- [floating_version_no_release](./floating_version_no_release.md)
- [highest_patch_release_should_be_latest](./highest_patch_release_should_be_latest.md)
- [immutable_releases_enabled](./immutable_releases_enabled.md)
- [patch_release_required](./patch_release_required.md)
- [release_should_be_immutable](./release_should_be_immutable.md)
- [release_should_be_published](./release_should_be_published.md)

## version_tracking

- [major_branch_missing](./major_branch_missing.md)
- [major_branch_tracks_highest_patch](./major_branch_tracks_highest_patch.md)
- [major_tag_missing](./major_tag_missing.md)
- [major_tag_tracks_highest_patch](./major_tag_tracks_highest_patch.md)
- [minor_branch_missing](./minor_branch_missing.md)
- [minor_branch_tracks_highest_patch](./minor_branch_tracks_highest_patch.md)
- [minor_tag_missing](./minor_tag_missing.md)
- [minor_tag_tracks_highest_patch](./minor_tag_tracks_highest_patch.md)
- [patch_tag_missing](./patch_tag_missing.md)


## Configuration precedence

Every rule page lists its findings and configuration keys. Precedence is finding > rule > concern > built-in default; finding overrides use `rules.<rule>.findings.<finding>.level` and `.fix` (`auto` or `manual`). Use `--level CONCERN=LEVEL` for any concern, `--rule-level RULE=LEVEL` for a rule, or `--finding-level FINDING=LEVEL` for a finding.
