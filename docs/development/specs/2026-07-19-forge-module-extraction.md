---
title: "Extract the forge/release layer into go/forge and per-provider modules"
description: "Extract pkg/vcs/release (the Provider contract, registry and release types) together with pkg/vcs's token-resolution helpers into gitlab.com/phpboyscout/go/forge, then split each forge provider into its own module. The release contract already has zero go-tool-base coupling, but auth lives in a sibling package and every real provider needs both — so the two must move together or out-of-tree provider authors are forced back into a go-tool-base dependency. Audit of the four in-tree providers found the true provider-authoring surface to be ten symbols, alongside nine contract gaps that would silently break a third-party author."
date: 2026-07-19
status: APPROVED
tags:
  - specification
  - vcs
  - forge
  - release
  - providers
  - module-extraction
  - reuse
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Extract the forge/release layer into `go/forge` and per-provider modules

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   2026-07-19

Status
:   APPROVED (2026-07-19 — Q1–Q6 resolved with user; see Resolutions). Phases 3 and 4 of
    the VCS stack extraction plan.

Related
:   [VCS stack extraction plan](2026-07-19-vcs-stack-extraction-plan.md) (IN PROGRESS — the parent plan; R1–R8),
    [module-extraction playbook](2026-07-12-go-module-extraction-playbook.md) (§5 procedure, §10.1 provider-authoring API, §10.10 compatibility matrix),
    [repo + aferobilly extraction](2026-07-19-vcs-stack-extraction-plan.md) (phases 1–2, COMPLETE),
    [chat extraction](2026-07-05-chat-module-extraction.md) (the provider-registry precedent),
    [extraction report](../reports/2026-07-07-package-extraction-report.md)

---

## Summary

`pkg/vcs/release` defines what a forge release provider *is*: a `Provider` contract, a
process-wide registry keyed by source-type string, the release/asset types, and two
optional capability interfaces discovered by runtime type assertion. `pkg/vcs` (root)
holds the token-resolution chain every real provider uses. Four provider packages
implement the contract for GitHub, GitLab, Gitea/Codeberg and Bitbucket.

This spec extracts the contract **and** the auth helpers into a single module,
`gitlab.com/phpboyscout/go/forge`, then splits each provider into its own module.

The headline evidence is good: **`pkg/vcs/release` imports zero go-tool-base packages**,
and exactly one file in the whole core carries a foreign dependency. But two findings
reshape the work, and both come from auditing rather than from the original plan:

1. **Auth cannot be left behind.** `ResolveToken`/`TokenConfig`/`AuthConfig` live in
   `pkg/vcs` root, the contract lives in `pkg/vcs/release`, and three of four providers
   need both. Extracting only the contract would force out-of-tree provider authors to
   import go-tool-base for auth — the exact lockstep trap `pkg/chat` fell into (playbook
   §10.1). **They move together.**
2. **`github` is not a release provider.** Its `GitHubClient` interface has thirteen
   methods, of which four are release-related; the rest (pull requests, repo creation,
   SSH key upload, file contents) are consumed by `pkg/setup/github`, outside the VCS
   tree. Settling what belongs in `forge-github` is **deferred to phase 4** (R2), once the
   contract has proven itself against three simpler providers.

A third finding contradicts an already-approved decision: `direct` was kept in the core
partly to serve as the contract's reference implementation (plan R2), but the code does not
support that role. It is rewritten, and a conformance harness ships alongside it (R1).

---

## Motivation

Release management is the capability tool-builders most often want standalone — the
extraction report ranks the VCS/release stack as the highest aggregate value remaining.
Beyond reuse, three forces specific to this layer:

- **The contract is already a plugin registry.** `Provider` + `Register(sourceType,
  factory)` at `init()` time, plus optional interfaces by type-assert, is structurally
  identical to `go/chat`. A module boundary formalises what is already a plugin
  architecture rather than inventing one.
- **A module boundary is expensive to add later and cheap to add now.** Gitea gained an
  official SDK in May 2026; Bitbucket may gain one. Adopting a vendor SDK inside an
  existing provider module is a dependency change; doing it after the fact to an in-tree
  package is a re-extraction.
- **Third-party providers are currently impossible to author cleanly.** Everything a
  provider needs is exported, but split across two packages, one of which drags the
  framework. Nine further gaps (below) would silently mislead an out-of-tree author; four are fixed
  before `v0.1.0` (R6) and two more are closed by the conformance harness (R1).

---

## Target modules

| Module | Contents | Depends on |
|---|---|---|
| `gitlab.com/phpboyscout/go/forge` | `Provider`, `Release`, `ReleaseAsset`, registry, `ReleaseSourceConfig`, `Config`, `SubConfig`, sentinels, optional capability interfaces, token resolution (`ResolveToken`, `TokenConfig`, `AuthConfig`), the `direct` provider, `forgetest` | `go/credentials`, `go/httpclient`, `cockroachdb/errors` |
| `go/forge-gitlab` | GitLab release provider | `forge`, `gitlab-org/api/client-go/v2`, `go/httpclient` |
| `go/forge-gitea` | Gitea **and** Codeberg providers | `forge`, `go/httpclient` (no SDK today — see D7) |
| `go/forge-bitbucket` | Bitbucket provider | `forge`, `go/httpclient`, `go/credentials`, `go/regexutil` |
| `go/forge-github` | GitHub provider — **scope deferred to phase 4 (R2)** | `forge`, `go-github/v88`, `go/httpclient`, possibly `cli/oauth` + `go/browser` + `afero` |

Docs live on the `forge` microsite (`forge.go.phpboyscout.uk`); provider repos get a
strong README with signposts, per plan R4 and the `chat` precedent.

---

## Current structure & coupling

### The core is already clean

`pkg/vcs/release` (`provider.go`, `registry.go`, `source_config.go`, `constants.go`)
imports only `context`, `io`, `sort`, `sync` and `cockroachdb/errors`. `ReleaseSourceConfig`
was *deliberately* decoupled from `props.ReleaseSource` to break an import cycle
(`source_config.go:3-6`), and the `ProviderFactory` signature already takes a narrow local
`Config` interface (`registry.go:12-15`) rather than `config.Containable`. That earlier
decoupling is what makes this extraction cheap.

`pkg/vcs` root: `auth.go` imports only `go/credentials`; `config.go` imports nothing.
**`config_adapter.go` is the sole coupling point** — it needs `config.Containable` from
`go/config` to implement `ConfigFromContainable`. It is a 20-line bridge and stays in GTB.

### The provider-authoring surface (measured, not assumed)

All four providers consume exactly these:

`release.Provider`, `release.Release`, `release.ReleaseAsset`, `release.Config`,
`release.ReleaseSourceConfig`, `release.Register`, `release.SubConfig`

Three of four additionally consume `vcs.TokenConfig`, `vcs.AuthConfig`,
`vcs.ResolveToken`. Bitbucket is the exception: it imports **no** `pkg/vcs` at all and
reimplements the whole credential chain locally; it converges on the shared chain during
phase 4 (R5).

### Provider inventory

| Provider | Prod / Test LOC | SDK | Registers | Notes |
|---|---|---|---|---|
| gitlab | 275 / 404 | `client-go/v2` v2.46.0 | `gitlab` | Smallest; cleanest |
| gitea | 324 / 659 | none (raw HTTP) | `gitea` **+ `codeberg`** | Two registry keys, one impl |
| bitbucket | 672 / 1166 | none (raw HTTP) | `bitbucket` | Only provider implementing the optional interfaces; bespoke auth |
| github | 582 / 1260 | `go-github/v88`, `cli/oauth` | `github` | 13-method client, 4 release-related; OAuth device flow |

Extraction difficulty, easiest first: **gitlab → gitea → bitbucket → github**. This matches
the parent plan's sequencing, so P4 ordering is unchanged.

### Nine provider-authoring gaps

Found by audit; each would silently mislead an out-of-tree author.

| # | Gap | Evidence |
|---|---|---|
| G1 | `DownloadReleaseAsset`'s second return value is undocumented (it is a redirect URL, by go-github convention) | `provider.go:53`; both impls return `""` |
| G2 | `maxBytes` capping is asymmetric — caller's job for checksums, provider's job for signatures — and the bound values live outside the extracted set | `direct/provider.go:175-177` vs `:188-189`; `setup.MaxChecksumsSize` |
| G3 | Duplicate registration **silently overwrites**, no error or warning | `registry.go:44` |
| G4 | `ErrReleaseNotFound` is documented but honoured by no production provider | `provider.go:24-29` |
| G5 | `ErrVersionUnknown` is `direct`-specific leakage into the shared contract | `provider.go:19-22` |
| G6 | No conformance harness — `releasetest` is a *fake for consumers*, and **no provider test imports it** | `releasetest.go`; grep |
| G7 | The `ErrNotSupported` fall-through protocol is documented in prose only, enforced nowhere | `provider.go:56-84` |
| G8 | `parseKeychainRef` is unexported | `auth.go:134` |
| G9 | `mocks/pkg/vcs/release/*` live in a root-level in-tree package, not with the interfaces | `mocks/pkg/vcs/release/` |

`DirectReleaseProvider.SetToolName` is a tenth item of a different kind: a dead seam whose
doc comment claims *"called by the setup package"* when it is never called anywhere
(`direct/provider.go:107`). Either wire it or delete it.

---

## Decisions

Carried from the parent plan (R1–R8) unless marked new.

**D1 — Split every provider into its own module,** including the SDK-free ones. Consistency
over special-casing; adopting an SDK later becomes a dependency change inside a module
rather than a re-extraction. *(plan R1)*

**D2 — `direct` stays in the core,** and is **rewritten to use `ResolveToken` and the
standard `AuthConfig` layout** so it is a faithful example of the path every real provider
takes. A `forgetest` **conformance harness** ships alongside it. *(plan R2, amended by R1)*

**D3 — Auth moves with the contract.** `ResolveToken`, `ResolveTokenContext`, `TokenConfig`
and `AuthConfig` ship in `go/forge`. *(new — the audit's central finding)* Without this,
out-of-tree providers import go-tool-base for auth and we reproduce the `chat` lockstep
trap.

**D4 — `ConfigFromContainable` stays in GTB.** It is the `go/config` bridge; the narrow
`Config` interface travels as the seam. *(new)*

**D5 — Gitea owns two registry keys.** `gitea` and `codeberg` ship from one module; the
Codeberg factory pre-seeds `Host` and uses `CODEBERG_TOKEN`. Splitting would mean
duplicating the provider. *(new)*

**D6 — Docs on the forge microsite; provider repos get READMEs only.** *(plan R4)*

**D7 — Adopt the Gitea SDK during extraction,** as a **separate commit in the same MR** so
the move stays reviewable and behaviour-preserving up to that point. *(plan R7)*

**D8 — Each module publishes its own `mocks` subpackage,** closing G9. *(convention)*

**D9 — Restore the recovered "Backend Agnosticism" intent** on the forge microsite, in its
original wording rather than the weaker surviving paraphrase. *(plan; see Documentation)*

**D10 — Lockstep versioning with a published compatibility matrix.** Core and providers move
in matching minor versions, as `chat` does. *(R3)*

**D11 — Host pinning becomes a shared core primitive.** One reviewed implementation replaces
three; third parties get a security primitive they would otherwise reinvent less carefully.
*(R4)*

**D12 — Bitbucket converges on the shared credential chain,** which widens to carry a dual
credential (username + app password) rather than a single token. Because this is a
behaviour change rather than a move, it lands as a **separate commit inside the same MR**,
so every commit before it is a provable pure move. *(R5, mitigation per D7's precedent)*

**D13 — Four contract gaps are fixed before `v0.1.0`** freezes the surface across five
modules: G1, G2, G3, G5. *(R6)*

**D14 — `maxBytes` is enforced provider-side, with the bounds shipped as `forge`
constants — PROVISIONAL.** The limit belongs at the trust boundary, a third party cannot
forget it, and the conformance harness can verify it mechanically. This moves
`setup.MaxChecksumsSize` and `verify.MaxSignatureSize` into the module.

This is a **starting position, deliberately held loosely** (agreed with user 2026-07-19):
G2 requires *symmetry*, and either side can supply it. Implementation decides. Pivot to
caller-side enforcement — or to an injectable bound — if any of these surface:

- **The bound needs to vary by consumer.** A `forge` constant serves one policy. If GTB
  and another consumer legitimately want different caps, provider-side hardcoding is the
  wrong home and the bound should be a constructor argument.
- **Providers cannot know the bound without a config knob.** If enforcing it pushes a
  configuration surface into every provider, the cure is worse than the disease — that is
  complexity multiplied across five modules.
- **The harness cannot verify it without network or fixtures so large they slow the
  suite.** Mechanical verifiability is half the justification; if it evaporates, so does
  the argument for this side.

Whichever way it lands, record the outcome here with a dated note rather than a silent
edit. *(new — the sub-decision G2 required)*

**D15 — Remove the suppressed mutable globals at cut-over; do not repoint them.**
*(new, 2026-07-19)*

`pkg/setup/checksum.go` declares its size bounds and checksum policy as **exported mutable
package variables**, each with a `//nolint:gochecknoglobals` suppression:

```go
//nolint:gochecknoglobals // size bounds are tool-author tunables
var (
	MaxChecksumsSize      int64 = 1 << 20
	MaxBinaryDownloadSize int64 = 512 << 20
)

//nolint:gochecknoglobals // tool-author compile-time override
var DefaultRequireChecksum = false
```

This is the pattern `AGENTS.md` bans — *"this pattern races under `t.Parallel()`"* — and the
suppression is what let it survive. The cost is already visible and already documented, in
`update_checksum_test.go`:

```go
// Mutates the package-level [MaxChecksumsSize] tunable, so
// cannot run with t.Parallel
```

The requirement behind it — tool authors tuning the bounds — is real, but never needed a
mutable global. `forge` expresses it as a `const` default plus a **caller-supplied
parameter**, which is strictly more flexible (per-call rather than per-process), needs no
suppression, and leaves tests parallel-safe. See D14.

At cut-over therefore: delete the three variables, take the bounds as `SelfUpdater` fields
or explicit arguments defaulting to `forge.DefaultMaxChecksumsSize` /
`forge.DefaultMaxSignatureSize`, and drop the two suppressions. The oversized-response test
regains `t.Parallel()` by passing its own bound.

**Scope, honestly.** Only these two suppressions are removed. The four
`//nolint:gochecknoglobals // sentinel error` annotations in the same file are **not** a
smell and stay: Go has no const errors, so `var ErrX = errors.New(...)` is the only way to
declare a sentinel, and `AGENTS.md` explicitly permits a narrowly-scoped suppression where
one is genuinely unavoidable. The compiled-regex global is idiomatic `MustCompile` and is
also out of scope. The point is to stop a banned pattern crossing into the new boundary
unexamined — not to run a suppression count to zero.

---

## Seams and decoupling

Little decoupling work is needed — the hard part was done by the earlier
`ReleaseSourceConfig` and `ProviderFactory` changes. What remains:

- **`props.Tool.ReleaseProvider`** (`props/tool.go:203`) is typed `release.Provider`, so
  `pkg/props` gains a dependency on `go/forge`. Acceptable and unavoidable: it is the
  documented direct-injection seam that takes precedence over registry lookup.
- **Host pinning is duplicated three ways** — `assetHostTrusted` in gitea and gitlab,
  `setBasicAuthIfHostMatches` in bitbucket — with no shared helper. Candidate for the core
  — now resolved: it moves into the core (R4, D11).
- **`ghClientID` is package-level mutable state** seeded from env in a second `init()`
  (`github/login.go:17-21`), likely also ldflags-injected. Same class as the
  `gitProgressOutput` global removed during the `repo` extraction; it should become an
  explicit constructor argument if the OAuth flow moves.

---

## Migration procedure

Per playbook §5, one phase per module, each landing green before the next.

**Phase 3 — `go/forge`.**
Bootstrap the repo to §3; move `pkg/vcs/release/*` + `auth.go` + `config.go` + the `direct`
provider + `releasetest` (renamed `forgetest`); apply D13's gap fixes (G1, G2, G3, G5) and
D14's bound relocation; rewrite `direct` per R1 and add the conformance harness; add the
shared host-pinning primitive (D11); add the `depfootprint` guard
(forbid go-tool-base, every forge SDK, viper/pflag/cobra/charm/OTel/cloud); publish mocks;
author the microsite; cut `v0.1.0`.

**Phase 4 — providers, in ascending difficulty.** `forge-gitlab` → `forge-gitea` (+ SDK
swap as a separate commit, D7) → `forge-bitbucket` (+ credential convergence as a separate
commit, D12). Each: move package + its own tests, repoint to `forge`, README with signposts,
`v0.1.0`.

**Then stop and reassess (R2).** With three providers proven against the contract, settle
GitHub's scope before extracting it. Only then `forge-github`.

**Phase 5 — GTB cut-over (single MR).** Delete the in-tree packages; **remove the suppressed
mutable globals rather than repointing them (D15)**; repoint `pkg/setup`,
`pkg/setup/github`, `pkg/props`, `pkg/vcs/repo/config_adapter.go`; keep
`ConfigFromContainable` and the GTB config-key schemas; blank-import the provider modules
in `pkg/setup/providers.go`; migration note; docs sweep.

---

## Test redistribution

Classify before moving (playbook §11.2). Favourable: **no provider test imports
`mocks/pkg/vcs/*`**, and all internal tests are same-package, so they travel with their
module. Specifics:

- `gitlab` — all internal, same-package. Moves verbatim.
- `gitea`, `bitbucket` — mixed internal/external; the external ones drive the `Provider`
  interface and are the more portable.
- `github` — all seven test files internal, reaching into `enterpriseURLs`,
  `deriveUploadURL` and client internals. Travels, but only with the whole package.
- `releasetest` self-test moves with it.
- GTB keeps the tests for `ConfigFromContainable` and the `pkg/setup` update flow, which
  should switch to the module's published mocks.

---

## Documentation: nothing left behind

An intent audit is an enforced per-phase deliverable (plan R4). The sweep for this phase is
**already done** and found the following.

### Intent to carry

The **Backend Agnosticism** design goal, recovered from
`docs/explanation/concepts/vcs-repositories.md` (deleted in `c567e94`), in its original
wording:

> **Backend Agnosticism**
> : Consuming code depends on `release.Provider`, not `*github.GHClient` or
> `*gitlab.GitLabReleaseProvider`. Switching from GitHub to GitLab releases is a one-line
> constructor change.

The surviving paraphrase in `components/vcs/index.md` lost both the concrete "one-line
constructor change" claim and the named anti-pattern types. Restore the original.

Two further pieces of intent that must survive:

- **Why opt-in interfaces rather than required methods** (`provider.go:63-68`): providers
  that do not implement a capability fall back to locating `checksums.txt` by filename,
  which "keeps third-party Provider implementations source-compatible: they gain the
  feature by opting in, not by implementing a new required method."
- **Why a DI seam rather than `release.Register` in tests** (`components/vcs/release.md`):
  "The global registry is process-wide mutable state; mutating it from tests cannot run
  under `t.Parallel()`." This is the same parallel-safety principle that drove the `repo`
  settings contract.

### Eight stale doc claims to fix at cut-over

| # | Claim | Reality |
|---|---|---|
| S1 | `direct` returns `ErrNotSupported` from `GetReleaseByTag` | It returns a synthetic release, no error (`direct/provider.go:127-129`) |
| S2 | `ProviderFactory` takes `config.Containable` | Takes the narrow `Config` (`registry.go:29`). **The docs predate the decoupling that makes this extraction possible.** |
| S3 | `ResolveTokenContext(ctx, cfg config.Containable, …)` | Takes `TokenConfig` (`auth.go:54`) |
| S4 | Root `pkg/vcs` "contains only `auth.go`" | Also `config.go`, `config_adapter.go`, `doc.go`; and `ResolveToken` is used by gitea and repo too |
| S5 | Bitbucket `workspace` Params key | Does not exist; `workspace := owner` is hardcoded (`bitbucket/release.go:170`) |
| S6 | Bitbucket credentials are a 2-step chain | It is 4 steps; the entire keychain-blob tier is undocumented (`release.go:521-556`) |
| S7 | GitLab usage example passes a config container | `NewReleaseProvider` takes a typed `Settings`; the example will not compile |
| S8 | `ErrReleaseNotFound` follow-up note | Drops bitbucket's exclusion and the spec cross-reference |

Also: `provider.go:28` cites `docs/development/specs/2026-06-19-injectable-release-source.md`,
**which does not exist** in the specs directory — recover it from history or fix the
reference, since it is the decision record for the injection seam.

### Migrate vs stay

**Migrate:** `components/vcs/release.md` (the bulk), the five provider pages collapsing into
a microsite provider index, `direct`'s URL-template and version-endpoint tables, the token
precedence chain, GitLab's platform-difference table.

**Stay:** `props.Tool.*` wiring, `pkg/setup.NewUpdater` and its three-tier precedence,
`update.require_checksum`/`require_signature` policy, all GTB config-key schemas
(`github.url.*`, `gitlab.url.api`, `bitbucket.*`, `direct.token`), `providers.go`
blank-import wiring, the `update` CLI reference and its feature file.

### Deleted content to audit

`concepts/auto-update.md` (73 lines) and `reference/cli/update.md` (100 lines) were deleted
in the same `c567e94` restructure. Recover both and confirm they were genuinely re-homed
rather than dropped. `components/version-control.md` is a 17-line redirect stub with an
already-incomplete link list; it will dangle entirely post-extraction — resolve it rather
than repoint it.

---

## Acceptance criteria

1. `go/forge` builds with **zero** go-tool-base imports; `depfootprint` guard proves it and
   forbids every forge SDK.
2. Each provider module builds against `forge` alone, with no go-tool-base import.
3. A third party can author a provider **without importing go-tool-base**, demonstrated by
   `direct` as a faithful worked example (R1) and validated by
   `forgetest.RunProviderConformance`.
   The harness must fail a provider that mishandles the `ErrNotSupported` fall-through.
4. All four in-tree providers register and resolve exactly as today; `gitea` still owns both
   `gitea` and `codeberg`.
5. Every module ≥90% coverage; `just ci` green in GTB after cut-over.
6. Scaffold verification: generated projects still self-update.
7. The eight stale claims are corrected and the Backend Agnosticism wording restored.
8. G1, G2, G3 and G5 are closed. `maxBytes` is **symmetric** — one side owns capping for
   both optional interfaces, and the docs say which. D14 starts provider-side; if
   implementation trips one of its pivot triggers, the outcome is recorded there with a
   dated note.
9. Bitbucket's credential convergence is a **separate, isolable commit**; every commit
   before it in that MR is behaviour-preserving.
10. `pkg/setup` no longer declares mutable exported size/policy globals, the two
    `gochecknoglobals` suppressions covering them are gone, and
    `TestDownloadChecksumManifest_RejectsOversizedResponse` runs under `t.Parallel()`
    (D15).

---

## Resolutions (confirmed with user 2026-07-19)

**R1 — `direct` is improved *and* a conformance harness ships.** The audit showed `direct`
could not serve as the reference implementation plan R2 intended: it bypasses `ResolveToken`
for `os.Getenv("DIRECT_TOKEN")`, uses a config idiom no other provider uses, stubs
`ListReleases`, and fabricates synthetic release types with `GetID()` hardcoded to `0` — so
it never demonstrates mapping a real API payload, which is the hard part of authoring a
provider. Resolution: rewrite `direct` onto `ResolveToken`/`AuthConfig`, **and** ship
`forgetest.RunProviderConformance(t, factory, caps)`. The harness is the load-bearing half —
it closes G6 and G7 by mechanically enforcing the `ErrNotSupported` fall-through protocol
that is currently prose-only and enforced nowhere, which no amount of example-copying does.

**R2 — GitHub's scope is deferred to phase 4.** Only 4 of `GitHubClient`'s 13 methods are
release-related; the rest serve `pkg/setup/github`. Rather than decide what `forge` means up
front, extract the core and the three straightforward providers first, then settle GitHub's
boundary once the contract has proven itself against three real implementations. Phase 4
**stops and reassesses** before GitHub. The options surveyed — move the whole client, move
only the release provider, or split into two modules — are recorded here so the question is
not re-litigated from scratch.

**R3 — Lockstep versioning plus a published compatibility matrix,** matching `chat`.
Consistency with the established toolkit convention beat the lower coordination cost of
letting providers float on a caret range, which would leave downstreams on untested
core/provider pairings with no matrix to consult.

**R4 — Host pinning moves into the forge core.** It is a security primitive — it stops
credentials being sent to a redirected host — and three separate implementations mean three
chances to get it subtly wrong. The three current call sites differ enough that the shared
signature needs care during implementation.

**R5 — Bitbucket converges on the shared credential chain** *(chosen against the drafting
recommendation to defer)*. Bitbucket needs username + app password, so the shared chain
widens to carry a dual credential rather than a single token. Because this is a genuine
behaviour change occurring during a move — the combination that makes regressions hardest to
attribute — it lands as a **separate commit inside the same MR**, exactly as D7 handles the
Gitea SDK swap. Every commit before it is then a provable pure move.

**R6 — Fix G1, G2, G3 and G5 before `v0.1.0`.** These freeze into a published surface across
five modules; fixing them afterwards is a breaking change to all five, whereas fixing them
now is nearly free while the surface is still ours. G6 and G7 are closed by R1's harness.
G4 (`ErrReleaseNotFound`) needs changes inside each provider and is deferred; G8
(`parseKeychainRef`) is minor and deferred; G9 is handled by D8.

## Open questions

_All resolved — see Resolutions above._

## Status

APPROVED (2026-07-19) — evidence gathered from three audits (core contract, four providers,
documentation intent); Q1–Q6 resolved with the user and recorded as R1–R6. Phase 3
(`go/forge`) may begin. Phase 4 halts after `forge-bitbucket` to settle GitHub's scope
per R2.
