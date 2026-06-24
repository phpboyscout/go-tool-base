---
title: Direct HTTP
description: Release provider for arbitrary HTTP download servers using URL templates and version detection.
date: 2026-03-31
tags: [components, vcs, direct, http, releases, downloads]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Direct HTTP

**Package:** `pkg/vcs/direct`

Provides a `release.Provider` implementation for arbitrary HTTP download servers. Unlike platform-specific providers (GitHub, GitLab, Bitbucket), this provider works with any server that hosts downloadable files at predictable URLs. Version information is resolved from a separate endpoint or pinned in configuration.

**Source type:** `"direct"`

---

## When To Use

Use the Direct HTTP provider when your releases are hosted on:

- A custom artifact server (Nexus, Artifactory, S3 static website)
- A CDN with predictable URL patterns
- An internal download server
- Any HTTP endpoint that doesn't match a supported platform

## Supported Operations

| Method | Supported | Notes |
|--------|-----------|-------|
| `GetLatestRelease` | Yes | Resolves version from `version_url` or `pinned_version`, constructs synthetic release |
| `GetReleaseByTag` | Yes | Constructs synthetic release for any tag without a network call |
| `ListReleases` | No | Returns `release.ErrNotSupported` |
| `DownloadReleaseAsset` | Yes | Streams asset from expanded URL template with optional Bearer token |

## Configuration

```go
props.ReleaseSource{
    Type:  "direct",
    Owner: "myorg",
    Repo:  "my-tool",
    Params: map[string]string{
        "url_template": "https://releases.example.com/{tool}/{version}/{tool}_{os}_{arch}.{ext}",
        "version_url":  "https://releases.example.com/{tool}/latest.json",
    },
}
```

### Required Parameters

| Parameter | Description |
|-----------|-------------|
| `url_template` | URL template for asset downloads (see placeholders below) |

### Optional Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `version_url` | — | Endpoint to fetch the latest version from |
| `version_format` | Auto-detect | Force format: `"text"`, `"json"`, `"yaml"`, `"xml"` |
| `version_key` | `"tag_name"` then `"version"` | Key to extract version from JSON/YAML/XML response |
| `pinned_version` | — | Hard-coded version (takes precedence over `version_url`) |

Either `version_url` or `pinned_version` must be set for `GetLatestRelease` to work. If neither is configured, `release.ErrVersionUnknown` is returned.

### URL Template Placeholders

| Placeholder | Value | Example |
|-------------|-------|---------|
| `{version}` | Full version string | `v1.2.3` |
| `{version_bare}` | Version without leading `v` | `1.2.3` |
| `{os}` | Title-cased operating system | `Linux`, `Darwin`, `Windows` |
| `{arch}` | CPU architecture | `x86_64`, `arm64`, `i386` |
| `{tool}` | Tool name (from `Repo` or `SetToolName`) | `my-tool` |
| `{ext}` | Archive extension | `tar.gz` |

Example expanded URL:
```
https://releases.example.com/my-tool/v1.2.3/my-tool_Linux_x86_64.tar.gz
```

## Version Resolution

When `GetLatestRelease` is called, the provider resolves the current version:

1. **Pinned version** — if `pinned_version` is set, use it directly (no network call)
2. **Version URL** — fetch from `version_url` and parse the response

### Version URL Format Detection

The provider auto-detects the response format from the `Content-Type` header:

| Content-Type | Format | Extraction |
|-------------|--------|------------|
| `text/plain` | Plain text | Entire body trimmed as version |
| `application/json` | JSON | Extracts `version_key` field (default: `"tag_name"` then `"version"`) |
| `application/yaml` | YAML | Extracts `version_key` field |
| `text/xml`, `application/xml` | XML | Extracts `version_key` element |

Override auto-detection with `version_format`:

```go
Params: map[string]string{
    "version_url":    "https://api.example.com/latest",
    "version_format": "json",
    "version_key":    "release.tag",
},
```

## Authentication

| Config Subtree | Fallback Env Variable |
|---------------|----------------------|
| `direct` | `DIRECT_TOKEN` |

When configured, the provider sends a `Authorization: Bearer {token}` header with download requests.

```go
props.ReleaseSource{
    Type:  "direct",
    Owner: "myorg",
    Repo:  "my-tool",
    Params: map[string]string{
        "url_template": "https://private.example.com/releases/{tool}/{version}/{tool}_{os}_{arch}.{ext}",
        "version_url":  "https://private.example.com/releases/{tool}/latest.txt",
    },
}
```

## Examples

### S3 Static Website

```go
props.ReleaseSource{
    Type:  "direct",
    Repo:  "my-tool",
    Params: map[string]string{
        "url_template":   "https://my-bucket.s3.amazonaws.com/releases/{version}/{tool}_{os}_{arch}.{ext}",
        "pinned_version": "v2.1.0",
    },
}
```

### GitHub Releases (without API)

```go
props.ReleaseSource{
    Type:  "direct",
    Owner: "myorg",
    Repo:  "my-tool",
    Params: map[string]string{
        "url_template": "https://github.com/myorg/my-tool/releases/download/{version}/{tool}_{os}_{arch}.{ext}",
        "version_url":  "https://api.github.com/repos/myorg/my-tool/releases/latest",
        "version_key":  "tag_name",
    },
}
```

### Plain Text Version Endpoint

```go
props.ReleaseSource{
    Type:  "direct",
    Repo:  "my-tool",
    Params: map[string]string{
        "url_template":   "https://releases.example.com/{tool}/{version}/{tool}_{os}_{arch}.{ext}",
        "version_url":    "https://releases.example.com/{tool}/LATEST",
        "version_format": "text",
    },
}
```

## Limitations

- `ListReleases` is not supported — returns `release.ErrNotSupported`
- No release metadata (titles, changelogs) — only version and download URL
- Version resolution requires either `version_url` or `pinned_version`
- Architecture mapping is fixed: `amd64` → `x86_64`, `386` → `i386`

## Related Documentation

- [Release Provider](release.md) — the `Provider` interface and registry
- [Configure Self-Updating](../../how-to/configure-self-updating.md) — wiring up update checks
- [Add a Custom Release Source](../../how-to/custom-release-source.md) — implementing your own provider
