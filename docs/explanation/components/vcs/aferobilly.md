---
title: aferobilly — billy→afero adapter
description: A pure, reusable adapter that presents a go-billy/v5 Filesystem (e.g. a git worktree) as an afero.Fs, with per-operation locking so a live handle is safe for concurrent use. Backs the WorkFS()/WithWorkFS() worktree accessors in pkg/vcs/repo.
date: 2026-06-22
tags: [components, vcs, repo, afero, billy, filesystem]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# aferobilly — billy→afero adapter

`pkg/vcs/repo/aferobilly` adapts a [`go-billy/v5`](https://github.com/go-git/go-billy)
`Filesystem` to an [`afero.Fs`](https://github.com/spf13/afero). go-git worktrees
expose their filesystem as **billy**; most GTB consumers are written against
**afero**. This adapter bridges the two so afero-based code can read and write a
git worktree directly — single source of truth, no materialise/sync step.

It is the engine behind [`repo`'s `WorkFS()` / `WithWorkFS()` accessors](repo.md#live-worktree-as-aferofs),
but it is a **standalone, reusable package**: it works for *any* billy filesystem
(`memfs`, `osfs`, `chroot`), not just a worktree.

## API

```go
// New returns an afero.Fs backed by bfs.
func New(bfs billy.Filesystem, opts ...Option) afero.Fs

// WithLocker serialises every Fs and File operation through l (default: a no-op
// locker). Pass the mutex that guards the filesystem's producer to make a live
// handle concurrency-safe.
func WithLocker(l sync.Locker) Option
```

```go
import (
    "github.com/go-git/go-billy/v5/memfs"
    "gitlab.com/phpboyscout/go-tool-base/pkg/vcs/repo/aferobilly"
)

fs := aferobilly.New(memfs.New())            // a fresh afero.Fs over an in-memory billy FS
_ = afero.WriteFile(fs, "a/b.txt", data, 0o644)
```

## Concurrency: per-operation locking

The adapter takes a `sync.Locker` and acquires it around **every** `Fs`
operation **and every `File` operation** (`Read`/`Write`/`Seek`/`Close`/
`Truncate`/`ReadAt`/`WriteAt`/`Readdir`/`Stat`/…). This is what makes a live
handle safe:

- Default (no `WithLocker`): a **no-op** locker — correct for a single-threaded
  backing filesystem, where the caller owns any synchronisation.
- `WithLocker(mu)`: pass the mutex that also guards the filesystem's *producer*.
  For a `ThreadSafeRepo`, that is the repo's own mutex — so the returned
  `afero.Fs` serialises through the **same** lock go-git uses, and the handle can
  never touch the worktree unsynchronised, even though the reference escapes the
  call that created it.

> **Non-reentrancy.** Because the supplied locker is typically a plain (non-
> reentrant) `sync.Mutex`, a handle (and any file it opens) must **not** be used
> from inside a critical section that already holds the same locker — re-locking
> would deadlock. The adapter exposes no accessor for the underlying billy
> object, so the lock boundary cannot be bypassed.

Operations are **individually** atomic, but a *sequence* is not — with a shared
locker, another goroutine's operation can interleave between two calls on the
handle. When a sequence must be atomic, hold the lock around the whole sequence
yourself (the `repo` package offers [`WithWorkFS`](repo.md#live-worktree-as-aferofs)
for exactly this).

## Semantics & edge cases

billy and afero are mostly 1:1; the adapter resolves the mismatches:

| afero expectation | Behaviour |
|-------------------|-----------|
| `Mkdir(name, perm)` | Maps to billy `MkdirAll` (billy has no plain `Mkdir`): creates parents, idempotent — **not** afero's strict "fail on missing parent / existing target" |
| `RemoveAll` | `go-billy/v5/util.RemoveAll` (billy has no `RemoveAll`) |
| `Chmod` / `Chown` / `Chtimes` | No-ops (billy `memfs` ignores modes; failing would break callers that set perms) |
| `Open(dir)` | billy cannot open a directory, so a **synthesised read-only directory handle** is returned: `Readdir`/`Stat` work; byte-level ops return `*os.PathError` with "is a directory" |
| `File.Readdir(count)` | Snapshots billy's `ReadDir` once and paginates to `os.File` semantics (`count<=0` → all; `count>0` → up to count, `io.EOF` when exhausted; on a regular file → "not a directory") |
| `File.ReadAt` | Direct (`billy.File` guarantees `io.ReaderAt`) |
| `File.WriteAt` | Native `io.WriterAt` when the file provides it (memfs/osfs do); otherwise a seek-based emulation that restores the offset |
| Symlinks | `afero.Lstater` / `Symlinker` / `Linker` / `LinkReader` implemented by delegating to billy `Lstat`/`Symlink`/`Readlink` (note the `Symlink(target, link)` ↔ `SymlinkIfPossible(oldname, newname)` mapping) |
| `Name()` | The backing FS root (`bfs.Root()`), resolved once at construction |

## Relationship to `repo`

The pure adapter lives here; the worktree accessors live in `pkg/vcs/repo`
because `ThreadSafeRepo`'s mutex is unexported (only `repo` can supply `&mu` as
the `WithLocker`). See [Repo › Live worktree as `afero.Fs`](repo.md#live-worktree-as-aferofs)
and the design spec `docs/development/specs/2026-06-22-vcs-repo-afero-worktree.md`.

Contrast with [`AddToFS`](repo.md#bridge-to-afero), which *copies* a git tree
into a **separate** afero FS (one-shot extraction); this adapter is the **live**
view (continuous read/write against the worktree).
