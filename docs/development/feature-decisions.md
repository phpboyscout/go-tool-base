---
title: Architectural Decisions & Conventions
description: Record of architectural decisions, development conventions, and rejected feature proposals that shape the GTB project.
date: 2026-03-31
tags: [development, decisions, architecture, conventions]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Architectural Decisions & Conventions

This page records the architectural decisions, development conventions, and feature evaluations that shape the GTB project. It serves as institutional memory for contributors — both to maintain consistency with established patterns and to reconsider past decisions when circumstances change.

GTB's guiding principle: **foundation for tools, not an application framework**. Features that belong in the tools built on GTB — not in GTB itself — are rejected.

---

## Rejected Features

### Plugin / Extension System
**Date:** 31 March 2026 | **Spec:** [`2026-03-21-plugin-extension-system.md`](specs/2026-03-21-plugin-extension-system.md) (status: REJECTED)

**Proposal:** Script-based command plugin system allowing users to extend CLI tools with custom commands discovered from a plugins directory.

**Rejection rationale:** GTB tools are compiled Go binaries where the tool author controls command registration. The `gtb generate command` workflow already converts scripts into native Cobra commands with full type safety, middleware integration, and telemetry support. A plugin system would be a parallel mechanism adding complexity (subprocess management, manifest parsing, security surface) without clear value over the existing approach.

### Secrets Manager / Vault Integration
**Date:** 31 March 2026

**Proposal:** A `SecretsProvider` interface with implementations for OS keychain, HashiCorp Vault, and environment variable fallback.

**Rejection rationale:** Secrets management is highly specific to deployment context. GTB already offers config injection via multiple mechanisms (env vars, config files, embedded assets, CLI flags) with a clear precedence chain. Introducing one secrets implementation opens a rabbit hole of vendor-specific adapters. Engineers should implement secrets access as part of their config composition (CI/CD pipelines, CSI mounts, etc.) — this is a tool-author concern, not a framework concern.

**Update (April 2026):** A narrower, scoped implementation did land as part of the [credential storage hardening work](specs/2026-04-02-credential-storage-hardening.md). The design respects the original rejection — GTB ships no vendor-specific adapters:

- [`pkg/credentials`](../components/credentials.md) defines a minimal `Backend` interface with a stub default.
- The opt-in [`pkg/credentials/keychain`](../components/credentials.md) subpackage (go-keyring) is the only adapter shipped in-tree.
- Vault / AWS SSM / 1Password / custom-store adapters are tool-author responsibility — the [custom credential backend how-to](../how-to/custom-credential-backend.md) shows the Vault KV v2 pattern as a worked example.

So: `Backend` is the extension point; `SecretsProvider` as originally proposed (a plugin registry with vendor implementations) is still rejected.

### Environment Profiles (dev/staging/prod)
**Date:** 31 March 2026

**Proposal:** A `--profile` flag that selects environment-specific config overlays (e.g. `config.dev.yaml`).

**Rejection rationale:** Assuming how users manage config is beyond GTB's scope. The existing `--config` flag already accepts multiple files (`--config config.yaml --config config.dev.yaml`), which is easily managed in a properly designed environment. Adding profile semantics would impose opinions that don't fit all deployment models.

### Caching Layer
**Date:** 31 March 2026

**Proposal:** A `pkg/cache` with file-based caching and TTL for API responses, version checks, etc.

**Rejection rationale:** Caching is a minefield — cache invalidation is one of the hardest problems in computer science. Engineers should implement caching according to their specific needs and invalidation strategies. GTB provides the building blocks (filesystem abstraction, config) but should not opinionate on caching.

### Command Aliases
**Date:** 31 March 2026

**Proposal:** Allow tool authors to define command aliases in config (e.g. `aliases: { ds: "doctor --output json" }`).

**Rejection rationale:** Most shells (bash, zsh, fish) already provide aliasing features. Developers can add aliases directly to their commands at development time, and users can alias via their shells. The value of a framework-level aliasing system is marginal given these existing mechanisms.

### Database / ORM Abstraction
**Date:** 31 March 2026

**Proposal:** A minimal interface-based abstraction for database connections.

**Rejection rationale:** This is an application-level concern that belongs in tools built on GTB, not in the foundation. Different tools need different data stores — imposing a database pattern would pull GTB into application framework territory.

### Event Bus / Pub-Sub
**Date:** 31 March 2026

**Proposal:** A lightweight event bus integrated into Props for intra-command communication.

**Rejection rationale:** Over-engineering for CLI tools. GTB's controls package handles service coordination. Event systems are an application pattern, not a foundation concern.

### Task Queues
**Date:** 31 March 2026

**Proposal:** An integrated job queue with retry and scheduling.

**Rejection rationale:** Application-level concern. Background job processing is specific to the tool being built and its deployment model.

### i18n / Localisation
**Date:** 31 March 2026

**Proposal:** An internationalisation abstraction with message catalogs.

**Rejection rationale:** Niche requirement that adds complexity for all tool authors. Most GTB tools target developer audiences with English as the common language. Tools requiring i18n can implement it at the application level.

### Distributed Tracing
**Date:** 31 March 2026

**Proposal:** OpenTelemetry-compatible span/trace correlation at the framework level.

**Rejection rationale:** The telemetry system covers CLI analytics needs (command invocations, errors, feature usage). Distributed tracing with spans is a service-level concern — tools that need it should use OpenTelemetry directly.

### Query DSL
**Date:** 31 March 2026

**Proposal:** A lightweight jq-like query language for local JSON/YAML data manipulation.

**Rejection rationale:** Tools should use standard Go for data manipulation. Adding a query DSL adds a learning curve and maintenance burden for a feature that `encoding/json` and third-party libraries already handle well.

---

## Key Architectural Decisions

These are project-wide choices established through implemented specs. Future work should maintain consistency with these patterns.

### Error Library: cockroachdb/errors
**Spec:** `2026-02-18-cockroachdb-errors-migration.md`

Chosen over standard library `errors`. Provides structured hints (`WithHint`), details, assertion failures, and stack traces — essential for user-facing error messaging. All error creation and wrapping must use `cockroachdb/errors`; do not mix with `fmt.Errorf` or standard `errors.New`.

### Logging: Unified Logger Interface
**Spec:** `2026-03-23-unified-logger-abstraction.md`

Replaced dual-library logging (charmbracelet/log + slog) with a unified `Logger` interface. Backends implement the interface directly (not wrappers). Exposes `Handler() slog.Handler` for OpenTelemetry and third-party integration. Charmbracelet is the default CLI backend. Printf-style methods (`Infof`, `Warnf`) are first-class, not discouraged.

### DI Pattern: Narrow Interfaces + Props God Object
**Spec:** `2026-03-21-props-interface-narrowing.md`

Props is an intentional god object — this is by design. Narrow role-based interfaces (`LoggerProvider`, `ConfigProvider`, etc.) allow consumers to declare minimal dependencies without replacing Props. New code should accept the narrowest interface that satisfies its needs.

### Middleware: Function Wrappers with Sealed Registry
**Spec:** `2026-03-24-command-middleware-system.md`

Middleware uses `func(next RunEFunc) RunEFunc` — the same pattern as Go HTTP middleware. Global middleware runs before feature-specific. The registry is sealed after command registration to prevent race conditions. No late registration is allowed.

### Security: Shared TLS Config, Scheme Protection
**Specs:** `2026-03-24-secure-http-client.md`, `2026-03-24-security-server-hardening.md`

TLS 1.2 minimum with curated AEAD cipher suites enforced across all HTTP and gRPC components. HTTP client rejects HTTPS-to-HTTP redirect downgrades. gRPC reflection is off by default. All security settings via standard config resolution (no hardcoded bypasses).

### Concurrency: Callback-Based Resource Access
**Spec:** `2026-03-25-vcs-repo-thread-safety.md`

`WithRepo`/`WithTree` callback pattern replaced raw pointer getters to keep resources inside critical sections. `sync.Mutex` (not `RWMutex`) for go-git because it mutates internal caches during reads. This pattern should be followed for any future resource that requires thread-safe access.

### Config Validation: Decentralised Per-Package
**Spec:** `2026-03-26-config-schema-validation.md`

Each package defines and validates its own config schema via struct tags. No centralised global schema. Unknown keys produce warnings (forward-compatible); missing required fields and enum violations produce errors. Strict mode upgrades unknowns to errors.

### Testing: Strategic Godog BDD
**Spec:** `2026-03-28-godog-bdd-strategy.md`

Godog is used strategically for CLI workflows and state machine scenarios (controls lifecycle, telemetry commands, chat persistence). Table-driven unit tests remain the baseline. BDD is not universal — packages where httptest mocks are effective (chat, HTTP) or AST manipulation (generator) use standard tests.

### Integration Tests: Env-Var Gating
**Spec:** `2026-03-24-test-coverage-follow-up.md`

Integration tests use `testutil.SkipIfNotIntegration(t, "tag")` with `INT_TEST=1` / `INT_TEST_<TAG>=1` env vars. Chosen over `//go:build` tags for compile-time safety and IDE discoverability. Tests live in dedicated `*_integration_test.go` files.

### Release Providers: Global Registry Pattern
**Spec:** `2026-03-29-extended-release-sources.md`

Providers register via `release.Register(sourceType, factory)` in `init()` functions. Written once at startup, read-only thereafter. `ReleaseSource.Params` (`map[string]string`) provides provider-specific config without polluting the shared struct. This pattern should be replicated for future extensibility points.

### Telemetry: Opt-In with Privacy by Design
**Spec:** `2026-03-21-opt-in-telemetry.md`

Telemetry is never enabled by default. Two-level gating: tool author enables `TelemetryCmd`, user opts in via command or env var. No PII collected. Machine IDs are SHA-256 hashed from multiple signals. Consent withdrawal (`telemetry disable`) immediately drops all buffered data. `ForceEnabled` for enterprise overrides consent but GDPR deletion still works.

---

## Development Conventions

These are opinionated positions enforced across the project. They are documented in `CLAUDE.md` and `.agent/skills/gtb-dev/SKILL.md` but recorded here for completeness.

### Library-First Principle

All new features must be implemented in `pkg/` as reusable components before being exposed via the CLI. The CLI is just a consumer of the library. When modifying library APIs that affect scaffolded output, also update templates in `internal/generator/`.

### Spec-Driven Development

Non-trivial features (new packages, public API changes, generator modifications, architectural changes) require a spec in `docs/development/specs/` with status `DRAFT` before implementation begins. Quick fixes and minor changes proceed directly. Spec and implementation live on the same branch — co-locating design rationale with code in git history.

### Test-Driven Development

Write failing tests first, derived from the spec's public API and edge cases. New `pkg/` features must have 90%+ test coverage. Table-driven tests with `t.Parallel()` is the standard pattern. Use `logger.NewNoop()` for test loggers. CLI commands and multi-step workflows must include Gherkin BDD scenarios — these are not optional for user-facing behaviour.

### No Linting Bypasses

Never add `//nolint` decorators — always address the root cause. Lint resolution order (simplest to most complex): `errcheck` → `gocritic` → `staticcheck` → `exhaustive` → `nestif` → `cyclop`. Run tests after every structural fix.

### Error Bubbling via RunE

Errors bubble up through `cobra.Command.RunE` to the central `Execute()` wrapper. Avoid early exit with `ErrorHandler.Fatal()` or `os.Exit()` inside business logic — forced exits prevent `defer` functions from executing, causing resource leaks or corrupted state.

### Interface Design: Accept Interfaces, Return Structs

Functions accept interface parameters for flexibility but return concrete types for clarity. Interfaces are defined where they're consumed, not where they're implemented. Keep interfaces small and focused — never create "kitchen sink" interfaces. All interfaces are designed with testing (Mockery) in mind.

### Command Constructor Pattern

All `cobra.Command` structs use `NewCmd*` constructors: `func NewCmdExample(props *props.Props) *cobra.Command`. No global state, no global command registration. Keep minimal logic in `Run()`; business logic resides in `pkg/`.

### Functional Options Naming

Option types use `*Opt` or `*Option` suffix. Factories use `With*` prefix. Negation uses `Without*` prefix. Constructors always provide sensible defaults so they work with zero options.

### Props Over Context for Dependencies

Prefer passing `Props` (or narrow provider interfaces) over using `context.Context` for dependency injection. Makes dependencies explicit and compiler-checked. Context is for cancellation and deadlines, not dependency wiring.

### Secrets Are Runtime Dependencies

Secrets belong to the environment, not the binary. Configuration priority: CLI flags > env vars > config files > embedded defaults. Never hardcode credentials. Use `.env` files for local development (git-ignored).

### Documentation Must Match Code

Any functional change must include a doc update in `docs/components/` or `docs/concepts/`, cross-referenced with the code for accuracy. Code and documentation should never diverge. Documentation examples must be functional.

### Automated Releases

Releases are automated via semantic-release on merge to `main` — never manually tag. Conventional Commits determine version bumps. No AI attribution in commits. Each commit represents one coherent change with a scope identifying the functional area.

### Three-Layer Project Structure

Discovery (`.gtb/manifest.yaml`), Orchestration (`cmd/` — wiring), Implementation (`pkg/cmd/` — logic). The manifest is the source of truth, enabling `regenerate` to update code while preserving custom logic. Assets are co-located with the code that consumes them.

---

## Review Protocol

When proposing a new feature, check this log first to see if it has been previously considered. If circumstances have changed (new use case, ecosystem shift, user demand), the decision can be revisited — reference this log entry and explain what changed.
