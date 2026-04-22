---
title: VCS
description: Version control subpackages — git operations, GitHub API, GitLab API, and release management.
date: 2026-03-29
tags: [components, vcs, git, github, gitlab, bitbucket, gitea, codeberg, direct, release]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# VCS

The `pkg/vcs/` directory is split into focused subpackages. Each has a distinct responsibility and can be used independently.

## Package Overview

| Package | Import path | Purpose |
|---------|-------------|---------|
| **[Release](release.md)** | `pkg/vcs/release` | Backend-agnostic `Provider`/`Release`/`ReleaseAsset` interfaces, sentinel errors, and the provider registry |
| **[Repo](repo.md)** | `pkg/vcs/repo` | Git repository operations (local and in-memory) via `go-git` |
| **[GitHub](github.md)** | `pkg/vcs/github` | GitHub Enterprise API client and GitHub release provider |
| **[GitLab](gitlab.md)** | `pkg/vcs/gitlab` | GitLab (and self-managed) release provider |
| **[Bitbucket](bitbucket.md)** | `pkg/vcs/bitbucket` | Bitbucket Cloud Downloads-based release provider (filename-pattern version detection) |
| **[Gitea / Codeberg](gitea.md)** | `pkg/vcs/gitea` | Gitea, Forgejo, and Codeberg REST API release provider |
| **[Direct HTTP](direct.md)** | `pkg/vcs/direct` | Direct HTTP release provider for arbitrary download servers |

The root `pkg/vcs` package contains only `auth.go` — the shared `ResolveToken` helper used by the GitHub and GitLab subpackages.

## Provider Registry

All built-in release providers register themselves at package `init` time. Consuming code looks up a provider by source type string rather than importing platform packages directly:

```go
factory, err := release.Lookup("gitea")
provider, err := factory(src, cfg)
```

Blank imports in `pkg/setup/providers.go` wire all built-in providers automatically — no manual registration is required when using `setup.NewUpdater`. See [Release Provider](release.md) for the full registry API and how to register a custom provider.

## Authentication

`vcs.ResolveTokenContext(ctx context.Context, cfg config.Containable, fallbackEnv string) string` resolves a token from a config subtree in this order:

1. `auth.env` — reads the named environment variable
2. `auth.keychain` — `"<service>/<account>"` reference resolved via [`credentials.Retrieve`](../credentials.md#api); silently skipped when no keychain-capable [`Backend`](../credentials.md#backend-interface) is registered
3. `auth.value` — uses the literal value stored in config
4. `fallbackEnv` — falls back to a well-known environment variable (e.g. `"GITHUB_TOKEN"`)

Returns an empty string when nothing is found. Callers decide whether that is an error — public repositories can operate without a token; private repositories will receive a 401. The context is propagated to the credentials backend so remote secret stores (Vault, AWS SSM, 1Password Connect) honour the caller's deadline and cancellation.

```go
import (
    "context"

    "github.com/phpboyscout/go-tool-base/pkg/vcs"
)

// Resolve a GitHub token from props.Config.Sub("github") with the
// cobra command's context.
token := vcs.ResolveTokenContext(cmd.Context(), props.Config.Sub("github"), "GITHUB_TOKEN")
```

The context-free form `vcs.ResolveToken(cfg, fallbackEnv)` is preserved as a compatibility shim that internally calls `ResolveTokenContext(context.Background(), …)`. Prefer the context-aware variant anywhere a context is already in scope.

## Design Goals

**Interface segregation**
: `RepoLike` (repo operations) and `GitHubClient` (API operations) are separate interfaces. Most features only need one of them.

**Backend agnosticism for releases**
: All release providers implement `release.Provider`. Consuming code (e.g. the auto-update command) depends only on that interface and never imports a platform-specific package directly.

**Extensibility**
: Custom providers can be registered at startup via `release.Register`. The registry is backed by a `sync.RWMutex` and safe for concurrent use. See the [how-to guide](../../how-to/custom-release-source.md) for a full walkthrough.

**Testability**
: All public interfaces have generated mocks in `mocks/pkg/vcs/`. In-memory git storage (`SourceMemory`) enables offline integration-style tests. HTTP-based providers use `httptest.NewServer` for network-free unit tests.

**`afero` integration**
: `Repo.AddToFS` bridges `go-git` object storage into any `afero.Fs`, so file operations are consistent between production (OS filesystem) and tests (memory-mapped filesystem).

## Related Documentation

- **[VCS Concepts](../../concepts/vcs-repositories.md)** — architectural rationale and usage patterns
- **[Auto-Update Lifecycle](../../concepts/auto-update.md)** — how `release.Provider` is used for version checks
- **[Interface Design](../../concepts/interface-design.md)** — `RepoLike` and `GitHubClient` in the interface hierarchy
- **[Custom Release Source](../../how-to/custom-release-source.md)** — register your own provider implementation
