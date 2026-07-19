---
title: "VCS stack extraction plan — go/repo, go/forge and the forge providers"
description: "Sequencing plan for decomposing pkg/vcs into independent modules. The current tree is not one component but three: git operations (repo), forge/release operations (forge + per-provider modules), and an afero-billy bridge that is not VCS at all. The repo-to-forge dependency is not merely separable but eliminable by injecting a forge name and token, so repo ships with zero forge dependency. Every provider becomes its own module — including the SDK-free ones — so adopting a vendor SDK later is a dependency change inside a module rather than a re-extraction. Direct stays in the core as its reference implementation."
date: 2026-07-19
status: IN PROGRESS
tags:
  - specification
  - vcs
  - repo
  - forge
  - releases
  - module-extraction
  - plan
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# VCS stack extraction plan — `go/repo`, `go/forge` and the forge providers

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   2026-07-19

Status
:   APPROVED (2026-07-19 — all open questions resolved with user)

Related
:   [module-extraction playbook](2026-07-12-go-module-extraction-playbook.md),
    [transport-stack extraction plan](2026-07-13-transport-stack-extraction-plan.md) (the precedent for a multi-module sequencing plan),
    [chat extraction](2026-07-05-chat-module-extraction.md) (the per-provider module precedent),
    [errorhandling](2026-07-18-errorhandling-module-extraction.md) / [config](2026-07-18-config-module-extraction.md) / [credentials](2026-07-18-credentials-module-extraction.md) (the prerequisites, all IMPLEMENTED),
    [extraction report](../reports/2026-07-07-package-extraction-report.md)

---

## Summary

`pkg/vcs` is the largest remaining extraction (~5000 LOC including tests) and the one
tool authors most want standalone: GitHub/GitLab release management and git repository
operations, usable without a framework.

The extraction report treats it as one component to be extracted in stages. **This plan
rejects that framing.** Examined closely, `pkg/vcs` is not one component with internal
structure; it is **three unrelated products sharing a directory**:

1. **Git operations** (`repo`, 1470 LOC) — clone, branch, commit, worktrees, over go-git.
2. **Forge/release operations** (`vcs` + `release` + five providers, ~2600 LOC) — a
   provider registry over release APIs.
3. **A billy→afero bridge** (`aferobilly`, 406 LOC) — not VCS at all; it presents a
   go-billy filesystem (such as a go-git worktree) as an `afero.Fs`.

The evidence for the split is in [§Evidence](#evidence-the-tree-is-three-products), and
it is stronger than "these could be separated": the coupling between (1) and (2) can be
**deleted**, not just moved.

This plan sequences the decomposition into **six modules**. It is a *plan*; each module
follows the [playbook](2026-07-12-go-module-extraction-playbook.md) §5 procedure, and
the phases below are the order and the boundaries, not a restatement of the procedure.

## Prerequisites — all met

The report listed `pkg/vcs`'s coupling as `config`, `credentials`, `errorhandling`,
`logger`, `props`, `telemetrytypes`, `version`. Of those, **`credentials`, `config` and
`errorhandling` are now standalone modules** (extracted 2026-07-18), and the logging seam
is already `*slog.Logger`. Nothing external blocks this work.

## Evidence: the tree is three products

### The `repo` → forge dependency is eliminable, not merely separable

`pkg/vcs/repo` imports `release` and `vcs`, which looks like git operations depending on
forge operations. It is not. The entire usage is:

```go
release.SourceTypeGitLab    → "oauth2"        // basic-auth username per forge
release.SourceTypeBitbucket → "x-token-auth"
release.SourceTypeDirect    → treated as GitHub
vcs.ResolveToken(settings.Auth, fallbackEnv)  // obtain a token
settings.ReleaseSource.Private                // fail fast for private repos
```

`repo` needs **a forge name (a string) and a token**. It never constructs a provider,
never calls a release API, never touches a vendor SDK. Inject those two values and the
dependency disappears entirely:

```go
type Settings struct {
    Forge   string // "github" | "gitlab" | "bitbucket" | …
    Token   string
    Private bool
    // … git-specific settings
}
```

That is the single most important design decision in this plan: **`go/repo` ships with no
dependency on `go/forge` at all.** Git-over-HTTPS needing credentials is not the same
thing as git depending on release APIs.

### The consumers are already disjoint

| Package | GTB consumers |
|---|---|
| `repo` | `internal/generator` (`gitinit.go`, `templatesource_clone_real.go`) — scaffolding |
| `release` + providers | `pkg/setup` (`update.go`, `update_signature.go`, `providers.go`), `pkg/props/tool.go`, `cmd/e2e` — self-update |

Two different subsystems, two different lifecycles. Nothing in GTB uses both together.

### The provider contract is already a plugin registry

```go
type Provider interface {
    GetLatestRelease(ctx, owner, repo) (Release, error)
    GetReleaseByTag(ctx, owner, repo, tag) (Release, error)
    ListReleases(ctx, owner, repo, limit) ([]Release, error)
    DownloadReleaseAsset(ctx, owner, repo, asset) (io.ReadCloser, string, error)
}

release.Register(sourceType string, factory ProviderFactory)  // init()-time
```

Plus an optional `ChecksumProvider` resolved by runtime type assertion. This is
structurally identical to `go/chat`'s provider registry, so per-provider modules need no
redesign — only a module boundary around what is already a plugin.

### `aferobilly` is not VCS

406 LOC, **zero** go-tool-base imports (in production *and* test code), and nothing to do
with version control: it adapts a `go-billy/v5` `Filesystem` to an `afero.Fs`. Its own
package doc says it "works for any billy filesystem (memfs, osfs, chroot)" — go-git's
worktree is merely the caller that happens to live next door.

It also carries a design worth preserving in its own right: every operation is serialised
through a configurable `sync.Locker`, and the returned `afero.Fs` deliberately exposes no
accessor for the wrapped billy object so the lock boundary cannot be bypassed. That makes
a live handle over a mutex-guarded worktree genuinely concurrency-safe — with the
corresponding footgun that a handle must not be used inside a critical section already
holding the same (non-reentrant) locker.

## Provider granularity: split every provider

**Decision: every forge provider becomes its own module**, including those that use no
vendor SDK today.

The dependency-weight argument alone would justify splitting only GitHub and GitLab:

| Provider | Vendor SDK today | LOC |
|---|---|---|
| GitHub | `google/go-github/v88`, `cli/oauth` | 582 |
| GitLab | `gitlab-org/api/client-go/v2` | 275 |
| Gitea/Codeberg | none — hand-rolled `net/http` | 324 |
| Bitbucket | none — hand-rolled `net/http` | 672 |
| Direct | none — plain HTTP downloads | 530 |

The decisive argument is **future SDK adoption**. A module boundary is expensive to add
later and cheap to add now; swapping a hand-rolled HTTP client for a vendor SDK *inside*
an existing module is a routine dependency change. And this is not hypothetical:

- **Gitea publishes an official Go SDK today** — [`gitea.dev/sdk`](https://pkg.go.dev/code.gitea.io/sdk/gitea)
  (formerly `code.gitea.io/sdk/gitea`; v0.25.1, May 2026). The current hand-rolled client
  could be replaced now.
- **Bitbucket** has no official Atlassian SDK, but mature community clients exist
  (e.g. [`ktrysmt/go-bitbucket`](https://github.com/ktrysmt/go-bitbucket) for Cloud v2.0).

Splitting only the currently-heavy providers would mean re-extracting Gitea the moment it
adopts its SDK. Consistency also removes a class of "why is this one different?" —
every provider is a module, no hidden mechanics.

### `direct` stays in the core

`direct` is the exception that proves the rule: it is **not a forge**. It fetches an asset
from a URL template, will never have an SDK, and is the fallback for tools with no forge
at all. Keeping it in `go/forge` makes it the contract's **reference implementation** —
every consumer gets a working provider with no additional module — and it is the natural
place to demonstrate `ChecksumProvider`, which it is the canonical user of.

## Target module map

| Module | LOC | Contents |
|---|---|---|
| **`go/repo`** | 1470 | go-git operations: clone, branch, commit, worktrees, safe-repo wrapper. `Forge`/`Token` injected — **zero forge dependency** |
| **`go/aferobilly`** | 406 | billy→afero bridge — standalone; useful to **any** `billy` consumer, not just go-git |
| **`go/forge`** | ~440 | `Provider`/`ChecksumProvider` contracts, registry, `ReleaseSourceConfig`, `SourceType*`, token resolution, the **`direct`** reference provider, `releasetest` |
| **`go/forge-github`** | 582 | go-github v88 + the `GHLogin` OAuth device flow |
| **`go/forge-gitlab`** | 275 | gitlab client-go/v2 |
| **`go/forge-gitea`** | 324 | registers **both** `gitea` and `codeberg` source types |
| **`go/forge-bitbucket`** | 672 | Bitbucket Cloud v2.0 |

Docs microsites per the naming convention: `repo.go.phpboyscout.uk`,
`forge.go.phpboyscout.uk`, etc. Provider modules are **README-only** (playbook §10.5) —
their docs live on the `forge` microsite.

### Where token resolution lands

`pkg/vcs` root (201 LOC) is really *forge credential resolution*: `ResolveToken`,
`ResolveTokenContext`, `TokenConfig`, `AuthConfig`. It belongs in **`go/forge`** — it
resolves *forge* credentials, and the providers are its main consumers. `go/repo` does not
take a dependency on it; GTB resolves the token and injects the string.

`ConfigFromContainable` is GTB glue (it returns a `release.Config` from a
`config.Containable`) and **stays in GTB**, alongside every other `config_adapter.go`.

### A note on the GitHub OAuth flow

`pkg/vcs/github/login.go` implements the GitHub OAuth **device flow** (`GHLogin`), used by
GTB's setup wizard — that is authentication, not release management, and it drags
`cli/oauth`. It travels with `go/forge-github` (it is GitHub-specific and belongs with the
GitHub client), but it is worth calling out because it means that module is "GitHub
integration", not strictly "GitHub releases".

## Sequencing

Dependency-forced, and deliberately front-loads the pieces that de-risk the rest:

**Phase 1 — `go/aferobilly`.** Zero go-tool-base coupling, zero risk. Warms up the procedure and
unblocks `repo`.

**Phase 2 — `go/repo`.** The dependency inversion (Forge/Token injected) happens **in
GTB first**, as an in-tree refactor with tests green, *before* the module boundary goes
up — cheaper to fix in-tree than across a published module (playbook §11.8). Then extract,
repointing `aferobilly` to its module and landing both cut-overs in one MR (`repo` is
`aferobilly`'s only consumer).

The inversion is confined to `config.go` and `repo.go`. Two `Settings` fields carry the
whole coupling — `ReleaseSource release.ReleaseSourceConfig` (only `.Type` and `.Private`
are read) and `Auth vcs.TokenConfig` (only fed to `vcs.ResolveToken`) — and the
`SourceType*` constants used for the basic-auth username are plain strings that become
repo's own.

**R8 — the token is injected as a lazy `TokenSource`, not a resolved string.** Token
resolution is currently *lazy*: SSH auth takes priority, and `vcs.ResolveToken` runs only
on the token path. Because that resolver **can reach the OS keychain**, having the adapter
eagerly resolve a `Token string` would make an SSH-configured repository perform a
keychain lookup it never needed — potentially an unlock prompt — which is a behaviour
regression tests would not catch. So:

```go
// TokenSource returns an access token, resolved lazily on first use.
type TokenSource func() string

// StaticToken adapts an already-resolved token to a TokenSource.
func StaticToken(t string) TokenSource
```

`repo` calls the source only where it calls `ResolveToken` today. Laziness is preserved
exactly, the forge dependency still disappears, and the adapter does not have to
replicate repo's SSH-priority branch (the alternative, rejected as fragile and
duplicated).

The inversion also sheds weight that is easy to miss: the TUI stack
(`charm.land/lipgloss`, `charm.land/log`) is in `repo`'s dependency graph today, arriving
via `config_adapter.go` → `props` → `logger`. That adapter stays in GTB, so the extracted
module drops charm entirely — and its depfootprint guard pins that.

**Phase 3 — `go/forge` core.** Contract, registry, config types, token resolution,
`direct`, `releasetest`. Must expose whatever the provider modules currently reach for
unexported — audit for a **provider-authoring API** before the providers move
(playbook §10.1).

**Phase 4 — the provider modules**, in ascending order of surprise:
`forge-gitlab` (smallest, has an SDK) → `forge-github` (SDK + OAuth) →
`forge-gitea` (two source types) → `forge-bitbucket` (largest, dual-credential model).

**Phase 5 — GTB cut-over.** One MR per module as each is published, following the
established clean-break pattern.

Phases 1–2 and 3–4 are independent tracks; only the GTB cut-over needs both.

## Cross-cutting concerns

**Mocks.** The tree ships 20 generated mocks (12 for `repo`, 6 for `release`, plus
`GitHubClient` and `TokenConfig`). Per the toolkit convention each module publishes its
own `mocks` subpackage and GTB consumes them; `mocks/pkg/vcs/**` is deleted.

**Version compatibility.** `forge` and its providers release in **lockstep at matching
minors**, with a compatibility matrix on the `forge` microsite (playbook §10.10). Cut the
core `v0.1.0` first; providers then drop their `replace` directives and release
(playbook §10.7). `go/repo` and `go/aferobilly` version independently — they share no API
with the forge line.

**Depfootprint guards.** `go/repo` must forbid `go/forge` and every forge SDK — that guard
is what keeps the inversion honest. `go/forge` core must forbid all vendor SDKs (the
providers hold them). Each provider allows exactly its own SDK.

**Test redistribution.** Expect this to be the bulk of the work (playbook §10.3). Build
the master `Test*`/`Example*` inventory up front and diff it after each move.

## Documentation: nothing left behind

The three extractions on 2026-07-18 established that the docs sweep is the step most
likely to lose value, and that a mechanical path-repoint is **not** sufficient. Each
found accumulated design intent stranded in GTB's docs, and between them surfaced **five
stale or incorrect claims** — two of which contradicted the code outright. This stack is
the largest surface yet, so the practice is made an explicit, per-phase deliverable.

### The audit, per module

Before a module's docs are considered done:

1. **Sweep the whole `docs/` tree**, not just the obvious component page — how-tos,
   concept pages, testing guides, `mocks.md`, migration notes and the CLI reference all
   reference this stack.
2. **Mine for intent, not just mechanics.** Specs, implementation notes and migration
   notes carry the *why*: rejected alternatives, invariants, footguns, and hard-won
   ordering rules. A microsite that documents only the API loses the reasoning that makes
   the API make sense. Read the spec trail for each area before writing.
3. **Verify every claim against the code.** Docs go stale silently. Where a doc and the
   implementation disagree, the code wins — correct the GTB doc as part of the cut-over
   rather than porting the error onto a new microsite.
4. **Classify each piece** as module-generic (moves) or GTB-specific (stays): `Props`/DI,
   GTB config-key schemas, CLI commands, the generator, and setup wizards stay.
5. **Audit deleted pages for loss.** Diff anything removed from GTB against the new
   microsite; content genuinely about the module that is missing must be authored, not
   dropped.
6. **Sweep links.** Deleting or stubbing a page breaks inbound links across the docset;
   finish at zero broken links.

Known intent to carry for this stack, as a starting point rather than an exhaustive list:
the credential-resolution precedence chain, the release-source/checksum verification
model and its security rationale, the signing/verification integration, the
worktree/safe-repo design, Bitbucket's dual-credential model, and the reasoning behind
the provider registry.

### Provider modules are README-only

Following the `go/chat` precedent (playbook §10.5), the provider modules ship **no
`docs/` directory, no `zensical.toml`, and no Pages job** — all narrative documentation
lives on the `forge` microsite so there is one place to read about forges.

Each provider README must nonetheless be **strong enough to stand alone** on pkg.go.dev
and in the repo:

- the toolkit header/badge and a one-line statement of what the provider does;
- **which source types it registers** (`forge-gitea` registers both `gitea` and
  `codeberg`) and any provider-specific configuration or host defaults;
- the vendor SDK it wraps, if any;
- the blank-import line that activates it, with a worked snippet;
- **prominent signposts** to `forge.go.phpboyscout.uk` for the contract, configuration
  and usage, and to its own pkg.go.dev for the API;
- a pointer to the compatibility matrix, since providers version in lockstep with the
  core.

The `forge` microsite in turn carries a provider index — capabilities, registered source
types, and required credentials per provider — so a reader can compare them in one place.

## Acceptance criteria

- Six modules resolve, build, and pass their guards; **`go list -deps` for `go/repo`
  contains no forge module and no forge SDK**.
- Each module ≥90% coverage; microsites live for `repo`, `forge` and `aferobilly`;
  provider modules ship a strong signposting README (no `docs/`).
- The docs audit above is complete per module: intent carried, claims verified against the
  code, GTB pages stubbed or repointed, deleted content audited for loss, zero broken
  links, and any stale GTB claims corrected in the same MR.
- GTB green consuming all six; `pkg/vcs` deleted; `mocks/pkg/vcs/**` deleted.
- A scaffolded project still initialises a git repo and self-updates from a release.
- Compatibility matrix published for the forge line.

## Resolutions (confirmed with user 2026-07-19)

- **R1 — every forge provider is its own module**, including the SDK-free ones; the
  boundary is cheap now and expensive later, and adopting a vendor SDK then becomes a
  dependency change inside a module rather than a re-extraction. Gitea's official SDK
  already exists, so this is not hypothetical. No hidden mechanics, no "why is this one
  different?".
- **R2 — `direct` stays in `go/forge`** as the contract's reference implementation: it is
  self-contained, will never acquire an SDK, and gives every consumer a working provider
  with no extra module.
- **R3 — `aferobilly` is a standalone module.** It is **not** scoped to git operations:
  anything consuming the `billy` filesystem interface can use an afero bridge, and making
  it a subpackage of `go/repo` would hide it from that audience behind a dependency they
  do not want. Its value is independent of this stack.
- **R4 — docs live on the `forge` microsite; provider modules are README-only**, matching
  the `go/chat` precedent (playbook §10.5). Each provider README is a *strong* signpost —
  see [§Documentation](#documentation-nothing-left-behind).

- **R5 — the git module is `go/repo`, not `go/git`.** The ecosystem already has many
  `git`-named Go libraries; a bare `git` invites confusion with them and competes for a
  name we do not need. `repo` also matches the existing package and call sites.
- **R6 — the forge core is `go/forge`, not `go/releases`.** Releases are what it targets
  today, but a forge has other surfaces (issues, pull requests, repositories) and the
  scope may widen. `forge` accommodates that without a later rename; `releases` would
  force one.
- **R7 — adopt the Gitea SDK *during* the extraction, not after.** Doing it afterwards
  means editing the same client twice. `forge-gitea` therefore swaps its hand-rolled
  `net/http` client for the official [`gitea.dev/sdk`](https://pkg.go.dev/code.gitea.io/sdk/gitea)
  as part of Phase 4.

  **Risk and mitigation.** This makes that one phase non-behaviour-preserving, so a
  failure is ambiguous between the move and the swap. The existing tests are the control:
  Gitea's suite is `httptest`-based (14 fake servers over ~615 LOC) driving the
  **provider interface** against Gitea-shaped JSON, so it asserts outcomes rather than
  request mechanics and should survive the swap largely intact. Move the package first
  with tests green, then swap the SDK as a **separate commit within the same MR**, so the
  diff stays reviewable and a bisect can tell the two apart. Expect the fixtures to need
  a server-version endpoint — the SDK typically probes it at client construction — and
  keep an eye out for extra calls the hand-rolled client never made.

## Open questions

_All resolved — see Resolutions above._

## Status

IN PROGRESS (2026-07-19) — all open questions resolved (R1–R8). Each phase follows the
playbook §5 procedure; the trivial ones proceed directly under this plan, the larger ones
(`repo`, `forge`) get a spec-lite of their own.

**Phase 1 — `go/aferobilly` — COMPLETE.** Published `v0.1.0`; docs live at
[aferobilly.go.phpboyscout.uk](https://aferobilly.go.phpboyscout.uk). Landed in GTB
together with Phase 2 (its only consumer was `pkg/vcs/repo`).

**Phase 2 — `go/repo` — COMPLETE.** Published `v0.1.0`; docs live at
[repo.go.phpboyscout.uk](https://repo.go.phpboyscout.uk). The R8 dependency inversion
was done in-tree first and shipped green, then extracted. Two settings changed shape
during extraction so that **every input arrives through `Settings`** and the module reads
no environment of its own: `SSHSettings.Env` was removed in favour of a resolved
`Path` (with the caller-side `KeyPath` helper), and the `GTB_GIT_ENABLE_PROGRESS`
environment variable — which was a package-level writer set in `init()`, and so raced
under `t.Parallel()` — became `Settings.Progress`. An AST-walking `envfootprint_test.go`
guard makes the no-environment claim enforceable, alongside the `depfootprint` guard that
proves the forge inversion. A latent bug surfaced during that work: `Progress` reached
`Pull` and both `Clone` paths but not `Push`, while `Push` did backfill `Auth` — so a
push rejected by a server-side hook lost the rejection reason, which arrives only on the
sideband channel. Fixed in the module before release.

**Phase 3 — `go/forge` — NEXT.** Audit for the provider-authoring API first (playbook
§10.1). Carry the "Backend Agnosticism" design goal recovered from the deleted
`docs/explanation/concepts/vcs-repositories.md` into its documentation — it is forge
intent, not repo intent.
