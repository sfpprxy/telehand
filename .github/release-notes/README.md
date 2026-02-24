# Release Notes Files

This repository uses `body_path` in GitHub Actions release workflow.

For every release tag, create a markdown file at:

- `.github/release-notes/<tag>.md`

Example:

- Tag `v0.1.2` -> `.github/release-notes/v0.1.2.md`
- Tag `v0.2.0-alpha.1` -> `.github/release-notes/v0.2.0-alpha.1.md`

## Pre-release tags

Release workflow keeps the same trigger (`v*`) and automatically marks a release as pre-release when tag contains one of:

- `-alpha`
- `-beta`
- `-rc`

Examples:

- `v0.2.0-alpha.1` -> pre-release
- `v0.2.0` -> normal release

The workflow fails if the file is missing or empty.

## Writing style

Use exactly two sections in every release notes file:

- `## Fixed`
- `## Changed`

Template:

```md
## Fixed

- ...

## Changed

- ...
```

Guidelines:

- `Fixed`: what was repaired, resolved, or corrected.
- `Changed`: what behavior or workflow was intentionally changed.
- Keep wording user-facing and concise.
- Do not dump commit-by-commit details.
