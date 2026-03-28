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
| `INT_TEST` | Yes | Set to any non-empty value to enable integration tests. Without this, all `INT_TEST`-gated tests are skipped. |
| `GITHUB_TOKEN` | Yes | GitHub personal access token with `repo` scope. Used by VCS tests to interact with the GitHub API (PR management, label operations). |
| `GITHUB_KEY` | No | Path to an SSH private key for git-over-SSH tests (clone, push). If unset, SSH-based tests are skipped. |

The `.env` file is loaded automatically by `just` via `dotenv-load`. You can also export these variables directly in your shell.

!!! warning "Never commit `.env`"
    The `.env` file is git-ignored. Use `.env.example` as the template — it contains no secrets.

## Gating Mechanisms

Integration tests use two complementary gating mechanisms:

### 1. Build Tags

Files that contain **only** integration tests use the `integration` build tag:

```go
//go:build integration

package controls_test
```

These files are excluded from `go test ./...` unless `-tags=integration` is passed. The `just test-integration` recipe includes this flag automatically.

### 2. Environment Variable Checks

Tests that live alongside unit tests in non-tagged files use a runtime skip:

```go
func TestGithubFindPullRequestByBranch(t *testing.T) {
    if it := os.Getenv("INT_TEST"); it == "" {
        t.Skip("Skipping integration test as INT_TEST not set")
    }
    // ... test body
}
```

This pattern is used when integration tests share helpers or fixtures with unit tests in the same file.

## Test Inventory

### `pkg/controls/` — Service Lifecycle

| File | Tests | Dependencies |
| :--- | :--- | :--- |
| `integration_test.go` | HTTP and gRPC servers on separate ports | Local network (localhost) |
| `shutdown_test.go` | Graceful shutdown via signals, context cancellation, and timeout | Local network, OS signals |

These tests use the `//go:build integration` tag and require **no external credentials** — only local network access.

### `pkg/vcs/repo/` — Git Operations

| File | Tests | Dependencies |
| :--- | :--- | :--- |
| `repo_test.go` | Branch creation, push, clone, checkout, file operations, in-memory repo | Network access to GitHub, `GITHUB_TOKEN`, optionally `GITHUB_KEY` |

All tests are gated by `INT_TEST` env check. Tests operate against `github.com/phpboyscout/gtb`.

### `pkg/vcs/github/` — GitHub API

| File | Tests | Dependencies |
| :--- | :--- | :--- |
| `client_test.go` | PR lookup by branch, label management | Network access to GitHub API, `GITHUB_TOKEN` |

Gated by `INT_TEST` env check. Tests interact with a real GitHub repository.

## Just Recipes

| Recipe | Command | Description |
| :--- | :--- | :--- |
| `just test-integration` | `INT_TEST=true go test -tags=integration ./... -v` | Run all integration tests |
| `just coverage-full` | `INT_TEST=true go test -tags=integration ./... -coverprofile=...` | Generate HTML coverage report including integration tests |
| `just test` | `go test ./... -v -cover` | Unit tests only (default) |
| `just ci` | `tidy, generate, test, test-race, lint` | CI suite — unit tests only |

## CI Configuration

In GitHub Actions, integration tests run as a separate job with secrets injected:

```yaml
- name: Integration Tests
  env:
    INT_TEST: "true"
    GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
  run: go test -tags=integration ./... -v
```

The `GITHUB_TOKEN` provided by GitHub Actions has `repo` scope by default for the current repository.

## Writing New Integration Tests

When adding integration tests:

1. **Prefer build tags** for files that contain only integration tests.
2. **Use `INT_TEST` env gating** when integration tests share a file with unit tests.
3. **Document dependencies** in this guide's test inventory.
4. **Use `t.Cleanup`** for teardown (remove branches, labels, temp files).
5. **Don't hardcode credentials** — always read from environment variables.
6. **Keep tests idempotent** — they should be safe to re-run without manual cleanup.
