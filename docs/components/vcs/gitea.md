---
title: Gitea / Forgejo / Codeberg
description: Release provider for Gitea, Forgejo, and Codeberg instances using the REST API.
date: 2026-03-31
tags: [components, vcs, gitea, forgejo, codeberg, releases]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Gitea / Forgejo / Codeberg

**Package:** `pkg/vcs/gitea`

Provides a `release.Provider` implementation for Gitea and Forgejo instances, including Codeberg (`codeberg.org`). Uses the Gitea REST API to list releases, resolve tags, and download release assets.

---

## Supported Platforms

| Platform | Host | Notes |
|----------|------|-------|
| **Gitea** | Self-hosted | Any Gitea instance with API v1 |
| **Forgejo** | Self-hosted | API-compatible with Gitea |
| **Codeberg** | `codeberg.org` | Public Forgejo instance; auto-detected by host |

## Configuration

### Gitea (self-hosted)

```go
props.ReleaseSource{
    Type:  "gitea",
    Host:  "https://git.example.com",
    Owner: "myorg",
    Repo:  "my-tool",
}
```

### Codeberg

```go
props.ReleaseSource{
    Type:  "codeberg",
    Owner: "myorg",
    Repo:  "my-tool",
}
```

The `codeberg` source type automatically sets `Host` to `https://codeberg.org` and resolves tokens from the `CODEBERG_TOKEN` environment variable.

## Authentication

| Source Type | Token Source | Env Variable |
|-------------|-------------|-------------|
| `gitea` | Config subtree or env var | `GITEA_TOKEN` |
| `codeberg` | Config subtree or env var | `CODEBERG_TOKEN` |

Tokens are optional for public repositories. Private repositories require a personal access token with `read:repository` scope.

## API Endpoints

The provider constructs API URLs from the host:

```
GET {host}/api/v1/repos/{owner}/{repo}/releases
GET {host}/api/v1/repos/{owner}/{repo}/releases/tags/{tag}
GET {host}/api/v1/repos/{owner}/{repo}/releases/{id}/assets/{asset_id}
```

## Features

- **Latest release detection** — fetches the first non-draft release from the paginated list
- **Tag-based lookup** — resolves a specific release by tag name
- **Release listing** — paginated listing with configurable limit
- **Asset download** — streams release assets with the correct `Accept` header
- **Draft filtering** — draft releases are excluded from latest/list results

## Related Documentation

- [Release Provider](release.md) — the `Provider` interface and registry
- [Configure Self-Updating](../../how-to/configure-self-updating.md) — wiring up update checks
- [Add a Custom Release Source](../../how-to/custom-release-source.md) — implementing your own provider
