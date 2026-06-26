---
title: GitLab
description: GitLab release provider implementing release.Provider (pkg/vcs/gitlab).
date: 2026-03-25
tags: [components, vcs, gitlab, release]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# GitLab

**Package:** `pkg/vcs/gitlab`

Provides a `release.Provider` implementation backed by the GitLab Releases API. Supports both GitLab.com and self-managed GitLab instances. GitLab support is focused on release management and self-updating — there is no full API client equivalent to `GHClient`.

---

## Constructor

```go
func NewReleaseProvider(cfg config.Containable) (release.Provider, error)
```

`cfg` should be a `props.Config.Sub("gitlab")` subtree. Reads `url.api` and authentication via `vcs.ResolveToken`.

Token is optional — public projects work without one. Private projects will receive a 401 if no token is set.

**Configuration keys** (under the `gitlab` subtree):

| Key | Default | Description |
|-----|---------|-------------|
| `url.api` | `https://gitlab.com/api/v4` | GitLab API base URL. Override for self-managed instances. |
| `auth.env` | — | Name of the environment variable holding the token |
| `auth.value` | — | Literal token value (use `auth.env` in preference) |

If neither `auth.env` nor `auth.value` is present, `NewReleaseProvider` falls back to the `GITLAB_TOKEN` environment variable.

---

## Usage

```go
import (
    "gitlab.com/phpboyscout/go-tool-base/pkg/vcs/gitlab"
    "gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

var provider release.Provider
provider, err = gitlab.NewReleaseProvider(props.Config.Sub("gitlab"))
if err != nil {
    return err
}

// Get latest release
latest, err := provider.GetLatestRelease(ctx, "my-group", "my-project")
if err != nil {
    return err
}
fmt.Println(latest.GetTagName(), latest.GetName())

// List releases (up to 20)
releases, err := provider.ListReleases(ctx, "my-group", "my-project", 20)

// Get a specific release
rel, err := provider.GetReleaseByTag(ctx, "my-group", "my-project", "v1.2.0")

// Download an asset
rc, _, err := provider.DownloadReleaseAsset(ctx, "my-group", "my-project", rel.GetAssets()[0])
if err != nil {
    return err
}
defer rc.Close()
io.Copy(outFile, rc)
```

---

## Platform Differences

| Behaviour | GitHub | GitLab |
|-----------|--------|--------|
| `GetDraft()` | Returns actual draft status | Always `false` — GitLab has no draft release concept |
| `DownloadReleaseAsset` redirect URL | May be non-empty (CDN redirect) | Always empty string |
| Assets | `github.ReleaseAsset` list | `gitlab.ReleaseLink` list |
| `GetLatestRelease` | Dedicated API endpoint | Fetches first page (1 item) of sorted releases |

---

## Testing

Use the `release.Provider` mock — there is no GitLab-specific mock:

```go
import mock_release "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/vcs/release"

mockProvider := mock_release.NewMockProvider(t)
mockProvider.EXPECT().
    GetLatestRelease(mock.Anything, "my-group", "my-project").
    Return(mockRelease, nil)
```

For HTTP-level tests, use `net/http/httptest` and pass the test server URL as `url.api` in the test config. See `pkg/vcs/gitlab/release_test.go` for the established pattern.

---

## Related Documentation

- **[Release Provider](release.md)** — `Provider`, `Release`, and `ReleaseAsset` interface reference
- **[GitHub](github.md)** — GitHub release provider (same interface, richer API client)
- **[VCS index](index.md)** — `ResolveToken` and package overview
- **[Auto-Update Lifecycle](../update.md)** — how `release.Provider` drives version checks
