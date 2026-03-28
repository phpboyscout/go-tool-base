---
title: "Test Coverage Follow-Up Specification"
description: "Address remaining test coverage gaps across packages to meet project coverage targets. Original six packages expanded to seventeen after Gemini partially implemented the spec."
date: 2026-03-24
status: IMPLEMENTED
tags:
  - specification
  - testing
  - coverage
  - quality
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-opus-4-6)
    role: AI drafting assistant
---

# Test Coverage Follow-Up Specification

Authors
:   Matt Cockayne, Claude (claude-opus-4-6) *(AI drafting assistant)*

Date
:   24 March 2026

Status
:   IMPLEMENTED

---

## Overview

The initial test coverage gaps specification (2026-03-21, IMPLEMENTED) addressed several `pkg/` packages that were below target. A subsequent review identified six additional packages that remained below their coverage thresholds. Gemini partially implemented this spec, completing `pkg/cmd/update` and `pkg/chat` but leaving the remaining four original packages untouched.

A further review (2026-03-25) expanded the scope to cover additional packages that have fallen below acceptable thresholds since the spec was written. Several packages contain code that is inherently difficult to test automatically — targets for those are set conservatively with explicit exclusion lists.

---

## Coverage Targets

| Package | Baseline (spec) | Gemini result | Current | **Target** | Notes |
|---------|-----------------|---------------|---------|------------|-------|
| `pkg/cmd/update` | 7.4% | 91.1% | 91.1% | ✅ **DONE** | |
| `pkg/chat` | 26.6% | 90.3% | 90.3% | ✅ **DONE** | |
| `pkg/vcs/repo` | 13.5% | — | 57.6% | **70%** | go-git/billyfs coupling; network ops excluded |
| `pkg/vcs/github` | 36.8% | — | 36.8% | **70%** | httptest pattern established; OAuth excluded |
| `pkg/vcs/gitlab` | *(added)* | — | 62.9% | **80%** | Good patterns; edge cases only |
| `internal/cmd/generate` | 7.4% | — | 7.3% | **35%** | ~70% is huh forms; pure logic only |
| `pkg/docs` | 15.1% | — | 15.1% | **40%** | `tui.go` (1079 lines) excluded entirely |
| `pkg/setup/ai` | *(added)* | — | 43.7% | **70%** | Form builders excluded |
| `pkg/setup/github` | *(added)* | — | 38.7% | **65%** | SSH gen/TUI excluded; mocks available |
| `pkg/props` | *(added)* | — | 58.1% | **80%** | Data transforms mostly testable |
| `pkg/config` | *(added)* | — | 59.8% | **70%** | File-watcher (fsnotify) excluded |
| `pkg/cmd/root` | *(added)* | — | 59.4% | **70%** | `newRootPreRunE` orchestration excluded |
| `pkg/setup` | *(added)* | — | 70.5% | **80%** | |
| `pkg/forms` | *(added)* | — | 67.9% | **80%** | |
| `pkg/utils` | *(added)* | — | 64.3% | **80%** | |
| `pkg/cmd/initialise` | *(added)* | — | 75.0% | **85%** | |
| `pkg/errorhandling` | *(added)* | — | 78.7% | **85%** | |
| `internal/agent` | *(added)* | — | 59.0% | **70%** | *(deferred — needs exploratory pass)* |

---

## Coverage Exclusions

The following code categories are explicitly out of scope for automated test coverage. Attempting to test these would produce brittle, high-maintenance tests with little safety-net value.

| Category | Location | Reason |
|----------|----------|--------|
| Bubble Tea TUI model | `pkg/docs/tui.go` (1079 lines) | Requires simulated keyboard events and render-output snapshot assertions |
| `charmbracelet/huh` form builders | `pkg/setup/ai`, `pkg/setup/github`, `internal/cmd/generate` | Form lifecycle exercised by integration/manual testing |
| `pkg/cmd/root/newRootPreRunE` | `pkg/cmd/root/root.go` | Orchestrates filesystem, config, logging, forms, and network; integration-level only |
| SSH key generation with passphrase TUI | `pkg/vcs/repo/GetSSHKey` | Interactive `huh.Input` passphrase prompt |
| OAuth / GitHub CLI login flow | `pkg/vcs/github/login.go` | External OAuth device-flow; no mock boundary |
| File-system watcher | `pkg/config` `watchConfig`/`WatchConfig` | fsnotify callback; requires real filesystem event |
| VCS network operations | `pkg/vcs/repo.OpenInMemory`, `Clone` with remote URLs | Require real network; covered by integration tests |

---

## Design Decisions

**Black-box testing (`package_test`)**: All new tests use external test packages to validate the public surface only.

**Table-driven tests with `t.Parallel()`**: Every test function runs subtests in parallel where safe.

**Afero `NewMemMapFs()` for filesystem**: All filesystem interactions use in-memory filesystem. Real filesystem only where OS-level operations (symlinks, exec) require it.

**Mockery-generated mocks**: All interface dependencies use mockery/v3-generated mocks. Hand-rolled mocks not permitted for interfaces with generated counterparts.

**`t.Cleanup()` over `defer`**: Resource cleanup uses `t.Cleanup()` to ensure cleanup runs even when subtests fail.

**Existing `httptest` mock-server pattern**: `pkg/vcs/github` already uses `httptest.NewServer` in `client_coverage_test.go` — extend this pattern, do not introduce new testing frameworks.

---

## Public API Changes

None. This spec adds tests only.

---

## Implementation Phases

### Phase 1 (complete) — `pkg/cmd/update`, `pkg/chat`

Completed by Gemini. Both packages now at ≥90%.

### Phase 2 — Easy wins: pure logic functions

Packages: `pkg/setup/ai`, `pkg/vcs/gitlab`, `pkg/utils`

- `pkg/setup/ai`: `maskKey()`, `providerEnvVar()`, `isValidProvider()` — table-driven unit tests
- `pkg/vcs/gitlab`: network-error edge cases in `DownloadReleaseAsset` using existing `httptest` pattern
- `pkg/utils`: per-function unit tests for any currently uncovered utilities

### Phase 3 — VCS packages: extend existing mock infrastructure

Package: `pkg/vcs/github`

- `release.go` accessor method unit tests (no server needed)
- `NewReleaseProvider()`, `GetLatestRelease()`, `GetReleaseByTag()`, `ListReleases()` via mock HTTP server
- `GetReleaseAssets()`, `GetReleaseAssetID()`, error paths

### Phase 4 — VCS repo: local git operations

Package: `pkg/vcs/repo`

- Extend `repo_unit_test.go` pattern (real `t.TempDir()` + `git.PlainInit`, no network)
- Cover: `CreateBranch`, `WalkTree`, `FileExists`, `DirectoryExists`, `GetFile`, `AddToFS`, getter/setter methods
- Excluded: `OpenInMemory`, `Clone` (network), `GetSSHKey` passphrase path

### Phase 5 — Setup packages

Packages: `pkg/setup/github`, `pkg/setup/ai` (deeper pass)

- SSH key discovery with `afero.MemMapFs`
- `generateKey()` edge cases
- Mock `MockGitHubClient` for `UploadKey` paths
- `Configure()` with `mockConfig` for key-present/absent/invalid-provider paths

### Phase 6 — Data and config

Packages: `pkg/props`, `pkg/config`

- `pkg/props`: `formatFlatKV`, `parseFlatKV`, `openMergedStructured`, `unmarshalStructuredData`, `marshalStructuredData`, `openMergedCSV`, `Mount`, `For`, `Merge`, `Exists`
- `pkg/config`: error paths in `LoadFilesContainer`, `NewFilesContainer`, merging precedence

### Phase 7 — Command packages

Packages: `pkg/cmd/root`, `pkg/cmd/initialise`, `pkg/forms`, `pkg/errorhandling`

- `pkg/cmd/root`: `configureLogging`, `shouldSkipUpdateCheck`, `extractFlags`, `mergeEmbeddedConfigs` — skip `newRootPreRunE`
- Other packages: gap-closing based on per-file coverage analysis

### Phase 8 — Internal generation (pure logic only)

Package: `internal/cmd/generate`

- `processAliasesInput`, `syncFlagsToOptions`, `syncOptionsToFlags`, `flagsSummary`, `findCommand`, `updateCommandMetadataRecursive`
- Excluded: all `charmbracelet/huh` form builders and wizard orchestration

### Phase 9 — Non-TUI docs coverage

Package: `pkg/docs`

- `AskAI()` with mock `ChatClient`
- `Serve()` binding to `:0`
- Remaining `docs.go` nav-parsing logic
- `tui.go` explicitly excluded with comment in test file

---

## Verification

```bash
just ci          # full suite: tidy, generate, test, test-race, lint
just coverage    # HTML coverage report

# Per-package spot checks
go test -coverprofile=coverage.out ./pkg/vcs/... && go tool cover -func=coverage.out
go test -coverprofile=coverage.out ./pkg/setup/... && go tool cover -func=coverage.out
go test -coverprofile=coverage.out ./internal/cmd/generate/... && go tool cover -func=coverage.out
go test -coverprofile=coverage.out ./pkg/docs/... && go tool cover -func=coverage.out
```

---

## Future Considerations

- **Coverage CI gate**: Enforce per-package coverage thresholds in CI, failing the build if any package drops below its target.
- **Integration test gating**: Standardised on `testutil.SkipIfNotIntegration(t, "tag")` with `INT_TEST`/`INT_TEST_<TAG>` environment variables. See the [Integration Testing](../integration-testing.md) guide.
- **Fuzz testing**: `pkg/vcs/github` response parsing and `pkg/docs` YAML parsing are strong candidates.
- **`internal/agent` and `internal/generator`**: Deferred from this spec — require a dedicated exploratory pass to set realistic targets.
