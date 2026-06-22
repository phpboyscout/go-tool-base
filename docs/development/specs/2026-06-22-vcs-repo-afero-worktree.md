---
title: "Afero view of a vcs/repo worktree (safe billy→afero adapter)"
description: "Add a first-class afero.Fs view of a pkg/vcs/repo worktree so afero-based consumers can read/write an in-memory (or local) git worktree directly — single source of truth, no materialise/sync. A pure, locker-parameterised billy→afero adapter (new sub-package pkg/vcs/repo/aferobilly) plus WorkFS() / WithWorkFS() accessors and a WorktreeFS role on *Repo and *ThreadSafeRepo. The adapter locks every Fs AND File operation through a supplied sync.Locker, so a live handle is genuinely concurrency-safe on ThreadSafeRepo (it serialises through the same mutex go-git uses), not merely 'caller must synchronise'. Purely additive."
status: IMPLEMENTED
date: 2026-06-22
tags:
  - specification
  - vcs
  - repo
  - afero
  - filesystem
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
---

# Afero view of a `vcs/repo` worktree (safe billy→afero adapter)

Authors
:   Matt Cockayne

Date
:   22 June 2026

Status
:   IMPLEMENTED — originated from a keryx feature request (2026-06-22); all open questions resolved with the user (see [Resolutions](#resolutions)). Delivered as `pkg/vcs/repo/aferobilly` + the `WorktreeFS` role on `*Repo`/`*ThreadSafeRepo`.

---

## Summary

`pkg/vcs/repo` opens a repository in memory (`memfs`) or on disk (`osfs`); in
both cases the worktree is a go-git `*git.Worktree` whose `Filesystem` is a
**billy `Filesystem`**. Most GTB consumers are written against **`afero.Fs`**.
Today there is no first-class way to read/write that worktree through afero, so a
consumer must hand-roll a billy→afero bridge or use `AddToFS` to *materialise* the
tree into a separate `MemMapFs` and copy changes back before committing — two
sources of truth, easy to miss a file.

This spec adds a **first-class, concurrency-safe afero view of the worktree**:

1. A pure, reusable **billy→afero adapter** in a new sub-package
   `pkg/vcs/repo/aferobilly`, parameterised by a `sync.Locker`.
2. A **`WorktreeFS` role** (`WorkFS()` + `WithWorkFS()`) on `*Repo` and
   `*ThreadSafeRepo`, wiring the adapter to the repo's own lock.

The result: an afero-based consumer (e.g. the keryx studio committing on save over
an in-memory remote repo) writes through afero, the worktree genuinely changes,
and go-git stages and commits it like any repo — with **one** source of truth.

## Motivation

The concrete driver is the keryx studio authoring a reel project that lives as a
git remote, in memory, with no local checkout (keryx spec `0012-studio-remote-git`):
its workspace/reel layer and commit-on-save (`AddGlob` → `Status` → `Commit`) are
afero-based and must run verbatim over an in-memory repo. But this is **not
keryx-specific** — `vcs/repo` already hands every consumer a billy worktree, and
afero is the de-facto FS abstraction for Go services. A worktree-as-afero accessor
belongs next to the thing that produces the worktree, tested once in GTB.

### Why an adapter beats the `AddToFS` materialise pattern

`AddToFS(fs afero.Fs, gitFile *object.File, fullPath string)` (`repo.go:695`) copies
a git tree file **into a separate** afero FS — perfect for one-shot extraction,
wrong for continuous read/write: it creates a *second* copy, so a commit only sees
edits after an explicit, easy-to-miss write-back. The two are **complementary**:
keep `AddToFS` for extraction; add the adapter for live worktree work.

## Validation against the current code

The originating request was checked against the package; all load-bearing claims
hold, with two terminology corrections:

| Claim | Reality |
|-------|---------|
| `AddToFS(afero.Fs, *object.File, string)` | ✅ exact (`repo.go:695`) |
| `WithTree` guarded accessor, `ErrNoWorktree` sentinel | ✅ (`repo.go:264`, `:88`; `safe_repo.go:233`) |
| `RepoLike` role-interface-split pattern established | ✅ (spec 2026-06-15; 8 roles + composite) |
| go-billy/v5, afero, go-billy/v5/util available | ✅ (no `go.mod` change; `util` unimported today) |
| `InMemoryRepo`/`LocalRepo` are types | ⚠ they are `RepoType` **string constants**; the concrete types are `*Repo`/`*ThreadSafeRepo` |
| `OpenInMemory`/`Clone`/`SetTree` are constructors | ⚠ they are **methods** on `*Repo` |
| No existing billy→afero bridge | ✅ confirmed. The near-miss `go-billy/v5/helper/iofs.New` → `afero.FromIOFS` composition is **read-only** (`Create`/`OpenFile(write)`/`Mkdir`/`Remove`/`Rename` return not-implemented), so it cannot back a writable worktree view — the hand-written adapter is required |

## Design decisions

### D1 — The adapter is parameterised by a `sync.Locker` (the safety core)

The worktree's billy FS is part of the go-git worktree, and `ThreadSafeRepo`
serialises **all** go-git access through an exclusive `sync.Mutex` because *go-git
mutates internal caches even on reads* (`safe_repo.go:33-36`). An afero handle that
touched the worktree outside that mutex would be unsafe under concurrency.

**Decision:** the adapter takes a `sync.Locker` and acquires it around **every**
`Fs` operation **and every `File` operation** (Read/Write/Seek/Close/Truncate/
ReadAt/WriteAt/Sync/Readdir/Stat/…). Then:

- `*Repo.WorkFS()` supplies a **no-op locker** (`*Repo` is explicitly not safe for
  concurrent use; the caller owns synchronisation — `repo.go:247`).
- `*ThreadSafeRepo.WorkFS()` supplies **`&r.mu`** (a `*sync.Mutex` is a
  `sync.Locker`).

Consequence: a **live `WorkFS()` handle is genuinely concurrency-safe on
`ThreadSafeRepo`** — every use re-locks the same mutex go-git's own ops use, so the
escaped handle can never touch the worktree unsynchronised. This is strictly
stronger than the request's "document that the caller holds the lock" option, and
it is the recommended resolution of the request's central open question.

> **Packaging constraint (load-bearing):** `ThreadSafeRepo.mu` is **unexported**,
> so `&r.mu` can only be obtained inside package `repo`. Therefore the `WorkFS()`/
> `WithWorkFS()` *methods* must live in `package repo`; the *pure adapter* lives in
> the sub-package and is fed the locker by those methods. (See D2.)

### D2 — Pure adapter in a sub-package; accessors in `repo`

```
pkg/vcs/repo/aferobilly/        ← NEW: pure, reusable billy→afero adapter
  aferobilly.go                 ← New(billy.Filesystem, ...Option) afero.Fs
  aferobilly_test.go            ← conformance round-trips over memfs (no repo dep)
pkg/vcs/repo/
  worktree_fs.go                ← NEW: WorktreeFS role + WorkFS()/WithWorkFS() on *Repo
  safe_repo.go                  ← MODIFIED: WorkFS()/WithWorkFS() on *ThreadSafeRepo
```

The sub-package keeps the `repo` surface lean and makes the adapter reusable for
**any** billy FS (memfs/osfs/chroot), fully unit-testable without a repo. It reads
as a sibling of `AddToFS` conceptually while staying decoupled.

```go
// pkg/vcs/repo/aferobilly
func New(bfs billy.Filesystem, opts ...Option) afero.Fs
func WithLocker(l sync.Locker) Option   // default: a no-op locker (NOT nil)
```

### D3 — Two accessors: a per-op handle and an atomic callback

Both are offered because they serve different needs (validated by the concurrency
analysis):

```go
// WorktreeFS is a RepoLike role: an afero view of the active worktree.
type WorktreeFS interface {
    // WorkFS returns the active worktree's filesystem as an afero.Fs, or
    // ErrNoWorktree if no worktree is open. On *ThreadSafeRepo the returned FS
    // is concurrency-safe (each operation re-locks the repo mutex); operations
    // are individually atomic but a SEQUENCE is not — a concurrent Commit/Add
    // may interleave between two writes. Use WithWorkFS for atomic sequences.
    WorkFS() (afero.Fs, error)

    // WithWorkFS runs fn with an afero view of the worktree while holding the
    // repo lock for the whole callback, so the sequence is atomic relative to
    // other repo operations. fn MUST NOT retain the afero.Fs past return, call
    // any ThreadSafeRepo method, or use a WorkFS() per-op handle inside it
    // (the mutex is non-reentrant → deadlock).
    WithWorkFS(fn func(afero.Fs) error) error
}

func (r *Repo) WorkFS() (afero.Fs, error)
func (r *Repo) WithWorkFS(fn func(afero.Fs) error) error
func (r *ThreadSafeRepo) WorkFS() (afero.Fs, error)
func (r *ThreadSafeRepo) WithWorkFS(fn func(afero.Fs) error) error
```

`*Repo` and `*ThreadSafeRepo` get compile-time assertions
(`var _ WorktreeFS = (*Repo)(nil)` / `(*ThreadSafeRepo)(nil)`), matching the
established role convention (`repo.go:184-204`). `WorktreeFS` is **embedded into
the `RepoLike` composite** so consumers using the full role get it for free.

**When to use which:**
- `WorkFS()` handle — long-lived/streaming/caller-driven I/O where you don't
  control the call sequence (hand the `afero.Fs` to a templating or workspace
  library). Tolerates interleaving with commits.
- `WithWorkFS(fn)` — a sequence that must be atomic relative to `Commit`/`Add`
  (write a coherent set of files, then commit). Keep `fn` short; it blocks all
  other repo access.

### D4 — Hard safety rules the implementation MUST enforce

From the concurrency analysis (non-reentrant `sync.Mutex`):

1. **Every `File` op locks**, not just `Fs` ops — an open handle outlives the
   `Open` call, so a file that locked only `Open` would touch the worktree
   unsynchronised on later `Read`/`Write`.
2. **Never mix modes on one goroutine:** a `WorkFS()` per-op handle (or its files)
   **must not** be used inside a `WithWorkFS`/`WithTree`/`WithRepo` callback —
   that region already holds `mu`, and re-locking a non-reentrant mutex on the same
   goroutine deadlocks. Documented as loudly as the existing callback warning.
3. **No raw-billy escape hatch:** the returned `afero.Fs`/`File` must expose no
   method that hands back the underlying billy object, or the unsynchronised-escape
   hole reopens. The wrapper is what makes the escaped reference safe.
4. The no-op locker is a real value with no-op `Lock`/`Unlock`, **never `nil`**
   (a nil `sync.Locker` panics on `Lock`).

## Adapter semantics & edge cases

afero.Fs (13 methods) and afero.File (io.Closer/Reader/ReaderAt/Seeker/Writer/
WriterAt + Name/Readdir/Readdirnames/Stat/Sync/Truncate/WriteString) over a billy
`Filesystem` (`Basic`+`Dir`+`Symlink`+`TempFile`+`Chroot`) and `billy.File`
(Name/Write/Read/ReadAt/Seek/Close/Lock/Unlock/Truncate). The sharp edges, all
verified against go-billy v5.9.0 / afero v1.15.0:

| afero need | billy reality | Adapter behaviour |
|------------|---------------|-------------------|
| `Mkdir(name, perm)` | billy has **no plain `Mkdir`** — only `MkdirAll` | Map to `MkdirAll` (resolved OQ-4), documented: it creates parents and is idempotent — afero's strict "fail if parent missing / already exists" semantics are not reproduced. Acceptable for worktree editing, where `MkdirAll` is the norm. |
| `MkdirAll` | `MkdirAll` | Direct |
| `RemoveAll` | billy has no `RemoveAll` | `go-billy/v5/util.RemoveAll(bfs, path)` (takes `billy.Basic`) |
| `Chmod`/`Chown`/`Chtimes` | memfs ignores modes; `Lock` not advertised | **No-op returning `nil`** (failing would break afero callers that set perms; correct for worktree editing) |
| `File.Readdir`/`Readdirnames` | on the **Filesystem** (`ReadDir`), not the File | File wrapper retains the parent `billy.Filesystem` + its path; `Readdir` calls `bfs.ReadDir(path)` (returns `[]os.FileInfo`, afero's element type), applying afero's `count` semantics (`count>0` → up to count, `io.EOF` when exhausted; non-dir → error) |
| `File.ReadAt` | `billy.File` **guarantees** `io.ReaderAt` | Direct |
| `File.WriteAt` | `billy.File` interface does **NOT** include `io.WriterAt` (deferred to billy v6) — but concrete memfs/osfs files *do* implement it | Type-assert the billy file to `io.WriterAt`; if absent, **Seek-based emulation** (save offset → SeekStart(off) → Write → restore offset). Backend-agnostic; memfs/osfs satisfy the assertion |
| `File.Sync` | none | No-op `nil` (billy has no fsync), or delegate if the file implements an optional `Sync` |
| `File.WriteString` | none | `f.Write([]byte(s))` |
| `Name()` (Fs) | `bfs.Root()` | Return a stable label (`bfs.Root()`, else `"aferobilly"`) |
| Symlinks (optional afero `Lstater`/`Symlinker`/`Linker`/`LinkReader`) | billy `Filesystem` **always** has `Symlink` (Lstat/Symlink/Readlink); memfs **and** osfs support it | **Implement** them, delegating to billy. Mind the arg order: billy `Symlink(target, link)` ↔ afero `SymlinkIfPossible(oldname=target, newname=link)`. `LstatIfPossible` returns `(FileInfo, true, err)` (lstat *was* used) |

## Concurrency model (summary of the safety analysis)

- **No deadlock for the per-op handle:** billy file/FS ops never re-enter any repo
  method, so the lock taken inside `File.Read()` is released before any code that
  could re-acquire it runs.
- **Per-op = individually atomic, not sequence-atomic:** every billy op (and every
  `Commit`/`Add`, also under `mu`) serialises, so go-git never sees concurrent
  operations on itself — no cache corruption. A multi-write file update *can* have a
  commit land between writes (capturing a half-written file): a semantic atomicity
  gap, not corruption. `WithWorkFS` closes it when needed.
- **Handle escape is rendered harmless:** `WorkFS()` retains a reference but every
  use re-locks, satisfying the *spirit* of the no-retain contract (no
  unsynchronised access) even though the pointer escapes — provided D4.3 (no raw
  escape hatch) holds.

## Testing strategy

afero exposes no reusable conformance suite, so the adapter self-tests the
round-trips afero's own MemMapFs tests exercise — over `aferobilly.New(memfs.New())`,
table-driven, `t.Parallel()`:

- create→write→read; `Stat` (size/mode/name); `MkdirAll` → nested file →
  `Readdir`/`Readdirnames` (incl. `count` semantics + non-dir errors);
  `ReadAt`/`WriteAt` at offsets (random access; offset restored after `WriteAt`);
  `Truncate`; `Seek` (Set/Cur/End); `Rename` (old fails, new succeeds);
  `Remove`/`RemoveAll` on a tree; missing-file → `os.ErrNotExist`.
- **Locking:** a counting `sync.Locker` (records Lock/Unlock) asserts that **every**
  Fs and File op locks exactly once; a concurrent `-race` test drives the FS from
  multiple goroutines through a real mutex.
- **Symlink** round-trip (create symlink → `LstatIfPossible` reports symlink →
  `ReadlinkIfPossible` returns target).
- **`WriteAt` fallback:** a billy file lacking `io.WriterAt` (a tiny test double)
  exercises the Seek-based emulation path.
- **Integration** (`*_integration_test.go`, gated): `OpenInMemory` → `WorkFS()` →
  write `storyboard.json` via afero → `AddAll`/`Commit` → assert the commit tree
  contains the afero-written file (proves single-source-of-truth end-to-end).
  A `ThreadSafeRepo` variant drives `WorkFS()` concurrently with `Commit` under
  `-race`. A `WithWorkFS` test asserts the callback holds the lock and that a nested
  repo call would deadlock (documented, not executed).
- **Coverage:** ≥90% on `aferobilly` and the new `repo` methods (per `pkg/` policy).

## Backwards compatibility

Purely additive: a new sub-package, a new `WorktreeFS` role (embedded into
`RepoLike`), and `WorkFS()`/`WithWorkFS()` on `*Repo`/`*ThreadSafeRepo`. No existing
signature changes; `AddToFS` stays as the extraction helper. The `RepoLike` mock
(`mocks/pkg/vcs/repo/RepoLike.go`) gains two methods — hand-added to avoid a
full-tree mockery regen (per project convention).

## Implementation phases

1. **`aferobilly` adapter** — `New` + the Fs/File types, all edge cases (WriteAt
   fallback, Readdir-from-parent, RemoveAll, symlinks), locker-parameterised;
   conformance + locking + symlink + race tests. No repo dependency.
2. **`repo` wiring** — `WorktreeFS` role + assertions; `WorkFS()`/`WithWorkFS()` on
   `*Repo` (no-op locker) and `*ThreadSafeRepo` (`&r.mu`); embed into `RepoLike`;
   regenerate/hand-extend the `RepoLike` mock; integration tests.
3. **Docs** — `docs/components/vcs/` (or the repo component doc): the worktree-afero
   view, the per-op-vs-callback choice, and the safety rules (D4). Cross-reference
   `AddToFS` as the complementary extraction helper.

## Open questions (resolve before implementation)

1. **OQ-1 — `WorkFS()` *and* `WithWorkFS()`, or one?** Proposed: **both** (D3) —
   the handle for caller-driven I/O, the callback for atomic sequences. The
   synchronised adapter makes the handle safe on `ThreadSafeRepo`, so the handle is
   no longer a footgun, but the callback is still the right tool for atomic
   stage-and-commit.
2. **OQ-2 — Sub-package `aferobilly` vs in-`repo`.** Proposed: **sub-package** for
   the pure adapter (lean surface, reusable, repo-free tests); the accessors must
   stay in `repo` regardless (unexported `mu`).
3. **OQ-3 — Symlinks in v1.** Proposed: **include** them — both backends support
   symlinks, the delegation is clean, and a worktree can contain them; omitting
   would silently drop symlinked files for afero consumers.
4. **OQ-4 — `Mkdir` semantics.** Proposed: map to `MkdirAll` and **document** the
   relaxed semantics (creates parents, idempotent), since billy offers no plain
   `Mkdir`. Acceptable for worktree editing.
5. **OQ-5 — Where the `FEATURE-REQUEST-*.md` file lives.** It currently sits in the
   repo root (untracked). Proposed: delete it once this spec is approved (the spec
   supersedes it), or move it under `docs/development/` if a request archive is
   wanted.

## Resolutions

Confirmed with the user 2026-06-22:

1. **OQ-1 Accessors** — **both** `WorkFS()` and `WithWorkFS()` (D3): the per-op
   handle for caller-driven I/O, the callback for sequences that must be atomic
   relative to `Commit`/`Add`.
2. **OQ-2 Placement** — **sub-package `pkg/vcs/repo/aferobilly`** for the pure
   adapter; the `WorkFS()`/`WithWorkFS()` accessors stay in `package repo`
   (unexported `mu`).
3. **OQ-3 Symlinks** — **included** in v1: implement `Lstater`/`Symlinker`/
   `Linker`/`LinkReader` by delegating to billy (both memfs and osfs support them),
   minding the `Symlink(target, link)` ↔ `SymlinkIfPossible(oldname, newname)` arg
   order.
4. **OQ-4 `Mkdir`** — **map to `MkdirAll`**, documented: it creates parents and is
   idempotent rather than reproducing afero's strict "fail on missing parent /
   existing target" semantics. Simplest, matches billy's actual behaviour, and
   fine for worktree editing. (Revised from an earlier strict-emulation choice.)
5. **OQ-5 Request file** — **delete** `FEATURE-REQUEST-vcs-repo-afero-worktree.md`;
   this spec is the system of record.
