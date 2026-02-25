# Telehand Agent Rules

## Version Rule (must follow)

- Every code change must update `telehandVersion` in `version.go`.
- If this is a normal development change, bump pre-release version (for example `0.2.1-alpha.3` -> `0.2.1-alpha.4`).
- If this is an official release, set `telehandVersion` to the release version without `v` prefix (for example tag `v0.2.1` => `telehandVersion = "0.2.1"`).

## Release Checklist (must follow)

- Create release notes file `.github/release-notes/<tag>.md` and keep it non-empty.
- Release notes must include exactly two sections:
  - `## Fixed`
  - `## Changed`
- Run `go test ./...` before tagging release.
- Create and push git tag in `vX.Y.Z` format.
