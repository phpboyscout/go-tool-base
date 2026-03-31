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

**Source types:** `"gitea"`, `"codeberg"`

---

## Supported Platforms

| Platform | Host | Source Type | Notes |
|----------|------|------------|-------|
| **Gitea** | Self-hosted | `"gitea"` | Any Gitea instance with API v1 |
| **Forgejo** | Self-hosted | `"gitea"` | API-compatible with Gitea |
| **Codeberg** | `codeberg.org` | `"codeberg"` | Public Forgejo instance; host auto-injected |

## Supported Operations

| Method | Supported | Notes |
|--------|-----------|-------|
| `GetLatestRelease` | Yes | Returns first non-draft release (newest first) |
| `GetReleaseByTag` | Yes | Fetches specific release by tag name |
| `ListReleases` | Yes | Paginated listing with configurable limit |
| `DownloadReleaseAsset` | Yes | Streams asset with optional token auth |

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

Host is required for Gitea — the provider returns an error if it's empty.

### Codeberg

```go
props.ReleaseSource{
    Type:  "codeberg",
    Owner: "myorg",
    Repo:  "my-tool",
}
```

The `codeberg` source type automatically sets `Host` to `https://codeberg.org`. No manual host configuration needed.

### Custom API Version

The default API version is `v1`. Override via `Params`:

```go
props.ReleaseSource{
    Type:  "gitea",
    Host:  "https://git.example.com",
    Owner: "myorg",
    Repo:  "my-tool",
    Params: map[string]string{
        "api_version": "v2",
    },
}
```

## Authentication

| Source Type | Config Subtree | Fallback Env Variable |
|-------------|---------------|----------------------|
| `gitea` | `gitea` | `GITEA_TOKEN` |
| `codeberg` | `gitea` | `CODEBERG_TOKEN` |

Token resolution follows the standard `vcs.ResolveToken` pattern:

1. Config key `gitea.auth.env` — name of an environment variable
2. Config key `gitea.auth.value` — literal token in config
3. Fallback env var (`GITEA_TOKEN` or `CODEBERG_TOKEN`)

Tokens are optional for public repositories. Private repositories require a personal access token with `read:repository` scope.

Authentication uses the `Authorization: token {token}` header format (Gitea's standard auth).

## API Endpoints

The provider constructs API URLs from the host and API version:

```
GET {host}/api/{version}/repos/{owner}/{repo}/releases
GET {host}/api/{version}/repos/{owner}/{repo}/releases?limit={limit}
GET {host}/api/{version}/repos/{owner}/{repo}/releases/tags/{tag}
```

Asset downloads use the `browser_download_url` from the release API response.

## Features

- **Latest release detection** — fetches the first non-draft release from the paginated list
- **Tag-based lookup** — resolves a specific release by tag name; returns error on 404
- **Release listing** — paginated listing with configurable limit
- **Asset download** — streams release assets with token auth if configured
- **Draft filtering** — draft releases are excluded from latest/list results
- **Full metadata** — release name, tag, body (changelog), and all assets are available

## Related Documentation

- [Release Provider](release.md) — the `Provider` interface and registry
- [Configure Self-Updating](../../how-to/configure-self-updating.md) — wiring up update checks
- [Add a Custom Release Source](../../how-to/custom-release-source.md) — implementing your own provider
