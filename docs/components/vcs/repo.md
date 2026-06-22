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

### Init-only and repository discovery

`OpenLocal` conflates "opened an existing repo" with "initialised a new one" and
does not walk upward to find an enclosing `.git`. When a caller must keep those
two outcomes distinct — for example the generator's first-commit step, which
must never `git init` inside an existing tree — use the init-only primitives:

```go
// Read-only probe: walks upward from the path (git's own discovery semantics).
// A subdirectory of an existing repo reports true. Never creates a repo.
found, err := repo.DiscoverRepository("/path/to/dest")

// Init-only: initialises a fresh repo on the named branch (default "main"),
// or returns ErrAlreadyRepository if the path is already inside one.
gitRepo, worktree, err := r.InitLocal("/path/to/dest", "main")
```

`DiscoverRepository` and `InitLocal` are the counterparts to `OpenLocal`'s
init-if-absent behaviour. `InitLocal` returns
[`ErrAlreadyRepository`](#error-values) rather than silently opening an existing
repository, so the caller's init/open decision stays explicit.

### Gitignore-aware staging

`AddAll` stages the entire worktree honouring the repository's `.gitignore`
rules — build artefacts and other ignored paths are excluded, while the
`.gitignore` file itself is staged. It wraps go-git's
`Worktree.AddWithOptions{All: true}`; a plain `Add(".")` would *not* apply the
ignore patterns.

```go
err := r.AddAll() // ignored paths skipped; .gitignore committed
hash, err := r.Commit("chore: scaffold widget with gtb", &git.CommitOptions{Author: sig})
```

These three (`DiscoverRepository`, `InitLocal`, `AddAll`) compose the
[`Initializer`](#role-interfaces) role — deliberately *not* part of `RepoLike`,
since init/discovery and ignore-aware staging are first-commit concerns a
clone/checkout consumer never needs. Both `*Repo` and `*ThreadSafeRepo` satisfy
`Initializer`.

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

### Live worktree as `afero.Fs`

`AddToFS` *copies* tree files into a **separate** `afero.Fs` — ideal for one-shot
extraction, but it gives you two sources of truth (the copy and the worktree). For
**continuous read/write against the live worktree**, use `WorkFS()` / `WithWorkFS()`
(the `WorktreeFS` role). Files written through the returned `afero.Fs` *are* the
worktree, so `go-git` stages and commits them with no materialise/sync step:

```go
fs, err := r.WorkFS()              // afero view of the active worktree
_ = afero.WriteFile(fs, "storyboard.json", data, 0o644)
_ = r.AddAll()                     // stages the afero-written file
_, _ = r.Commit("save", opts)      // it's in the commit
```

This works for an in-memory repo (`memfs`) and a local one (`osfs`) alike — the
underlying billy filesystem is bridged by `pkg/vcs/repo/aferobilly` (a reusable
`billy.Filesystem` → `afero.Fs` adapter). `ErrNoWorktree` is returned if no
worktree is open.

**Concurrency.** On `ThreadSafeRepo`, the `WorkFS()` handle is safe for concurrent
use: every operation re-locks the repo mutex, so the handle can never touch the
worktree unsynchronised. Operations are individually atomic, but a *sequence* is
not — a concurrent `Commit`/`AddAll` may interleave between two writes. When a
sequence must be atomic (write a coherent set of files, then commit), use
`WithWorkFS(fn)`, which holds the lock for the whole callback:

```go
err := ts.WithWorkFS(func(fs afero.Fs) error {
    _ = afero.WriteFile(fs, "a.json", a, 0o644)
    _ = afero.WriteFile(fs, "b.json", b, 0o644)
    return nil
})
```

> **Do not** use a `WorkFS()` handle (or a file it opened) from inside a
> `WithWorkFS` / `WithTree` / `WithRepo` callback — that region already holds the
> (non-reentrant) repo mutex, so re-locking would deadlock. The adapter exposes no
> accessor for the underlying billy object, so the lock boundary cannot be bypassed.

Semantics worth noting: `Chmod`/`Chown`/`Chtimes` are no-ops (billy ignores modes);
`Mkdir` behaves like `MkdirAll` (billy has no plain `Mkdir`); symlinks are
supported via afero's optional `Symlinker` interface.

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

`RepoLike` is a **composite** built by embedding eight focused **role interfaces**,
mirroring the `pkg/controls.Controllable` precedent. Its method set is exactly the
sum of the embedded roles — unchanged from the previous flat declaration — so
`*Repo`, `*ThreadSafeRepo`, and the generated `MockRepoLike` satisfy it without any
code change.

```go
type RepoLike interface {
    TreeReader
    Opener
    Authenticator
    WorktreeController
    Committer
    SourceState
    GitAccessor
    Brancher
}
```

### Role interfaces

Prefer depending on the **narrowest role** that covers what a function actually
touches, rather than the full `RepoLike`. A reader that only inspects the tree
should take `TreeReader`, not `RepoLike` — the signature then documents what it
touches and needs only a narrow fake. The concrete types satisfy every role via
the composite, so callers still pass their `*Repo` / `*ThreadSafeRepo` unchanged.

| Role | Methods | Concern |
|------|---------|---------|
| `TreeReader` | `WalkTree`, `FileExists`, `DirectoryExists`, `GetFile`, `AddToFS` | Read-only queries over the committed tree |
| `Opener` | `Open`, `OpenLocal`, `OpenInMemory`, `Clone` | Repository lifecycle / acquisition |
| `Authenticator` | `SetKey`, `SetBasicAuth`, `GetAuth`, `SetRepo` | Credential / repo-handle configuration |
| `WorktreeController` | `SetTree`, `Checkout`, `CheckoutCommit` | Working-tree / checkout state |
| `Committer` | `Commit`, `Push` | Write path (record + publish) |
| `SourceState` | `SourceIs`, `SetSource` | Backend discriminator (memory vs local) |
| `GitAccessor` | `WithRepo`, `WithTree` | Raw go-git escape hatches |
| `Brancher` | `CreateBranch` | Branch creation |
| `WorktreeFS` | `WorkFS`, `WithWorkFS` | Live worktree as an `afero.Fs` (see [Live worktree as afero.Fs](#live-worktree-as-aferofs)) |
| `Initializer` | `InitLocal`, `AddAll` | Init-only + gitignore-aware staging (first-commit path) |

`Initializer` is satisfied by both concrete types but is **not embedded in
`RepoLike`** — init/discovery and ignore-aware staging are scaffold-time
concerns a clone/checkout consumer never touches, so callers that need them
(the generator git step) depend on the narrow role directly. The package-level
`DiscoverRepository` helper is the read-only discovery probe that complements
`InitLocal`.

```go
// before — advertises Push, auth mutation, branch creation it never uses
func indexTree(r repo.RepoLike) error { return r.WalkTree(...) }

// after — honest, narrow contract; same *Repo passed at the call site
func indexTree(r repo.TreeReader) error { return r.WalkTree(...) }
```

The named-remote operations `CreateRemote` / `Remote` are **concrete-only on
`*Repo`** — they are deliberately not part of any role or of `RepoLike`, because
`ThreadSafeRepo` does not wrap them. Reach for `*Repo` directly when you need them.

> Per-role mocks (`MockTreeReader`, …) are **not** generated yet; they will be
> added lazily when the first narrow consumer needs one. Today only the composite
> `MockRepoLike` exists.

### Unopened-repo contract

Every `RepoLike` method that needs the underlying repository or worktree returns the sentinel `ErrNoRepository` / `ErrNoWorktree` when called before `Open*`/`Clone` (or `SetRepo`/`SetTree`) — none of them panic. Methods touching the worktree (`Checkout`, `CheckoutCommit`, `Commit`) return `ErrNoWorktree`; methods touching the repository (`Push`, `CreateBranch`, `WalkTree`, `FileExists`, `DirectoryExists`, `GetFile`, `CreateRemote`, `Remote`) return `ErrNoRepository`. Check with `errors.Is`.

### Error values

| Sentinel | Returned by | Meaning |
|----------|-------------|---------|
| `ErrNoRepository` | repository-touching methods | called before the repo was opened/initialised |
| `ErrNoWorktree` | worktree-touching methods | called before the worktree was bound |
| `ErrAlreadyRepository` | `InitLocal` | the target path is already inside a git repository — init refused |

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
