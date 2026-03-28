---
title: Integration Testing
description: How to configure and run integration tests that depend on external services.
tags: [testing, integration, ci, environment]
---

# Integration Testing

GTB includes integration tests that exercise real external services — GitHub APIs, git operations over the network, and multi-service lifecycle coordination. These tests are **excluded from the default test suite** and must be explicitly enabled.

## Quick Start

```bash
# 1. Copy the example env file
cp .env.example .env

# 2. Fill in your credentials
#    At minimum: GITHUB_TOKEN with `repo` scope

# 3. Run integration tests
just test-integration

# 4. Generate coverage including integration tests
just coverage-full
```

## Environment Variables

| Variable | Required | Description |
| :--- | :--- | :--- |
| `GITHUB_TOKEN` | Yes | GitHub personal access token with `repo` scope. Used by VCS tests to interact with the GitHub API (PR management, label operations). |
| `GITHUB_KEY` | No | Path to an SSH private key for git-over-SSH tests (clone, push). If unset, SSH-based tests are skipped. |

The `.env` file is loaded automatically by `just` via `dotenv-load`. You can also export these variables directly in your shell.

!!! warning "Never commit `.env`"
    The `.env` file is git-ignored. Use `.env.example` as the template — it contains no secrets.

## Gating Mechanism

All integration tests use the `//go:build integration` build tag:

```go
//go:build integration

package controls_test
```

Files with this tag are excluded from `go test ./...` unless `-tags=integration` is passed. The `just test-integration` recipe includes this flag automatically.

Integration tests **must** live in dedicated `*_integration_test.go` files — do not mix integration and unit tests in the same file using runtime skip checks.

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

## Just Recipes

| Recipe | Command | Description |
| :--- | :--- | :--- |
| `just test-integration` | `go test -tags=integration ./... -v` | Run all integration tests |
| `just coverage-full` | `go test -tags=integration ./... -coverprofile=...` | Generate HTML coverage report including integration tests |
| `just test` | `go test ./... -v -cover` | Unit tests only (default) |
| `just ci` | `tidy, generate, test, test-race, lint` | CI suite — unit tests only |

## CI Configuration

In GitHub Actions, integration tests run as a separate job with secrets injected:

```yaml
- name: Integration Tests
  env:
    GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
  run: go test -tags=integration ./... -v
```

The `GITHUB_TOKEN` provided by GitHub Actions has `repo` scope by default for the current repository.

## Writing New Integration Tests

When adding integration tests:

1. **Always use build tags** — place integration tests in dedicated `*_integration_test.go` files with `//go:build integration`.
2. **Never use runtime env var gating** — the build tag is the sole gating mechanism.
3. **Document dependencies** in this guide's test inventory.
4. **Use `t.Cleanup`** for teardown (remove branches, labels, temp files).
5. **Don't hardcode credentials** — always read from environment variables.
6. **Keep tests idempotent** — they should be safe to re-run without manual cleanup.
