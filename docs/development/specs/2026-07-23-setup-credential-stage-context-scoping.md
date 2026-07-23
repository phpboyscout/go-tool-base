---
title: "Setup credential stage: scope the keychain timeout to keychain operations, not the whole interactive flow"
description: "The forge-aware setup wizard creates a single 5-second context (credentials.KeychainOpTimeout) and threads it through the entire credential stage — storage-mode forms, the OAuth device flow, and the config write. The OAuth device flow can never complete inside 5 seconds, so interactive login always deadline-expires and silently falls back to manual PAT entry. Scope the timeout to individual keychain operations and give Login the caller's context."
date: 2026-07-23
status: IMPLEMENTED
tags:
  - specification
  - setup
  - forge
  - auth
  - credentials
  - security
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# Setup credential stage: scope the keychain timeout to keychain operations, not the whole interactive flow

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   IMPLEMENTED (2026-07-23). GTB: Initialiser ctx plumbing + per-operation
    keychain scoping with red-first regression tests (pkg/setup/forge/
    ctx_scoping_test.go). Providers: forge-github + forge-gitlab v0.2.1 bound
    device.Wait by the code's ExpiresIn; GTB pins bumped. Open questions were
    resolved by the maintainer:
    **Q1 — full context plumbing** through the `setup.Initialiser` seam
    (breaking `Configure`, `RunGitHubInit`, `RunBitbucketInit` signatures;
    migration note at `docs/reference/migration/v0.x-initialiser-context.md`),
    with regression tests validating the defect before the fix. **Q2 — same
    change**: forge-github and forge-gitlab internally bound `device.Wait` by
    the device code's `ExpiresIn` (fallback 15 min for zero/negative values),
    released in lockstep and picked up by GTB.

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (CRITICAL finding),
    [forge-aware setup: auth + SSH](2026-07-22-forge-aware-setup-auth-ssh.md) (the migration that introduced the flow),
    [credentials module extraction](2026-07-18-credentials-module-extraction.md) (origin of `KeychainOpTimeout`)

---

## 1. Problem

`credentials.KeychainOpTimeout` (5 s, `go/credentials/wizard.go:16`) is documented as a
bound on "any **single** credentials-backend operation". The forge-aware setup wizard
instead uses it as a deadline for the **entire interactive credential stage**:

- `pkg/setup/forge/single.go:147-148` creates
  `ctx, cancel := context.WithTimeout(context.Background(), credentials.KeychainOpTimeout)`
  for the already-configured `ResolveTokenContext` check — then reuses that same ctx for
  everything that follows: it is passed into `runAuthCredentialStage` (single.go:172,
  178-209), from there into `captureToken` → `auth.Login(ctx, i.prompter)`
  (single.go:274), and into `writeSingleCredential` → `credentials.Store(ctx, …)`
  (single.go:339 via 313).
- `pkg/setup/forge/dual.go:81-93` has the identical shape: one 5 s ctx created at
  dual.go:81-82 spans the whole form flow and the `writeDualCredentials` call at
  dual.go:93.

The 5-second clock starts **before** the storage-mode selector, the env-var-name form,
the fetch-token confirmation, the device-code prompt, and the OAuth poll loop. The
GitHub and GitLab providers honour the deadline in `device.Wait(ctx, …)`
(`go/forge-github/auth.go:57`, `go/forge-gitlab/auth.go:54`).

**Failure scenario.** No human completes a browser device flow in under 5 seconds, so
the forge-driven OAuth login — the headline capability of the forge-aware-setup
migration — **always** expires its deadline. `captureToken` then logs a warning and
falls back to manual PAT entry (single.go:279-282), which masks the bug: setup
"works", but the OAuth path is dead in production while unit tests (which stub the
authenticator) pass. Additionally, any context-honouring credentials backend (e.g. a
future Vault/SSM backend) fails `Store` for the same reason; the built-in OS-keychain
backend survives only because its implementation ignores ctx.

The codebase already contains the correct pattern:
`singleStorageModeForm`/`dualStorageModeForm` (dual.go:105-112) and
`bitbucket.SettingsFromConfig` (`go/forge-bitbucket/config_adapter.go:13-14`) each
derive a **fresh** `KeychainOpTimeout` ctx immediately around the single keychain call.

## 2. Proposed change

Re-scope contexts so each deadline covers only the operation it was designed for:

1. **`configureAuth` (single.go)** — keep a `KeychainOpTimeout`-derived ctx for the
   `ResolveTokenContext` already-configured check, but **cancel it there**; do not pass
   it onward. `runAuthCredentialStage` receives the caller's ctx (see point 4).
2. **`captureToken` / `Login`** — run the OAuth device flow under the caller's ctx.
   The device flow already carries its own expiry (`DeviceCode.ExpiresIn`, surfaced by
   the provider); optionally derive `context.WithTimeout(ctx, expiresIn)` inside the
   providers, but the setup layer must not impose a shorter artificial bound.
3. **`writeSingleCredential` / `writeDualCredentials` → `credentials.Store`** — derive
   a fresh `context.WithTimeout(ctx, credentials.KeychainOpTimeout)` **at the call
   site of each keychain operation**, matching the documented contract and the
   existing `dualStorageModeForm` pattern.
4. **Thread a real caller ctx.** `configureAuth`/`configureDual` currently start from
   `context.Background()`. Plumb the command's ctx (from the cobra command via the
   initialiser entry point) through so Ctrl-C cancels a pending device-flow poll. If
   plumbing the command ctx through the `setup.Initialiser` seam is too invasive for
   this fix, an acceptable interim is `context.Background()` at the stage entry with
   the keychain-scoped deadlines applied per-operation — but note it in the code.

**Alternative considered:** raising `KeychainOpTimeout` to cover a device flow
(e.g. 15 min). Rejected — it would gut the timeout's purpose (a locked keychain
stalling first-run setup) and still couples two unrelated concerns to one constant.

## 3. Scope & release plan

- **go-tool-base only**: `pkg/setup/forge/single.go`, `pkg/setup/forge/dual.go`
  (+ tests). No `go/forge*` or `go/credentials` API change is required —
  `KeychainOpTimeout`'s documented contract is already correct; the consumer violates it.
- Ships as `fix(setup): …` — a patch release via the releaser-pleaser Release MR.
- If point 4 (caller ctx) changes the exported initialiser signatures in
  `pkg/setup/forge`, that is a permitted pre-1.0 break; note it in the commit body and
  add a migration note in `docs/migration/`.

## 4. Acceptance criteria

- The ctx passed to `auth.Login` carries **no** `KeychainOpTimeout`-derived deadline;
  a regression test drives `captureToken` with a fake `Authenticator` whose `Login`
  blocks ≥ 6 s (fake clock or short injected constant) and asserts it completes
  without falling back to manual entry.
- Every `credentials.Store`/`Probe`/`Retrieve` call in `pkg/setup/forge` derives its
  own `KeychainOpTimeout` ctx at the call site; a test with a fake backend asserts the
  observed deadline is ~5 s from the *store* call, not from stage entry.
- `dual.go` `configureDual` gets the same treatment as `single.go`; the shared shape is
  covered by tests for both.
- A test asserts the fallback-to-manual path still engages on a genuine `Login` error
  (e.g. `forge.ErrNotSupported`), so the graceful degradation contract is preserved.
- Manual verification on a real terminal: `gtb init` against GitHub completes a full
  device flow taking > 5 s and stores the token without the "falling back" warning.
- `just ci` green; coverage on touched `pkg/setup/forge` files stays ≥ 90%.

## 5. Open questions

1. Should the command ctx be plumbed through the initialiser seam now (breaking the
   `pkg/setup/forge` entry-point signatures), or is the per-operation scoping with
   `context.Background()` at stage entry acceptable for this fix, deferring ctx
   plumbing to a follow-up?
2. Should providers internally bound `device.Wait` by the device code's `ExpiresIn`
   (defence against a caller passing an unbounded ctx), and if so, should that land in
   the same change or as a forge-github/forge-gitlab follow-up?
