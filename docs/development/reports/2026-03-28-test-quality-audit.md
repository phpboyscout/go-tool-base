# Test Quality Audit Report

**Date:** 2026-03-28
**Scope:** All unit tests across `pkg/` and `internal/`
**Method:** Automated linting + manual review of every test file against its source

---

## Executive Summary

Reviewed **~100 test files** containing **~24,000 lines** of test code across 7 package groups. The codebase demonstrates strong fundamentals — table-driven tests, `t.Parallel()`, proper mock injection via mockery, and good use of afero for filesystem abstraction.

However, the audit identified **188 lint issues** (currently suppressed by `tests: false`), **~30 false-positive/trivial tests**, **~15 duplicative test groups**, and **~40 meaningful coverage gaps**. The most systemic issues are: trivial getter/constructor tests that don't validate behaviour, brittle string-matching assertions on log output, and simulation-based tests that re-implement production logic rather than testing it.

---

## Part 1: Test Quality Findings

### 1.1 False Positives & Trivial Tests

Tests that pass trivially without exercising business logic. These inflate test counts and coverage without catching regressions.

| Location | Test | Issue |
|----------|------|-------|
| `pkg/props/interfaces_test.go:27-64` | `TestGet*_ReturnsField` (7 tests) | Each creates Props, calls getter, asserts same value returned. Tests Go struct access, not logic. |
| `pkg/config/container_test.go:84-121` | `TestContainer_Get` subtests | 7 subtests testing Viper type getters (`GetString`, `GetBool`, `GetInt`, etc.). Tests stdlib delegation. |
| `pkg/config/config_test.go:28-32` | `TestNewContainerFromViper` | Creates container with nil viper, asserts non-nil. No behaviour tested. |
| `pkg/cmd/update/update_test.go:19-39` | `TestNewCmdUpdate` | Asserts Cobra flag defaults. Tests framework, not business logic. |
| `pkg/cmd/update/update_test.go:256-269` | `TestNewCmdUpdate_FromFileFlag` | Same — tests flag default is empty string. |
| `pkg/cmd/version/version_test.go:142-166` | `TestVersionInfo_JSONOutput` | Tests JSON marshalling of a struct. stdlib behaviour. |
| `pkg/version/version_test.go:9-32` | `TestNewInfo` | Tests that `GetVersion()`, `GetCommit()` return constructor args. |
| `pkg/setup/offline_test.go:139-150` | `TestNewOfflineUpdater` | Asserts constructor sets fields. |
| `pkg/setup/github/github_test.go:195-215` | `TestNewGitHubInitialiser_NilAssets`, `TestNewCmdInitGitHub_Wiring` | Constructor returns non-nil; `cmd.Use == "github"`. |
| `pkg/setup/ai/ai_test.go:373-391` | `TestNewAIInitialiser_WithAssets/NilAssets` | Same pattern — constructor + Name() assertion. |
| `pkg/setup/update_test.go:459-501` | `TestIsDevelopmentVersion` | Tests `return s.CurrentVersion == "v0.0.0"`. Single equality check. |
| `pkg/http/tls_test.go:10-30` | `TestDefaultTLSConfig_*`, `TestNewServer_NoPreferServerCipherSuites` | Tests boolean field is false; tests two calls return same config. |
| `pkg/http/client_test.go:15-58` | `TestNewClient_*` (5 tests) | Tests that option functions set struct fields. |
| `pkg/output/output_test.go:103-109` | `TestStatusConstants` | Tests that string constants equal expected values. |
| `pkg/forms/forms_test.go:173-186` | `TestNewNavigable_ReturnsForm`, `TestWizard_Group_ChainReturnsSelf` | Non-nil check; object identity check. |
| `pkg/changelog/format_test.go:9-14` | `TestFormatSummary_EmptyChangelog` | Empty input returns empty string. |
| `pkg/changelog/parse_test.go:20-25` | `TestParse_WhitespaceOnly` | Whitespace input returns empty releases. |
| `pkg/errorhandling/handling_test.go:73-86` | `TestNew_WithOptions` | Tests option pattern sets exit function. |
| `pkg/config/validate_test.go:232-238` | `TestValidationResult_ValidEmpty` | Empty result returns `Valid() == true`. |
| `internal/generator/commands_unit_test.go:95` | `TestConvertManifestFlagsToTemplate` | Tests struct-to-struct field mapping. |
| `internal/generator/config_unit_test.go:16-100` | `TestResolveProvider`, `TestResolveToken` | Tests config key lookups via Viper. |

**Recommendation:** Remove or consolidate ~25 of these into meaningful behavioural tests. Keep only where they serve as regression guards for non-obvious wiring.

---

### 1.2 Duplicative Tests

| Files | Overlap | Recommendation |
|-------|---------|----------------|
| `pkg/cmd/root/root_test.go` + `root_coverage_test.go` | `TestNewCmdRoot` and `TestCheckForUpdates` are **identical** in both files. | Delete from `root_coverage_test.go`. |
| `pkg/vcs/repo/repo_unit_test.go` + `repo_test.go` + `repo_coverage_test.go` | `Commit()`, `FileExists()`, `DirectoryExists()` tested in multiple files with overlapping paths. | Consolidate into single test file per concern. |
| `pkg/setup/checksum_test.go` | `TestVerifyChecksum_ValidHash` + `TestVerifyChecksum_ValidHash_UpperCase` test the same path (code uses `EqualFold`). | Combine into table-driven test. |
| `pkg/config/config_test.go:97-158` | `TestNewFilesContainer` + `TestNewReaderContainer` both test multi-file merge override behaviour. | Consolidate or differentiate. |
| `pkg/config/validate_test.go:49-89` | `TestValidate_RequiredFieldMissing` + `TestValidate_RequiredFieldEmpty` test same validation rule. | Parametrise. |
| `pkg/version/version_test.go` | `TestInfo_Compare` + `TestCompareVersions` duplicate version comparison logic. | Remove one. |
| `pkg/errorhandling/help_test.go` | `TestSlackHelp_SupportMessage` + `TestTeamsHelp_SupportMessage` are structurally identical. | Parametrise with message template. |
| `pkg/controls/controls_test.go` | Stop/state transition tested in `TestController_Controls` subtests AND dedicated `TestStop_ConcurrentCalls`, `TestStop_AlreadyStopped`. | Consolidate overlapping paths. |
| `internal/generator/docs_unit_test.go` + `docs_test.go` | Both test doc generation with mocked AI clients, similar setup. | Differentiate or merge. |
| `internal/cmd/generate/command_test.go:105-106` | Two identical assertions on same line (`"NewCmdTestCmd"`). | Remove duplicate assertion. |

---

### 1.3 Significant Coverage Gaps

#### Critical (Business Logic Not Tested)

| Location | Gap |
|----------|-----|
| `pkg/setup/update_test.go:26-134` | `TestGetReleaseNotes` uses `simulateGetReleaseNotes()` — a **re-implementation** of the real function. The actual `GetReleaseNotes()` method is never tested. Comment at line 120 acknowledges this. |
| `pkg/chat/` (all providers) | **ReAct max-steps limit never tested to fail.** `DefaultMaxSteps = 20` exists but no test verifies behaviour when exceeded. |
| `pkg/chat/claude_test.go` | No test for multiple tool calls in a single response turn. |
| `pkg/chat/openai_test.go` | `Chat()` method missing non-tool response path. `chunkByTokens()` tokenization logic untested at unit level. |
| `pkg/chat/gemini_test.go:163` | `empty_question` subtest is **mislabelled** — sends `"test"`, not an empty string. Real empty-input validation untested for Ask. |
| `pkg/chat/gemini_test.go` | History preservation across `Add()` + `Chat()` not verified. |
| `pkg/setup/ai/ai_test.go` | No test for form cancellation errors (source handles `Run()` errors at lines 291, 301). |
| `pkg/setup/update_coverage_test.go` | No timeout behaviour test despite 3-minute timeout in source (line 48). |
| `pkg/cmd/root/root_test.go` | `TestCheckForUpdates` only tests "user declines" path. No test for "user accepts update". |
| `pkg/cmd/initialise/init_test.go` | Flags `--skip-login`, `--skip-key`, `--clean` never tested. |
| `internal/generator/` | No tests for malformed Go code, missing imports, or AST parsing errors. |
| `internal/generator/hash_test.go` | No test for interactive conflict resolution (always uses `GTB_NON_INTERACTIVE=true`). |
| `internal/generator/manifest_test.go:85` | `TestRemoveFromManifest_Missing` tests error case only — no positive test for successful removal. |

#### Moderate

| Location | Gap |
|----------|-----|
| `pkg/chat/client_test.go:68` | `ProviderClaudeLocal binary not found` test relies on Claude binary not being in PATH. Environment-dependent fragility. |
| `pkg/chat/` (all providers) | Mock server handlers never inspect request bodies to verify parameters actually sent. |
| `pkg/vcs/github/client_test.go` | No unit tests for error cases (404, malformed responses) — only integration tests. |
| `pkg/docs/docs_test.go` | Missing tests for map entries, nested nav structures, deeply nested directories. |
| `pkg/output/output_test.go:113-134` | `TestRenderMarkdown_*` only checks output is non-empty, not actual rendering. |
| `pkg/grpc/chain_test.go:111-133` | `TestInterceptorChain_MultipleInterceptors_Ordering` doesn't verify actual execution order. |
| `pkg/setup/checksum_test.go` | Missing edge cases: multiple whitespace separators, trailing whitespace in hash. |

---

### 1.4 Code Smells in Tests

#### Simulation Instead of Testing

| Location | Issue |
|----------|-------|
| `pkg/setup/update_test.go:138` | `simulateGetReleaseNotes()` re-implements production logic. If the real function diverges, tests still pass. Cyclomatic complexity of the simulation itself is 13 (flagged by linter). |

#### Brittle String Assertions on Log Output

| Location | Assertion |
|----------|-----------|
| `pkg/setup/init_test.go:105` | `assert.Contains(t, buf.String(), "API keys")` |
| `pkg/setup/middleware_builtin_test.go:36-38` | `assert.Contains(t, out, "command completed")`, `"duration="` |
| `pkg/errorhandling/handling_test.go:43` | `assert.Contains(output, "WARN")` |
| `pkg/errorhandling/handling_debug_test.go:35` | `assert.NotContains(output, "stacktrace=")` |

#### Tests Without Meaningful Assertions

| Location | Issue |
|----------|-------|
| `pkg/cmd/update/update_test.go:133-145` | `TestUpdateConfig` "handles_init_error" subtest calls `UpdateConfig` but never asserts anything. |
| `pkg/cmd/doctor/doctor_test.go:121-142` | `TestRunChecks` asserts `report.Checks` is not empty but doesn't verify which checks ran. |
| `pkg/chat/openai_test.go:167-177` | `TestOpenAIProvider_Add` "chunking" subtest calls `Add()` but doesn't verify chunking occurred. |
| `internal/generator/docs_unit_test.go:158-192` | Tool handler tests verify mock was called, not actual handler logic. |

#### Hardcoded Paths / Environment Coupling

| Location | Issue |
|----------|-------|
| `pkg/vcs/repo/repo_unit_test.go:26` | `testRepo = "/home/mcockayne/workspace/phpboyscout/gtb"` — hardcoded developer path. |
| `pkg/cmd/initialise/init_test.go:72` | `t.Setenv("HOME", "/tmp/home")` — couples to internal path logic. |

#### Resource Leaks in Tests

| Location | Issue |
|----------|-------|
| `pkg/props/assets_test.go` | Multiple tests open files (`Open()`, `OpenMergedCSV()`) but never call `Close()`. Pattern modelled 6+ times. |

#### Missing `t.Helper()` / `t.Parallel()`

| Location | Issue |
|----------|-------|
| `pkg/setup/offline_test.go:17` | `setupOfflineUpdater` helper missing `t.Helper()`. |
| `pkg/changelog/format_test.go` (all tests) | None use `t.Parallel()`. |
| `pkg/changelog/archive_test.go` (all tests) | None use `t.Parallel()`. |
| `pkg/chat/claude_local_test.go:235` | `with_optional_args` subtest missing `t.Parallel()`. |

#### Global State Without Cleanup

| Location | Issue |
|----------|-------|
| `pkg/setup/middleware_builtin_test.go:97` | `viper.Reset()` called without `t.Cleanup()`. Repeated in every subtest. |
| `pkg/chat/gemini_test.go:56` | `ExportGenaiNewClient` mock not restored in subtest scope. |

---

## Part 2: Test Linting Analysis

Currently tests are excluded from linting (`tests: false` in `.golangci.yaml`). Running with `--tests` produces **188 issues**. Here's the breakdown by category with recommendations on which to enforce:

### Category Breakdown

| Linter | Count | Severity | Recommendation |
|--------|-------|----------|----------------|
| **wsl_v5** | 47 | Cosmetic | **Suppress in tests.** Whitespace rules add noise without value in test code. |
| **errcheck** | 29 | Moderate | **Enforce selectively.** Fix `json.Encode`, `yaml.Unmarshal`, `Close()` calls. Exempt `fmt.Fprint` in test handlers. |
| **err113** | 23 | Low | **Suppress in tests.** Dynamic errors (`fmt.Errorf("test error")`) are idiomatic in test fixtures. Requiring sentinel errors in tests adds boilerplate without benefit. |
| **testifylint** | 21 | Low | **Enforce.** These are easy wins: `require.Error` instead of `assert.Error` for fatal checks, `assert.Contains` instead of manual string checks, `assert.InEpsilon` for floats. |
| **noctx** | 14 | Low | **Suppress in tests.** Using `http.Get()` and `httptest.NewRequest()` without context is standard in test code. |
| **gosec** | 13 | Mixed | **Enforce G602** (slice bounds — in production code `flag.go`). **Suppress G102** (bind all interfaces), **G112** (Slowloris), **G306** (file perms) in tests. |
| **nlreturn** | 6 | Cosmetic | **Suppress in tests.** |
| **staticcheck** | 5 | Low | **Fix SA1019** (deprecated `grpc.WithBlock`) — use `grpc.NewClient`. Fix **QF1011** (type inference) — trivial. |
| **whitespace** | 4 | Cosmetic | **Suppress in tests.** |
| **prealloc** | 3 | Low | **Suppress in tests.** Slice preallocation in tests is premature optimisation. |
| **unparam** | 4 | Low | **Suppress in tests.** Test helpers with constant params are common and intentional. |
| **godot** | 3 | Cosmetic | **Suppress in tests.** Comment punctuation in tests is noise. |
| **gofmt/goimports** | 3 | Low | **Enforce.** Auto-fixable. |
| **cyclop** | 1 | High | **Fix.** `simulateGetReleaseNotes` (complexity 13) should be deleted — it re-implements production code. |
| **bodyclose** | 1 | Moderate | **Fix.** `update_coverage_test.go:129` — `resp.Body` not closed. |
| **gocritic** | 1 | Low | **Fix.** `update_test.go:71` — replace `else { if` with `else if`. |
| **exhaustive** | 1 | Low | **Fix.** `root_test.go:547` — missing `FatalLevel` case in switch. |
| **unused** | 1 | Low | **Fix.** `repo_test.go:27` — unused `testBranch` variable. |

### Recommended Lint Configuration for Tests

```yaml
# .golangci.yaml (proposed changes)
run:
  tests: true  # Enable test linting

linters:
  # ... existing config ...
  exclusions:
    rules:
      # Suppress in test files only
      - path: _test\.go
        linters:
          - wsl_v5
          - nlreturn
          - whitespace
          - godot
          - err113
          - noctx
          - prealloc
          - unparam
          - mnd
      # Suppress specific gosec rules in tests
      - path: _test\.go
        text: "G102|G112|G306"
        linters:
          - gosec
```

This would reduce the 188 issues to approximately **35 actionable items** (errcheck, testifylint, staticcheck, gofmt, bodyclose, cyclop, gocritic, exhaustive, unused) — all of which should be fixed.

---

## Part 3: Godog & Integration Testing

### Current Integration Test State

At the time of this audit, only `pkg/controls/` had integration tests (2 files, 4 test functions). Since then, integration tests have been added across 8 packages and the gating mechanism has been standardised to environment variables via `testutil.SkipIfNotIntegration()`. See the [Integration Testing](../integration-testing.md) guide for current state.

### Godog Suitability Assessment

| Package | Godog Fit | Rationale |
|---------|-----------|-----------|
| `pkg/controls` | **Strong** | Complex state machine (Unknown -> Running -> Stopping -> Stopped), multi-step shutdown sequences. Feature files improve readability for ops teams. |
| CLI E2E (`test/e2e/`) | **Strong** | User-facing workflows (`gtb generate project`, `gtb update --from-file`). Non-developers can write/review scenarios. |
| `pkg/setup/github` | **Moderate** | Sequential wizard flow (discover -> generate -> upload). Clear user-facing workflow. |
| `pkg/chat` | **Low** | Current httptest mock pattern is already effective. BDD adds overhead without clarity. |
| `pkg/forms` | **No** | Bubble Tea/Huh testing requires simulated keyboard events — can't express in Gherkin. |
| `internal/generator` | **No** | AST manipulation too complex for feature files. Table-driven tests are the right fit. |
| `pkg/config` | **No** | afero MemMapFs is already ergonomic. Godog adds no clarity. |

**Verdict:** Use Godog strategically for `pkg/controls` and future CLI E2E tests. Keep table-driven Go tests as the baseline everywhere else.

### Integration Test Gaps (Priority Order)

#### Phase 1 — High Impact (Near Term)

1. **`pkg/cmd/root` — E2E command orchestration**
   - Config loading precedence (file -> env -> flag)
   - Feature flag resolution with real config
   - Update check doesn't block command execution

2. **`pkg/controls` — Expand existing suite**
   - TLS/mTLS handshake test
   - Shutdown under load (100+ concurrent connections)
   - Health check endpoint resilience

3. **Create `pkg/testing/` helper package**
   - Move `freePort()`, `syncBuffer` from controls tests
   - Add `TestLogger()`, `TestConfig()` factories
   - Shared `testdata/` fixtures

#### Phase 2 — VCS Integration (Next Quarter)

4. **`pkg/vcs/repo` — Real git operations**
   - Clone from public test repo
   - Branch creation, tracking, checkout

5. **`pkg/vcs/github` — Read-only API tests**
   - List releases with real pagination
   - Download asset and verify checksum

#### Phase 3 — E2E CLI (Future)

6. **`test/e2e/` — Full CLI invocation tests**
   - `gtb generate project` -> compile -> run
   - `gtb update --from-file` end-to-end
   - Config init with real filesystem
   - Godog feature files for user workflows

### Recommended Test Infrastructure Improvements

| Need | Current State | Recommendation |
|------|---------------|----------------|
| Shared helpers | Each package defines its own (`freePort`, `syncBuffer`) | Create `pkg/testing/` with reusable utilities |
| Test fixtures | Only `pkg/changelog/testdata/` exists | Add `testdata/` for config, VCS, setup packages |
| Integration harness | Ad-hoc per-package | Standardised with `testutil.SkipIfNotIntegration()` + `INT_TEST`/`INT_TEST_<TAG>` env vars |
| CI integration secrets | None | Add read-only GitHub token for VCS integration tests |

---

## Part 4: Summary of Recommendations

### Immediate Actions (This Sprint)

1. **Delete duplicate tests** in `root_coverage_test.go` (2 tests identical to `root_test.go`)
2. **Delete `simulateGetReleaseNotes`** — refactor `SelfUpdater` for proper DI instead
3. **Fix 35 actionable lint issues** and enable `tests: true` with exclusion rules
4. **Add `t.Helper()`** to test helpers missing it
5. **Close file handles** in `assets_test.go` tests (6 instances)

### Short Term (Next 2 Sprints)

6. **Remove ~25 trivial getter/constructor tests** or merge into behavioural tests
7. **Add ReAct max-steps tests** across all chat providers
8. **Add form cancellation tests** in `pkg/setup/ai`
9. **Fix mislabelled tests** (`gemini_test.go:163` "empty_question", `doctor_test.go` "Correctness")
10. **Replace brittle log-string assertions** with behavioural checks

### Medium Term (Next Quarter)

11. **Enable test linting in CI** with the proposed exclusion config
12. **Create `pkg/testing/` helper package** for integration test infrastructure
13. **Implement Phase 1 integration tests** (root command E2E, controls expansion)
14. **Evaluate Godog** for `pkg/controls` feature files
15. **Consolidate duplicative test files** in `pkg/vcs/repo`

---

## Appendix: Issue Counts by Package

| Package | Trivial | Duplicate | Coverage Gap | Code Smell | Lint Issues |
|---------|---------|-----------|-------------|------------|-------------|
| `pkg/cmd/` | 4 | 4 (root) | 5 | 3 | 18 |
| `pkg/setup/` | 4 | 1 | 6 | 8 | 22 |
| `pkg/chat/` | 3 | 1 | 8 | 5 | 8 |
| `pkg/config/` | 5 | 2 | 2 | 1 | 12 |
| `pkg/props/` | 7 | 1 | 0 | 2 | 0 |
| `pkg/version/` | 1 | 1 | 0 | 0 | 0 |
| `pkg/changelog/` | 2 | 0 | 1 | 1 | 0 |
| `pkg/errorhandling/` | 1 | 1 | 1 | 2 | 4 |
| `pkg/vcs/` | 0 | 3 | 3 | 4 | 14 |
| `pkg/controls/` | 0 | 2 | 2 | 1 | 16 |
| `pkg/http/` | 5 | 0 | 2 | 0 | 22 |
| `pkg/grpc/` | 0 | 0 | 1 | 1 | 10 |
| `pkg/output/` | 1 | 1 | 1 | 0 | 6 |
| `pkg/forms/` | 2 | 0 | 2 | 0 | 0 |
| `pkg/docs/` | 0 | 0 | 2 | 0 | 2 |
| `internal/` | 5 | 2 | 8 | 5 | 16 |
| **Totals** | **~40** | **~19** | **~44** | **~33** | **188** |
