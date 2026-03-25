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

---

## Stability Tiers

### Stable

APIs in this tier will not have breaking changes within a major version.
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
| Pre-v1.0 (`v0.x`) | Minor versions **may** contain breaking changes to Beta and Experimental APIs. Patch versions are bug fixes only. Stable-tier APIs are best-effort stable. |
| Post-v1.0 (`v1.x+`) | Standard Go semver. Breaking changes require a major version bump. Stable-tier APIs are guaranteed stable within a major version. |

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
go install golang.org/x/exp/cmd/apidiff@latest
apidiff -m github.com/phpboyscout/go-tool-base v0.9.0 v1.0.0
```

Breaking changes to Stable-tier APIs detected by `apidiff` must not be merged
without a major version bump.
