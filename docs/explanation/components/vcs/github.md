---
title: GitHub
description: GitHub Enterprise API client and GitHub release provider (pkg/vcs/github).
date: 2026-03-25
tags: [components, vcs, github, api, pull-requests, releases]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# GitHub

**Package:** `pkg/vcs/github`

Provides the GitHub Enterprise API client (`GHClient`) and a `release.Provider` implementation backed by the GitHub Releases API.

---

## Constructor

```go
func NewGitHubClient(cfg config.Containable) (*GHClient, error)
```

`cfg` should be a `props.Config.Sub("github")` subtree. Reads `url.api`, `url.upload`, and authentication via `vcs.ResolveToken`.

Token is optional — public repositories work without one. Private repositories will receive a `401` from the API if no token is set.

**Configuration keys** (under the `github` subtree):

| Key | Default | Description |
|-----|---------|-------------|
| `url.api` | `""` | GitHub Enterprise API base URL. Empty string uses `https://api.github.com`. |
| `url.upload` | `""` | GitHub Enterprise upload URL. When `url.api` is set (or host-derived) and this is left empty, the upload URL is derived from the API host as `https://<api-host>/api/uploads/`. |
| `auth.env` | — | Name of the environment variable holding the token |
| `auth.value` | — | Literal token value (use `auth.env` in preference) |

If neither `auth.env` nor `auth.value` is present, `NewGitHubClient` falls back to the `GITHUB_TOKEN` environment variable.

---

## Token Helper

```go
func GetGitHubToken(cfg config.Containable) (string, error)
```

Returns the resolved token or an error if none is found. Use this where a token is strictly required (e.g. authenticated git operations). For release/update operations on public repos, `vcs.ResolveToken` directly is sufficient.

---

## GitHubClient Interface

All operations are available through the `GitHubClient` interface. Use the interface type in function signatures for testability:

```go
type GitHubClient interface {
    GetClient() *github.Client

    // Pull requests
    CreatePullRequest(ctx context.Context, owner, repo string, pull *github.NewPullRequest) (*github.PullRequest, error)
    GetPullRequestByBranch(ctx context.Context, owner, repo, branch, state string) (*github.PullRequest, error)
    AddLabelsToPullRequest(ctx context.Context, owner, repo string, number int, labels []string) error
    UpdatePullRequest(ctx context.Context, owner, repo string, number int, pull *github.PullRequest) (*github.PullRequest, *github.Response, error)

    // Repository management
    CreateRepo(ctx context.Context, owner, slug string) (*github.Repository, error)
    UploadKey(ctx context.Context, name string, key []byte) error
    GetFileContents(ctx context.Context, owner, repo, path, ref string) (string, error)

    // Releases (lower-level — prefer NewReleaseProvider for release workflows)
    ListReleases(ctx context.Context, owner, repo string) ([]string, error)
    GetReleaseAssets(ctx context.Context, owner, repo, tag string) ([]*github.ReleaseAsset, error)
    GetReleaseAssetID(ctx context.Context, owner, repo, tag, assetName string) (int64, error)
    DownloadAsset(ctx context.Context, owner, repo string, assetID int64) (io.ReadCloser, error)
    DownloadAssetTo(ctx context.Context, fs afero.Fs, owner, repo string, assetID int64, filePath string) error
}
```

**Sentinel errors:**

| Error | Condition |
|-------|-----------|
| `ErrNoPullRequestFound` | `GetPullRequestByBranch` finds no matching PR |
| `ErrRepoExists` | `CreateRepo` target already exists |

---

## Usage Examples

### Pull Request Workflow

```go
client, err := github.NewGitHubClient(props.Config.Sub("github"))
if err != nil {
    return err
}

pr, err := client.CreatePullRequest(ctx, "my-org", "my-repo", &gogithub.NewPullRequest{
    Title: gogithub.Ptr("feat: add new command"),
    Head:  gogithub.Ptr("feature/my-branch"),
    Base:  gogithub.Ptr("main"),
    Body:  gogithub.Ptr("Automated PR from my-tool"),
})
if err != nil {
    return err
}

_ = client.AddLabelsToPullRequest(ctx, "my-org", "my-repo", pr.GetNumber(), []string{"automated"})
```

### File Contents

```go
content, err := client.GetFileContents(ctx, "my-org", "my-repo", "config/defaults.yaml", "main")
```

### Asset Download (low-level)

```go
assetID, err := client.GetReleaseAssetID(ctx, "my-org", "my-repo", "v1.2.0", "mytool_linux_amd64.tar.gz")
if err != nil {
    return err
}
err = client.DownloadAssetTo(ctx, props.FS, "my-org", "my-repo", assetID, "/tmp/mytool.tar.gz")
```

For release workflows (auto-update, version checking), prefer the higher-level `release.Provider` — see below.

---

## Release Provider

```go
func NewReleaseProvider(client GitHubClient) release.Provider
```

Wraps a `GitHubClient` and returns a `release.Provider`. This is the preferred way to work with releases — it keeps consuming code (e.g. the auto-update command) decoupled from GitHub specifics.

```go
client, err := github.NewGitHubClient(props.Config.Sub("github"))
if err != nil {
    return err
}
provider := github.NewReleaseProvider(client)

latest, err := provider.GetLatestRelease(ctx, "my-org", "my-repo")
fmt.Println(latest.GetTagName())

rc, _, err := provider.DownloadReleaseAsset(ctx, "my-org", "my-repo", latest.GetAssets()[0])
```

See **[Release Provider](release.md)** for the full interface reference.

---

## Testing

The `GitHubClient` mock is generated by mockery:

```go
import mock_github "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/vcs/github"

mockClient := mock_github.NewMockGitHubClient(t)
mockClient.EXPECT().
    ListReleases(mock.Anything, "my-org", "my-repo").
    Return([]string{"v2.0.0", "v1.0.0"}, nil)
```

For HTTP-level tests, use `net/http/httptest` to stand up a mock server and pass its URL as `url.api` in the test config. See `pkg/vcs/github/client_coverage_test.go` for the established pattern.

---

## Related Documentation

- **[GitLab](gitlab.md)** — GitLab release provider (same `release.Provider` interface)
- **[Release Provider](release.md)** — interface reference for `Provider`, `Release`, `ReleaseAsset`
- **[VCS index](index.md)** — `ResolveToken` and package overview
