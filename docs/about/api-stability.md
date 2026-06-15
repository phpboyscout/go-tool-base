---
title: "API Stability Policy"
description: "Stability tiers for GTB public APIs, semver commitments, and deprecation policy."
date: 2026-03-25
tags: [api, stability, semver, deprecation]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# API Stability Policy

GTB uses a three-tier stability classification to set clear expectations for
consumers and contributors. Each public type, interface, and function belongs
to one of the tiers below.

!!! warning "Pre-1.0 — tiers describe *intent*, not a current guarantee"
    GTB is currently **pre-1.0** (`v0.x`). While the module is `v0.x` the public API is **not** frozen: breaking changes are permitted and ship as a **minor** version bump (`v0.N` → `v0.N+1`). The version annotations and tier guarantees in the tables below describe the stability commitment that **takes effect from v1.0** — they are not in force yet. Use them to understand which APIs are *intended* to be stable, and expect that any of them may still change before v1.0. The `just apidiff` target and the advisory (non-blocking) CI `apidiff` job exist to make pre-1.0 API changes **visible** so they are seen and intentional; that gate becomes **blocking at v1.0**.

    The `Since` columns below record when each symbol first appeared in its current shape; some annotations (e.g. `v1.11`, `v1.12`) predate the move to `v0.x` versioning and should be read as relative ordering, not literal released tags.

---

## Stability Tiers

### Stable

APIs in this tier are intended for long-term use. Breaking changes require a major version bump.

Deprecations require at least one minor-version notice before removal in
the next major version.

| Package | Type / Interface / Function | Since |
|---------|----------------------------|-------|
| `pkg/props` | `Props` struct (public fields) | v0.1 |
| `pkg/props` | `LoggerProvider`, `ConfigProvider`, `ErrorHandlerProvider` | v0.1 |
| `pkg/props` | `Tool`, `Version`, `Assets` types | v0.1 |
| `pkg/props` | `SetFeatures`, `Enable`, `Disable`, feature constants | v0.1 |
| `pkg/config` | `Containable` interface | v0.1 |
| `pkg/config` | `Container` (all public methods) | v0.1 |
| `pkg/config` | `NewFilesContainer`, `NewReaderContainer`, `LoadFilesContainer` | v0.1 |
| `pkg/logger` | `Logger` interface | v1.0 |
| `pkg/logger` | `Level`, `Formatter` types and constants | v1.0 |
| `pkg/logger` | `NewCharm`, `NewNoop` | v1.0 |
| `pkg/controls` | `Controllable`, `Runner`, `StateAccessor`, `Configurable`, `ChannelProvider` interfaces | v0.1 |
| `pkg/controls` | `StartFunc`, `StopFunc`, `StatusFunc` types | v0.1 |
| `pkg/controls` | `ServiceOption`, `WithStart`, `WithStop`, `WithStatus` | v0.1 |
| `pkg/controls` | `NewController`, `WithLogger`, `WithoutSignals` | v0.1 |
| `pkg/controls` | `State`, `Message`, `HealthMessage`, `HealthReport` types | v0.1 |
| `pkg/errorhandling` | `ErrorHandler` interface | v0.1 |
| `pkg/errorhandling` | `New`, `WithHint`, `WithHintf`, `WrapWithHint` | v0.1 |
| `pkg/setup` | `Register*` functions | v0.1 |
| `pkg/credentials` | `Mode` type, `ModeEnvVar`/`ModeKeychain`/`ModeLiteral` constants, `ErrCredentialNotFound`/`ErrCredentialUnsupported` sentinels | v1.11 |
| `pkg/credentials` | `Backend` interface, `RegisterBackend`, `KeychainAvailable`, `AvailableModes`, `Probe` | v1.12 |
| `pkg/credentials` | `Store`/`Retrieve`/`Delete` package-level helpers | v1.12 |
| `pkg/credentials/keychain` | `Backend` struct (go-keyring implementation) | v1.12 |
| `pkg/credentials/credtest` | `MemoryBackend`, `Install` | v1.12 |
| `pkg/vcs` | `ResolveToken`, `ResolveTokenContext` | v1.11 (`ResolveToken`), v1.12 (`ResolveTokenContext`) |

### Beta

These APIs are functionally complete but may have minor breaking changes in
minor versions. Changes will be documented in [migration guides](migration/v0.x-to-v1.0.md).

| Package | Type / Interface / Function | Since |
|---------|----------------------------|-------|
| `pkg/chat` | `ChatClient` interface and all methods | v0.x |
| `pkg/chat` | `New`, `ProviderClaude`, `ProviderOpenAI`, `ProviderGemini` | v0.x |
| `pkg/cmd/doctor` | Doctor check registration API | v0.x |
| `pkg/http` | `Start`, `Stop`, `NewSecureClient` | v0.x |
| `pkg/grpc` | `New`, `Start`, `Stop` | v0.x |
| `pkg/controls` | `WithLiveness`, `WithReadiness`, `WithRestartPolicy`, `RestartPolicy` | v0.x |
| `pkg/vcs/github` | `NewClient`, `ReleaseProvider` interface | v0.x |
| `pkg/vcs/gitlab` | `NewClient`, `ReleaseProvider` interface | v0.x |
| `pkg/vcs/repo` | `Repo` struct and all public methods | v0.x |
| `pkg/setup` | `NewUpdater(ctx, props, version, force)` | v1.12 (signature updated from v1.11's context-free form) |

### Experimental

These APIs may change significantly or be removed without notice. Do not
depend on them in production code without pinning to a specific version.

| Package | Scope | Note |
|---------|-------|------|
| `internal/*` | All packages | Always unstable — import not supported |
| `pkg/forms` | All types and functions | Subject to charmbracelet/huh API changes |
| `pkg/setup/ai`, `pkg/setup/github` | All types and functions | Configuration UX still evolving |

---

## Semver Commitments

| Version range | Policy |
|---------------|--------|
| `v0.x` (current) | **Unstable — pre-1.0:** Breaking changes to any public API are permitted and ship as a **minor** bump. The tier tables above describe the *intended* v1.0 commitment, not a current guarantee. Prefer backward-compatible changes where cheap; document any break in `docs/migration/`. |
| `v1.0.0+` | **Guaranteed Stability (future):** From the first `v1.0.0` release, standard Go semver applies — breaking changes to Stable and Beta tier APIs require a major version bump (v2.0.0+), and the advisory CI `apidiff` job becomes a blocking gate. |

The `internal/` directory is always unstable regardless of version — it is not
part of the public API surface.

---

## Deprecation Policy

1. A deprecated API is annotated with a `// Deprecated:` Go doc comment
   describing what to use instead.
2. The deprecation is documented in the next minor-version migration guide.
3. The API is removed no earlier than the following major version.
4. For pre-v1.0 releases, deprecated APIs may be removed in the next minor
   version (with a migration guide entry).

---

## Checking for Breaking Changes

Use `apidiff` to detect API-level breaking changes between versions:

```bash
just apidiff   # compares the working tree against the latest release tag
```

`just apidiff` installs `golang.org/x/exp/cmd/apidiff` on demand and runs
`apidiff -m gitlab.com/phpboyscout/go-tool-base <latest-tag> .`. The CI
`apidiff` job runs the same comparison on every merge request.

**Pre-1.0:** the comparison is **advisory** — incompatible changes are expected
(they ship as minor bumps) and the CI job is non-blocking (`allow_failure:
true`). Its purpose is visibility: an API change should be *seen* and
*intentional*, reflected in the diff a reviewer reads.

**From v1.0:** breaking changes to Stable- and Beta-tier APIs detected by
`apidiff` must not be merged without a major version bump, and the CI job
becomes blocking.
