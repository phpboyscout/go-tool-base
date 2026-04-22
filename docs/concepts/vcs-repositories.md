---
title: VCS & Repository Abstraction
description: Polymorphic repository management and unified Git/GitHub/GitLab automation across subpackages.
date: 2026-03-25
tags: [concepts, vcs, git, github, gitlab, abstraction, memfs, release]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# VCS & Repository Abstraction

GTB's VCS layer is split into focused subpackages, each with a single responsibility. Together they provide a consistent abstraction over `go-git`, the GitHub API, and the GitLab API.

---

## Package Layout

```
pkg/vcs/
├── auth.go          — shared token resolution (ResolveToken)
├── release/         — backend-agnostic release Provider interface
├── repo/            — go-git repository operations (RepoLike / Repo)
├── github/          — GitHub API client and GitHub release provider
└── gitlab/          — GitLab release provider
```

---

## `pkg/vcs/release` — Provider Interface

The `release` subpackage defines the backend-agnostic contract. Both the GitHub and GitLab providers implement it, so consuming code never imports a platform-specific package directly.

```go
// Provider fetches release metadata and downloads assets.
type Provider interface {
    GetLatestRelease(ctx context.Context) (Release, error)
    GetReleaseByTag(ctx context.Context, tag string) (Release, error)
    ListReleases(ctx context.Context) ([]string, error)
    DownloadReleaseAsset(ctx context.Context, asset ReleaseAsset, dest string) error
}

type Release interface {
    GetName() string
    GetTagName() string
    GetBody() string
    IsDraft() bool
    GetAssets() []ReleaseAsset
}

type ReleaseAsset interface {
    GetID() int64
    GetName() string
    GetBrowserDownloadURL() string
}
```

Consuming code works against `release.Provider` and receives either a GitHub or GitLab implementation at construction time.

---

## `pkg/vcs/repo` — Git Repository Operations

### RepoLike Interface

`RepoLike` defines the full contract for git repository operations. This is the type accepted by functions that need to manipulate repositories, enabling mock substitution in tests.

```go
type RepoLike interface {
    SourceIs(int) bool
    SetSource(int)
    SetRepo(*git.Repository)
    SetKey(*ssh.PublicKeys)
    SetBasicAuth(string, string)
    GetAuth() transport.AuthMethod
    SetTree(*git.Worktree)
    Checkout(plumbing.ReferenceName) error
    CheckoutCommit(plumbing.Hash) error
    CreateBranch(string) error
    OpenInMemory(string, string, ...CloneOption) (*git.Repository, *git.Worktree, error)
    OpenLocal(string, string) (*git.Repository, *git.Worktree, error)
    Open(RepoType, string, string, ...CloneOption) (*git.Repository, *git.Worktree, error)
    WalkTree(func(*object.File) error) error
    AddToFS(fs afero.Fs, gitFile *object.File, fullPath string) error
    WithRepo(func(*git.Repository) error) error
    WithTree(func(*git.Worktree) error) error
}
```

### Creating a Repo

```go
import "github.com/phpboyscout/go-tool-base/pkg/vcs/repo"

r, err := repo.NewRepo(props)
```

`NewRepo` resolves authentication automatically (see [Authentication](#authentication)) and returns a `*Repo` that satisfies `RepoLike`.

### Storage Strategies

**Local repositories** (`SourceLocal`) operate on the host filesystem. Use these for tools that need persistent state or user interaction with the working tree.

**In-memory repositories** (`SourceMemory`) live entirely in RAM via `go-git`'s `memfs`/`memory.Storage`. Use these for temporary analysis or code generation where leaving filesystem artifacts is undesirable.

```go
// Clone a remote into RAM (no disk writes)
gitRepo, worktree, err := r.OpenInMemory(url, branch)

// Open an existing local repo
gitRepo, worktree, err := r.OpenLocal(path, branch)

// Polymorphic open — caller decides the storage type
gitRepo, worktree, err := r.Open(repo.SourceMemory, url, branch,
    repo.WithShallowClone(),
    repo.WithSingleBranch(),
)
```

### Clone Options

| Option | Effect |
|--------|--------|
| `WithShallowClone()` | Fetch only the latest commit (depth 1) |
| `WithSingleBranch()` | Limit fetch to the specified branch |
| `WithNoTags()` | Skip tag fetch |
| `WithRecurseSubmodules()` | Initialise submodules after clone |

### Bridged Filesystem Pattern

`AddToFS` copies a file from a `go-git` object into an `afero.Fs`. This lets you hydrate a virtual filesystem with files from any point in a repository's history.

```go
err := r.WalkTree(func(f *object.File) error {
    return r.AddToFS(props.FS, f, f.Name)
})
```

The target `afero.Fs` is typically `props.FS` — an `afero.MemMapFs` in tests, the real OS filesystem in production.

### Thread Safety

`go-git` is not thread-safe. For single-goroutine use, `*Repo` is sufficient. For concurrent access, wrap with `ThreadSafeRepo` which serialises all operations via `sync.Mutex`. Use `WithRepo` / `WithTree` (callback-style) to access the underlying `*git.Repository` or `*git.Worktree` safely. See the [Repo component reference](../components/vcs/repo.md#thread-safety) for details.

---

## `pkg/vcs/github` — GitHub Client

### Creating a Client

```go
import "github.com/phpboyscout/go-tool-base/pkg/vcs/github"

client, err := github.NewGitHubClient(cfg)
```

`NewGitHubClient` reads token and base-URL configuration from `cfg` and returns a `*GHClient` that satisfies `GitHubClient`.

`GetGitHubToken(cfg)` exposes the resolved token (PAT or environment variable) if you need it independently.

### GitHubClient Interface

The full interface covers PR management, repository setup, release discovery, and asset downloads:

```go
type GitHubClient interface {
    GetClient() *github.Client
    CreatePullRequest(ctx, owner, repo string, pull *github.NewPullRequest) (*github.PullRequest, error)
    GetPullRequestByBranch(ctx, owner, repo, branch, state string) (*github.PullRequest, error)
    AddLabelsToPullRequest(ctx, owner, repo string, number int, labels []string) error
    UpdatePullRequest(ctx, owner, repo string, number int, pull *github.PullRequest) (*github.PullRequest, *github.Response, error)
    CreateRepo(ctx, owner, slug string) (*github.Repository, error)
    UploadKey(ctx, name string, key []byte) error
    ListReleases(ctx, owner, repo string) ([]string, error)
    GetReleaseAssets(ctx, owner, repo, tag string) ([]*github.ReleaseAsset, error)
    GetReleaseAssetID(ctx, owner, repo, tag, assetName string) (int64, error)
    DownloadAsset(ctx, owner, repo string, assetID int64) (io.ReadCloser, error)
    DownloadAssetTo(ctx, fs afero.Fs, owner, repo string, assetID int64, filePath string) error
    GetFileContents(ctx, owner, repo, path, ref string) (string, error)
}
```

### GitHub Release Provider

```go
provider := github.NewReleaseProvider(client)
// provider implements release.Provider
latest, err := provider.GetLatestRelease(ctx)
```

`NewReleaseProvider` wraps a `GitHubClient` and returns a `release.Provider`. This is the recommended way to use release functionality — it keeps consuming code decoupled from the GitHub-specific client type.

---

## `pkg/vcs/gitlab` — GitLab Release Provider

```go
import "github.com/phpboyscout/go-tool-base/pkg/vcs/gitlab"

provider, err := gitlab.NewReleaseProvider(cfg)
// provider implements release.Provider
```

`NewReleaseProvider` reads GitLab token and endpoint configuration from `cfg`. The returned provider satisfies the same `release.Provider` interface as the GitHub provider — swap providers without changing the consuming code.

---

## Authentication

Token resolution is handled by `pkg/vcs/auth.go`. Two entry points:

```go
// Context-aware (preferred): honours caller deadlines for remote
// secret stores (Vault, AWS SSM) via the credentials backend.
token := vcs.ResolveTokenContext(ctx, cfg, "FALLBACK_ENV_VAR")

// Context-free shim for callers without ctx in scope. Uses
// context.Background() internally.
token := vcs.ResolveToken(cfg, "FALLBACK_ENV_VAR")
```

Both forms check, in order:

1. `auth.env` — name of an environment variable holding the token
2. `auth.keychain` — `"<service>/<account>"` reference resolved via the registered [credentials Backend](../components/credentials.md); silently skipped when no backend is registered
3. `auth.value` — literal token value stored in config
4. The named fallback environment variable — e.g. `GITHUB_TOKEN`

Returns empty when nothing is found; callers decide whether absence is an error.

For SSH operations (`OpenLocal`/`OpenInMemory` with SSH URLs), `repo.NewRepo` attempts:

| Priority | Method | Source |
|----------|--------|--------|
| 1 | SSH agent | Standard Unix SSH agent socket |
| 2 | Identity file | `github.ssh.key.path` in config |
| 3 | PAT / basic auth | Resolved via `ResolveTokenContext` |

---

## Design Goals

**Testability**
: Every interface (`RepoLike`, `GitHubClient`, `release.Provider`) has a mock in `mocks/`. In-memory storage (`SourceMemory`) enables integration-style tests without network access.

**Backend Agnosticism**
: Consuming code depends on `release.Provider`, not `*github.GHClient` or `*gitlab.GitLabReleaseProvider`. Switching from GitHub to GitLab releases is a one-line constructor change.

**`afero` Integration**
: `AddToFS` bridges the `go-git` object model into any `afero.Fs`, enabling consistent filesystem abstraction across production (OS) and test (memory-mapped) environments.

---

## Related Documentation

- **[VCS component reference](../components/vcs.md)** — full API reference for all VCS subpackages
- **[Interface Design](interface-design.md)** — `RepoLike` and `GitHubClient` in the interface hierarchy
- **[Auto-Update Lifecycle](auto-update.md)** — how `release.Provider` is used for version checks
