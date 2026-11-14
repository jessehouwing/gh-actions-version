# Rules

One page per validation rule. Levels and auto-fix are configured per concern or rule in [action-version.yml](../../internal/policy/action-version.yml) or with flags; see the [README](../../README.md).

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

Every rule page lists the flags and keys that affect it. In general: command-line flag > `rules.<rule>` > `concerns.<concern>` in `action-version.yml` > built-in default. Only the minor, releases, immutability and marketplace concerns have a `--check-*` flag; the others are configured in the file.
