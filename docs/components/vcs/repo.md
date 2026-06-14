---
title: Repo
description: Git repository operations with local and in-memory storage via go-git (pkg/vcs/repo).
date: 2026-03-25
tags: [components, vcs, git, repo, memfs, afero]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Repo

**Package:** `pkg/vcs/repo`

Provides git repository operations backed by `go-git`. Supports both local filesystem storage and in-memory storage (`memfs`), and integrates with `afero` for testable file operations.

---

## Constructor

```go
func NewRepo(props *props.Props, ops ...RepoOpt) (*Repo, error)
```

`NewRepo` reads authentication from `props.Config` and returns a configured `*Repo`. Authentication is resolved automatically per forge (see [Authentication](#authentication)) — you rarely need to call `SetKey` or `SetBasicAuth` directly.

**Options:**

```go
// Inject a custom go-git config (advanced — rarely needed)
func WithConfig(cfg *config.Config) RepoOpt
```

---

## Storage Strategies

### In-memory (`SourceMemory`)

The repository lives entirely in RAM using `go-git`'s `memfs`/`memory.Storage`. No files are written to disk.

```go
gitRepo, worktree, err := r.OpenInMemory(url, "main",
    repo.WithShallowClone(1),
    repo.WithSingleBranch("main"),
    repo.WithNoTags(),
)
```

Use for: temporary analysis, code generation, CI pipelines where disk artifacts are undesirable.

### Local filesystem (`SourceLocal`)

Opens an existing repository with `git.PlainOpen`, or initialises a new one if the directory does not contain a git repo.

```go
gitRepo, worktree, err := r.OpenLocal("/path/to/repo", "main")
```

Use for: persistent working trees, development tools that need a full working directory.

### Clone to disk

`Clone` is distinct from `OpenLocal` — it clones a remote URL to a target path.

```go
gitRepo, worktree, err := r.Clone(url, "/path/to/target",
    repo.WithShallowClone(1),
    repo.WithNoTags(),
)
```

### Polymorphic open

`Open` dispatches to `OpenLocal` or `OpenInMemory` based on the `RepoType` value (a defined string type):

```go
gitRepo, worktree, err := r.Open(repo.InMemoryRepo, url, "main",
    repo.WithShallowClone(1),
)
```

---

## Clone Options

| Function | Effect |
|----------|--------|
| `WithShallowClone(depth int)` | Fetch only the last `depth` commits |
| `WithSingleBranch(branch string)` | Limit fetch to the named branch |
| `WithNoTags()` | Skip tag fetch |
| `WithRecurseSubmodules()` | Initialise submodules after clone |

---

## Branch Operations

```go
// Create a branch (checks out an existing branch and pulls if it already exists)
err := r.CreateBranch("feature/my-feature")

// Checkout an existing branch
err := r.Checkout(plumbing.NewBranchReferenceName("main"))

// Detached HEAD at a specific commit
err := r.CheckoutCommit(hash)
```

---

## Commit and Push

```go
// Create a commit on the current worktree
hash, err := r.Commit("chore: update generated files", &git.CommitOptions{
    Author: &object.Signature{
        Name:  "My Tool",
        Email: "tool@example.com",
        When:  time.Now(),
    },
})

// Push with the pre-configured auth
err := r.Push(nil) // nil uses default PushOptions with repo auth
```

---

## Tree Operations

All tree operations work against the HEAD commit of the current repository state.

### Walk all files

```go
err := r.WalkTree(func(f *object.File) error {
    content, err := f.Contents()
    // process f.Name, content ...
    return err
})
```

### Check existence

```go
exists, err := r.FileExists("go.mod")
exists, err := r.DirectoryExists("pkg")
```

`DirectoryExists` returns true if any file under that path prefix exists — git has no directory objects.

### Retrieve a single file

```go
f, err := r.GetFile("config/defaults.yaml")
content, err := f.Contents()
```

### Bridge to afero

`AddToFS` copies a `go-git` file object into an `afero.Fs`. The target file is skipped if it already exists.

```go
err := r.WalkTree(func(f *object.File) error {
    return r.AddToFS(props.FS, f, f.Name)
})
```

This is the standard pattern for hydrating a virtual filesystem with files from any point in a repository's history.

---

## Authentication

`NewRepo` configures authentication from `props.Config` automatically, reading the config subtree of the tool's **forge**. The forge is derived from `Tool.ReleaseSource.Type` (`github`, `gitlab`, `bitbucket`, `gitea`, `codeberg`), overridable with the `vcs.provider` config key. An empty or `direct` type falls back to `github` — the `direct` release source is a download URL with no git remote, so it has no forge of its own.

| Priority | Condition | Auth method |
|----------|-----------|-------------|
| 1 | `<forge>.ssh.key.type = "agent"` | SSH agent (`ssh.DefaultAuthBuilder`) |
| 2 | `<forge>.ssh.key.path` or the env var named by `<forge>.ssh.key.env` | Identity file (`GetSSHKey`) |
| 3 | No `<forge>.ssh` config at all | Token via `vcs.ResolveToken` on the `<forge>` subtree (`auth.env` → `auth.keychain` → `auth.value` → `<FORGE>_TOKEN` env var) |

Token auth uses HTTP basic auth with a forge-appropriate username: `x-access-token` for GitHub (and unknown forges), `oauth2` for GitLab, `x-token-auth` for Bitbucket.

**Missing credentials are non-fatal for public repositories.** When no token resolves and `ReleaseSource.Private` is false, `NewRepo` proceeds with unauthenticated access (clones of public repos need no token). Only `Private: true` enforces a token, failing fast with a hint naming the fallback env var.

Existing `github.*` configs keep working unchanged — `github` was always the GitHub forge subtree; the other forges' subtrees are simply read alongside. No migration is required.

You can override auth manually after construction:

```go
r.SetKey(publicKeys)            // SSH
r.SetBasicAuth("user", "pass") // Basic / PAT
auth := r.GetAuth()            // Retrieve current method
```

### `GetSSHKey` / `GetSSHKeyWithPassphrase`

```go
func GetSSHKey(filePath string, localfs afero.Fs) (*ssh.PublicKeys, error)
func GetSSHKeyWithPassphrase(filePath string, localfs afero.Fs, passphrase string) (*ssh.PublicKeys, error)
```

Reads a PEM/OpenSSH private key from `localfs`. The library never blocks on a TUI: if the key is passphrase-protected, `GetSSHKey` returns an error wrapping the typed `*ssh.PassphraseMissingError` from `golang.org/x/crypto/ssh`. Interactive callers — the CLI layer, not this package — are responsible for detecting it, prompting the user, and retrying:

```go
keys, err := repo.GetSSHKey(path, fs)

var missing *gossh.PassphraseMissingError
if errors.As(err, &missing) {
    passphrase := promptUser() // CLI-layer TUI (e.g. huh input form)
    keys, err = repo.GetSSHKeyWithPassphrase(path, fs, passphrase)
}
```

The same typed error propagates out of `NewRepo` when the configured `<forge>.ssh.key` is encrypted.

---

## Source Constants

```go
const (
    SourceUnknown = iota // 0 — not yet configured
    SourceMemory         // 1 — in-memory storage
    SourceLocal          // 2 — filesystem storage
)

const (
    LocalRepo    RepoType = "local"
    InMemoryRepo RepoType = "inmemory"
)
```

`r.SourceIs(repo.SourceMemory)` and `r.SetSource(repo.SourceLocal)` are available for code that needs to branch on storage type.

---

## RepoLike Interface

All methods above are part of `RepoLike`. Functions that accept a repository should depend on `RepoLike` rather than `*Repo`:

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
    Push(*git.PushOptions) error
    Commit(string, *git.CommitOptions) (plumbing.Hash, error)
    OpenInMemory(string, string, ...CloneOption) (*git.Repository, *git.Worktree, error)
    OpenLocal(string, string) (*git.Repository, *git.Worktree, error)
    Open(RepoType, string, string, ...CloneOption) (*git.Repository, *git.Worktree, error)
    Clone(string, string, ...CloneOption) (*git.Repository, *git.Worktree, error)
    WalkTree(func(*object.File) error) error
    FileExists(string) (bool, error)
    DirectoryExists(string) (bool, error)
    GetFile(string) (*object.File, error)
    AddToFS(fs afero.Fs, gitFile *object.File, fullPath string) error
    WithRepo(func(*git.Repository) error) error
    WithTree(func(*git.Worktree) error) error
}
```

### Unopened-repo contract

Every `RepoLike` method that needs the underlying repository or worktree returns the sentinel `ErrNoRepository` / `ErrNoWorktree` when called before `Open*`/`Clone` (or `SetRepo`/`SetTree`) — none of them panic. Methods touching the worktree (`Checkout`, `CheckoutCommit`, `Commit`) return `ErrNoWorktree`; methods touching the repository (`Push`, `CreateBranch`, `WalkTree`, `FileExists`, `DirectoryExists`, `GetFile`, `CreateRemote`, `Remote`) return `ErrNoRepository`. Check with `errors.Is`.

---

## Thread Safety

`go-git` is not thread-safe — its internal caches mutate during reads. Two types are available depending on your concurrency needs:

### `Repo` (default, no locking)

Use `*Repo` when the repository is accessed from a single goroutine. `WithRepo` and `WithTree` simply call the callback with the underlying pointer:

```go
err := r.WithRepo(func(gr *git.Repository) error {
    head, err := gr.Head()
    // ...
    return err
})
```

Returns `ErrNoRepository` / `ErrNoWorktree` if the repository or worktree has not been initialised.

### `ThreadSafeRepo` (opt-in mutex wrapper)

Use `*ThreadSafeRepo` when sharing a repository across goroutines. Every method acquires an exclusive mutex for its full duration.

```go
ts, err := repo.NewThreadSafeRepo(props)

// Safe to call from multiple goroutines:
err = ts.WithRepo(func(gr *git.Repository) error {
    // mutex is held for the duration of this callback
    return nil
})
```

**Caveats:**

- Do not retain the pointer received in `WithRepo`/`WithTree` after the callback returns.
- Do not call any `ThreadSafeRepo` method from inside a callback — `sync.Mutex` is not re-entrant and this will deadlock.
- `Open*` / `Clone` return raw pointers for setup-time convenience. Do not share these across goroutines; use `WithRepo` / `WithTree` for subsequent concurrent access.

---

## Testing

Use the generated mock for unit tests:

```go
import mock_repo "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/vcs/repo"

mockRepo := mock_repo.NewMockRepoLike(t)
mockRepo.EXPECT().CreateBranch("feature/test").Return(nil)
mockRepo.EXPECT().Checkout(plumbing.NewBranchReferenceName("feature/test")).Return(nil)
```

For integration-style tests without network access, use `git.PlainInit` in a `t.TempDir()`:

```go
tmpDir := t.TempDir()
gitRepo, err := git.PlainInit(tmpDir, false)
// Add a commit, then call r.OpenLocal(tmpDir, "main")
```

Enable git progress output in tests by setting `GTB_GIT_ENABLE_PROGRESS=1`.

---

## Related Documentation

- **[VCS index](index.md)** — package overview and authentication helper
- **[GitHub](github.md)** — GitHub API client (separate from git operations)
- **[VCS Concepts](../../concepts/vcs-repositories.md)** — bridged filesystem pattern and storage strategy rationale
