---
title: Add a Custom Release Source
description: How to implement and register a custom release.Provider so your tool can self-update from any backend.
date: 2026-03-29
tags: [how-to, update, release, custom, registry]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Add a Custom Release Source

GTB's self-update system is built around a pluggable provider registry. The six built-in source types (`github`, `gitlab`, `bitbucket`, `gitea`, `codeberg`, `direct`) cover most hosting platforms, but you can register your own provider for any backend — private artifact stores, S3 buckets with custom layout, Nexus, Artifactory, or anything else.

This guide walks through:

1. Implementing `release.Provider`
2. Registering the factory with `release.Register`
3. Wiring `props.Tool.ReleaseSource` in `main.go`
4. Writing unit tests for the provider

---

## Step 1: Implement `release.Provider`

The provider interface has four methods:

```go
type Provider interface {
    GetLatestRelease(ctx context.Context, owner, repo string) (Release, error)
    GetReleaseByTag(ctx context.Context, owner, repo, tag string) (Release, error)
    ListReleases(ctx context.Context, owner, repo string, limit int) ([]Release, error)
    DownloadReleaseAsset(ctx context.Context, owner, repo string, asset ReleaseAsset) (io.ReadCloser, string, error)
}
```

If your backend has no concept of versioned releases or individual tags, return `release.ErrNotSupported` for those methods — the update command handles this sentinel gracefully.

Create a new package, e.g. `pkg/vcs/s3`:

```go
// Package s3 provides a release.Provider that fetches release metadata and
// assets from a private S3 bucket with a fixed object layout.
package s3

import (
    "context"
    "io"
    "net/http"

    "github.com/cockroachdb/errors"

    "github.com/phpboyscout/go-tool-base/pkg/config"
    "github.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

type S3Provider struct {
    bucket     string
    region     string
    httpClient *http.Client
}

func NewProvider(src release.ReleaseSourceConfig, cfg config.Containable) (*S3Provider, error) {
    bucket := src.Params["bucket"]
    if bucket == "" {
        return nil, errors.New("s3: bucket param is required")
    }

    region := src.Params["region"]
    if region == "" {
        region = "us-east-1"
    }

    return &S3Provider{
        bucket:     bucket,
        region:     region,
        httpClient: &http.Client{},
    }, nil
}

func (p *S3Provider) GetLatestRelease(ctx context.Context, owner, repo string) (release.Release, error) {
    // Fetch the latest-version sentinel from S3 and return a synthetic Release.
    // ...
}

func (p *S3Provider) GetReleaseByTag(ctx context.Context, owner, repo, tag string) (release.Release, error) {
    // Construct a synthetic Release for the given tag without a network call,
    // or return release.ErrNotSupported if tags are not meaningful for this backend.
    // ...
}

func (p *S3Provider) ListReleases(ctx context.Context, owner, repo string, limit int) ([]release.Release, error) {
    return nil, errors.WithHint(
        release.ErrNotSupported,
        "ListReleases is not supported for the S3 provider.",
    )
}

func (p *S3Provider) DownloadReleaseAsset(ctx context.Context, _, _ string, asset release.ReleaseAsset) (io.ReadCloser, string, error) {
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.GetBrowserDownloadURL(), nil)
    if err != nil {
        return nil, "", errors.WithStack(err)
    }

    resp, err := p.httpClient.Do(req)
    if err != nil {
        return nil, "", errors.WithStack(err)
    }

    if resp.StatusCode != http.StatusOK {
        _ = resp.Body.Close()
        return nil, "", errors.Newf("S3 download failed: HTTP %d", resp.StatusCode)
    }

    return resp.Body, "", nil
}
```

You also need concrete types that implement `release.Release` and `release.ReleaseAsset`:

```go
type s3Release struct {
    tagName string
    assets  []release.ReleaseAsset
}

func (r *s3Release) GetName() string                   { return r.tagName }
func (r *s3Release) GetTagName() string                { return r.tagName }
func (r *s3Release) GetBody() string                   { return "" }
func (r *s3Release) GetDraft() bool                    { return false }
func (r *s3Release) GetAssets() []release.ReleaseAsset { return r.assets }

type s3Asset struct {
    name string
    url  string
}

func (a *s3Asset) GetID() int64                  { return 0 }
func (a *s3Asset) GetName() string               { return a.name }
func (a *s3Asset) GetBrowserDownloadURL() string { return a.url }
```

---

## Step 2: Register the Factory

Register the provider factory **before** any update operation runs. The cleanest place is `main()`:

```go
package main

import (
    "github.com/phpboyscout/go-tool-base/pkg/vcs/release"
    "github.com/phpboyscout/go-tool-base/pkg/config"

    "github.com/myorg/mytool/pkg/vcs/s3"
)

func main() {
    release.Register("s3", func(src release.ReleaseSourceConfig, cfg config.Containable) (release.Provider, error) {
        return s3.NewProvider(src, cfg)
    })

    // ... build and run the Cobra root command
}
```

If your project has multiple entry points, or you want the provider to be available as a library, you can use an `init()` function in your provider package instead:

```go
// pkg/vcs/s3/init.go
package s3

import (
    "github.com/phpboyscout/go-tool-base/pkg/vcs/release"
    "github.com/phpboyscout/go-tool-base/pkg/config"
)

func init() {
    release.Register(release.SourceType("s3"), func(src release.ReleaseSourceConfig, cfg config.Containable) (release.Provider, error) {
        return NewProvider(src, cfg)
    })
}
```

Then import the package with a blank identifier in `main.go` to trigger the `init()`:

```go
import _ "github.com/myorg/mytool/pkg/vcs/s3"
```

---

## Step 3: Wire `props.Tool.ReleaseSource` in `main.go`

Set `Type` to the string you registered, and pass any provider-specific parameters via `Params`:

```go
tool := props.Tool{
    Name:    "mytool",
    Summary: "My developer tool",
    ReleaseSource: props.ReleaseSource{
        Type:  "s3",
        Owner: "myorg",
        Repo:  "mytool",
        Params: map[string]string{
            "bucket": "myorg-releases",
            "region": "eu-west-1",
        },
    },
}
```

`setup.NewUpdater` calls `release.Lookup(src.Type)` internally and forwards `ReleaseSourceConfig` (including `Params`) to your factory — no other changes are needed.

---

## Step 4: Write Unit Tests

Use `httptest.NewServer` to serve mock responses without any network access:

```go
func TestS3Provider_GetLatestRelease(t *testing.T) {
    t.Parallel()

    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Serve a mock latest-version response
        w.Header().Set("Content-Type", "application/json")
        _, _ = w.Write([]byte(`{"tag_name":"v2.0.0"}`))
    }))
    defer srv.Close()

    src := release.ReleaseSourceConfig{
        Type: "s3",
        Params: map[string]string{
            "bucket":      "test-bucket",
            "version_url": srv.URL + "/latest.json",
        },
    }

    provider, err := s3.NewProvider(src, nil)
    require.NoError(t, err)

    rel, err := provider.GetLatestRelease(context.Background(), "myorg", "mytool")
    require.NoError(t, err)
    assert.Equal(t, "v2.0.0", rel.GetTagName())
}
```

---

## Registering Multiple Variants

You can register multiple factories from the same package for different deployment configurations:

```go
release.Register("s3-us", makeS3Factory("us-east-1"))
release.Register("s3-eu", makeS3Factory("eu-west-1"))
```

Each source type must be unique. Calling `release.Register` with an existing key overwrites the previous factory — useful for overriding a built-in provider in tests.

---

## Related Documentation

- **[Release Provider component](../components/vcs/release.md)** — full interface and registry API reference
- **[Configure Self-Updating](configure-self-updating.md)** — wiring `UpdateCmd` end-to-end
- **[Setup component](../components/setup/index.md)** — how `NewUpdater` selects and constructs providers
- **[Auto-Update Lifecycle](../concepts/auto-update.md)** — how `release.Provider` drives version checks
