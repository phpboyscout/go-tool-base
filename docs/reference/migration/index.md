---
title: Migration Guides
description: Step-by-step guides for upgrading between GTB versions.
---

# Migration Guides

Each guide covers the breaking changes introduced in a specific release and
provides before/after code examples with a clear migration path.

## Available guides

| From | To | Guide |
|---|---|---|
| v0.4 | v0.5 | [Command composition redesign](v0.4-to-v0.5.md) |
| v0.5 | v0.6 | [Web-service components and shared TLS](v0.5-to-v0.6.md) |
| v0.16 | v0.17 | [Repo provider-aware auth](v0.16-repo-provider-auth.md) |
| v0.16 | v0.17 | [Hot-reload observer contract](v0.16-hot-reload-observer.md) |
| v0.16 | v0.17 | [Controls supervisor & lifecycle hardening](v0.16-controls-supervisor.md) |
| v0.16 | v0.17 | [Browser allowlist is immutable](v0.16-browser-allowed-schemes.md) |
| v0.17 | v0.18 | [Opt-in ForcedUpdate policy](v0.17-update-policy.md) |
| v0.19 | v0.20 | [`--wrap-subcommands` removed](v0.19-wrap-subcommands-removed.md) |
| v0.19 | v0.20 | [Deprecated `setup` middleware helpers removed](v0.20-deprecated-middleware-helpers-removed.md) |
| v0.21 | v0.22 | [`Containable.ConfigFiles()` added](v0.21-config-files-accessor.md) |
| v0.24 | v0.25 | [HCL/Terraform asset support removed](v0.25-assets-hcl-removed.md) |
| v0.25 | v0.26 | [Deprecated gRPC config keys & update test seams removed](v0.26-deprecation-removals.md) |
| v0.27 | v0.28 | [Signing & verification extracted to standalone modules](v0.28-signing-modules-extracted.md) |
| v0.x | v0.x | [Typed config section adapters](v0.x-config-section-adapters.md) |
| v0.x | v0.x | [Configuration moves to the go/config Store (Viper removed)](v0.x-config-store.md) |
| v0.x | v0.x | [Chat settings constructors](v0.x-chat-settings.md) |
| v0.x | v0.x | [Telemetry observability settings](v0.x-telemetry-observability-settings.md) |
| v0.x | v0.x | [Logger slog-first boundary](v0.x-logger-slog-first.md) |
| v0.x | v0.x | [Telemetry value types moved to `pkg/telemetrytypes`](v0.x-telemetry-props-decoupling.md) |
| v0.x | v0.x | [Annotation-based update-check exemption & auxiliary fast path](v0.x-update-check-annotations.md) |
| v0.x | v0.x | [Server bind address & loopback metrics default](v0.x-server-bind-address.md) |
| v0.x | v0.x | [Props provider interfaces pruned; `props.New` constructor](v0.x-props-interface-prune.md) |
| v0.x | v1.0 | [Migrating to v1.0](v0.x-to-v1.0.md) |
| v0.x | v0.x | [`errorhandling.Fatal` exits non-zero on usage/special errors](v0.x-errorhandling-fatal-exit-code.md) |
| v0.x | v0.x | [`props.FeatureCmd` renamed to `props.FeatureID`](v0.x-featurecmd-to-featureid.md) |
| v1.x | v1.12 | [Secure credential storage](v1.12-credential-storage.md) |
| v1.x | v1.x | [Context-aware credentials Backend](v1.x-credentials-context.md) |

## Writing a new guide

Use the `_template.md` file in this directory as a starting point:

1. Copy it to `docs/migration/vX.Y-to-vX.Z.md`.
2. Replace all placeholder text.
3. Group changes by package.
4. Include before/after code blocks and a prose migration path for each change.
5. Remove the template warning admonition at the top.
