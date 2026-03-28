---
title: "Godog BDD Strategy — Evaluation & Phased Rollout"
description: "Strategic introduction of Godog (Cucumber BDD) for controls lifecycle and CLI E2E testing, with phased rollout and integration into existing test infrastructure."
date: 2026-03-28
status: IMPLEMENTED
tags:
  - specification
  - testing
  - bdd
  - godog
  - e2e
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude Opus 4.6
    role: AI drafting assistant
---

# Godog BDD Strategy — Evaluation & Phased Rollout

Authors
:   Matt Cockayne, Claude Opus 4.6 *(AI drafting assistant)*

Date
:   28 March 2026

Status
:   IMPLEMENTED

---

## Overview

The test quality audit (`docs/development/reports/2026-03-28-test-quality-audit.md`) identified Godog as a strong fit for two areas: `pkg/controls/` (complex state machine lifecycle) and CLI E2E workflows. The audit's Phase 3 explicitly recommends Godog feature files for user workflows. No Godog dependency, `test/e2e/` directory, or binary compilation harness exists yet.

### Motivation

The controls package manages a 4-state FSM (Unknown → Running → Stopping → Stopped) with signal handling, health monitoring, restart policies, and graceful shutdown orchestration. Existing integration tests in `shutdown_test.go` are 316+ lines of imperative goroutine/channel coordination that are difficult to review and maintain. Gherkin feature files make these multi-step scenarios readable to ops teams and reduce test setup boilerplate.

CLI commands like `gtb init`, `gtb update`, and `gtb doctor` represent user-facing workflows that are natural Given/When/Then narratives. Non-Go stakeholders can review and author scenarios. Several critical paths remain untested: `gtb update --from-file`, config merge precedence E2E, and `--skip-login`/`--skip-key`/`--clean` flags.

### Terminology

| Term | Definition |
|------|-----------|
| **Godog** | The official Go implementation of Cucumber, providing Gherkin-based BDD testing. v1+ stable, latest release July 2025. |
| **Feature file** | A `.feature` file written in Gherkin syntax containing scenarios expressed as Given/When/Then steps. |
| **Step definition** | A Go function bound to a Gherkin step pattern that implements the test logic. |
| **Scenario context** | Per-scenario state passed between steps via `context.Context`, preventing state leakage. |

### Design Decisions

1. **Strategic, not universal**: Use Godog only where BDD adds clarity (controls lifecycle, CLI E2E). Keep table-driven Go tests as the baseline everywhere else.
2. **`go test` integration**: Use `godog.TestSuite` with `TestFeatures(t *testing.T)` — runs alongside regular Go tests, no separate CLI runner.
3. **Testify compatibility**: Use `godog.T(ctx)` for testify assertion compatibility within step definitions.
4. **Feature files at project root**: `features/` directory follows Cucumber convention and is discoverable by non-Go developers.
5. **Step definitions in `test/e2e/`**: Keeps BDD test code separate from unit and integration tests.
6. **Build-once-test-many**: CLI E2E tests compile the `gtb` binary once in `TestMain`, reuse across all CLI scenarios.
7. **Env var gating**: Integrates with existing `testutil.SkipIfNotIntegration(t, "e2e")` pattern.

---

## Suitability Assessment

| Package | Godog Fit | Rationale |
|---------|-----------|-----------|
| `pkg/controls/` | **Strong** | 4-state FSM, multi-step shutdown sequences, signal handling — feature files improve readability for ops teams |
| CLI E2E (`test/e2e/`) | **Strong** | User-facing command workflows are natural Given/When/Then; non-Go stakeholders can review |
| `pkg/setup/github` | **Moderate** | Sequential wizard flow — can evaluate in Phase 3 |
| `pkg/chat/` | **Low** | httptest mock pattern is already effective; BDD adds overhead without clarity |
| `pkg/forms/` | **No** | Bubble Tea/Huh testing requires simulated keyboard events — cannot express in Gherkin |
| `internal/generator/` | **No** | AST manipulation too implementation-specific for feature files |
| `pkg/config/` | **No** | afero MemMapFs is already ergonomic; Godog adds no clarity |

---

## Directory Structure

```
go-tool-base/
  cmd/
    e2e/
      main.go                       # Test-only binary with all features enabled
      assets/init/config.yaml       # Minimal embedded config for E2E binary
  features/                          # Gherkin feature files (project root)
    controls/
      lifecycle.feature
      graceful_shutdown.feature
      health_monitoring.feature
    cli/
      version.feature
      doctor.feature
      help.feature
      update.feature
      init.feature
  test/
    e2e/
      steps/
        steps_test.go               # TestFeatures entry point + tag filtering
        controls_steps_test.go      # Step defs for controls features
        cli_steps_test.go           # Step defs for CLI features (incl. shared output assertions)
      support/
        binary.go                   # Binary compilation helper (builds cmd/e2e)
        controller.go               # Controller test harness
```

---

## Public API

### Test Entry Point (`test/e2e/e2e_test.go`)

```go
func TestMain(m *testing.M) {
    // Build binary once: go build -o <tmpdir>/gtb ./cmd/gtb
    // Set GTB_BINARY env var for CLI steps
    os.Exit(m.Run())
}

func TestFeatures(t *testing.T) {
    testutil.SkipIfNotIntegration(t, "e2e")
    suite := godog.TestSuite{
        ScenarioInitializer: InitializeScenario,
        Options: &godog.Options{
            Format:   "pretty",
            Paths:    []string{"../../features"},
            Tags:     buildTagExpression(),
            TestingT: t,
        },
    }
    if suite.Run() != 0 {
        t.Fatal("non-zero status returned, failed to run feature tests")
    }
}
```

### Tag Filtering

The `buildTagExpression()` function reads env vars and composes Godog tag filters:

| Env Var | Effect |
|---------|--------|
| `INT_TEST_E2E=1` | Runs all E2E/Godog tests |
| `INT_TEST_E2E_SMOKE=1` | Runs only `@smoke` tagged scenarios |
| `INT_TEST_E2E_CONTROLS=1` | Runs only `@controls` tagged scenarios |
| `INT_TEST_E2E_CLI=1` | Runs only `@cli` tagged scenarios |

### Tagging Convention

```gherkin
@controls @integration        # Controls lifecycle tests
@controls @integration @slow  # Long-running restart/health tests
@cli @smoke                   # Fast CLI tests (no external deps)
@cli @integration             # CLI tests needing config/filesystem setup
```

### Scenario Context

Per-scenario state prevents leakage between tests:

```go
type controllerWorld struct {
    controller *controls.Controller
    counters   map[string]*StateCounters
    logBuf     *syncBuffer
    httpPort   int
    grpcPort   int
}

func InitializeScenario(ctx *godog.ScenarioContext) {
    world := &controllerWorld{...}
    ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
        return context.WithValue(ctx, worldKey, world), nil
    })
    ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
        // Cleanup: stop controller if still running
        return ctx, nil
    })
}
```

---

## Phased Rollout

### Phase 1: Foundation + Controls Lifecycle

**Goal:** Establish Godog infrastructure, prove value with controls state machine.

**Steps:**

1. Add dependency: `go get github.com/cucumber/godog@latest`
2. Create directory skeleton: `features/controls/`, `test/e2e/`, `test/e2e/steps/`, `test/e2e/support/`
3. Extract controller test helpers to `test/e2e/support/controller.go` (from `pkg/controls/controls_test.go` patterns)
4. Implement `test/e2e/e2e_test.go` entry point
5. Write `features/controls/lifecycle.feature`
6. Write `features/controls/graceful_shutdown.feature`
7. Implement step definitions in `test/e2e/steps/controls_steps_test.go`
8. Add `test-e2e` and `test-e2e-smoke` recipes to justfile

**Scenarios — Lifecycle:**

```gherkin
@controls @integration
Feature: Service Lifecycle Management
  The controller manages services through a 4-state FSM:
  Unknown -> Running -> Stopping -> Stopped

  Background:
    Given a controller with no OS signal handling

  @smoke
  Scenario: Start and stop a service
    Given a service "worker" is registered
    When the controller starts
    Then the controller state is "Running"
    And the service "worker" has been started
    When the controller receives a stop request
    Then the controller state is "Stopped"
    And the service "worker" has been stopped exactly 1 time

  Scenario: Context cancellation triggers shutdown
    Given a service "worker" is registered
    When the controller starts
    And the parent context is cancelled
    Then the controller reaches "Stopped" state within 5 seconds

  Scenario: Concurrent stop calls are idempotent
    Given a service "worker" is registered
    When the controller starts
    And 100 goroutines call Stop concurrently
    Then the controller state is "Stopped"
    And the service "worker" has been stopped exactly 1 time
```

**Scenarios — Graceful Shutdown:**

```gherkin
@controls @integration @slow
Feature: Graceful Shutdown
  When the controller receives SIGINT, it must drain in-flight
  requests before stopping.

  Scenario: SIGINT triggers clean shutdown with HTTP and gRPC servers
    Given a controller with HTTP server on a free port
    And a controller with gRPC server on a free port
    When the controller starts
    And all servers are healthy
    And the controller receives SIGINT
    Then the controller reaches "Stopped" state within 10 seconds
    And no "server shutdown failed" messages appear in logs

  Scenario: In-flight HTTP requests complete during shutdown
    Given a controller with HTTP server on a free port
    And the HTTP server has a handler "/api/slow" that takes 2 seconds
    When the controller starts
    And a client sends a request to "/api/slow"
    And the request is in-flight
    And the controller receives SIGINT
    Then the in-flight request completes successfully
    And the controller reaches "Stopped" state within 10 seconds

  Scenario: SIGINT during startup still shuts down cleanly
    Given a controller with HTTP and gRPC servers
    And a service "slow-init" that takes 500ms to start
    When the controller starts in the background
    And the "slow-init" service has begun starting
    And the controller receives SIGINT
    Then the controller reaches "Stopped" state within 10 seconds
    And "Stopping Services" appears in logs
```

### Phase 2: Health Monitoring + CLI Smoke

**Goal:** Extend controls coverage, introduce CLI binary testing.

**Steps:**

1. Write `features/controls/health_monitoring.feature`
2. Write `features/cli/version.feature` and `features/cli/doctor.feature`
3. Implement `test/e2e/support/binary.go` (binary compilation in TestMain)
4. Implement `test/e2e/steps/cli_steps_test.go`
5. Add `test-e2e-smoke` to `just ci` recipe once stable

**Scenarios — Health Monitoring:**

```gherkin
@controls @integration
Feature: Health Monitoring

  Scenario: Healthy service reports OK on readiness
    Given a controller with no OS signal handling
    And a health check "db" of type "readiness" that returns healthy
    When the controller starts
    Then the readiness report is overall healthy
    And the readiness report includes "db" with status "OK"

  Scenario: Unhealthy check makes readiness report unhealthy
    Given a controller with no OS signal handling
    And a health check "db" of type "readiness" that returns unhealthy with "connection refused"
    When the controller starts
    Then the readiness report is not overall healthy

  @slow
  Scenario: Service restarts after health failure threshold
    Given a controller with no OS signal handling
    And a service "flaky" that starts successfully
    And the service "flaky" becomes unhealthy after 1 status check
    And the service "flaky" has a restart policy with threshold 2 and interval 10ms
    When the controller starts
    Then the service "flaky" restarts at least 2 times within 5 seconds
```

**Scenarios — CLI Smoke:**

```gherkin
@cli @smoke
Feature: CLI Basic Commands

  Background:
    Given the gtb binary is built

  Scenario: gtb version prints version information
    When I run "gtb version"
    Then the exit code is 0
    And the output contains "version"

  Scenario: gtb version --output json returns valid JSON
    When I run "gtb version --output json"
    Then the exit code is 0
    And the output is valid JSON
    And the JSON field "version" is not empty

  Scenario: gtb doctor runs diagnostic checks
    When I run "gtb doctor"
    Then the exit code is 0

  Scenario: gtb help lists available commands
    When I run "gtb --help"
    Then the exit code is 0
    And the output contains "version"
    And the output contains "update"
```

### Phase 3: CLI E2E Workflows

**Goal:** Test complex multi-step CLI workflows and evaluate remaining candidates.

**Implemented:**

1. `features/cli/update.feature` — Flag validation (semver format), help/usage, error paths
2. `features/cli/init.feature` — Non-interactive init (`--skip-login --skip-key --skip-ai`), config merge with existing values, clean reset, JSON output, help/usage
3. `cmd/e2e/main.go` — Dedicated E2E test binary with all feature flags enabled (including `InitCmd`), embedded minimal config, not shipped in releases. Decouples E2E testing from gtb's feature flag choices.

**Evaluated and deferred:**

4. `gtb update --from-file` end-to-end — Deferred until `feat/offline-update-support` merges; the `--from-file` flag does not exist on `develop` yet. Once merged, add scenarios for: offline archive with checksum sidecar, missing sidecar warning, invalid archive error.
5. Config precedence E2E (file → env → flag) — **Not viable via CLI**. No `config get` command exists to query resolved values. The suitability assessment already rates `pkg/config/` as "No" Godog fit. Existing integration tests in `pkg/config/integration_test.go` provide comprehensive coverage of merge precedence.

---

## TUI/Interactive Command Testing

Commands requiring Bubble Tea/Huh interaction cannot be tested through Godog directly. Strategy:

- Commands with `--non-interactive` or `GTB_NON_INTERACTIVE=true` — test the non-interactive path
- Interactive commands (`gtb init` wizard) — test **outcomes** not interactions: "Given a config directory does not exist / When I run gtb init with default options / Then a config.yaml exists"
- Full interactive testing stays as targeted Go integration tests with programmatic stdin/pty simulation

---

## Just Recipes

```just
test-e2e:
    INT_TEST_E2E=1 go test ./test/e2e/... -v -timeout 5m

test-e2e-smoke:
    INT_TEST_E2E=1 INT_TEST_E2E_SMOKE=1 go test ./test/e2e/... -v -timeout 2m
```

Phase 1: `test-e2e` and `test-e2e-smoke` are opt-in.
Phase 2: `test-e2e-smoke` added to `just ci` once stable.
Phase 3: `test-e2e` runs in separate CI job with longer timeout.

---

## Key Files

| File | Action |
|------|--------|
| `go.mod` | Add `github.com/cucumber/godog` dependency |
| `cmd/e2e/main.go` | Create — Test-only binary with all features enabled |
| `cmd/e2e/assets/init/config.yaml` | Create — Minimal embedded config for E2E binary |
| `features/controls/*.feature` | Create — Gherkin scenarios (lifecycle, graceful shutdown, health monitoring) |
| `features/cli/*.feature` | Create — Gherkin scenarios (help, version, doctor, update, init) |
| `test/e2e/steps/steps_test.go` | Create — TestFeatures entry point + tag filtering |
| `test/e2e/steps/*_test.go` | Create — Step definitions (controls, CLI) |
| `test/e2e/support/binary.go` | Create — Binary compilation helper (builds `cmd/e2e`) |
| `test/e2e/support/controller.go` | Create — Controller test harness |
| `justfile` | Modify — Add `test-e2e`, `test-e2e-smoke` recipes |
| `CLAUDE.md` | Modify — Document E2E test commands |
| `docs/development/integration-testing.md` | Modify — Add Godog/E2E section |

---

## Verification

```bash
# After Phase 1:
INT_TEST_E2E=1 go test ./test/e2e/... -v -timeout 5m
just test          # Ensure existing unit tests unaffected
just lint          # Verify new files pass linting

# After Phase 2:
just test-e2e-smoke
just ci            # Full CI with smoke tests

# Ongoing:
just test-e2e      # All E2E tests
just test-e2e-smoke  # Fast smoke only
```
