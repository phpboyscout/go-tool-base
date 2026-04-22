---
title: Release Provider
description: Backend-agnostic interfaces, provider registry, and built-in release providers (pkg/vcs/release).
date: 2026-03-29
tags: [components, vcs, release, github, gitlab, bitbucket, gitea, codeberg, direct]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Release Provider

**Package:** `pkg/vcs/release`

Defines the three interfaces that abstract over platform-specific release APIs, and the provider registry that lets consuming code (and downstream tools) work with any backend without importing platform packages directly.

---

## Interfaces

### Provider

```go
type Provider interface {
    GetLatestRelease(ctx context.Context, owner, repo string) (Release, error)
    GetReleaseByTag(ctx context.Context, owner, repo, tag string) (Release, error)
    ListReleases(ctx context.Context, owner, repo string, limit int) ([]Release, error)
    DownloadReleaseAsset(ctx context.Context, owner, repo string, asset ReleaseAsset) (io.ReadCloser, string, error)
}
```

`DownloadReleaseAsset` returns `(io.ReadCloser, redirectURL string, error)`. The redirect URL is populated by the GitHub implementation when the API redirects to a CDN; all other providers return an empty string.

!!! note "Not all providers support every method"
    `GetReleaseByTag` and `ListReleases` return `ErrNotSupported` for providers whose platform has no versioned-release concept (Bitbucket, Direct). Check for this sentinel before treating it as a fatal error.

### Release

```go
type Release interface {
    GetName() string
    GetTagName() string
    GetBody() string
    GetDraft() bool
    GetAssets() []ReleaseAsset
}
```

### ReleaseAsset

```go
type ReleaseAsset interface {
    GetID() int64
    GetName() string
    GetBrowserDownloadURL() string
}
```

---

## Sentinel Errors

```go
var (
    // ErrProviderNotFound is returned by Lookup when no factory is registered
    // for the requested source type.
    ErrProviderNotFound = errors.New("no release provider registered for source type")

    // ErrNotSupported is returned by provider methods not applicable to the
    // underlying platform (e.g. ListReleases on Bitbucket).
    ErrNotSupported = errors.New("operation not supported by this release provider")

    // ErrVersionUnknown is returned by the direct provider when neither
    // version_url nor pinned_version is configured.
    ErrVersionUnknown = errors.New("cannot determine latest version: configure version_url or pinned_version in Params")
)
```

---

## Provider Registry

The registry maps source type strings to factory functions. All built-in providers register themselves at package init via blank imports in `pkg/setup/providers.go` — no manual wiring is needed.

### Built-in source type constants

```go
const (
    SourceTypeGitHub    = "github"
    SourceTypeGitLab    = "gitlab"
    SourceTypeBitbucket = "bitbucket"
    SourceTypeGitea     = "gitea"
    SourceTypeCodeberg  = "codeberg"
    SourceTypeDirect    = "direct"
)
```

### Registering a custom provider

Call `release.Register` in your `main()` before any update operations. The registry is backed by a `sync.RWMutex` and is safe to call concurrently.

```go
import (
    "github.com/phpboyscout/go-tool-base/pkg/vcs/release"
    "github.com/phpboyscout/go-tool-base/pkg/config"
)

func main() {
    release.Register("s3", func(src release.ReleaseSourceConfig, cfg config.Containable) (release.Provider, error) {
        return myS3Provider(src, cfg)
    })

    // ... build and run the Cobra root command
}
```

### `ReleaseSourceConfig`

Passed to every `ProviderFactory`. Populated from `props.ReleaseSource` by `NewUpdater`.

```go
type ReleaseSourceConfig struct {
    Type    string
    Host    string            // provider base URL (Gitea, GitLab, direct)
    Owner   string            // org / workspace / username
    Repo    string            // repository slug
    Private bool              // require authentication
    Params  map[string]string // provider-specific key/value pairs (snake_case keys)
}
```

### Querying registered types

```go
types := release.RegisteredTypes() // sorted []string of all registered source types
```

---

## Built-in Providers

### GitHub — `pkg/vcs/github`

Uses the `go-github` SDK. Supports GitHub Enterprise via `ReleaseSource.Host`.

**Authentication:** `GITHUB_TOKEN` env var or `github.auth.value` / `github.auth.env` config keys.

### GitLab — `pkg/vcs/gitlab`

Uses the `gitlab-org/api/client-go` SDK. Supports self-hosted GitLab via `ReleaseSource.Host` (defaults to `https://gitlab.com/api/v4`).

**Authentication:** `GITLAB_TOKEN` or `gitlab.auth.*` config keys.

### Bitbucket Cloud — `pkg/vcs/bitbucket`

Uses the Bitbucket Downloads API (`/2.0/repositories/{workspace}/{repo}/downloads`). Bitbucket has no native "Releases" concept — version information is inferred from asset filenames using a configurable regular expression.

**Authentication:** HTTP Basic auth. Credentials are read in order:

1. `bitbucket.username` + `bitbucket.app_password` config keys
2. `BITBUCKET_USERNAME` + `BITBUCKET_APP_PASSWORD` environment variables

**`Params` keys:**

| Key | Description | Default |
|-----|-------------|---------|
| `filename_pattern` | Go regex for asset matching. Capture group 1 = version string. | GoReleaser convention (see below) |
| `workspace` | Bitbucket workspace slug, if different from `Owner` | same as `Owner` |

**Default filename pattern** matches GoReleaser output:

```
tool_v1.2.3_Linux_x86_64.tar.gz  →  version = "v1.2.3"
tool_Linux_x86_64.tar.gz          →  version = RFC3339 upload timestamp
```

The most recently uploaded set of matching assets is returned as the "latest release". `GetReleaseByTag` and `ListReleases` return `ErrNotSupported`.

**Example configuration:**
```go
props.Tool{
    ReleaseSource: props.ReleaseSource{
        Type:    "bitbucket",
        Owner:   "my-workspace",
        Repo:    "my-tool",
        Private: true,
    },
}
```

### Gitea / Forgejo — `pkg/vcs/gitea`

Uses the Gitea REST API v1 (`{host}/api/v1/repos/{owner}/{repo}/releases`). Compatible with any Gitea or Forgejo instance.

**Authentication:** `GITEA_TOKEN` or `gitea.auth.*` config keys. Token is sent as `Authorization: token <value>`.

**`Params` keys:**

| Key | Description | Default |
|-----|-------------|---------|
| `api_version` | API path version segment | `v1` |

`ReleaseSource.Host` is required and must be the full base URL of the instance (e.g. `https://git.example.com`).

**Example configuration:**
```go
props.Tool{
    ReleaseSource: props.ReleaseSource{
        Type:  "gitea",
        Host:  "https://git.example.com",
        Owner: "my-org",
        Repo:  "my-tool",
    },
}
```

### Codeberg — `pkg/vcs/gitea`

Codeberg (`https://codeberg.org`) runs Forgejo and is registered as a first-class source type. The `Host` field defaults to `https://codeberg.org` — no extra configuration is needed.

**Authentication:** `CODEBERG_TOKEN` or `codeberg.auth.*` config keys.

**Example configuration:**
```go
props.Tool{
    ReleaseSource: props.ReleaseSource{
        Type:  "codeberg",
        Owner: "my-org",
        Repo:  "my-tool",
    },
}
```

### Direct HTTP — `pkg/vcs/direct`

For tools distributed via arbitrary HTTP servers — S3, GCS, Artifactory, Nexus, static web servers, internal CDNs. Asset download URLs are constructed from a configurable template; version detection is optional.

**Authentication:** `DIRECT_TOKEN` env var or `direct.token` config key. Sent as `Authorization: Bearer <value>`.

**`Params` keys:**

| Key | Required | Description |
|-----|----------|-------------|
| `url_template` | Yes | Download URL template. See placeholders below. |
| `version_url` | No | URL returning the latest version string. |
| `version_format` | No | Override format detection: `text`, `json`, `yaml`, or `xml`. |
| `version_key` | No | Field name to extract from structured responses. Tries `tag_name` then `version` by default. |
| `pinned_version` | No | Static version string. Disables all network version checks. |
| `checksum_url_template` | No | Template for the SHA-256 checksum sidecar URL. Same placeholders as `url_template`. |

**URL template placeholders:**

| Placeholder | Example value |
|------------|---------------|
| `{version}` | `v1.2.3` |
| `{version_bare}` | `1.2.3` (no leading `v`) |
| `{os}` | `Linux`, `Darwin`, `Windows` |
| `{arch}` | `x86_64`, `arm64` |
| `{tool}` | value of `ReleaseSource.Repo` |
| `{ext}` | `tar.gz` |

**Version endpoint formats** — auto-detected from `Content-Type`, overridable via `version_format`:

```
# Plain text (text/plain)
v1.2.3

# JSON (application/json)
{"tag_name": "v1.2.3", "prerelease": false}

# YAML (application/yaml)
version: v1.2.3

# XML (application/xml)
<release><version>v1.2.3</version></release>
```

**Example configuration:**
```go
props.Tool{
    ReleaseSource: props.ReleaseSource{
        Type: "direct",
        Repo: "mytool",
        Params: map[string]string{
            "url_template": "https://releases.example.com/{tool}/{version}/{tool}_{os}_{arch}.{ext}",
            "version_url":  "https://releases.example.com/latest.json",
            "version_key":  "tag_name",
        },
    },
}
```

---

## Usage

### Via `NewUpdater` (recommended)

`pkg/setup.NewUpdater` handles provider lookup automatically using `props.Tool.ReleaseSource.Type`. Simply set the type and call `NewUpdater` — no provider import needed. The context is forwarded through private-repo token resolution, so remote-store credential backends (Vault, AWS SSM) honour the caller's deadline.

```go
updater, err := setup.NewUpdater(cmd.Context(), props, "", false)
```

### Direct provider construction

For use cases outside the update command:

```go
import (
    "github.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

factory, err := release.Lookup("gitea")
if err != nil {
    return err
}

src := release.ReleaseSourceConfig{
    Host:  "https://git.example.com",
    Owner: "my-org",
    Repo:  "my-tool",
}

provider, err := factory(src, props.Config)
```

### Getting the latest release

```go
rel, err := provider.GetLatestRelease(ctx, "my-org", "my-repo")
if err != nil {
    return err
}

fmt.Println(rel.GetTagName(), rel.GetName())
for _, asset := range rel.GetAssets() {
    fmt.Println(" -", asset.GetName(), asset.GetBrowserDownloadURL())
}
```

### Downloading an asset

```go
rc, _, err := provider.DownloadReleaseAsset(ctx, "my-org", "my-repo", asset)
if err != nil {
    return err
}
defer rc.Close()

outFile, _ := props.FS.Create("/tmp/mytool.tar.gz")
defer outFile.Close()
io.Copy(outFile, rc)
```

---

## Testing

Mocks for all three interfaces are generated by mockery:

```go
import (
    "testing"
    mock_release "github.com/phpboyscout/go-tool-base/mocks/pkg/vcs/release"
)

func TestAutoUpdate(t *testing.T) {
    mockRel := mock_release.NewMockRelease(t)
    mockRel.EXPECT().GetTagName().Return("v2.0.0")
    mockRel.EXPECT().GetDraft().Return(false)

    mockProvider := mock_release.NewMockProvider(t)
    mockProvider.EXPECT().
        GetLatestRelease(mock.Anything, "my-org", "my-repo").
        Return(mockRel, nil)

    // Pass mockProvider wherever release.Provider is required
}
```

For HTTP-based providers (Gitea, Bitbucket, Direct), unit tests use `httptest.NewServer` to serve mock responses without any network access.

---

## Related Documentation

- **[GitHub](github.md)** — `github.NewReleaseProvider` implementation
- **[GitLab](gitlab.md)** — `gitlab.NewReleaseProvider` implementation
- **[Setup](../setup/index.md)** — how `NewUpdater` selects and constructs providers
- **[Auto-Update Lifecycle](../../concepts/auto-update.md)** — how `release.Provider` drives version checks
