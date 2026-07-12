---
title: "Standalone signing & verification module (dependency-inverted, reusable by non-GTB projects)"
description: "Extract the OpenPGP/WKD release signing + verification primitives from pkg/signing and pkg/setup into one light, standalone Go module. The module owns the contract (a crypto.Signer-producing Backend interface), the signing mechanics, and the verification trust model; heavy cloud backends (AWS KMS, GCP, Azure) are NOT implemented in the module — consumers implement the interface and inject. Lets afmpeg verify ffmpeg-wasi's signed wasm releases, and lets any project sign without the go-tool-base framework. Driven by work item #1."
date: 2026-06-30
status: IMPLEMENTED
tags:
  - specification
  - signing
  - verification
  - openpgp
  - wkd
  - module-extraction
  - dependency-inversion
  - reuse
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Standalone signing & verification module

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   30 June 2026

Status
:   IMPLEMENTED

Tracking
:   work item #1

---

!!! success "Implemented — verified 2026-07-12"

    Shipped as [`gitlab.com/phpboyscout/signing`](https://gitlab.com/phpboyscout/signing)
    **v0.1.0**. Verified: the module builds and its full suite (core, `local`,
    `openpgpkey`, `verify`, e2e steps) passes; the `depfootprint_test.go` guard
    holds (no go-tool-base / cloud / viper / charm / pflag in the core graph); the
    standard cicd framework is wired (lint, test+Godog, security, releaser-pleaser,
    zensical-pages, renovate); docs publish to `signing.phpboyscout.uk`. go-tool-base
    consumes it (`pkg/signing` and `pkg/openpgpkey` deleted; every caller repointed),
    and afmpeg imports `signing/verify` with a module graph free of go-tool-base and
    AWS. Work items 1–7 complete; item 8 (ffmpeg-wasi pipeline `signs:` block) is a
    downstream-pipeline follow-up tracked in that repo.

## 1. Context & motivation

`phpboyscout/afmpeg` must authenticate the `phpboyscout/ffmpeg-wasi` wasm release
assets it depends on, using the org's existing **embedded-keys + WKD
fingerprint cross-check + detached-OpenPGP-signature** model — the same model
that powers `gtb update`. We want **one signing/verification model across the
org**, reused rather than reimplemented, and usable by projects that are **not**
built on the go-tool-base framework.

Consumer roles (agreed):

- **afmpeg** — uses the **verification** mechanics to verify ffmpeg-wasi assets.
- **ffmpeg-wasi** — **signs** its assets via the **`gtb sign` CLI** (no library
  work needed on its side).
- **GTB CLI** — consumes the module for both `gtb sign` (signing) and `gtb
  update` (verification); provides the AWS KMS backend.
- **Any other consumer** — can build its own signing tool on the module's
  mechanics (no GTB framework weight), and can implement its own backend.

## 2. Validity assessment (confirmed against the code)

- **Verification surface** (`pkg/setup/signing*.go`) references only `pkg/http`,
  `pkg/logger`, `pkg/openpgpkey` from gtb — no config/vcs/updater/AWS. Light.
- **Signing is already dependency-inverted.** `pkg/signing` is a `crypto.Signer`
  **`Backend` registry** (`backend.go`, `registry.go`) with **zero heavy deps**;
  the signing mechanic (`openpgpkey.Sign`) operates on a stdlib `crypto.Signer`
  and has no knowledge of any provider. Backends self-register via blank-import:
  `pkg/signing/local` (PEM, light) and `pkg/signing/kms` (**57 AWS SDK
  modules** — heavy).
- **`openpgpkey`** (sign + verify) depends only on `go-crypto`.
- **Logger is slog-compatible** — `logger.NewSlog(slog.Handler)` and
  `(*slogLogger).Handler()` give bidirectional interop, so stdlib `slog` is the
  injection seam.

**Why a standalone module (not an in-module package).** Under Go 1.17+
module-graph pruning, any module that *provides an imported package* contributes
its **entire `go.mod` require list** to the consumer's module graph. An in-module
`pkg/signing/verify` would still force afmpeg to `require go-tool-base` and
inherit its full requires (AWS, Viper, OTel, charm) in `go.mod`/`go.sum`. Only a
separate module keeps go-tool-base out of consumers' module graphs.

**Why dependency-inverted backends (not bundled cloud backends).** The same
module-graph rule applies one level down: if the module's own `go.mod` required
the AWS SDK (because it shipped a `kms` package), **every** consumer — including
verify-only afmpeg — would inherit 57 AWS modules. So the module must **not**
implement cloud backends at all. It defines the `Backend` interface; heavy
implementations live at the point of use and are injected.

## 3. Goals / non-goals

**Goals**

- One light, independently-versioned module exposing: the `Backend` contract +
  registry, the signing mechanics (`crypto.Signer` → armored detached
  signature), the verification trust model, and `openpgpkey`.
- Module `go.mod` carries **no cloud SDK, ever** — only `go-crypto` (+
  `x/crypto`) and `cockroachdb/errors`.
- A consumer's module graph contains **no go-tool-base and no AWS/GCP/Azure**
  unless it explicitly imports a backend that needs them.
- go-tool-base consumes the module and behaves identically (`gtb sign` / `gtb
  update` unchanged); internal callers are repointed to the new import paths
  (clean break, no aliases — pre-1.0).

**Non-goals**

- New signing/verification *logic* (packaging + decoupling only).
- A second CLI (the GTB CLI is the CLI; the module is mechanics).
- Implementing AWS/GCP/Azure backends in the module (they are injected).

## 4. Design

### 4.1 The module (proposed `gitlab.com/phpboyscout/signing`)

A single light module containing:

- **`Backend` contract + registry** — minimal interface so a backend is trivial
  to implement:

  ```go
  type Backend interface {
      Name() string
      NewSigner(ctx context.Context, keyID string) (crypto.Signer, error)
  }
  ```

  `keyID` carries the backend-specific identifier (KMS ARN/alias, PEM path, …),
  so CLI flag wiring stays a consumer concern and the contract needs no `pflag`
  (refinement vs today's `RegisterFlags`; see §4.4). A global `Register`/`Get`/
  `Names` registry (a plain map, no heavy deps) supports `--backend <name>`
  selection via blank-import.

- **Signing mechanics** — produce an ASCII-armored OpenPGP detached signature
  from a `crypto.Signer` + an armored public identity (`openpgpkey.Sign`). It
  never references a provider.

- **`local` backend** — the PEM-on-disk backend, **included** as a light
  (stdlib-crypto) default and reference implementation of the contract.

- **Verification** — the resolvers (`NewEmbeddedResolver`, the WKD resolver,
  `CompositeResolver{RequireAll}`), `BuildKeyResolver`, `KeyResolverConfig`,
  `TrustSet`, `LoadTrustSet`, `VerifyManifestSignature[Signer]`, sentinel errors.

- **`openpgpkey`** — sign + verify primitives, shared by both halves.

### 4.2 Backends are injected, not bundled (cloud backends excluded)

The module ships **only** the `Backend` contract + the light `local` backend. Any
heavy/remote backend is implemented by the consumer and supplied by either:

- **explicit injection** — pass the `crypto.Signer` (or a `Backend`) directly to
  the signing call (best for "build your own tool"); or
- **the named registry** — blank-import a backend package so it self-registers,
  then select `--backend <name>` (what the GTB CLI uses).

GTB keeps `pkg/signing/kms` (AWS) and any future Azure/GCP backends in its own
tree, implementing the module's `Backend`. The AWS weight therefore stays in the
GTB binary, exactly as today. **The org library never implements or maintains a
cloud SDK**, and the backlog Azure/GCP KMS specs reduce to "implement the
`Backend` interface", not library work.

### 4.3 Seams (decoupled, GTB-consistent)

- **Signing key:** stdlib `crypto.Signer` (via `Backend`).
- **Logging:** stdlib `*slog.Logger` (nil → discard). GTB injects
  `slog.New(<gtb logger handler>)`; any consumer passes `slog.Default()` or its
  own. Replaces the `pkg/logger` (charmbracelet) dependency.
- **HTTP (WKD):** stdlib `*http.Client` (already on `KeyResolverConfig`; the
  `gtbhttp.NewClient()` nil-default becomes a stdlib client). Replaces the
  `pkg/http` (Viper/OTel/config) dependency.

### 4.4 Refinements vs current code

- **CLI-agnostic contract.** Today `Backend.RegisterFlags(*pflag.FlagSet)`
  couples the contract to `pflag`. Drop it from the core interface; backends
  needing extra CLI flags implement an **optional** `FlagRegistrar` interface the
  *CLI* checks for. Keeps `pflag` out of the module core.
- **Two wiring styles** offered (injection + registry), per above.

### 4.5 Resulting footprint

Module `go.mod`: `github.com/ProtonMail/go-crypto`,
`github.com/cockroachdb/errors` (+ `golang.org/x/crypto` transitively). No cloud
SDK, no `pkg/http`/`pkg/logger`, no `pflag` in core. (Open question §6: drop
`cockroachdb/errors` for stdlib to make it crypto-only.)

## 5. go-tool-base integration & migration

- go-tool-base depends on the new module.
- The moved surfaces (`pkg/signing` interface+registry+`local`, the `pkg/setup`
  verification files, `pkg/openpgpkey`) are **deleted** from gtb. **No re-export
  aliases** — every internal caller is repointed to the new module's import paths
  in the same change (pre-1.0 clean break). `SelfUpdater`, `gtb sign`, and
  `gtb update` behave identically; only their imports change. A migration note in
  `docs/reference/migration/` documents the moved paths for any downstream
  importing `setup.*` / `signing.*` / `openpgpkey.*` directly.
- GTB's `pkg/signing/kms` is repointed to implement the module's `Backend`; the
  `cmd/gtb` blank-imports wire `kms` (GTB-owned) + `local` (from the module).
- GTB injects its logger via `slog.New(...)` and its hardened `*http.Client`.
- Migration note in `docs/reference/migration/` for any downstream importing the
  old `setup.*` / `signing.*` paths directly.

## 6. Development model (TDD / BDD)

The module follows the same workflow as go-tool-base.

- **TDD, test-first.** Derive failing tests from the public contract, error
  cases, and edge cases before implementing. Table-driven tests with
  `t.Parallel()`; `github.com/cockroachdb/errors` for error creation/wrapping;
  **≥90% coverage** (the `pkg/` policy applies to the whole module).
- **No package-level mocking hooks** (they race under `t.Parallel`). Inject seams
  via interfaces / struct fields / functional options. The `Backend` seam (and a
  test `crypto.Signer`/`Backend` fake) are the primary injection points;
  **mockery** generates the interface mocks under `mocks/`.
- **Logging in tests** uses a discard `*slog.Logger` (`slog.New(slog.DiscardHandler)`),
  not a gtb logger — the module must not depend on `pkg/logger`.
- **BDD (Godog/Gherkin).** Although this is a library (no CLI), its *observable
  security contract* is exactly the kind of behaviour BDD expresses well, so a
  **full Godog suite ships with v0.1.0** as the executable behaviour spec —
  exhaustive coverage of the scenarios below plus their error/edge paths. Feature
  files in `features/`, steps in `test/e2e/steps/`, mirroring go-tool-base's
  Godog setup (env-var-gated). Core scenarios:
  - sign → verify round-trip succeeds for a matching key;
  - a tampered manifest / signature is **rejected**;
  - WKD fingerprint mismatch is **rejected** (`ErrKeyResolverMismatch`);
  - embedded-only, WKD-only, and composite (`RequireAll` true/false) resolution;
  - the `local` backend signs a payload the verifier accepts end-to-end.

## 7. Documentation (Diátaxis)

A **full Diátaxis zensical-pages site ships with v0.1.0**, following the org's
layout and its standing concessions (API reference → pkg.go.dev; the
hand-maintained `reference/` tree is CLI/config/migration only; site served via
zensical):

- **Reference → pkg.go.dev.** Thorough, example-bearing godoc on every exported
  symbol is the API reference; no hand-duplicated API tables.
- **How-to** (the bulk for a library): *verify a signed release*; *sign with the
  `local` backend*; *implement and inject a custom `Backend`*; *configure
  embedded + WKD trust*.
- **Explanation**: the trust model (embedded keys + WKD fingerprint cross-check +
  detached OpenPGP signature), the dependency-inversion design (why no cloud
  backends in the module), and a **threat model / security boundaries** page in
  the style of the existing `pkg/components/*` threat-model docs.
- **Tutorial**: a short getting-started ("verify your first signed release").
- A README is the entry point; runnable `Example` tests double as docs and are
  surfaced on pkg.go.dev. The zensical site is published via the `zensical-pages`
  cicd component (as go-tool-base does).

## 8. Repository & project framework

New repo `gitlab.com/phpboyscout/signing` bootstrapped with the same framework as
the other org Go projects (it is a *library*, so no `cmd/`, no goreleaser/binaries):

- **CI** via the shared cicd components on the dev-tools image: `go-lint`,
  `go-test` (with the Godog e2e enabled), `go-security` (govulncheck + scanners).
  **releaser-pleaser** for versioning + changelog; **renovate-self** for dep
  bumps; **`zensical-pages`** for the Diátaxis docs site. No `goreleaser`
  component (library — no binaries; and per decision 6 no release-asset signing).
- **`.golangci.yaml`** (v2) mirroring go-tool-base's linter set, with local
  import prefix `gitlab.com/phpboyscout/signing`.
- **`justfile`** with the standard recipes (`test`, `test-race`, `lint`,
  `lint-fix`, `mocks`, `ci`, `coverage`, `bench`, `vuln`, `deadcode`).
- **`.mockery.yml`** for the `Backend` (and any seam) mocks.
- **Conventional Commits** + the **spec-driven workflow** (the repo carries its
  own `docs/development/specs/` for future changes; this foundational spec stays
  in go-tool-base as the extraction record).
- **Dependency-footprint guard** (the §10 test) promoted to a first-class CI
  check so the "stays light" invariant is enforced, not just documented.
- `go` directive matching the org toolchain (currently `1.26.x`).

Note: gtb's generator scaffolds *CLI tools*, not libraries, so this repo is
hand-bootstrapped from the conventions above (a future "library skeleton" for the
generator is out of scope here).

## 9. Resolved decisions

1. **Module name** → `gitlab.com/phpboyscout/signing`.
2. **Errors** → keep `github.com/cockroachdb/errors` (consistent org error style;
   footprint stays go-crypto + errors).
3. **Backend wiring** → **both** the named registry (`--backend <name>` via
   blank-import, for the gtb CLI) **and** direct injection (pass a
   `Backend`/`crypto.Signer`, for embedding/tests/custom tools).
4. **Backend contract** → **minimal** (`Name()` + `NewSigner(ctx, keyID)`);
   backends needing CLI flags implement an **optional `FlagRegistrar`** the CLI
   checks for. No `pflag` in the module core.
5. **go-tool-base transition** → **hard cut-over, no re-export aliases.** Update
   all internal callers to the new import paths and remove the old symbols in one
   change; document the break with a migration note. (We are pre-1.0, so a clean
   break is preferred over carrying shims.)
6. **Self-signing the module's releases** → **N/A (dropped).** A pure Go library
   ships no binary release asset to sign; `go get` integrity is already provided
   by `go.sum` + the checksum transparency log (`GOSUMDB`), not detached OpenPGP
   signatures (that model is for binary distributions like `gtb update`).
   GPG-signed git tags are general repo hygiene, orthogonal to this module and
   not part of the `go get` verification path.
7. **BDD scope** → **full Godog feature suite from v0.1.0** — exhaustive
   Given/When/Then coverage of the security contract (round-trip, tamper +
   WKD-mismatch rejection, embedded/WKD/composite resolution, local backend,
   plus error/edge paths), not just the core scenarios.
8. **Docs** → **full Diátaxis zensical-pages site at v0.1.0** (tutorial, how-to,
   explanation incl. threat model) alongside godoc/pkg.go.dev as the API
   reference — consistent with go-tool-base.

## 10. Work items (once approved)

1. **Bootstrap the repo to the standard framework (§8):** `gitlab.com/phpboyscout/signing`
   module (`go 1.26.x`); cicd `go-lint`/`go-test`(+Godog)/`go-security` on the
   dev-tools image; releaser-pleaser; renovate-self; `.golangci.yaml` (v2, local
   prefix); `justfile`; `.mockery.yml`; Conventional Commits; `docs/development/specs/`.
2. **Move the code:** `openpgpkey`; the `Backend` contract + registry; the signing
   mechanics; the `local` backend; the verification surface. Decouple HTTP (stdlib
   client) and logging (`*slog.Logger`); apply the CLI-agnostic contract refinement.
3. **TDD/BDD + guard:** carry the existing signing/verification unit tests over
   (test-first for any new seams), keep **≥90% coverage**, generate mocks via
   mockery, and add the **dependency-footprint guard test** asserting
   `go list -deps ./...` contains no `go-tool-base`, `aws`, `gcp`, `azure`,
   `opentelemetry`, `viper`, `charmbracelet`, or `pflag` (core) — wired as a CI gate.
   Add the Godog security scenarios (§6).
4. **Docs (Diátaxis, §7):** godoc + `Example` tests on the exported surface;
   README; the full zensical-pages site (tutorial, how-to, explanation +
   threat-model) wired via the `zensical-pages` component.
5. Cut `signing v0.1.0`.
6. **go-tool-base:** depend on it; **delete the moved files and repoint every
   internal caller to the new import paths (no aliases)**; repoint
   `pkg/signing/kms` to the module's `Backend`; fix blank-imports; verify
   `gtb sign` + `gtb update` + the full suite; add a migration note.
7. **afmpeg:** import the module's verification; confirm its module graph excludes
   go-tool-base and AWS.
8. *(Pipeline-side)* **ffmpeg-wasi:** adopt the GoReleaser `signs:` block + a key
   so its wasm releases carry the signature afmpeg verifies (uses `gtb sign`).
