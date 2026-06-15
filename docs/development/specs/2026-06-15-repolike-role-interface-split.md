---
title: "RepoLike role-interface split — break the 23-method kitchen sink into focused roles"
description: "pkg/vcs/repo.RepoLike is a 23-method kitchen-sink interface mixing auth-config, lifecycle (open/clone), branch, commit, push, remote, and tree-inspection concerns. This spec analyses splitting it into focused role interfaces (reader / writer / brancher / remote / worktree …) following the pkg/controls precedent, keeping RepoLike as a composite that embeds them so the change is non-breaking, and decides whether to land the additive roles pre-1.0 now or stage the kitchen-sink reduction for v2."
status: APPROVED
date: 2026-06-15
tags:
  - specification
  - vcs
  - repo
  - architecture
  - idiomatic-go
  - interface-segregation
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude (claude-opus-4-8)
    role: AI drafting assistant
---

# RepoLike role-interface split

Authors
:   Matt Cockayne, Claude (claude-opus-4-8) *(AI drafting assistant)*

Date
:   2026-06-15

Status
:   APPROVED (open questions resolved in review 2026-06-15)

## Summary

`pkg/vcs/repo.RepoLike` is a **23-method "kitchen-sink" interface** that bundles seven unrelated concerns behind a single type: auth-credential mutation, repository lifecycle (open/clone), branch management, commit, push, remote management, working-tree inspection, and raw go-git escape hatches. A consumer that only wants to *read* a file from an in-memory tree must today depend on the full 23-method surface — including `Push`, `SetBasicAuth`, and `CreateBranch` — violating the interface-segregation principle and giving every consumer a far wider contract (and a far larger mock) than it uses.

The audit recommends splitting `RepoLike` into **focused role interfaces** (reader / writer / brancher / remote / worktree / auth …), mirroring the `pkg/controls` precedent where `Controllable` is composed from `Runner`, `HealthReporter`, `StateAccessor`, `Configurable`, and `ChannelProvider`, and the doc-comment explicitly tells callers to *"prefer the narrower interfaces where possible."*

This spec performs the impact analysis (exact method inventory, role grouping, consumer/usage data), proposes the role interfaces with `RepoLike` retained as a **composite that embeds them** — making the change additive and non-breaking — and lays out the genuine decision: **land the additive role interfaces pre-1.0 now** (so callers can begin narrowing immediately) **vs stage the split for v2**. The recommendation and open decisions are laid out for maintainer sign-off.

Finding addressed (from `docs/development/reports/codebase-audit-2026-06-12.md`, §3.4 Idiomatic Go, tabled in §3.5 Improvements):

- `repolike-kitchen-sink-weak-typing` — **Low, idiomatic-go / interface-segregation drift** (interface-split portion; the const/typed-`RepoType` and sentinel-guard portions were addressed in the provider-aware-repo-auth work — see Out of scope).

## Motivation

GTB recommends the narrow-interface pattern to downstream authors and follows it in `pkg/controls`. `RepoLike` is the most conspicuous exception: a single interface that has accreted every git operation the framework has ever needed. The cost is real:

1. **Dishonest contracts.** A function taking `RepoLike` to read a file advertises that it might `Push`, mutate auth, or create branches. The signature does not state what it touches.
2. **Over-large fakes.** `MockRepoLike` is a ~1370-line generated file. Any test that needs to fake *one* method inherits the whole surface. A reader-only consumer cannot get a one- or two-method fake.
3. **Mixed mutability and concern.** The interface co-locates pure-query methods (`FileExists`, `WalkTree`), destructive lifecycle (`Clone`, `OpenInMemory`), credential setters (`SetBasicAuth`, `SetKey`), and raw go-git escape hatches (`WithRepo`, `WithTree`) with no structure signalling which is which.

Splitting into roles makes each consumer's dependency self-documenting, shrinks the fakes it needs, and turns `RepoLike` from a flat list into a structured composite — at the cost of a wider type surface (more named interfaces) and a one-time naming/boundary decision.

## Impact analysis

### The exact method inventory

`RepoLike` (`pkg/vcs/repo/repo.go`) declares **23 methods**. Listed with signatures, grouped by the concern they actually serve:

**Auth / credential configuration (4)**

| Method | Signature |
|---|---|
| `SetKey` | `SetKey(*ssh.PublicKeys)` |
| `SetBasicAuth` | `SetBasicAuth(string, string)` |
| `GetAuth` | `GetAuth() transport.AuthMethod` |
| `SetRepo` | `SetRepo(*git.Repository)` |

**Source-state discriminator (2)**

| Method | Signature |
|---|---|
| `SourceIs` | `SourceIs(int) bool` |
| `SetSource` | `SetSource(int)` |

**Lifecycle — open / clone (4)**

| Method | Signature |
|---|---|
| `OpenInMemory` | `OpenInMemory(string, string, ...CloneOption) (*git.Repository, *git.Worktree, error)` |
| `OpenLocal` | `OpenLocal(string, string) (*git.Repository, *git.Worktree, error)` |
| `Open` | `Open(RepoType, string, string, ...CloneOption) (*git.Repository, *git.Worktree, error)` |
| `Clone` | `Clone(string, string, ...CloneOption) (*git.Repository, *git.Worktree, error)` |

**Worktree / checkout state (3)**

| Method | Signature |
|---|---|
| `SetTree` | `SetTree(*git.Worktree)` |
| `Checkout` | `Checkout(plumbing.ReferenceName) error` |
| `CheckoutCommit` | `CheckoutCommit(plumbing.Hash) error` |

**Branch (1)**

| Method | Signature |
|---|---|
| `CreateBranch` | `CreateBranch(string) error` |

**Write — commit / push (2)**

| Method | Signature |
|---|---|
| `Commit` | `Commit(string, *git.CommitOptions) (plumbing.Hash, error)` |
| `Push` | `Push(*git.PushOptions) error` |

**Tree inspection — read-only queries over the committed tree (5)**

| Method | Signature |
|---|---|
| `WalkTree` | `WalkTree(func(*object.File) error) error` |
| `FileExists` | `FileExists(string) (bool, error)` |
| `DirectoryExists` | `DirectoryExists(string) (bool, error)` |
| `GetFile` | `GetFile(string) (*object.File, error)` |
| `AddToFS` | `AddToFS(afero.Fs, *object.File, string) error` |

**Raw go-git escape hatches (2)**

| Method | Signature |
|---|---|
| `WithRepo` | `WithRepo(func(*git.Repository) error) error` |
| `WithTree` | `WithTree(func(*git.Worktree) error) error` |

That is **23 methods** spanning **7 concerns**.

**Two source notes that bear on the split:**

1. **Sentinel-guard status (provider-aware-repo-auth work).** The recent provider-aware-repo-auth change added `ErrNoRepository` / `ErrNoWorktree` sentinel guards to the methods that deref `r.repo` / `r.tree`. `WithRepo`, `WithTree`, `Checkout`, `CheckoutCommit`, `CreateBranch`, `Push`, `Commit`, and the tree-inspection methods (via the `headTree()` helper, which returns `ErrNoRepository`) now return a sentinel rather than panicking. The lifecycle setters/getters (`SetSource`, `SourceIs`, `Set*`, `GetAuth`) have nothing to guard. **The split must not regress these guards** — each method keeps its current body; it is only *re-grouped*, not rewritten.
2. **`CreateRemote` and `Remote` are NOT on the interface.** The concrete `*Repo` also implements `CreateRemote(string, []string) (*git.Remote, error)` and `Remote(string) (*git.Remote, error)` (both sentinel-guarded), but **neither appears in `RepoLike`**. So a "remote" role would today have *zero* interface methods to draw from unless we also promote these two into the interface surface — a decision in its own right (see O3). This is why the headline count is 23 (interface) even though the concrete type has 25 exported git methods.

### Proposed role grouping (the `pkg/controls` precedent)

`pkg/controls` is the in-repo precedent: `Controllable` is *composed* from `Runner`, `HealthReporter`, `StateAccessor`, `Configurable`, and `ChannelProvider`, each a single-concern interface, with the doc-comment *"Prefer using the narrower interfaces … where possible."* We mirror that exactly. Proposed roles:

| Role interface | Methods | Rough count | Concern |
|---|---|---|---|
| `TreeReader` | `WalkTree`, `FileExists`, `DirectoryExists`, `GetFile`, `AddToFS` | 5 | Read-only queries over the committed tree |
| `Opener` | `Open`, `OpenLocal`, `OpenInMemory`, `Clone` | 4 | Repository lifecycle / acquisition |
| `Authenticator` | `SetKey`, `SetBasicAuth`, `GetAuth`, `SetRepo` | 4 | Credential / repo-handle configuration |
| `WorktreeController` | `SetTree`, `Checkout`, `CheckoutCommit` | 3 | Working-tree / checkout state |
| `Committer` | `Commit`, `Push` | 2 | Write path (record + publish) |
| `Brancher` | `CreateBranch` | 1 | Branch creation |
| `SourceState` | `SourceIs`, `SetSource` | 2 | Backend discriminator (memory vs local) |
| `GitAccessor` | `WithRepo`, `WithTree` | 2 | Raw go-git escape hatches |
| *(deferred)* `RemoteManager` | `CreateRemote`, `Remote` | 2 | Named-remote management — **not defined now** (O3 resolved: concrete-only on `*Repo`; add only when a real consumer needs it) |

That is **8 roles** covering the existing 23 interface methods. `RemoteManager` is **not** introduced (O3 resolved): `CreateRemote`/`Remote` exist only on the concrete `*Repo`, not on `ThreadSafeRepo`, so the roles cover the common subset both implementers satisfy. Boundaries are deliberately concern-aligned, not size-balanced — `Brancher` is one method by design, exactly as `pkg/controls` keeps single-purpose roles.

`RepoLike` is then **redefined as a composite that embeds every role**, identical in shape to `Controllable`:

```go
// RepoLike is the full git-repository interface, composed of focused role
// interfaces. Prefer the narrower roles (TreeReader, Committer, …) where possible.
type RepoLike interface {
    SourceState
    Authenticator
    Opener
    WorktreeController
    Brancher
    Committer
    TreeReader
    GitAccessor
    // RemoteManager is NOT embedded (O3 resolved): CreateRemote/Remote stay
    // concrete-only on *Repo, since ThreadSafeRepo does not wrap them.
}
```

The method *set* of `RepoLike` is unchanged (same 23 methods), so `*Repo` and `*ThreadSafeRepo` still satisfy it with no code change.

### Consumers and actual usage — the headline finding

This is the load-bearing data point, and it is decisive for the timing question.

`grep -rn "RepoLike" --include=*.go` across the whole tree finds references in exactly **three places**:

1. `pkg/vcs/repo/repo.go` — the interface definition and the `// Repo implements RepoLike` comment.
2. `pkg/vcs/repo/doc.go` — a doc reference.
3. `mocks/pkg/vcs/repo/RepoLike.go` — the generated `MockRepoLike`.

There are **zero external consumers**. No function, method, struct field, or constructor anywhere in `pkg/`, `internal/`, `cmd/`, or the test tree takes `RepoLike` as a parameter, stores it as a field, or returns it. Searches for `repo.NewRepo`, `repo.NewThreadSafeRepo`, `repo.RepoLike`, and `MockRepoLike` outside the package itself all return empty. The concrete `*Repo` / `*ThreadSafeRepo` are constructed and used directly where the repo layer is exercised; the interface exists purely as a contract/mock target and is not yet a dependency-injection seam anywhere.

**What this means for the split:**

- **There is no caller to narrow today.** The interface-segregation *payoff* (a reader-only consumer depending on `TreeReader`) is currently hypothetical — there are no consumers to benefit. The roles would be *available* for the first real consumer rather than fixing an existing pain point.
- **The split is essentially free of caller churn.** With zero consumers, splitting `RepoLike` into a composite-of-roles touches only the interface definition (and, if we add per-role mocks, the mockery config). No call site changes.
- **The mock surface is the only concrete beneficiary today.** Per-role mockery interfaces (e.g. `MockTreeReader`) would let *future* tests fake a narrow role instead of the 1370-line `MockRepoLike`. That benefit is real but only realised when a narrow consumer exists.

### Backward-compatibility and `apidiff` assessment

The composite-retention approach makes this **non-breaking**, precisely:

- **`*Repo` / `*ThreadSafeRepo` still satisfy `RepoLike`.** Because `RepoLike`'s method set is unchanged (it now lists the same 23 methods via embedded roles instead of inline), the concrete implementers satisfy it with zero edits. The existing compile-time satisfaction (implicit, via usage) is preserved.
- **Existing `RepoLike` value/param holders are unaffected.** There are none today, but were a downstream to hold a `RepoLike`, the redefinition is invisible to it — same method set, same name.
- **The new role interfaces are purely additive.** `apidiff` reports them as **new exported types** (compatible additions), not as changes to an existing symbol. `RepoLike` itself shows as "definition changed" only in the sense that its declaration now embeds named interfaces — its method set is byte-for-byte the same, so it remains assignable both ways. No method is added, removed, or re-typed.
- **Pre-1.0 context.** Even if `apidiff` flagged anything, the project is `v0.x` and the `apidiff` CI job is **advisory / `allow_failure: true`** — a break would ship as a *minor* bump, not a major. But the design intent here is **no break at all**: additive roles + an unchanged-method-set composite.

The only non-additive *option* is the v2 direction (O4): dropping methods *out* of `RepoLike` so it is no longer a god-interface but itself a small role-set. That **would** be breaking (it removes methods from the named type) and is explicitly **not** proposed for the pre-1.0 line.

### Test / mock impact

`mockery` currently generates `MockRepoLike` from the one interface. After the split we can either (a) keep generating only `MockRepoLike` (the composite) — zero new mock files, existing tests unchanged — or (b) additionally generate a mock per role so future narrow consumers get narrow fakes. Option (b) adds generated files but no hand-written test churn; existing tests that use `MockRepoLike` keep working because the composite mock is unchanged. This is opt-in upside, not a tax (see O5).

## Design decisions

> These are **proposals for review**, not yet ratified. Each is paired with an open question where the maintainer's call is genuinely needed.

### D1 — Eight concern-aligned roles, composite `RepoLike` retained

Split into the **8 roles** in the table above (`TreeReader`, `Opener`, `Authenticator`, `WorktreeController`, `Committer`, `Brancher`, `SourceState`, `GitAccessor`), and **keep `RepoLike` as the composite embedding all of them** — directly mirroring `controls.Controllable`. Boundaries follow concern, not size. (See O1, O2.)

### D2 — Names follow the `controls` `-er` / role-noun convention

Use behavioural role nouns consistent with `pkg/controls` (`Runner`, `HealthReporter`, `Configurable`). `TreeReader`, `Committer`, `Brancher`, `Opener`, `Authenticator`, `WorktreeController`, `SourceState`, `GitAccessor`. Exact names are open (e.g. `Reader` vs `TreeReader`, `Lifecycle` vs `Opener`). (See O2.)

### D3 — `RepoLike` keeps its current method set (no `RemoteManager` defined yet)

Do **not** promote `CreateRemote` / `Remote` into `RepoLike` as part of the split — that would *change* the composite's method set (additive but a deliberate API growth) and is a separate decision from re-grouping the existing 23. Resolved (O3): do **not** define a `RemoteManager` role now either. `CreateRemote`/`Remote` exist only on the concrete `*Repo`, not on `ThreadSafeRepo`; promoting them would force speculative thread-safe wrappers (and a locking decision) and a mock regen for an interface with zero consumers. The roles therefore cover the **common subset** both `Repo` and `ThreadSafeRepo` satisfy; `CreateRemote`/`Remote` stay concrete-only on `*Repo`. Add a `RemoteManager` role + the `ThreadSafeRepo` wrappers only if/when a real consumer needs remote management through the interface. (See O3.)

### D4 — Land additive roles now; defer any method *removal* to v2

Land the **additive** part (the role interfaces + composite `RepoLike`) on the pre-1.0 line because it is non-breaking and free of caller churn. Defer the **subtractive** part — shrinking `RepoLike` itself so it is no longer a god-interface, forcing consumers onto roles — to **v2**, where removing methods from the named composite is an acceptable breaking change with a migration note. (See O1, O4.)

### D5 — Generate per-role mocks only if/when a narrow consumer lands

Keep generating `MockRepoLike` (the composite). Add per-role mockery interfaces lazily — when the first real narrow consumer is written — so we do not carry eight extra generated mock files with no test using them. (See O5.)

## Migration / compatibility

**Why additive roles + a composite `RepoLike` is non-breaking:**

- The composite's **method set is identical** before and after, so every existing and future implementer (`*Repo`, `*ThreadSafeRepo`, `MockRepoLike`) satisfies it unchanged.
- The role interfaces are **new exported types** — additive in `apidiff` terms, never a change to an existing symbol.
- No call site changes anywhere (there are no `RepoLike` consumers to update).

**What a caller does to narrow (once roles exist):**

A function that only reads files changes its parameter from `repo.RepoLike` to `repo.TreeReader`:

```go
// before
func indexTree(r repo.RepoLike) error { return r.WalkTree(...) }
// after — honest contract, narrow fake
func indexTree(r repo.TreeReader) error { return r.WalkTree(...) }
```

Callers still pass their `*Repo` / `*ThreadSafeRepo` (which satisfies `TreeReader` via the composite) with **no change at the pass-the-argument site** — exactly the `*Props`-satisfies-`LoggerProvider` ergonomics from the G1 provider-interface spec.

**v2 direction (not this line):** if v2 chooses to make `RepoLike` itself a small role-set (or remove it entirely in favour of roles), consumers holding `RepoLike` would migrate to the specific role(s) they use. That is a breaking change reserved for the major bump, with a `docs/migration/` entry.

## Pre-1.0-now vs stage-for-v2

This is the explicit decision the maintainer asked both G specs to surface.

**The case for landing the additive roles now (pre-1.0):**

- It is **non-breaking and caller-churn-free** — the roles are new types, `RepoLike` is an unchanged-method-set composite, and there are zero consumers to touch.
- It makes the narrow roles **available** so the *next* repo consumer (and the framework already has repo-layer growth on the roadmap per Plan 3 Phase 14) can depend on a role from day one instead of importing the kitchen sink and later refactoring.
- It is the **same pattern we already ship** in `pkg/controls`, so it is low-novelty and self-consistent — closing the "we don't dogfood our own narrow-interface advice" gap for the most egregious offender.
- The audit itself frames the fix as "consts now; **plan a v2 role-interface split**" — i.e. it explicitly anticipates the split direction; landing the *additive* half early is compatible with that framing.

**The case for staging the whole thing for v2:**

- With **zero current consumers**, the interface-segregation payoff is entirely prospective — we would be adding eight named types that *nothing uses yet*, which is its own (mild) API-surface smell.
- The genuinely valuable part — making `RepoLike` *stop* being a god-interface by removing methods from it — is **inherently breaking** and can only happen at v2 anyway. Landing only the additive half now produces a transitional state (a composite that still lists all 23 methods *and* eight roles) that some may read as clutter until a consumer narrows.
- The maintainer leaned **defer** previously, and the finding is **Low** severity.

**Recommendation:** land the **additive role interfaces + composite `RepoLike` now** (D1–D4), and stage the **subtractive** reduction of `RepoLike` for v2. *One-line why:* it is non-breaking and free, it mirrors the existing `controls` precedent, and it lets the next repo-layer consumer (Plan 14 work) be born narrow instead of being refactored later — while the only genuinely breaking part (shrinking the god-interface) correctly waits for the major bump. If the maintainer weights the "eight unused types" smell more heavily than the readiness benefit, the honest alternative is to **defer entirely to v2** and merely annotate `RepoLike` with a doc-comment pointing at the planned split. Both are defensible; the recommendation is *land-additive-now* by a margin, contingent on O1.

## Open questions

1. **O1 — Land additive roles now, or defer the whole split to v2?** The gating decision. *Drafting recommendation: land the additive roles + composite now; stage the subtractive reduction for v2.* If deferred, the finding closes with a doc-comment note on `RepoLike` only. *Resolved (2026-06-15):* **LAND** the additive role interfaces + keep `RepoLike` as a composite embedding them, now (non-breaking). The subtractive god-interface reduction (dropping methods from `RepoLike`) is staged for **v2**.
2. **O2 — Role boundaries and names.** Are the 8 roles (and their boundaries) right? Specifically: should `Authenticator` include `SetRepo` (repo-handle injection) or is that a `SourceState`/lifecycle concern? Should `AddToFS` (writes to an *afero* FS, reads from git) live in `TreeReader` or its own `TreeExporter`? Should `SetTree` sit with `WorktreeController` or `Authenticator`-style setters? And the names: `TreeReader` vs `Reader`, `Opener` vs `Lifecycle`, `GitAccessor` vs `RawAccessor`. (D1, D2) *Resolved (2026-06-15):* adopt the roles **as drafted** — `TreeReader`, `Opener`, `Authenticator`, `WorktreeController`, `Committer`, `SourceState`, `GitAccessor`, `Brancher` (boundaries and names as proposed in D1/D2).
3. **O3 — Promote `CreateRemote` / `Remote` into the interface?** They exist on `*Repo` but not on `RepoLike`. Define a `RemoteManager` role and (a) leave it out of the composite (recommended — re-grouping only), (b) add it to the composite (grows `RepoLike`'s method set — additive but a deliberate expansion), or (c) leave `CreateRemote`/`Remote` off the abstraction entirely. (D3) *Resolved (2026-06-15):* **NO — do not promote.** Investigation showed `CreateRemote`/`Remote` exist only on the concrete `*Repo`, **not** on `ThreadSafeRepo` (which is `{mu, repo *Repo}` with explicit per-method mutex delegation and never wrapped remote ops). Promoting them would force writing speculative thread-safe `ThreadSafeRepo` wrappers (plus a locking decision) and a mock regen for an interface with **zero** current consumers. The roles therefore cover the **common subset** both `Repo` and `ThreadSafeRepo` satisfy; `CreateRemote`/`Remote` stay concrete-only on `*Repo`. Add a `RemoteManager` role + the `ThreadSafeRepo` wrappers only if/when a real consumer needs remote management via the interface.
4. **O4 — Does the eventual v2 drop methods from `RepoLike`?** Confirm the v2 direction: make `RepoLike` a *small* composite (or remove it) and force consumers onto roles, vs keep it as a convenience god-composite indefinitely. This determines whether the now-additive roles are a stepping stone or the permanent end state. (D4) *Resolved (2026-06-15):* **out of scope** for this line — the method-dropping (god-interface shrink) is explicitly a future **v2** direction, not decided here.
5. **O5 — Per-role mocks now or lazily?** Generate `MockTreeReader` et al. up front (eight new generated files, no test using them yet) or only when the first narrow consumer lands (recommended). (D5) *Resolved (2026-06-15):* generate per-role mocks **lazily** — only when a consumer actually needs one — not all upfront.
6. **O6 — Split the concrete `Repo` too?** This spec splits only the *interface*. The concrete `*Repo` stays one struct (its fields — `source`, `config`, `repo`, `auth`, `tree` — are genuinely shared across roles). Confirm we are **not** splitting the concrete type, only exposing role-shaped views of it. (Recommended: interface-only.) *Resolved (2026-06-15):* `Repo` stays a **single concrete struct**; the segregation is interface-only.
7. **O7 — `ThreadSafeRepo` parity.** Confirm `ThreadSafeRepo` needs no change (it already implements every method and therefore every role); the only question is whether its doc-comment should reference the roles like `RepoLike` will. *Resolved (2026-06-15):* `ThreadSafeRepo` keeps **parity with the composite `RepoLike`** — it satisfies the same composite as `Repo` (the common subset; remote ops remain concrete-only on `*Repo` per O3).

## Verification plan

1. **Compiles unchanged** — after introducing the roles + composite, the whole tree builds with no edits outside `pkg/vcs/repo` (proving the zero-consumer / unchanged-method-set claims).
2. **Implementers still satisfy `RepoLike`** — the existing implicit satisfaction by `*Repo` and `*ThreadSafeRepo` holds; add explicit `var _ RepoLike = (*Repo)(nil)` / `(*ThreadSafeRepo)(nil)` and, optionally, `var _ TreeReader = (*Repo)(nil)` etc. compile-time checks.
3. **Sentinel guards preserved** — the existing `ErrNoRepository` / `ErrNoWorktree` tests still pass unchanged; re-grouping must not alter any method body.
4. **`apidiff` advisory reviewed** — confirms the diff is *only* new exported role types plus a `RepoLike` declaration change with an **identical method set** (no method added/removed/re-typed), i.e. additive.
5. **Narrow-fake demonstration (if landing now)** — at least one role gains a mockery mock and a unit test driven by it, to prove the segregation payoff is realisable.
6. **Lint** — `golangci-lint` clean; `just deadcode` confirms the new role types are not flagged once at least one is referenced (or are accepted as intentional exported API if landed ahead of consumers).
7. **Docs accuracy** — `docs/concepts/interface-design.md` and `docs/components/` (the vcs/repo component doc) updated to describe the roles and the "prefer the narrow role" guidance, cross-referenced to code.

## Out of scope

- **The non-interface parts of `repolike-kitchen-sink-weak-typing`.** The `int` source-state → typed consts, `type RepoType = string` → defined `RepoType string`, and mutable `var LocalRepo/InMemoryRepo` → consts were addressed in the provider-aware-repo-auth work; the sentinel-guard portion (`uninitialised-repo-methods-panic`) is likewise done. This spec is **interface split only**.
- **Behaviour changes.** No method body is rewritten; methods are only re-grouped under role interfaces.
- **Splitting the concrete `Repo` struct.** Interface-only — the struct stays one type (O6).
- **Removing methods from `RepoLike`** (the breaking, god-interface-shrinking step) — reserved for **v2** (O4), with a `docs/migration/` entry when it happens.
- **The provider-aware auth work** — done and `IMPLEMENTED`; this spec depends on its sentinel guards but does not touch them.

## Related

- [2026-06-12 codebase audit](../reports/codebase-audit-2026-06-12.md) §3.4 Idiomatic Go (finding detail) / §3.5 Improvements (tabled) — `repolike-kitchen-sink-weak-typing`
- [Plan 3 — improvements](../plans/2026-06-12-audit-plan-3-improvements.md) Phase 14 — `pkg/vcs` repo-layer evolution (the deferred-from-Plan-2 interface-split note)
- [2026-06-12 provider-aware repository auth](2026-06-12-provider-aware-repo-auth.md) — the sentinel-guard / consts / typed-`RepoType` work this spec builds on (IMPLEMENTED)
- [2026-06-15 provider-interface adoption](2026-06-15-props-provider-interface-adoption.md) — the sibling G-group spec applying the same narrow-interface / composite-satisfaction pattern to `pkg/props`
- `pkg/controls/controls.go` — the role-interface precedent (`Controllable` composed from `Runner`, `HealthReporter`, `StateAccessor`, `Configurable`, `ChannelProvider`)
- `pkg/vcs/repo/repo.go`, `pkg/vcs/repo/safe_repo.go`, `docs/concepts/interface-design.md`
- `CLAUDE.md` → API Stability (pre-1.0) — the advisory, non-blocking `apidiff` job
