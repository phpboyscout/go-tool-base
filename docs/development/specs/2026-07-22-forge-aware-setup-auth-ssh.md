---
title: "Forge-aware setup: migrate in-use GitHub auth + SSH operations into the forge providers"
description: "Reframes and significantly extends the wave-2 'GitHub wider-client' item. The wide pkg/vcs/github client is legacy from a GitHub-centric predecessor of go-tool-base; most of it (PR management, repo creation, file contents, and its release methods) is dead — releases already route through go/forge, and self-update is already forge-aware. The genuinely in-use surface is narrow: interactive auth (GitHub OAuth device flow) and SSH-key upload, still done GitHub-specifically in pkg/setup/github. This spec brings ONLY those in-use operations into the go/forge provider abstraction as optional capability interfaces (Authenticator, KeyManager), implements them across all four forge adapters in sync (accounting for provider variance — device flow vs PAT vs app-password, SSH-API availability), makes pkg/setup drive the configured provider instead of hardcoded GitHub, then decommissions pkg/vcs/github. Dead paths (PR/repo/contents) are not built here; they move to a go/forge widening spec for a future wave."
date: 2026-07-22
status: DRAFT
tags:
  - specification
  - forge
  - setup
  - auth
  - ssh
  - wave-2
  - module-extraction
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Forge-aware setup: migrate in-use GitHub auth + SSH operations into the forge providers

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   2026-07-22

Status
:   IN PROGRESS (2026-07-22). Re-scope endorsed by the maintainer: bring only the
    in-use auth + SSH into forge across all providers; park the dead PR/repo/contents
    in the go/forge widening spec. §8 open questions resolved — **Q4: prove the model
    on GitHub first (the old `pkg/vcs/github` code is the proven reference), then build
    the other three adapters and PUBLISH ALL FOUR SIMULTANEOUSLY (lockstep)**; Q3:
    collapse `setup/github` + `setup/bitbucket` into one forge-driven initialiser; Q5:
    Bitbucket stays on the manual app-password fallback (no `Authenticator`); Q1: the
    two coarse capabilities; Q2: minimal `Prompter`, drafted in Phase 1.

Related
:   [extraction report](../reports/2026-07-07-package-extraction-report.md) (the wave-2 github row this replaces),
    [VCS/forge extraction plan](2026-07-19-vcs-stack-extraction-plan.md) (the release-provider extraction this builds on),
    `go/forge` widening spec (companion — the decommissioned PR/repo/contents API, drafted in the go/forge repo)

---

## 1. Context & re-scope

The wave-2 "restore GitHub's wider client across every provider in lockstep" item was
framed around `pkg/vcs/github` — a wide GitHub client (PR management, repo creation,
SSH keys, file contents, releases, device login). **That client is legacy** from a
GitHub-centric predecessor of go-tool-base; it predates the `go/forge` provider
abstraction.

Investigation changes the shape of the work:

- **Releases already moved.** `go/forge`'s `Provider` covers releases (+ checksum and
  signature verification), all four adapters implement it, and **self-update is
  already forge-aware** (`pkg/setup/update.go` uses `forge.Provider`/`forge.Lookup`).
  `pkg/vcs/github`'s own release methods are therefore **dead**.
- **PR management, repo creation, file contents are dead** — called nowhere in GTB.
- **What is genuinely in use** is narrow and lives in the *setup* flow, not the VCS
  client's wide API: **interactive authentication** (`GHLogin`, GitHub's OAuth device
  flow) and **SSH-key upload** (`UploadKey`), driven by `pkg/setup/github`.

So the right work is **not** to extract the wide client. It is to bring the two
*in-use* operations into the `go/forge` provider abstraction — across all providers,
kept in sync — and make `pkg/setup` **forge-provider-driven** instead of hardcoded to
GitHub. This extends wave 2 (a real, multi-module effort) rather than closing it with
a deletion.

## 2. Current state (investigation findings)

| Concern | Where it lives today | Status |
|---|---|---|
| Release fetch/verify | `go/forge` + 4 adapters; `pkg/setup/update.go` consumes `forge.Provider` | **done, forge-aware** |
| Token *resolution* | `go/forge` `ResolveToken`/`ResolveTokenContext` | done |
| Interactive **auth** (OAuth device flow) | `pkg/vcs/github/login.go` (`GHLogin`) + `pkg/setup/github` | **GitHub-only, to migrate** |
| **SSH-key** upload | `pkg/vcs/github` (`UploadKey`) + `pkg/setup/github/ssh.go` | **GitHub-only, to migrate** |
| PR / repo / file-contents | `pkg/vcs/github` (`CreatePullRequest`, `CreateRepo`, `GetFileContents`, …) | **dead → widening spec** |
| `pkg/vcs/github` release methods | `pkg/vcs/github` (`ListReleases`, `DownloadAsset`, …) | **dead (forge superseded)** |

`pkg/setup` already has a provider/registry shape (`setup/{github,bitbucket,ai,telemetry}`
initialisers), but the github initialiser is bound to `pkg/vcs/github` for auth + SSH.

## 3. Scope

**Build only what is in use.** Bring **interactive auth** and **SSH-key management**
into `go/forge` as optional capabilities, implement them in each adapter as far as the
provider supports, and make `pkg/setup` drive them via forge. **Decommission** the dead
PR/repo/contents (and dead release) methods — captured in the go/forge *widening spec*
for a future wave, not implemented here.

## 4. Design

### 4.1 Optional capability interfaces on `go/forge`

`forge` already uses optional interfaces a provider *may* implement (`ChecksumProvider`,
`SignatureProvider`) — a caller type-asserts for the capability. Extend that pattern:

```go
// Authenticator is an optional capability: a provider that supports an interactive
// login flow yielding a token. Providers that only accept a pre-issued token/app
// password do not implement it (setup falls back to manual entry).
type Authenticator interface {
    // Login runs the provider's interactive flow (e.g. OAuth device flow) and
    // returns a usable token. It is UI-agnostic: it reports the user-code / prompts
    // via the supplied Prompter so the CLI owns presentation.
    Login(ctx context.Context, p Prompter) (token string, err error)
}

// KeyManager is an optional capability: a provider that can register an SSH public
// key on the account via its API.
type KeyManager interface {
    UploadKey(ctx context.Context, name string, publicKey []byte) error
}
```

`Prompter` is a small interface the CLI (GTB) implements so forge stays TUI-free
(no forms/huh dependency in forge — GTB renders the device-code prompt).

### 4.2 Per-provider variance (the crux)

Auth models differ; capability interfaces make the differences explicit rather than
forcing a lowest common denominator:

| Provider | Interactive auth | SSH-key API |
|---|---|---|
| GitHub | OAuth **device flow** (migrate `GHLogin`) | yes (migrate `UploadKey`) |
| GitLab | OAuth/device flow or PAT | yes (user keys API) |
| Gitea | PAT (likely no device flow) | yes |
| Bitbucket | **app password** (no device flow) | yes (or repo/account keys) |

A provider implements `Authenticator` only where an interactive flow exists; otherwise
`pkg/setup` detects the missing capability and drives manual token/app-password entry
(the existing fallback path). `KeyManager` likewise where an SSH-key API exists.

### 4.3 `pkg/setup` becomes forge-driven

- The setup credential + SSH stages resolve the **configured** forge provider via
  `forge.Lookup`, then type-assert for `Authenticator` / `KeyManager` and drive
  whichever the provider offers, with the manual fallback otherwise.
- `pkg/setup/github` and `pkg/setup/bitbucket` collapse toward a **single
  forge-driven initialiser** (provider-parameterised), keeping only genuinely
  provider-specific UI copy where unavoidable (§8 Q3).
- `pkg/vcs/github` and `pkg/vcs` (its config adapters) are **deleted** once auth+SSH
  live in forge-github and setup no longer imports them.

### 4.4 Keep forge framework-free

The new capabilities must not pull GTB or a TUI into forge (depfootprint-guarded).
Auth/SSH use the provider SDK + `Prompter`; presentation stays in GTB.

## 5. Delivery (phased)

**The proving phase, then the lockstep build (Q4):**

1. **Forge contract** — add `Authenticator`, `KeyManager`, `Prompter` to `go/forge`
   (interfaces + docs + conformance harness additions). No behaviour yet.
2. **forge-github — the proving ground.** Implement `Authenticator` (port `GHLogin`'s
   device flow) and `KeyManager` (port `UploadKey`) into the github adapter, TDD. The
   old `pkg/vcs/github` is the proven reference implementation, so this validates the
   *seam* (capability interfaces + `Prompter`) against known-good behaviour before it
   is generalised. **Do not publish yet** — hold until the model is proven end-to-end
   through the GTB setup migration (step 4a).
3. **forge-{gitlab,gitea,bitbucket}** — once the GitHub seam is proven, implement the
   capabilities each supports, in lockstep, documenting which capability each provides
   (Gitea/Bitbucket per §4.2; Bitbucket = manual fallback, no `Authenticator`).
   **Publish all four adapters simultaneously** (matched versions) so a downstream
   never sees a partial-parity release.
4. **GTB setup migration** — (a) prove the seam end-to-end by driving GitHub auth+SSH
   through the new forge capability in `pkg/setup` while forge-github is still local;
   (b) after the simultaneous provider publish, make `pkg/setup` fully forge-driven for
   all providers, repoint off `pkg/vcs/github`, then **delete `pkg/vcs/github` +
   `pkg/vcs`**; `just ci` green.
5. **Decommission dead paths** — remove PR/repo/contents (+ dead release methods); land
   the **go/forge widening spec** capturing them for a future wave.

Step 1 is a `go/forge` minor; step 3 is a **single simultaneous lockstep release** of
the four adapters (after GitHub proves the model in step 2 + 4a); 4–5 are GTB MRs.

## 6. Testing (TDD/BDD + integration)

- **TDD** for the capability implementations; **BDD** (godog) for the setup auth/SSH
  behaviour contract (given a provider with/without `Authenticator`, the right flow
  runs). Full Diátaxis docs per the module conventions.
- **Integration** against provider APIs uses `httptest` fakes for the OAuth/device and
  key endpoints by default; where a container image exists (e.g. Gitea), the
  testcontainers pattern from `go/config-consul/test/integration` applies —
  `INT_TEST_INTEGRATION`-gated, testcontainers a test-only dep out of the module graph.

## 7. The go/forge widening spec (companion)

The decommissioned wide API (PR management, repo creation, file contents) is not
deleted into the void — it moves to a **widening spec in the go/forge repo** that
captures the API surface and reference source, to be implemented across providers in a
**future wave** when a concrete consumer needs it. This spec's decommission step
(§5.5) lands that companion spec.

## 8. Open questions — RESOLVED (2026-07-22)

- **Q1 — Capability granularity. → Two coarse capabilities** (`Authenticator`,
  `KeyManager`); provider auth variance lives *inside* `Authenticator`.
- **Q2 — `Prompter` shape. → Minimal `Prompter`** (surface the device user-code +
  verification URL; the CLI owns polling UX), drafted in Phase 1 against the GitHub
  device flow.
- **Q3 — Collapse setup providers. → Yes**, one forge-driven initialiser
  parameterised by provider; provider-specific strings live in data, not code.
- **Q4 — Rollout. → Prove on GitHub first, then lockstep-publish all four.** The old
  `pkg/vcs/github` is the proven reference; forge-github validates the seam
  (capability interfaces + `Prompter`) against known-good behaviour, and is *held
  unpublished* until proven end-to-end through the GTB setup migration. Then the other
  three adapters are built and **all four publish simultaneously** at matched versions
  — a downstream never sees partial parity.
- **Q5 — Bitbucket auth. → Manual fallback** (no `Authenticator`); app-password entry,
  as `setup/bitbucket` already does.

## 9. Acceptance criteria

1. `go/forge` defines `Authenticator`, `KeyManager`, `Prompter` (optional capabilities,
   depfootprint-clean, TUI-free).
2. `forge-github` implements both (device-flow auth + SSH-key upload), migrated from
   `pkg/vcs/github`; the other adapters implement what they support, documented per
   provider; lockstep releases.
3. `pkg/setup` drives auth + SSH via the configured forge provider (capability-detected,
   manual fallback); no import of `pkg/vcs/github`.
4. `pkg/vcs/github` and `pkg/vcs` are **deleted**; `just ci` green; migration note added.
5. Dead PR/repo/contents API decommissioned and captured in the **go/forge widening
   spec**.
6. Full Diátaxis docs, TDD+BDD, integration per §6; ≥90% coverage on new module code.
