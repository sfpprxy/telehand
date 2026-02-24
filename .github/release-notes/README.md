# Release Notes Files

This repository uses `body_path` in GitHub Actions release workflow.

For every release tag, create a markdown file at:

- `.github/release-notes/<tag>.md`

Example:

- Tag `v0.1.2` -> `.github/release-notes/v0.1.2.md`

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
