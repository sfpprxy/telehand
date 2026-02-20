# Release Notes Files

This repository uses `body_path` in GitHub Actions release workflow.

For every release tag, create a markdown file at:

- `.github/release-notes/<tag>.md`

Example:

- Tag `v0.1.2` -> `.github/release-notes/v0.1.2.md`

The workflow fails if the file is missing or empty.

## Writing style

- Summarize user-visible changes in plain language.
- Focus on outcomes and behavior changes.
- Avoid dumping raw commit logs.
- Keep it concise (typically 4-10 bullets).
