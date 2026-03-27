---
title: "VCS Repo Thread-Safety Wrapper"
description: "Remove GetRepo/GetTree from RepoLike, replace with WithRepo/WithTree on the interface, and introduce ThreadSafeRepo — an opt-in mutex-backed implementation that makes all RepoLike calls safe for concurrent goroutines."
date: 2026-03-25
status: IMPLEMENTED
tags:
  - specification
  - vcs
  - repo
  - concurrency
  - thread-safety
  - breaking-change
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-sonnet-4-6)
    role: AI drafting assistant
---

# VCS Repo Thread-Safety Wrapper

Authors
:   Matt Cockayne, Claude (claude-sonnet-4-6) *(AI drafting assistant)*

Date
:   25 March 2026

Status
:   IMPLEMENTED

---

## 1. Overview

The `go-git` library (`github.com/go-git/go-git/v5`) is inherently not thread-safe.
Its internal storage, object caches, and index state mutate freely during reads as well
as writes.  The upstream maintainers are aware of this limitation and there is no
remediation on the roadmap.

The `RepoLike` interface currently exposes `GetRepo() *git.Repository` and
`GetTree() *git.Worktree`.  These methods return raw pointers that escape any mutex
boundary the moment the method returns, making it structurally impossible to provide a
thread-safety guarantee while they remain on the interface.

This spec makes two related changes:

1. **Interface breaking change**: Remove `GetRepo` and `GetTree` from `RepoLike`.
   Replace them with `WithRepo` and `WithTree` — callback-style methods that execute
   under the caller's chosen locking strategy, keeping the raw pointer inside the
   critical section.

2. **New opt-in type**: Introduce `ThreadSafeRepo`, a `RepoLike`-satisfying wrapper
   around `*Repo` that holds a `sync.Mutex` and serialises every method call.
   `WithRepo` and `WithTree` on this type execute the callback while the lock is held.

The existing `Repo` type also gains `WithRepo` and `WithTree` (without locking), so it
continues to satisfy the updated interface.  All other `Repo` behaviour is unchanged.

### Migration cost

`GetRepo` and `GetTree` have five callers in the repository, all in test files within
`pkg/vcs/repo/`.  No external package declares a `RepoLike` parameter.  Migration is
mechanical and low-risk.

---

## 2. Goals & Non-Goals

### Goals

- Remove the structural impossibility of a thread-safe `RepoLike` by eliminating raw
  pointer getters from the interface.
- Provide `WithRepo` and `WithTree` on both `Repo` (no lock) and `ThreadSafeRepo`
  (mutex held for the duration of the callback).
- Provide `ThreadSafeRepo` as an opt-in, zero-cost-when-unused type.
- Achieve ≥ 90 % test coverage on new code, including race-detector tests.
- Regenerate the `RepoLike` mock after the interface change.

### Non-Goals

- Making `go-git` itself thread-safe.
- Preventing deliberate misuse (e.g. storing a pointer captured inside a `WithRepo`
  callback and using it after the callback returns) — this is documented, not enforced.
- Modifying the return signatures of `Open*` / `Clone` (the raw pointers returned from
  those methods are setup-time artefacts, not shared state).
- Adding concurrency primitives to `Repo` itself.

---

## 3. Public API

### 3.1 `RepoLike` interface changes

```go
type RepoLike interface {
    SourceIs(int) bool
    SetSource(int)

    SetRepo(*git.Repository)
    // GetRepo and GetTree are removed. Use WithRepo / WithTree instead.
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
    AddToFS(afero.Fs, *object.File, string) error

    // WithRepo acquires any necessary lock and calls fn with the *git.Repository.
    // Implementations must guarantee that no concurrent method call can observe
    // the repository while fn is executing.
    //
    // Returns ErrNoRepository if the repository has not been initialised.
    WithRepo(func(*git.Repository) error) error

    // WithTree acquires any necessary lock and calls fn with the *git.Worktree.
    // Implementations must guarantee that no concurrent method call can observe
    // the worktree while fn is executing.
    //
    // Returns ErrNoWorktree if the worktree has not been initialised.
    WithTree(func(*git.Worktree) error) error
}
```

**Removed from interface**: `GetRepo() *git.Repository`, `GetTree() *git.Worktree`

### 3.2 Sentinel errors (added to `repo.go`)

```go
var (
    // ErrNoRepository is returned by WithRepo when no *git.Repository has been set.
    ErrNoRepository = errors.New("repository not initialised; call Open, Clone, or SetRepo first")

    // ErrNoWorktree is returned by WithTree when no *git.Worktree has been set.
    ErrNoWorktree = errors.New("worktree not initialised; call Open, Clone, or SetTree first")
)
```

These errors are declared in `repo.go` so they are shared by both `Repo` and
`ThreadSafeRepo`.

### 3.3 `Repo` additions (no locking)

```go
// WithRepo calls fn with the underlying *git.Repository.
// Repo is not safe for concurrent use; callers are responsible for external
// synchronisation if sharing a *Repo across goroutines.
//
// Returns ErrNoRepository if the repository has not been initialised.
func (r *Repo) WithRepo(fn func(*git.Repository) error) error

// WithTree calls fn with the underlying *git.Worktree.
// Repo is not safe for concurrent use; callers are responsible for external
// synchronisation if sharing a *Repo across goroutines.
//
// Returns ErrNoWorktree if the worktree has not been initialised.
func (r *Repo) WithTree(fn func(*git.Worktree) error) error
```

`Repo.GetRepo()` and `Repo.GetTree()` are **removed**.

### 3.4 New type: `ThreadSafeRepo`

```go
// ThreadSafeRepo wraps a *Repo with a mutex so that all RepoLike methods are safe
// to call concurrently from multiple goroutines.
//
// # Thread-safety guarantee
//
// Every method acquires the internal mutex for its full duration.  Concurrent callers
// are serialised; no two calls to any method execute simultaneously.
//
// # WithRepo and WithTree
//
// These are the only way to interact with the underlying go-git objects safely.
// The callback executes while the mutex is held.  Callers must not:
//   - Retain the pointer after the callback returns.
//   - Call any ThreadSafeRepo method from inside the callback (deadlock).
//   - Spawn goroutines inside the callback that access the pointer after it returns.
//
// # go-git concurrency model
//
// go-git mutates internal caches during read operations.  ThreadSafeRepo uses
// sync.Mutex (exclusive) rather than sync.RWMutex; concurrent reads are not permitted.
type ThreadSafeRepo struct {
    mu   sync.Mutex
    repo *Repo
}
```

### 3.5 `ThreadSafeRepo` constructor

```go
// NewThreadSafeRepo creates a ThreadSafeRepo backed by a freshly constructed *Repo.
// The props and opts arguments have the same semantics as NewRepo.
func NewThreadSafeRepo(props *props.Props, opts ...RepoOpt) (*ThreadSafeRepo, error)
```

### 3.6 `ThreadSafeRepo` method overview

All `RepoLike` methods acquire `mu` before delegating to the inner `*Repo`, releasing on
return.  The table below lists every method and its critical-section content.

| Method | Lock | Notes |
|---|---|---|
| `SourceIs(int) bool` | exclusive | consistent with concurrent mutations |
| `SetSource(int)` | exclusive | |
| `SetRepo(*git.Repository)` | exclusive | |
| `SetKey(*ssh.PublicKeys)` | exclusive | |
| `SetBasicAuth(string, string)` | exclusive | |
| `GetAuth() transport.AuthMethod` | exclusive | returns interface value, not a raw go-git pointer |
| `SetTree(*git.Worktree)` | exclusive | |
| `Checkout(plumbing.ReferenceName) error` | exclusive | |
| `CheckoutCommit(plumbing.Hash) error` | exclusive | |
| `CreateBranch(string) error` | exclusive | |
| `Push(*git.PushOptions) error` | exclusive | |
| `Commit(string, *git.CommitOptions) (plumbing.Hash, error)` | exclusive | |
| `OpenInMemory(string, string, ...CloneOption) (*git.Repository, *git.Worktree, error)` | exclusive | raw pointers returned; safe for setup-time single-goroutine use |
| `OpenLocal(string, string) (*git.Repository, *git.Worktree, error)` | exclusive | same caveat |
| `Open(RepoType, string, string, ...CloneOption) (*git.Repository, *git.Worktree, error)` | exclusive | same caveat |
| `Clone(string, string, ...CloneOption) (*git.Repository, *git.Worktree, error)` | exclusive | same caveat |
| `WalkTree(func(*object.File) error) error` | exclusive | |
| `FileExists(string) (bool, error)` | exclusive | |
| `DirectoryExists(string) (bool, error)` | exclusive | |
| `GetFile(string) (*object.File, error)` | exclusive | |
| `AddToFS(afero.Fs, *object.File, string) error` | exclusive | |
| `WithRepo(func(*git.Repository) error) error` | exclusive | callback runs under lock |
| `WithTree(func(*git.Worktree) error) error` | exclusive | callback runs under lock |

> **Note on `Open*` / `Clone` return values**: These methods return raw pointers as a
> convenience for single-goroutine setup flows (e.g. open a repo then immediately add
> files in the same goroutine).  Do not share these pointers across goroutines; use
> `WithRepo` / `WithTree` for subsequent concurrent access.

---

## 4. Internal Implementation

### 4.1 `Repo.WithRepo` / `Repo.WithTree` (no lock)

```go
func (r *Repo) WithRepo(fn func(*git.Repository) error) error {
    if r.repo == nil {
        return ErrNoRepository
    }
    return fn(r.repo)
}

func (r *Repo) WithTree(fn func(*git.Worktree) error) error {
    if r.tree == nil {
        return ErrNoWorktree
    }
    return fn(r.tree)
}
```

### 4.2 `ThreadSafeRepo` delegation pattern

```go
func (r *ThreadSafeRepo) Checkout(branch plumbing.ReferenceName) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    return r.repo.Checkout(branch)
}
```

### 4.3 `ThreadSafeRepo.WithRepo` / `WithTree`

```go
func (r *ThreadSafeRepo) WithRepo(fn func(*git.Repository) error) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    if r.repo.repo == nil {
        return ErrNoRepository
    }
    return fn(r.repo.repo)
}
```

### 4.4 No re-entrancy

`sync.Mutex` is not re-entrant.  Calling any `ThreadSafeRepo` method from inside a
`WithRepo` or `WithTree` callback will deadlock.  This is documented in the type-level
godoc (§3.4) and in the method godoc.

---

## 5. Project Structure

```
pkg/vcs/repo/
├── repo.go                    MODIFIED — remove GetRepo/GetTree; add WithRepo/WithTree; add sentinel errors
├── repo_test.go               MODIFIED — 1 GetTree call → WithTree
├── repo_unit_test.go          MODIFIED — 4 GetRepo/GetTree calls → WithRepo/WithTree
├── repo_coverage_test.go      UNCHANGED
├── doc.go                     UNCHANGED
├── safe_repo.go               NEW — ThreadSafeRepo type and constructor
└── safe_repo_test.go          NEW — table-driven and race-detector tests

mocks/pkg/vcs/repo/
└── RepoLike.go                REGENERATED — run `just mocks` after interface change
```

---

## 6. Error Handling

All error creation and wrapping must use `github.com/cockroachdb/errors`.

| Scenario | Error |
|---|---|
| `NewRepo` fails inside `NewThreadSafeRepo` | propagate via `errors.WithStack` |
| `WithRepo` called before repo initialised | return `ErrNoRepository` |
| `WithTree` called before tree initialised | return `ErrNoWorktree` |
| Callback `fn` returns an error | propagate as-is |
| Any delegated `RepoLike` method error | propagate from inner `*Repo` as-is |

---

## 7. Testing Strategy

### 7.1 Updated tests in `repo_test.go` and `repo_unit_test.go`

The five existing callers of `GetRepo` / `GetTree` are migrated to `WithRepo` /
`WithTree`.  Example:

```go
// Before
assert.Equal(t, repo, r.GetRepo())

// After
_ = r.WithRepo(func(gr *git.Repository) error {
    assert.Equal(t, repo, gr)
    return nil
})
```

### 7.2 Unit tests for `Repo.WithRepo` / `Repo.WithTree` (in `repo_unit_test.go`)

| Test | Scenario |
|---|---|
| `TestRepo_WithRepo_NoRepo` | returns `ErrNoRepository` when `r.repo == nil` |
| `TestRepo_WithRepo_CallsFn` | fn receives correct pointer; no error |
| `TestRepo_WithRepo_PropagatesError` | fn error propagated |
| `TestRepo_WithTree_NoTree` | returns `ErrNoWorktree` when `r.tree == nil` |
| `TestRepo_WithTree_CallsFn` | fn receives correct pointer; no error |
| `TestRepo_WithTree_PropagatesError` | fn error propagated |

### 7.3 Unit tests for `ThreadSafeRepo` (`safe_repo_test.go`)

Table-driven, `t.Parallel()` throughout.  Use `logger.NewNoop()` for test loggers.

| Test | Scenario |
|---|---|
| `TestNewThreadSafeRepo_Success` | constructs successfully; inner `*Repo` not nil |
| `TestThreadSafeRepo_ImplementsRepoLike` | compile-time: `var _ RepoLike = (*ThreadSafeRepo)(nil)` |
| `TestThreadSafeRepo_SourceIs_SetSource` | round-trip under concurrent calls |
| `TestThreadSafeRepo_SetGetAuth` | `SetBasicAuth` / `GetAuth` round-trip |
| `TestThreadSafeRepo_SetRepo_SetTree` | `SetRepo` / `SetTree` store values accessible in `WithRepo` / `WithTree` |
| `TestThreadSafeRepo_WithRepo_NoRepo` | returns `ErrNoRepository` |
| `TestThreadSafeRepo_WithRepo_CallsFn` | fn called with correct pointer under lock |
| `TestThreadSafeRepo_WithRepo_PropagatesError` | fn error propagated |
| `TestThreadSafeRepo_WithTree_NoTree` | returns `ErrNoWorktree` |
| `TestThreadSafeRepo_WithTree_CallsFn` | fn called with correct pointer under lock |
| `TestThreadSafeRepo_WithTree_PropagatesError` | fn error propagated |

### 7.4 Concurrency / race tests (`safe_repo_test.go`)

Run with `go test -race ./pkg/vcs/repo/...`.

| Test | Scenario |
|---|---|
| `TestThreadSafeRepo_ConcurrentSetSource` | N goroutines call `SetSource` / `SourceIs`; no race |
| `TestThreadSafeRepo_ConcurrentSetGetAuth` | N goroutines call `SetBasicAuth` / `GetAuth`; no race |
| `TestThreadSafeRepo_ConcurrentWithRepo` | N goroutines call `WithRepo` on the same instance; no race |
| `TestThreadSafeRepo_ConcurrentSetRepo` | goroutines race `SetRepo` and `WithRepo`; no race |

All race tests use `sync.WaitGroup` with a fixed goroutine count (e.g. 10).

### 7.5 Quality gates

- Coverage ≥ 90 % on `safe_repo.go` (`just coverage`)
- `go test -race ./pkg/vcs/repo/...` — zero races
- `just lint` — zero warnings, no `//nolint` directives

---

## 8. Migration & Compatibility

This is a **breaking change** to the `RepoLike` interface.

| Change | Impact |
|---|---|
| `GetRepo()` removed from `RepoLike` | 5 test-only callers in `pkg/vcs/repo/` — migrated to `WithRepo` |
| `GetTree()` removed from `RepoLike` | 5 test-only callers in `pkg/vcs/repo/` — migrated to `WithTree` |
| `GetRepo()` removed from `Repo` concrete type | same 5 callers |
| `GetTree()` removed from `Repo` concrete type | same 5 callers |
| `WithRepo` / `WithTree` added to `RepoLike` | existing `Repo` implementation gains two new methods |
| `RepoLike` mock regenerated | run `just mocks` |

No external package uses `RepoLike` as an interface parameter.  The `mocks/` directory
is auto-generated and will be updated in Phase 1.

### Commit strategy

Each phase is one conventional commit.  Phase 1 (interface + `Repo` changes) is
committed before `ThreadSafeRepo` is introduced, so `just mocks` can be run against the
updated interface immediately.

---

## 9. Documentation Maintenance

### 9.1 `docs/components/vcs/repo.md`

- Remove `GetRepo` / `GetTree` from the API reference.
- Add `WithRepo` / `WithTree` to the API reference for both `Repo` and `ThreadSafeRepo`.
- Add a **Thread Safety** section covering:
  - When to use `ThreadSafeRepo` vs `Repo`.
  - `NewThreadSafeRepo` constructor and example.
  - `WithRepo` / `WithTree` usage examples.
  - The re-entrancy deadlock caveat.
  - A note that `Open*` / `Clone` return values are setup-time only.

### 9.2 `docs/concepts/vcs-repositories.md`

Add a short paragraph linking to the Thread Safety section of the component docs.

### 9.3 Godoc

All new public types, methods, variables, and sentinel errors must have accurate godoc
as described in §3.

---

## 10. Future Considerations

- **Context-aware locking**: a `ctx context.Context` variant of `WithRepo` / `WithTree`
  could let callers set a deadline on lock acquisition.
- **Read-safe go-git**: if go-git ever introduces internal synchronisation, the
  `sync.Mutex` in `ThreadSafeRepo` can be relaxed to `sync.RWMutex` without changing
  the public API.
- **Repo pool**: a `RepoPool` managing a fixed set of `ThreadSafeRepo` instances could
  serve high-concurrency scenarios more efficiently than a single shared instance.

---

## 11. Implementation Phases

### Phase 1 — Interface and `Repo` changes

1. Add `ErrNoRepository` and `ErrNoWorktree` to `repo.go`.
2. Remove `GetRepo()` and `GetTree()` from `RepoLike` and from `Repo`.
3. Add `WithRepo(func(*git.Repository) error) error` and `WithTree(...)` to `RepoLike`.
4. Implement `Repo.WithRepo` and `Repo.WithTree` (no lock).
5. Migrate the 5 test-only callers in `repo_test.go` and `repo_unit_test.go`.
6. Add unit tests for `Repo.WithRepo` / `Repo.WithTree`.
7. Run `just mocks` to regenerate `mocks/pkg/vcs/repo/RepoLike.go`.
8. Run `go test ./pkg/vcs/repo/...` — all tests must pass.

### Phase 2 — `ThreadSafeRepo` skeleton

1. Create `pkg/vcs/repo/safe_repo.go`.
2. Declare `ThreadSafeRepo` struct.
3. Add compile-time interface check: `var _ RepoLike = (*ThreadSafeRepo)(nil)`.
4. Implement `NewThreadSafeRepo`.
5. Write failing tests for the interface check and constructor.

### Phase 3 — `ThreadSafeRepo` full delegation

1. Implement all `RepoLike` methods on `ThreadSafeRepo` with the lock/unlock pattern.
2. Implement `WithRepo` and `WithTree` on `ThreadSafeRepo`.
3. Write and pass all unit tests from §7.3.

### Phase 4 — Race-detector tests and coverage

1. Write and pass all concurrency tests from §7.4.
2. Run `go test -race ./pkg/vcs/repo/...`.
3. Check coverage (`just coverage`); reach ≥ 90 % on `safe_repo.go`.
4. Run `just lint` — zero warnings.

### Phase 5 — Documentation

1. Update `docs/components/vcs/repo.md` as described in §9.1.
2. Update `docs/concepts/vcs-repositories.md` as described in §9.2.
3. Update this spec's status to `IMPLEMENTED`.
4. Run `/gtb-verify` for final sign-off.

---

## Verification

```bash
# Unit tests
go test ./pkg/vcs/repo/... -v

# Race detector (critical for this feature)
go test -race ./pkg/vcs/repo/...

# Regenerate mock after interface change
just mocks

# Coverage
just coverage

# Lint
just lint
```
