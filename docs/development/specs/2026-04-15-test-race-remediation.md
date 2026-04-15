---
title: "Test Race Remediation: Restoring t.Parallel() Across the Codebase"
description: "Make registries, hooks, and package-level globals goroutine-safe so that t.Parallel() can be restored everywhere it was dropped by PR #16."
date: 2026-04-15
status: DRAFT
tags:
  - specification
  - testing
  - concurrency
  - tech-debt
  - quality
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
---

# Test Race Remediation: Restoring t.Parallel() Across the Codebase

Authors
:   Matt Cockayne

Date
:   15 April 2026

Status
:   DRAFT

---

## Overview

PR #16 fixed a set of pre-existing CI race conditions that only manifested when Go's test runner scheduled certain subtests on to the same goroutine pool concurrently. The **expedient** fix was to remove `t.Parallel()` from tests that transitively mutate package-level state. This restored green CI but represents acknowledged tech debt: several packages still depend on globally mutable state that cannot safely participate in parallel tests, and test-only mocking hooks are applied by overwriting exported package-level function variables — a pattern that is fundamentally incompatible with concurrency.

This spec proposes the follow-up work to make the affected subsystems **genuinely goroutine-safe** so that `t.Parallel()` can be restored in every location where it was dropped, without re-introducing races under `go test -race`.

The scope is intentionally narrow: the packages touched by PR #16 plus the small number of public API shifts required to replace package-level mocking hooks with dependency injection. No behavioural changes are expected for runtime users — only internal concurrency semantics and test ergonomics.

---

## Goals

1. Every test file that had `t.Parallel()` removed in PR #16 must be able to restore it without tripping the Go race detector.
2. All registries that are mutated by init-time registration **or** by tests must be safe for concurrent reads while registration is in progress, and safe for concurrent writes across tests that hold a fresh registry.
3. Test-only mocking hooks (`ExportExecLookPath`, `newGitHubClientFunc`) are replaced by dependency injection through options or constructors — no package-level function variables remain.
4. Datadog region endpoint resolution no longer relies on a mutable package-level map. The test pattern of writing test-server URLs into `regionEndpoints` is replaced by an explicit option.
5. Root-command construction no longer mutates `cobra`'s package-level finalizer slice via `cobra.OnFinalize`. Telemetry shutdown is threaded through per-command state that is safe under parallel invocation.
6. CI gains a guard that fails if a race is re-introduced in the restored test set.

### Non-Goals

- Changes to the behaviour of GTB at runtime are **not** in scope. This is a testability / concurrency-safety remediation, not a feature change.
- Refactoring the upstream `leodido/go-conventionalcommits` parser. That race is already correctly worked around in `pkg/changelog` by constructing a fresh `parser.Machine` per subtest; this spec documents it as a one-time, library-specific accommodation rather than a pattern to standardise.
- Broader audit of goroutine-safety beyond the packages enumerated in PR #16. Additional races surfaced during implementation should be filed as separate specs.

---

## Inventory of Affected Code

The following table summarises the races fixed in PR #16 by removing `t.Parallel()`. Each is addressed by the phases of this spec.

| # | Package | Global State | Tests Affected | Fix Phase |
|---|---------|--------------|----------------|-----------|
| 1 | `pkg/setup/registry.go` | `globalRegistry` map; `Register*`, `Get*`, `ResetRegistryForTesting` | `pkg/setup/*_test.go`, `pkg/cmd/doctor/*_test.go` | Phase 1 |
| 2 | `pkg/telemetry/datadog/datadog.go` | `regionEndpoints` map mutated with test-server URLs | `pkg/telemetry/datadog/datadog_test.go` (6 tests) | Phase 1 |
| 3 | `pkg/setup/github/ssh.go` | `newGitHubClientFunc` and `ghLoginFunc` package-level function variables | `pkg/setup/github/github_test.go` | Phase 2 |
| 4 | `pkg/chat/claude_local.go` | `ExportExecLookPath` / `ExportExecCommand` package-level function variables | `pkg/chat/claude_local_test.go`, `pkg/chat/streaming_test.go` | Phase 2 |
| 5 | `pkg/chat/gemini.go` | `ExportGenaiNewClient` package-level function variable | gemini provider tests | Phase 2 |
| 6 | `pkg/setup/update.go` | `osExecutable` and `execLookPath` package-level function variables | `pkg/setup` update tests | Phase 2 |
| 7 | `pkg/cmd/update/update.go` | `ExportExecCommand` package-level function variable | `pkg/cmd/update/update_test.go` | Phase 2 |
| 8 | `pkg/cmd/root/root.go:364` | `cobra.OnFinalize(...)` mutates `cobra`'s package-level finalizer slice | Every parallel test that constructs a root command | Phase 3 |
| 9 | `pkg/changelog` (`leodido/go-conventionalcommits`) | Upstream `parser.Machine` has unsynchronised state | Already fixed by constructing a fresh `Machine` per subtest | Documented, no action |

Registries already using a mutex (for example `pkg/setup/middleware.go`) are **not** in scope. They serve as the reference pattern for Phase 1.

---

## Design Decisions

**Internal locking, unchanged API shape.** The `FeatureRegistry` in `pkg/setup/registry.go` gains a `sync.RWMutex`. Every `Register*` call takes the write lock; every `Get*` call takes the read lock. The public function signatures are unchanged — callers see no API difference. The Go memory model still guarantees init-ordering for production registration, but the new locking makes the type correctly usable from tests and from late registration paths.

**Eliminate the `regionEndpoints` test-mutation pattern outright.** Rather than locking the map, the Datadog backend gains a `WithEndpoint(url string)` option. Tests construct a backend with the explicit URL of an `httptest.Server` — they no longer write to the package-level map. The built-in `regionEndpoints` table becomes a truly read-only lookup of production region URLs. This is cleaner than locking because the map is conceptually static configuration; the only thing that ever mutated it was tests working around the absence of a per-call override.

**Inject mockable functions via options, not package variables.** The `newGitHubClientFunc` and `ExportExecLookPath` variables exist solely to enable test mocking. They are replaced by functional options on the constructors that use them. Tests pass a fake factory via an option; production callers omit the option and get the default. This eliminates the race at its root — there is no global to mutate — and it also improves the public API by making the dependency explicit.

**Options over Props injection.** For the Phase-2 items we considered routing the dependencies through `Props` (keeping call sites terse) but rejected it. `Props` is intended for coarse-grained, tool-wide context (logger, config, FS, metadata). Threading per-function mocking hooks through `Props` would pollute it and blur its responsibility. Functional options on the specific constructor or operation keep the dependency local to where it is used.

**Preserve `OnFinalize` behaviour without the global mutation.** The `cobra.OnFinalize` call in `NewCmdRootWithConfig` exists to flush telemetry on process exit, regardless of which subcommand handled the invocation, and regardless of whether that subcommand defined its own `PostRunE`. Replacing it requires a mechanism that fires after any `RunE`/`PostRunE` on the root command tree, including the error path. The chosen approach wraps the root command's `RunE`/`PersistentPostRunE` chain via the existing middleware system (`pkg/setup/middleware.go`) so that telemetry flush is a middleware applied at root scope. No package-level state is mutated during root construction.

**One-time, library-specific accommodation for the changelog parser.** The `leodido/go-conventionalcommits` parser's `Machine` type is not safe for concurrent use. Upstream is unlikely to change this. We document the "fresh Machine per subtest" pattern already used in `pkg/changelog` as the correct approach and do not attempt to standardise a broader pattern around it. If additional packages ever use the same parser, this spec is the reference.

**Test-only helpers remain permissive about ordering.** `ResetRegistryForTesting` continues to exist. It becomes a proper method that takes the write lock and replaces the internal maps atomically, but its contract (tests call it to get a clean slate) does not change.

---

## Public API Changes

### `pkg/setup/registry.go`

The `FeatureRegistry` struct gains an embedded `sync.RWMutex`. **No exported signatures change**:

```go
type FeatureRegistry struct {
    mu           sync.RWMutex
    initialisers map[props.FeatureCmd][]InitialiserProvider
    subcommands  map[props.FeatureCmd][]SubcommandProvider
    flags        map[props.FeatureCmd][]FeatureFlag
    checks       map[props.FeatureCmd][]CheckProvider
}

// Register, RegisterChecks, RegisterInitialiser, RegisterSubcommand,
// RegisterFeatureFlags, GetInitialisers, GetSubcommands, GetFeatureFlags,
// GetChecks — all unchanged signatures.
```

A new unexported helper `ResetRegistryForTesting` (or the existing one, relocated) uses the mutex to swap in fresh maps.

### `pkg/telemetry/datadog`

A new option replaces the package-level map mutation pattern:

```go
// WithEndpoint overrides the resolved region endpoint with an explicit URL.
// This is intended for tests and for environments using Datadog-compatible
// proxies or on-prem ingest endpoints.
func WithEndpoint(url string) Option
```

`regionEndpoints` becomes unexported and documented as read-only. It may optionally be replaced by a `switch` or a `map` returned from a getter — implementation detail.

### `pkg/setup/github`

The `newGitHubClientFunc` package variable is removed. The functions that use it (`uploadSSHKeyToGitHub` and its callers on the `ConfigureSSHKey` path) accept a factory via a new option:

```go
// WithGitHubClientFactory injects the constructor used to create the GitHub
// client. Tests pass a fake; production callers omit to get the default.
func WithGitHubClientFactory(factory func(cfg config.Containable) (githubvcs.GitHubClient, error)) ConfigureSSHKeyOption
```

The existing `ConfigureSSHKeyOption` chain already exists in the package (see `WithSSHKeySelectForm`, `WithSSHKeyPathForm`, `WithGenerateKeyOptions`), so this slots into the established pattern.

### `pkg/chat`

The `ExportExecLookPath` and `ExportExecCommand` package variables are removed. `newClaudeLocal` (the `ProviderClaudeLocal` factory) accepts the lookup functions via `Config` extension fields or via provider-scoped options. Preferred shape:

```go
type Config struct {
    // ... existing fields ...

    // execLookPath is an optional override used by ProviderClaudeLocal to
    // locate the claude binary. When nil, exec.LookPath is used. Tests may
    // set this to a fake. This field is unexported on Config to keep the
    // public surface clean; it is populated via WithExecLookPath in tests.
}

// WithExecLookPath (test helper, lives in `internal/exectest`) injects
// a fake lookup function.
```

Test fakes for `exec.LookPath` and `exec.CommandContext` live in a new shared `internal/exectest` package — see [Resolved Decisions #2](#resolved-decisions). Both `pkg/chat` and `pkg/cmd/update` consume from it (both currently expose `ExportExec*` package-level variables for the same purpose). No cycle: the test files are in `package chat_test` / `package update_test`, so production code does not import `internal/exectest`.

### `pkg/cmd/root`

The `cobra.OnFinalize(...)` call in `NewCmdRootWithConfig` is removed. Telemetry flush is relocated to a root-scope middleware registered during command construction:

```go
// Inside NewCmdRootWithConfig, after setupRootFlags:
setup.RegisterGlobalMiddleware(newTelemetryFlushMiddleware(props))
```

Where `newTelemetryFlushMiddleware` returns a middleware that calls `next`, captures the error, invokes `props.Collector.Close(ctx)` if enabled, and returns the captured error. The middleware registration is idempotent (guarded by a call-once semantic within the root builder) and, critically, does not mutate any `cobra` package-level state.

If the middleware system cannot be reused cleanly for this (for example because subcommands override `RunE` after registration), a lightweight alternative is a `defer props.flushTelemetry(cmd.Context())` injected into the root command's own `RunE` wrapper. Both options are explored in [Internal Implementation](#internal-implementation).

---

## Internal Implementation

### Phase 1: Registry locking

**`pkg/setup/registry.go`** — add `sync.RWMutex` to `FeatureRegistry`. Update every `Register*` function to take the write lock and every `Get*` function to take the read lock and return a defensive copy (to prevent callers iterating on a map that is subsequently mutated — cheap because the maps are small and reads are infrequent at runtime). `Register`, `RegisterChecks`, `RegisterInitialiser`, `RegisterSubcommand`, `RegisterFeatureFlags` all route through the same lock.

The existing `pkg/setup/middleware.go` pattern is the reference: a `sync.RWMutex`, a `sealed bool`, and `Seal()` / `ResetRegistryForTesting()` helpers. `FeatureRegistry` will adopt this pattern in full — see [Resolved Decisions #1](#resolved-decisions). The mutex is required for memory visibility of the `sealed` flag across goroutines, not only for mutual exclusion on the map/slice writes.

**`pkg/telemetry/datadog/datadog.go`** — add `WithEndpoint` option; in `NewBackend`, an explicit endpoint option takes precedence over region resolution:

```go
func NewBackend(apiKey string, log logger.Logger, opts ...Option) telemetry.Backend {
    cfg := &config{region: RegionUS1, source: "gtb"}
    for _, o := range opts {
        o(cfg)
    }

    endpoint := cfg.endpoint
    if endpoint == "" {
        var ok bool
        endpoint, ok = regionEndpoints[cfg.region]
        if !ok {
            endpoint = regionEndpoints[RegionUS1]
        }
    }
    // ... rest unchanged ...
}
```

All six test sites that currently mutate `regionEndpoints[...]` are rewritten to pass `datadog.WithEndpoint(srv.URL)` to `NewBackend`. After the rewrite, `regionEndpoints` is never written to.

### Phase 2: Mocking-hook removal

**`pkg/setup/github/ssh.go`** — add `WithGitHubClientFactory` to the `ConfigureSSHKeyOption` family. The call site on line 361 reads the factory from the resolved options rather than the package variable:

```go
func ConfigureSSHKey(..., opts ...ConfigureSSHKeyOption) (..., error) {
    c := &configureSSHKeyConfig{
        clientFactory: defaultGitHubClientFactory,
    }
    for _, o := range opts {
        o(c)
    }
    // ... eventually:
    client, err := c.clientFactory(cfg)
    // ...
}
```

Any production paths that call the affected function without passing the option get the default factory, which wraps `githubvcs.NewGitHubClient`. Tests in `pkg/setup/github/github_test.go` pass `WithGitHubClientFactory(...)` instead of reassigning the package variable.

**`pkg/chat/claude_local.go`** — the preferred resolution is to remove `ExportExecLookPath` and `ExportExecCommand` from the public surface entirely, and to accept them via an unexported field on `ClaudeLocal` populated by provider construction. Tests in `pkg/chat/claude_local_test.go` and `pkg/chat/streaming_test.go` (both in `package chat_test`) are rewritten to construct `ClaudeLocal` through helper factories in `internal/exectest`.

**`pkg/cmd/update/update.go`** — same treatment for its `ExportExecCommand`. Tests in `pkg/cmd/update/update_test.go` (in `package update_test`) consume the same `internal/exectest` helpers.

`internal/exectest` exposes pure, test-only factories such as `OkLookPath()`, `MissingLookPath()`, and `MockCommandContext(t, ...)` that return functions matching the `exec.LookPath` / `exec.CommandContext` signatures. It does not depend on `pkg/chat` or `pkg/cmd/update` directly — those packages depend on `internal/exectest` only from their test files via `package *_test`. See [Resolved Decisions #2](#resolved-decisions).

### Phase 3: Replace `cobra.OnFinalize`

**`pkg/cmd/root/root.go`** — remove the `cobra.OnFinalize(...)` block at line 364.

**`pkg/cmd/root/execute.go`** — add a `defer flushTelemetry(props)` in `Execute()` immediately before `rootCmd.Execute()`. This is the chosen replacement; see [Resolved Decisions #3](#resolved-decisions). The defer fires for every cobra dispatch path (RunE, help, parse error, missing subcommand, `--version`, suggestion path) — matching the coverage `cobra.OnFinalize` provided. The flush helper itself preserves the three properties of the original `OnFinalize` call:

- Fires regardless of whether subcommands define their own `PostRunE`.
- Checks the dynamic enabled state (`telemetry.enabled` may have been toggled mid-session).
- Bounded by `telemetryFlushTimeout` via `context.WithTimeout(context.Background(), ...)` so command-context cancellation does not interrupt the flush.

A middleware-based alternative was considered and rejected — `pkg/setup/middleware.Chain` only wraps non-nil `RunE`, so it would not fire on the help, parse-error, or no-subcommand paths that `OnFinalize` covers today. Building a "universal middleware" that wraps `Execute()` would be Option B with extra abstraction.

**Trade-off:** consumers that bypass `pkgRoot.Execute(...)` and call `rootCmd.Execute()` directly lose the telemetry flush. The `2026-03-18-cobra-rune-integration` spec already recommends `pkgRoot.Execute(...)` as the canonical entry point; this consolidates around it. Migration guide should call this out.

### Documenting the changelog parser accommodation

**`pkg/changelog`** — no code changes. Add a comment to the relevant test file (where `parser.Machine` is constructed per subtest) explaining that the upstream library's `Machine` is unsynchronised, that each subtest must construct a fresh `Machine`, and linking this spec as the record of the decision.

---

## Project Structure

| File | Action | Description |
|------|--------|-------------|
| `pkg/setup/registry.go` | Modify | Add `sync.RWMutex`, `sealed bool`, `Seal()`, and `ResetRegistryForTesting()` — matches `pkg/setup/middleware.go` |
| `pkg/setup/registry_test.go` | Modify / New | Add race-focused tests; restore `t.Parallel()` |
| `pkg/telemetry/datadog/datadog.go` | Modify | Add `WithEndpoint` option; make `regionEndpoints` read-only |
| `pkg/telemetry/datadog/datadog_test.go` | Modify | Replace all `regionEndpoints[...] =` mutations with `WithEndpoint`; restore `t.Parallel()` |
| `pkg/setup/github/ssh.go` | Modify | Remove `newGitHubClientFunc` and `ghLoginFunc` variables; add `WithGitHubClientFactory` and `WithGHLogin` options |
| `pkg/setup/github/github_test.go` | Modify | Inject fakes via options; restore `t.Parallel()` |
| `pkg/chat/claude_local.go` | Modify | Remove `ExportExecLookPath`/`ExportExecCommand` package variables |
| `pkg/chat/claude_local_test.go` | Modify | Inject fakes via test helper; restore `t.Parallel()` |
| `pkg/chat/streaming_test.go` | Modify | Same as above |
| `pkg/chat/gemini.go` | Modify | Remove `ExportGenaiNewClient` variable; add `WithGenaiClientFactory` option |
| `pkg/chat/gemini_test.go` | Modify | Inject fake via option; restore `t.Parallel()` if applicable |
| `pkg/cmd/update/update.go` | Modify | Remove `ExportExecCommand` variable; option-inject |
| `pkg/cmd/update/update_test.go` | Modify | Use `internal/exectest` factories; restore `t.Parallel()` |
| `pkg/setup/update.go` | Modify | Remove `osExecutable` and `execLookPath` variables; option-inject on `SelfUpdater` |
| `pkg/setup/update_test.go` | Modify | Inject fakes via options; restore `t.Parallel()` |
| `internal/exectest/` | **New** | Test-only helper package exposing factories for `exec.LookPath` / `exec.CommandContext` fakes; consumed by `pkg/chat` and `pkg/cmd/update` test files |
| `pkg/cmd/root/root.go` | Modify | Remove `cobra.OnFinalize`; attach telemetry flush via middleware or inline defer |
| `pkg/cmd/root/root_test.go` | Modify | Verify telemetry flush on success, failure, and disabled paths; restore `t.Parallel()` |
| `pkg/cmd/root/root.go` | Modify | Remove the `cobra.OnFinalize(...)` block in `NewCmdRootWithConfig` |
| `pkg/cmd/root/execute.go` | Modify | Add `defer flushTelemetry(props)` and the `flushTelemetry` helper |
| `pkg/changelog/*_test.go` | Modify | Add comment documenting the per-subtest `Machine` construction rationale |
| `.github/workflows/*.yml` | Modify | Ensure `go test -race ./...` is run and enforced on the restored test set |
| `docs/development/testing.md` (if present) | Modify | Document the "no package-level mocking hooks" rule and the registry-locking pattern |

---

## Generator Impact

**None.** This spec touches concurrency and test plumbing only; it does not modify any generator templates, scaffolded APIs, or the shape of generated CLI tools. If Phase 2 decides to remove `ExportExecLookPath` entirely, downstream tools that import `pkg/chat` and depended on the exported variable for their own tests would be affected — see [Migration & Compatibility](#migration--compatibility).

---

## Error Handling

No new error types are introduced. Behaviour on the error path is preserved in every phase:

- Registry locking is transparent — `Register*` functions that previously could not fail still cannot fail. The existing `panic` in `middleware.go` on post-seal registration remains the reference model if the feature registry adopts a `Seal()`.
- The Datadog `WithEndpoint` option accepts any string; endpoint validation is deferred to the HTTP client layer, matching current behaviour.
- Injected factories (`WithGitHubClientFactory`, chat execLookPath) return errors with the same signatures as the defaults they replace; the calling code already handles those errors.
- The Phase 3 telemetry middleware must preserve the error returned by the wrapped `RunE`. The flush operation itself must not overwrite or mask a user-facing error; flush errors are logged at debug level only, matching the current `_ = props.Collector.Close(ctx)` pattern.

---

## Testing Strategy

### Unit Tests

| Test | File | Description |
|------|------|-------------|
| `TestFeatureRegistry_ConcurrentRegisterAndGet` | `pkg/setup/registry_test.go` | Spawn N goroutines registering providers while M goroutines call `Get*`; assert no race under `-race` |
| `TestFeatureRegistry_ResetForTesting` | `pkg/setup/registry_test.go` | Verify reset produces a clean registry |
| `TestDatadog_WithEndpoint` | `pkg/telemetry/datadog/datadog_test.go` | Verify `WithEndpoint` overrides region resolution |
| `TestDatadog_RegionFallback` | `pkg/telemetry/datadog/datadog_test.go` | Unknown region falls back to US1 (existing behaviour, no regression) |
| `TestConfigureSSHKey_FakeFactory` | `pkg/setup/github/github_test.go` | Inject fake via `WithGitHubClientFactory`; verify factory is invoked |
| `TestClaudeLocal_FakeLookPath` | `pkg/chat/claude_local_test.go` | Inject fake lookup; verify "claude binary not found" message when fake returns error |
| `TestRootCmd_TelemetryFlushOnSuccess` | `pkg/cmd/root/root_test.go` | Successful `RunE` still triggers flush |
| `TestRootCmd_TelemetryFlushOnError` | `pkg/cmd/root/root_test.go` | `RunE` returning an error still triggers flush; error is returned unchanged |
| `TestRootCmd_TelemetryFlushDisabled` | `pkg/cmd/root/root_test.go` | `telemetry.enabled = false` path skips flush |
| `TestRootCmd_NoCobraFinalizerPollution` | `pkg/cmd/root/root_test.go` | Constructing N root commands in parallel does not accumulate finalizers or race |

### Parallelism Restoration

Every test file that had `t.Parallel()` removed by PR #16 is re-visited. Each test regains `t.Parallel()` and `go test -race ./...` is run to confirm no regression.

### CI Enforcement

The existing `just test-race` target is extended or re-confirmed to run the full restored test set. A new CI job (or an assertion in the existing job) fails if a parallel test races. The decision was made *not* to add a static check that flags package-level function variables — see [Resolved Decisions #4](#resolved-decisions).

### E2E / Integration

No new E2E scenarios. Existing Gherkin scenarios in `features/` continue to run; they do not exercise race conditions directly but confirm that runtime behaviour is unchanged.

---

## Migration & Compatibility

**Internal API stability.** All Phase 1 changes are backward-compatible at the API level. The `FeatureRegistry` struct's new mutex is unexported-by-usage (internal to the package's locking). Public functions retain their signatures.

**Phase 2 has downstream implications.** Removing `ExportExecLookPath`, `ExportExecCommand`, `ExportGenaiNewClient`, and the implicit assumption that `newGitHubClientFunc`-style package-level hooks are a pattern downstream tools can imitate is a **breaking change for test code in any downstream project that mirrored the pattern**. Specifically:

- Downstream tools that built atop `pkg/chat` and reassigned `chat.ExportExecLookPath`, `chat.ExportExecCommand`, or `chat.ExportGenaiNewClient` in their own tests will need to migrate to the new option-based injection points.
- This is captured in the next release notes as a test-only breaking change. Production code is unaffected.
- **A migration guide entry will be added under `docs/migration/`** with concrete before/after snippets for all affected symbols — see [Resolved Decisions #5](#resolved-decisions). Written defensively regardless of whether we know of any downstream users; cost is a single markdown page and saves anyone who does depend on it from an unexplained test failure.

**Phase 3 preserves runtime semantics** — the observable behaviour of root command invocation is unchanged. The only visible effect is that `cobra`'s internal finalizer list is no longer mutated per-construction, which is a net improvement for anyone constructing multiple root commands in-process (tests and embedders).

**API stability tier.** `pkg/setup/registry.go`, `pkg/telemetry/datadog`, `pkg/chat`, and `pkg/cmd/root` are all covered by the v1.11.0 stability guarantee. Each change in this spec is either backward-compatible (Phase 1, Phase 3) or scoped to the test surface (Phase 2). Where the public API shifts at all (adding options), the additions are additive and do not break existing callers.

---

## Future Considerations

1. **Broader goroutine-safety audit.** Other packages may harbour similar latent races. A follow-up spec can inventory all package-level mutable state across `pkg/` and categorise each as safe, locked, or needs-locking.
2. **Drop `ResetRegistryForTesting` helpers entirely.** Long-term, each test could construct a fresh registry rather than mutating a package-level one. This would require threading a `*FeatureRegistry` through `props.Props` or similar and is a larger architectural change deferred beyond this spec.
3. **Upstream patch to `leodido/go-conventionalcommits`.** Contribute a thread-safe `Machine` or document the concurrency contract upstream so the "fresh per subtest" workaround can eventually be dropped.
4. **Generalise the `WithEndpoint` pattern.** Other telemetry backends may benefit from a uniform way to override the destination URL for tests and on-prem deployments.

---

## Implementation Phases

Phases are ordered by risk (lowest first) so that value is delivered incrementally and each phase can ship independently if needed.

### Phase 1: Registries and Read-Only Configuration Maps

- Add `sync.RWMutex` to `FeatureRegistry` in `pkg/setup/registry.go`; lock writes and reads.
- Add `WithEndpoint` option to `pkg/telemetry/datadog`; make `regionEndpoints` effectively read-only.
- Rewrite the six Datadog tests to use the option.
- Restore `t.Parallel()` in the affected `pkg/setup` and `pkg/cmd/doctor` tests.
- Restore `t.Parallel()` in the Datadog tests.
- Run `just test-race` and confirm green.

**Risk:** Low. No public API changes, limited blast radius.

### Phase 2: Mocking-Hook Removal

A complete sweep of the codebase identified **8 package-level function variables, every one a test mocking hook** — there are no legitimate non-test uses (no `http.DefaultTransport`-style indirection, no plugin/registration patterns). Phase 2 removes all of them so the codebase contains zero of this anti-pattern.

- `pkg/setup/github/ssh.go` — remove `newGitHubClientFunc`; add `WithGitHubClientFactory` option.
- `pkg/setup/github/ssh.go` — remove `ghLoginFunc`; add `WithGHLogin` option (or wire through the existing `ConfigureSSHKeyOption` chain).
- `pkg/chat/claude_local.go` — remove `ExportExecLookPath` and `ExportExecCommand`; tests construct `ClaudeLocal` through factories in `internal/exectest` (see [Resolved Decisions #2](#resolved-decisions)).
- `pkg/chat/gemini.go` — remove `ExportGenaiNewClient`; option-inject the `genai.NewClient` factory on the gemini provider constructor.
- `pkg/cmd/update/update.go` — remove `ExportExecCommand`; tests use `internal/exectest` factories.
- `pkg/setup/update.go` — remove `osExecutable` and `execLookPath`; option-inject on the `SelfUpdater` constructor (or equivalent entry point).
- Rewrite all affected tests to use the new injection points.
- Restore `t.Parallel()` across `pkg/setup/github`, `pkg/setup`, `pkg/chat`, and `pkg/cmd/update` tests.
- Update the migration guide entry.

**Risk:** Medium. Touches public API (`pkg/chat` in particular) with a test-only breaking change. Requires care to keep downstream migration painless.

### Phase 3: Replace `cobra.OnFinalize`

- Pick between middleware-based and inline-defer-based telemetry flush (decision gate).
- Remove the `cobra.OnFinalize(...)` call from `NewCmdRootWithConfig`.
- Attach the new flush mechanism; verify it fires on success, error, and disabled paths.
- Add `TestRootCmd_NoCobraFinalizerPollution` and related regression tests.
- Restore `t.Parallel()` in `pkg/cmd/root` tests and any downstream tests that construct root commands.
- Run full `just ci` including `just test-race`.

**Risk:** Highest. Requires understanding the current finalizer semantics, the middleware chain, and any subcommands with custom `PostRunE`. Implementation should include a careful audit of the telemetry shutdown path before the change is merged.

### Post-Implementation

- Document the "no package-level mocking hooks" rule in the testing guide.
- Update the relevant section of `CLAUDE.md` or the testing docs to reference this spec as the canonical pattern for new packages.
- Consider opening the follow-up spec for broader goroutine-safety audit ([Future Considerations](#future-considerations) item 2).

---

## Resolved Decisions

1. **`FeatureRegistry` will adopt the same `sync.RWMutex` + `sealed bool` + `Seal()` + `ResetRegistryForTesting()` pattern as `pkg/setup/middleware.go`.** The mutex is needed for memory visibility of the `sealed` flag (Go's memory model does not guarantee cross-goroutine visibility of writes without synchronisation) in addition to mutual exclusion on map/slice mutation — removing the mutex would let a concurrent `Register*` call observe a stale `sealed == false` even after `Seal()` returned, silently violating the barrier. No public `Unseal()`; the test-only `ResetRegistryForTesting()` covers that need under its explicit name. Atomic-bool variants were considered and rejected because the map writes still require a mutex, making a plain-bool-under-mutex design more uniform.

2. **Test fakes for `exec.LookPath` and `exec.CommandContext` live in a new `internal/exectest` package**, shared by both `pkg/chat` (claude_local provider) and `pkg/cmd/update` (which has the same `ExportExecCommand` package-level variable). `internal/` keeps the helpers off-limits to downstream tools, matching the `internal/testutil` precedent and reinforcing test-helper boundaries. No cycle risk: the consumer test files are already in `package chat_test` / `package update_test`, so production packages never import `internal/exectest`. A combined exec-mocking package was preferred over per-consumer (`internal/chattest`, `internal/updatetest`) because the mocked symbols are stdlib `os/exec` functions, not chat-specific or update-specific. Chat-specific helpers (mock providers, fake snapshots) can be added in their own package later if needed; they are out of scope here.

3. **Phase 3 uses Option B — `defer flushTelemetry(props)` inside `pkg/cmd/root/execute.go`** rather than a middleware-based replacement. Middleware (`pkg/setup/middleware.Chain`) only wraps non-nil `RunE` and therefore does NOT fire for the help, `--version`, parse-error, no-subcommand, or unknown-subcommand-suggestion dispatch paths that `cobra.OnFinalize` covers today. A defer in `Execute()` fires for every cobra dispatch path, eliminates the package-level `cobra.OnFinalize` mutation entirely, and aligns the flush with its actual scope (process lifecycle, not per-command). The defer's flush helper creates a fresh `context.Background()` bounded by `telemetryFlushTimeout` — same shape as the existing `OnFinalize` body — which also resolves the previously open context-lifetime question (the command's context may be cancelled by the time flush runs; using a bounded background context is the established behaviour).

   **Trade-off:** consumers that call `rootCmd.Execute()` directly bypass the flush. The `2026-03-18-cobra-rune-integration` spec already recommends `pkgRoot.Execute(...)` as the canonical entry point; this consolidates around it. Migration guide entry required.

4. **No lint rule forbidding package-level function variables.** A complete sweep of the codebase found 8 such variables, all of them test mocking hooks — zero legitimate non-test uses. Phase 2 has been expanded to remove all 8, leaving the codebase free of the anti-pattern. A blanket lint rule was rejected because (a) the false-positive risk that motivated worrying about it doesn't yet exist in this codebase but is plausible in the future (e.g. registration tables, `http.DefaultTransport`-style singletons), (b) GTB policy forbids `//nolint` decorators, so any future legitimate exception would require a more invasive allowlist mechanism in golangci-lint config, and (c) the spec itself plus the absence of any precedent in the cleaned-up tree is sufficient enforcement at code-review time. See the inventory in [Phase 2 Implementation](#phase-2-mocking-hook-removal).

5. **A migration guide entry will be written for Phase 2 regardless of known downstream usage.** There are no known downstream tools that depend on any of the `Export*` hooks, but rather than rely on that assumption, Phase 2 delivers a `docs/migration/` entry with before/after snippets for every removed symbol (`ExportExecLookPath`, `ExportExecCommand`, `ExportGenaiNewClient`). Cost is minimal (one markdown page), and it saves any undiscovered consumer from a silent test-compilation break at upgrade time.

6. **`ResetRegistryForTesting` helpers stay; per-test registry instances are deferred.** Replacing the package-level registries with per-instance `*FeatureRegistry` threaded through `props.Props` would eliminate shared test state entirely, but breaks the `init()`-time registration pattern that every consumer of `setup.RegisterMiddleware` / `RegisterChecks` / `RegisterInitialiser` / etc. currently uses, and grows `props.Props` with a new field. Phase 1 of this spec already achieves race-free tests via mutex locking, so the marginal benefit of going further is small. Worth a separate spec when a concrete driver emerges (e.g. running multiple CLI tools in the same process, property-based tests that need fresh state per case). Tracked in [Future Considerations](#future-considerations).

## Open Questions

*All resolved. Spec is ready for approval.*
