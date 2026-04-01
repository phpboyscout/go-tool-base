---
description: Release preparation workflow for GTB
---
1. **Pre-release checks**:
   - Confirm the current branch is `main` and is clean (`git status`).
   - Run `just ci` to execute the full local CI suite before proceeding.
2. **Review pending changes**:
   - Run `git log --oneline $(git describe --tags --abbrev=0)..HEAD` to list commits since the last release.
   - Verify all commits follow the Conventional Commits format (`feat:`, `fix:`, `refactor:`, `chore:`, etc.) — semantic-release uses these to determine the version bump.
   - Flag any commits that are missing a type prefix or use an incorrect type.
   - Flag any commits that contain AI attribution (`Co-Authored-By:` trailers naming an AI, or references to AI tools in the message body) — these must be amended before release. Commits are the sole responsibility of the developer who created them.
3. **Determine version bump**:
   - `feat:` commits → minor bump
   - `fix:` / `perf:` / `refactor:` commits → patch bump
   - Any commit with `BREAKING CHANGE:` in the footer → major bump
   - `ci:`, `chore:`, `style:`, `docs:`, `test:` commits → no release triggered
   - Confirm the expected bump is appropriate for the changes included.
   - Flag any commits that use a non-application type (e.g. `ci:`, `chore:`) for changes that actually affect library or CLI behaviour — these must be retyped before release.
   - **API Stability (v1.10.0+):** If a `BREAKING CHANGE:` footer is present, verify the justification is sound and a migration guide exists in `docs/migration/`. Breaking changes to Stable/Beta `pkg/` APIs trigger a major bump — confirm this is intentional and unavoidable. Run `apidiff` against the previous release tag to detect unintended breakage.
4. **Validate goreleaser config**:
   - Run `goreleaser check` to validate `.goreleaser.yaml`.
   - Run `just snapshot` to build a local snapshot and verify binaries compile cleanly:
     ```bash
     just snapshot
     ```
   - Check the output in `dist/` for expected platforms and binary names.
5. **Review documentation**:
   - Verify `docs/` is up to date with all changes included in this release.
   - Check that any new components or commands are documented.
6. **Tag and release**:
   - Do not manually tag — semantic-release handles versioning automatically via CI on merge to `main`.
   - Confirm the `semantic-release.yaml` CI workflow is enabled and passing.
   - Clean up snapshot artefacts:
     ```bash
     just cleanup
     ```
