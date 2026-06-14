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
| v0.x | v1.0 | [Migrating to v1.0](v0.x-to-v1.0.md) |

## Writing a new guide

Use the `_template.md` file in this directory as a starting point:

1. Copy it to `docs/migration/vX.Y-to-vX.Z.md`.
2. Replace all placeholder text.
3. Group changes by package.
4. Include before/after code blocks and a prose migration path for each change.
5. Remove the template warning admonition at the top.
