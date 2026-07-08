---
name: env-gated-integration-tests
description: Gate slow or external-dependency tests behind an environment variable rather than a build tag, so they stay compiled, IDE-discoverable, and selectable by group. Use when adding integration tests that hit the network, a real API, or real git/disk.
---

# Env-gated integration tests

Use this when a test needs something a unit test shouldn't: the network, a real
API (GitHub/GitLab), real git operations, or multi-component lifecycle
coordination. You want those tests in the tree but **off by default** — and the
*how* matters.

## Env var, not build tag

The common reflex is a `//go:build integration` tag. go-tool-base deliberately
uses an **environment-variable gate** instead, because build tags have real
costs:

- **Compile-time safety.** Tag-gated files aren't compiled in the default build,
  so they rot silently — a renamed symbol breaks them and nobody notices until
  someone flips the tag. Env-gated tests compile every time; they just *skip* at
  runtime, so a breaking API change fails the normal build immediately.
- **IDE discoverability.** Tag-gated tests are invisible to the editor's test
  runner until you reconfigure build flags. Env-gated tests show up like any
  other and run with one env var set.
- **Granular control.** A single env var can enable *all* integration tests,
  while a per-group var enables just one slice.

## The pattern

A tiny helper skips the test unless the gate is set, keyed by a group tag:

```go
func TestRepoClone_RealGitHub(t *testing.T) {
    testutil.SkipIfNotIntegration(t, "vcs") // first line of the test
    // ...real network/API work...
}
```

`SkipIfNotIntegration(t, "vcs")` skips unless **`INT_TEST=1`** (everything) or
**`INT_TEST_VCS=1`** (just the `vcs` group) is set. Pick a tag that names the
package's group (`vcs`, `controls`, `config`, …).

Conventions that keep it clean:

- Put integration tests in dedicated **`*_integration_test.go`** files, separate
  from unit tests.
- Use `t.Cleanup()` for teardown so a failure still tears down.
- Keep an inventory doc listing each group, its gate var, and what external
  dependency it needs — so a new contributor knows what `INT_TEST_VCS=1`
  actually talks to.

## Running them

```bash
INT_TEST=1 go test ./...                 # everything
INT_TEST_VCS=1 go test ./pkg/vcs/... -v  # one group
```

Wire a `just test-integration` (or Make target) for the all-on case so it's one
command in CI and locally.

## The same idea scales to E2E

End-to-end / BDD suites use the same shape with their own variables
(`INT_TEST_E2E=1`, plus per-area `INT_TEST_E2E_CLI=1` / `..._SMOKE=1`). One
mechanism, layered gates — see [bdd-when-and-how](../bdd-when-and-how/SKILL.md).
