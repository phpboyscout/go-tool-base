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
3. **Determine version bump**:
   - `feat:` commits → minor bump
   - `fix:` / `perf:` commits → patch bump
   - Any commit with `BREAKING CHANGE:` in the footer → major bump
   - Confirm the expected bump is appropriate for the changes included.
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
