---
title: "Standalone signing & verification module (dependency-inverted, reusable by non-GTB projects)"
description: "Extract the OpenPGP/WKD release signing + verification primitives from pkg/signing and pkg/setup into one light, standalone Go module. The module owns the contract (a crypto.Signer-producing Backend interface), the signing mechanics, and the verification trust model; heavy cloud backends (AWS KMS, GCP, Azure) are NOT implemented in the module — consumers implement the interface and inject. Lets afmpeg verify ffmpeg-wasi's signed wasm releases, and lets any project sign without the go-tool-base framework. Driven by work item #1."
date: 2026-06-30
status: DRAFT
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
:   DRAFT

Tracking
:   work item #1

---

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
  update` unchanged), via re-export aliases for source compatibility.

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
  verification files, `pkg/openpgpkey`) are deleted from gtb; `pkg/signing` and
  `pkg/setup` **re-export** the symbols via type aliases + wrappers for source
  compatibility. `SelfUpdater` and `gtb sign`/`gtb update` are unchanged.
- GTB's `pkg/signing/kms` is repointed to implement the module's `Backend`; the
  `cmd/gtb` blank-imports wire `kms` (GTB-owned) + `local` (from the module).
- GTB injects its logger via `slog.New(...)` and its hardened `*http.Client`.
- Migration note in `docs/reference/migration/` for any downstream importing the
  old `setup.*` / `signing.*` paths directly.

## 6. Open questions

1. **Module name.** `gitlab.com/phpboyscout/signing` (clean, canonical) vs
   `releasetrust` / `sigkit` / `opgpsign`. (Proposed: `signing`.)
2. **Drop `cockroachdb/errors`?** stdlib `errors`/`fmt.Errorf` → crypto-only
   module, but loses the org's error-hint style. (Proposed: keep; revisit if a
   consumer objects.)
3. **Keep the global registry, or injection-only?** Registry adds the
   `--backend <name>` ergonomics GTB relies on; it's light. (Proposed: keep
   both.)
4. **`RegisterFlags` removal.** Confirm no GTB backend needs per-backend CLI
   flags beyond `keyID`; if one does, define the optional `FlagRegistrar`.
   (Proposed: minimal contract + optional interface.)
5. **Re-export window in gtb.** Deprecated aliases now, remove later — vs cut
   internal callers over immediately. (Proposed: aliases, deprecate, remove.)
6. **Self-sign the module's own releases?** (Proposed: defer; not blocking.)

## 7. Work items (once approved)

1. Create the `signing` repo + module + CI (releaser-pleaser, go-* cicd
   components, govulncheck) + a **dependency-footprint guard test** asserting
   `go list -deps ./...` contains no `go-tool-base`, `aws`, `gcp`, `azure`,
   `opentelemetry`, `viper`, `charmbracelet`, or `pflag` (core).
2. Move into it: `openpgpkey`; the `Backend` contract + registry; the signing
   mechanics; the `local` backend; the verification surface. Decouple HTTP
   (stdlib client) and logging (`*slog.Logger`); apply the CLI-agnostic contract
   refinement.
3. Cut `signing v0.1.0`.
4. go-tool-base: depend on it; delete moved files; add re-export aliases; repoint
   `pkg/signing/kms` to the module's `Backend`; fix blank-imports; verify
   `gtb sign` + `gtb update` + tests.
5. afmpeg: import the module's verification; confirm its module graph excludes
   go-tool-base and AWS.
6. *(Pipeline-side)* ffmpeg-wasi: adopt the GoReleaser `signs:` block + a key so
   its wasm releases carry the signature afmpeg verifies (uses `gtb sign`).
