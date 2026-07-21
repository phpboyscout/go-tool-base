---
title: Integration Testing
description: How to configure and run integration tests that depend on external services.
tags: [testing, integration, ci, environment]
---

# Integration Testing

!!! tip "See also"
    [Manual credential testing](testing/manual-credentials.md) — hands-on walkthrough of the OS-keychain storage mode against a real workstation, for scenarios that are awkward to mock.

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
| `INT_TEST_CONTROLS=1` | Enables only tests tagged `"controls"` |
| `INT_TEST_GENERATOR=1` | Enables only tests tagged `"generator"` |
| `INT_TEST_GENERATOR_BUILD=1` | Enables only tests tagged `"generator_build"` — the toolchain-backed generator tests that scaffold a project and run `go build`/`go test`/`golangci-lint` against it (also enabled by `INT_TEST_GENERATOR`) |
| `INT_TEST_SETUP=1` | Enables only tests tagged `"setup"` |
| `INT_TEST_CMD=1` | Enables only tests tagged `"cmd"` |
| `INT_TEST_E2E=1` | Enables all E2E BDD tests (Godog) |
| `INT_TEST_E2E_SMOKE=1` | Enables only `@smoke`-tagged E2E scenarios |
| `INT_TEST_E2E_CONTROLS=1` | Enables only `@controls`-tagged E2E scenarios |
| `INT_TEST_E2E_CLI=1` | Enables only `@cli`-tagged E2E scenarios |
| `INT_TEST_E2E_CHAT=1` | Enables only `@chat`-tagged E2E scenarios |

When neither `INT_TEST` nor the relevant `INT_TEST_<TAG>` is set, the test is skipped with a message explaining how to enable it:

```
skipping integration test; set INT_TEST=1 to run all or INT_TEST_VCS=1 for this group
```

### Usage in Test Files

```go
package mypackage_test

import (
    "testing"

    "gitlab.com/phpboyscout/go-tool-base/internal/testutil"
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
| `BITBUCKET_USERNAME` | Yes (Bitbucket tests) | Bitbucket username for Downloads API integration tests. |
| `BITBUCKET_APP_PASSWORD` | Yes (Bitbucket tests) | Bitbucket app password with read access to the test repository's Downloads. |
| `BITBUCKET_TEST_WORKSPACE` | Yes (Bitbucket tests) | Workspace slug for the test repository. |
| `BITBUCKET_TEST_REPO` | Yes (Bitbucket tests) | Repository slug for the test repository. |
| `GITEA_TOKEN` | Yes (Gitea tests) | Personal access token for the Gitea/Forgejo instance under test. |
| `GITEA_HOST` | Yes (Gitea tests) | Base URL of the Gitea/Forgejo instance (e.g. `https://git.example.com`). |
| `GITEA_TEST_OWNER` | Yes (Gitea tests) | Org or username that owns the test repository. |
| `GITEA_TEST_REPO` | Yes (Gitea tests) | Repository slug for the test repository. |

The `.env` file is loaded automatically by `just` via `dotenv-load`. You can also export these variables directly in your shell.

!!! warning "Never commit `.env`"
    The `.env` file is git-ignored. Use `.env.example` as the template — it contains no secrets.

## Test Inventory

### `test/integration/controls/` — Service Lifecycle

The `controls` package itself was extracted to
[`gitlab.com/phpboyscout/go/controls`](https://controls.go.phpboyscout.uk), but
these tests stayed: they exercise **GTB's wiring of** the service stack, not the
module's own behaviour.

| File | Tests | Dependencies |
| :--- | :--- | :--- |
| `controls_integration_test.go` | HTTP and gRPC servers on separate ports | Local network (localhost) |
| `shutdown_integration_test.go` | Graceful shutdown via signals, context cancellation, and timeout | Local network, OS signals |
| `server_integration_test.go` | Health endpoints, middleware bypass, custom health checks, gRPC probes, interceptors, graceful shutdown, app handlers | Local network |

These tests require **no external credentials** — only local network access.

### `pkg/cmd/root/` — Feature Flags

| File | Tests | Dependencies |
| :--- | :--- | :--- |
| `root_integration_test.go` | Command registration based on feature flags, tool metadata propagation | None |

### `pkg/setup/` — Init Flow

| File | Tests | Dependencies |
| :--- | :--- | :--- |
| `init_integration_test.go` | Directory creation, config merge/clean, gitignore, initialisers, API key warnings | Filesystem (in-memory) |

### `pkg/vcs/repo/` — Git Operations

!!! info "Moved — now in the `go/repo` module"

    Git operations were extracted to
    [`gitlab.com/phpboyscout/go/repo`](https://repo.go.phpboyscout.uk), and
    `repo_integration_test.go` moved with them. It is gated and run in that
    module's own pipeline, not GTB's.

    What remains here is `config_adapter_test.go`, which is a **pure unit test** —
    it exercises the props/config→`Settings` mapping with an in-memory filesystem
    and needs no network, token or SSH key.

### `pkg/vcs/github/` — GitHub API

!!! warning "Not yet implemented"
    The previous `client_integration_test.go` was removed — it hardcoded a fake
    `GITHUB_TOKEN`, so it could never authenticate, and the archived GitHub
    mirror rejects writes after the GitLab migration. A real GitHub (and GitLab)
    VCS integration suite is specified as ready-to-pick-up follow-up work in
    [`specs/2026-06-20-desktop-gated-integration-tests.md`](specs/2026-06-20-desktop-gated-integration-tests.md)
    (Work Item 1); it needs a token (`repo` scope) and a throwaway test repo.

### Release providers — moved out of GTB

!!! info "Now owned by the provider modules"
    The GitLab, Gitea, Bitbucket and GitHub **release providers** no longer live in
    this repository. Each ships as its own module
    (`gitlab.com/phpboyscout/go/forge-<forge>`), so their live-API integration
    coverage belongs there, gated by that module's own CI.

    GTB retains only the `forge.Provider` **consumers** (`pkg/setup`), which are
    exercised against the in-memory conformance harness at
    `gitlab.com/phpboyscout/go/forge/test` — no network, no credentials.

    See [`forge.go.phpboyscout.uk`](https://forge.go.phpboyscout.uk) for the
    contract every provider must satisfy.

### Extracted suites — chat, config, errorhandling, signing

!!! info "Now owned by their modules"
    These suites left GTB with their packages and run in each module's own
    pipeline:

    | Was | Now |
    | :--- | :--- |
    | `pkg/chat/{parallel,streaming}_integration_test.go` | [`go/chat`](https://chat.go.phpboyscout.uk) |
    | `pkg/config/config_integration_test.go` | [`go/config`](https://config.go.phpboyscout.uk) |
    | `pkg/errorhandling/propagation_integration_test.go` | [`go/errorhandling`](https://errorhandling.go.phpboyscout.uk) |
    | signing coverage | [`go/signing`](https://signing.go.phpboyscout.uk) |

    Their `INT_TEST_CHAT`, `INT_TEST_CONFIG`, `INT_TEST_ERRORHANDLING` and
    `INT_TEST_SIGNING` gates no longer exist in this repository — setting them
    here has no effect.

### `internal/generator/` — Code Generation Pipeline

| File | Tests | Dependencies |
| :--- | :--- | :--- |
| `pipeline_integration_test.go` | Full lifecycle, deep hierarchy, manifest consistency, protection, command options, dry-run, manifest recovery, feature flags | Filesystem (in-memory) |
| `templatesource_integration_test.go` | The provider-aware template clone leg (`realCloneTemplate` → `pkg/vcs/repo`) against a real on-disk git repo: resolves a ref to a concrete commit, checks it out, returns the matching SHA | Local git repo — **no network**; tagged `"vcs"` (`INT_TEST_VCS=1`) |
| `compile_integration_test.go` | Scaffold a project and compile it (`go build`) | **Go toolchain** — tagged `"generator_build"` |
| `signing_integration_test.go`, `signing_enable_integration_test.go` | Generate with signing enabled, then build/verify the scaffolded tree | **Go toolchain** — tagged `"generator_build"` |
| `verifier/verifier_integration_test.go` | The post-generation verifier runs the real `go build`/`go test`/`golangci-lint` toolchain over a scaffold | **Go toolchain** (+ `golangci-lint` on PATH) — tagged `"generator_build"` |

The `"generator_build"` tag marks the project's strongest real-dependency coverage — it actually compiles and lints the generated output. These tests also run under `INT_TEST_GENERATOR=1`; use `INT_TEST_GENERATOR_BUILD=1` to run only them.

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
| `features/cli/update.feature` | Help/usage, semver validation, and hermetic self-update outcomes (already-latest no-op, version-not-found, corrupt-checksum, bad-signature) via an in-memory stub release source (`GTB_E2E_RELEASE_SCENARIO`) | Binary compilation |
| `features/cli/init.feature` | Non-interactive init, config merge, clean reset, JSON output | Binary compilation, filesystem |
| `features/cli/config.feature` | Get/set/list/validate, sensitive masking, JSON output | Binary compilation, filesystem |
| `features/cli/telemetry.feature` | Enable/disable/status/reset, consent withdrawal, machine ID | Binary compilation, filesystem |
| `features/chat/persistence.feature` | Save/load/list/delete snapshots, encryption, provider mismatch, tool exclusion | None (in-process) |

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

This repository runs on **GitLab CI** (`.gitlab-ci.yml`, assembled from the
`phpboyscout/cicd` components). The default `test` stage runs **unit tests
only** — the same `just ci` suite — so integration tests never gate a normal
merge request. Because the gating is env-var-based rather than build-tag-based,
enabling a group in CI is just a matter of exporting the matching variable on a
dedicated job:

```yaml
integration:
  stage: test
  variables:
    INT_TEST: "1"          # all groups; or INT_TEST_VCS: "1" for a single group
  script:
    - go test ./... -v
  rules:
    # Keep it off the normal merge gate — run on a schedule or manually
    - if: $CI_PIPELINE_SOURCE == "schedule"
      when: always
    - when: manual
```

Jobs that talk to the GitLab API can authenticate with the pipeline-provided
`CI_JOB_TOKEN` (or a project/group access token exposed as a masked CI/CD
variable) rather than a hardcoded secret. Several groups (VCS, chat-live,
keychain, WKD) additionally require real credentials or a desktop environment
and are intentionally left to run locally — see the desktop-gated integration
tests spec for the rationale.

## Writing New Integration Tests

When adding integration tests:

1. **Use the shared helper** — call `testutil.SkipIfNotIntegration(t, "tag")` at the top of every integration test function, choosing an appropriate tag for the test group.
2. **Place in dedicated files** — integration tests must live in `*_integration_test.go` files, separate from unit tests.
3. **Document dependencies** in this guide's test inventory.
4. **Use `t.Cleanup`** for teardown (remove branches, labels, temp files).
5. **Don't hardcode credentials** — always read from environment variables.
6. **Keep tests idempotent** — they should be safe to re-run without manual cleanup.
