---
title: Integration Testing
description: How to configure and run integration tests that depend on external services.
tags: [testing, integration, ci, environment]
---

# Integration Testing

GTB includes integration tests that exercise real external services — GitHub APIs, git operations over the network, and multi-service lifecycle coordination. These tests are **excluded from the default test suite** and must be explicitly enabled via environment variables.

## Quick Start

```bash
# 1. Copy the example env file
cp .env.example .env

# 2. Fill in your credentials
#    At minimum: GITHUB_TOKEN with `repo` scope

# 3. Run all integration tests
just test-integration

# 4. Run only VCS integration tests
INT_TEST_VCS=1 go test ./pkg/vcs/... -v

# 5. Generate coverage including integration tests
just coverage-full
```

## Gating Mechanism

Integration tests are gated at runtime using `testutil.SkipIfNotIntegration` from `internal/testutil/`. This approach was chosen over `//go:build` tags for:

- **Compile-time safety** — integration tests are always compiled, so breakages are caught by `go build` and `go vet` even when not running them.
- **Discoverability** — tests appear in IDE test explorers and `go test -list` output.
- **Granular control** — targeted `INT_TEST_*` variables allow running specific test groups without all-or-nothing gating.

### Environment Variables

| Variable | Effect |
| :--- | :--- |
| `INT_TEST=1` | Enables **all** integration tests |
| `INT_TEST_VCS=1` | Enables only tests tagged `"vcs"` |
| `INT_TEST_CONFIG=1` | Enables only tests tagged `"config"` |
| `INT_TEST_CONTROLS=1` | Enables only tests tagged `"controls"` |
| `INT_TEST_GENERATOR=1` | Enables only tests tagged `"generator"` |
| `INT_TEST_SETUP=1` | Enables only tests tagged `"setup"` |
| `INT_TEST_CMD=1` | Enables only tests tagged `"cmd"` |
| `INT_TEST_ERRORHANDLING=1` | Enables only tests tagged `"errorhandling"` |
| `INT_TEST_E2E=1` | Enables all E2E BDD tests (Godog) |
| `INT_TEST_E2E_SMOKE=1` | Enables only `@smoke`-tagged E2E scenarios |
| `INT_TEST_E2E_CONTROLS=1` | Enables only `@controls`-tagged E2E scenarios |
| `INT_TEST_E2E_CLI=1` | Enables only `@cli`-tagged E2E scenarios |

When neither `INT_TEST` nor the relevant `INT_TEST_<TAG>` is set, the test is skipped with a message explaining how to enable it:

```
skipping integration test; set INT_TEST=1 to run all or INT_TEST_VCS=1 for this group
```

### Usage in Test Files

```go
package mypackage_test

import (
    "testing"

    "github.com/phpboyscout/go-tool-base/internal/testutil"
)

func TestSomethingIntegration(t *testing.T) {
    testutil.SkipIfNotIntegration(t, "mytag")

    // ... test code that talks to external services
}
```

Integration tests **must** live in dedicated `*_integration_test.go` files to keep them clearly separated from unit tests.

## Credential Variables

| Variable | Required | Description |
| :--- | :--- | :--- |
| `GITHUB_TOKEN` | Yes (VCS tests) | GitHub personal access token with `repo` scope. Used by VCS tests to interact with the GitHub API (PR management, label operations). |
| `GITHUB_KEY` | No | Path to an SSH private key for git-over-SSH tests (clone, push). If unset, SSH-based tests are skipped. |

The `.env` file is loaded automatically by `just` via `dotenv-load`. You can also export these variables directly in your shell.

!!! warning "Never commit `.env`"
    The `.env` file is git-ignored. Use `.env.example` as the template — it contains no secrets.

## Test Inventory

### `pkg/controls/` — Service Lifecycle

| File | Tests | Dependencies |
| :--- | :--- | :--- |
| `integration_test.go` | HTTP and gRPC servers on separate ports | Local network (localhost) |
| `shutdown_test.go` | Graceful shutdown via signals, context cancellation, and timeout | Local network, OS signals |
| `server_integration_test.go` | Health endpoints, middleware bypass, custom health checks, gRPC probes, interceptors, graceful shutdown, app handlers | Local network |

These tests require **no external credentials** — only local network access.

### `pkg/config/` — Configuration Merging

| File | Tests | Dependencies |
| :--- | :--- | :--- |
| `integration_test.go` | Multi-source merge, env var overrides, deep nesting, embedded config, dotenv loading, schema validation | Filesystem |

### `pkg/errorhandling/` — Error Propagation

| File | Tests | Dependencies |
| :--- | :--- | :--- |
| `propagation_integration_test.go` | Cross-package error chains, hints, stacktraces, special errors, help config | None |

### `pkg/cmd/root/` — Feature Flags

| File | Tests | Dependencies |
| :--- | :--- | :--- |
| `integration_test.go` | Command registration based on feature flags, tool metadata propagation | None |

### `pkg/setup/` — Init Flow

| File | Tests | Dependencies |
| :--- | :--- | :--- |
| `init_integration_test.go` | Directory creation, config merge/clean, gitignore, initialisers, API key warnings | Filesystem (in-memory) |

### `pkg/vcs/repo/` — Git Operations

| File | Tests | Dependencies |
| :--- | :--- | :--- |
| `repo_integration_test.go` | Branch creation, push, clone, checkout, file operations, in-memory repo | Network access to GitHub, `GITHUB_TOKEN`, optionally `GITHUB_KEY` |

### `pkg/vcs/github/` — GitHub API

| File | Tests | Dependencies |
| :--- | :--- | :--- |
| `client_integration_test.go` | PR lookup by branch, label management | Network access to GitHub API, `GITHUB_TOKEN` |

### `internal/generator/` — Code Generation Pipeline

| File | Tests | Dependencies |
| :--- | :--- | :--- |
| `pipeline_integration_test.go` | Full lifecycle, deep hierarchy, manifest consistency, protection, command options, dry-run, manifest recovery, feature flags | Filesystem (in-memory) |

### `test/e2e/` — E2E BDD Tests (Godog)

E2E tests use [Godog](https://github.com/cucumber/godog) (Cucumber for Go) to express multi-step behavioural scenarios in Gherkin feature files. Feature files live in `features/`, step definitions in `test/e2e/steps/`.

| Feature File | Scenarios | Dependencies |
| :--- | :--- | :--- |
| `features/controls/lifecycle.feature` | State machine transitions, status messages, context cancellation, concurrent stop idempotency, start errors | None (in-process) |
| `features/controls/graceful_shutdown.feature` | SIGINT with HTTP+gRPC, in-flight request draining, early signal during startup | Local network (localhost) |
| `features/controls/health_monitoring.feature` | Health check types (readiness/liveness/both), status mapping, registration rules, async caching, health-triggered restarts | None (in-process) |
| `features/cli/help.feature` | Root help lists commands, unknown command error | Binary compilation |
| `features/cli/version.feature` | Text output, JSON output, help flag | Binary compilation |
| `features/cli/doctor.feature` | Text diagnostic output, JSON structured report | Binary compilation |
| `features/cli/update.feature` | Help/usage, semver validation, error paths | Binary compilation |

These tests require **no external credentials**. Run via `just test-e2e` or filter with `INT_TEST_E2E_CONTROLS=1` or `INT_TEST_E2E_CLI=1`.

See `docs/development/specs/2026-03-28-godog-bdd-strategy.md` for the full BDD strategy and phased rollout plan.

## Just Recipes

| Recipe | Command | Description |
| :--- | :--- | :--- |
| `just test-integration` | `INT_TEST=1 go test ./... -v` | Run all integration tests |
| `just coverage-full` | `INT_TEST=1 go test ./... -coverprofile=...` | Generate HTML coverage report including integration tests |
| `just test` | `go test ./... -v -cover` | Unit tests only (default) |
| `just test-e2e` | `INT_TEST_E2E=1 go test ./test/e2e/... -v -timeout 5m` | E2E BDD tests via Godog |
| `just test-e2e-smoke` | `INT_TEST_E2E=1 INT_TEST_E2E_SMOKE=1 go test ./test/e2e/... -v -timeout 2m` | E2E smoke tests only (fast) |
| `just ci` | `tidy, generate, test, test-race, lint` | CI suite — unit tests only |

## CI Configuration

In GitHub Actions, integration tests run as a separate job with secrets injected:

```yaml
- name: Integration Tests
  env:
    INT_TEST: "1"
    GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
  run: go test ./... -v
```

The `GITHUB_TOKEN` provided by GitHub Actions has `repo` scope by default for the current repository.

## Writing New Integration Tests

When adding integration tests:

1. **Use the shared helper** — call `testutil.SkipIfNotIntegration(t, "tag")` at the top of every integration test function, choosing an appropriate tag for the test group.
2. **Place in dedicated files** — integration tests must live in `*_integration_test.go` files, separate from unit tests.
3. **Document dependencies** in this guide's test inventory.
4. **Use `t.Cleanup`** for teardown (remove branches, labels, temp files).
5. **Don't hardcode credentials** — always read from environment variables.
6. **Keep tests idempotent** — they should be safe to re-run without manual cleanup.
