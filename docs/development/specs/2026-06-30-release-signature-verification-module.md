---
title: "Standalone release-signature verification module (reusable by non-GTB projects)"
description: "Extract the OpenPGP/WKD release-signature VERIFICATION primitives currently in pkg/setup into a standalone, dependency-light Go module so non-go-tool-base projects (afmpeg verifying ffmpeg-wasi's signed wasm releases) can verify signatures without dragging in the go-tool-base module dependency tree. Driven by work item #1."
date: 2026-06-30
status: DRAFT
tags:
  - specification
  - signing
  - verification
  - openpgp
  - wkd
  - module-extraction
  - reuse
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Standalone release-signature verification module

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   30 June 2026

Status
:   DRAFT

Tracking
:   work item #1 (issue: "Extract release-signature verification from pkg/setup
    into a dependency-light package")

---

## 1. Context & motivation

`phpboyscout/afmpeg` needs to verify `phpboyscout/ffmpeg-wasi`'s signed releases —
the wasm assets afmpeg depends on are essential and must be authenticated. The
exact verification model afmpeg wants (**embedded keys + WKD fingerprint
cross-check + detached OpenPGP signature over the checksums manifest**) already
exists and is proven in go-tool-base, powering `gtb update`'s signed self-update.

The goal is **reuse, not reimplementation** — one signing/verification model
across the org. The verification API is already designed for reuse (`KeyResolverConfig`
is deliberately Viper-decoupled with injectable `HTTPClient`/`Logger`).

### Scope clarification: verify vs sign

- **Verification** (consuming a signature) is a Go library surface → this is what
  we extract for afmpeg to `import`.
- **Signing** (producing a signature) is **not** a Go API in go-tool-base. It is
  release-pipeline config: GoReleaser's `signs:` block calls
  `scripts/sign-release.sh` → `gtb sign --backend aws-kms`. A project that wants
  to *sign* its releases (e.g. `ffmpeg-wasi`) adopts that pipeline pattern + a
  signing key; it does not import a "signing library". **This spec covers the
  verification module only.** Signing-pipeline adoption for ffmpeg-wasi is a
  separate, small pipeline task (noted in §8).

## 2. Validity assessment

Confirmed against the code:

- The verification surface lives in five files (`pkg/setup/signing.go`,
  `signing_resolver.go`, `signing_wkd.go`, `signing_composite.go`,
  `signing_embedded.go`) and references **only three** gtb packages —
  `pkg/http`, `pkg/logger`, `pkg/openpgpkey` — and otherwise stdlib +
  `ProtonMail/go-crypto` + `cockroachdb/errors`. No `pkg/config`, `pkg/vcs`,
  `SelfUpdater`, `pkg/changelog`, or AWS SDK. The surface is self-contained.
- `pkg/openpgpkey` is itself clean (only `go-crypto` + `golang.org/x/crypto`).

**The work item's premise needs one correction.** It proposes an *in-module*
package (`pkg/signing/verify`). That yields a light **compile** graph for the
consumer, but does **not** meet the stated goal, because of Go module-graph
rules:

> Under Go 1.17+ module-graph pruning, any module that **provides an imported
> package** contributes its **entire `go.mod` require list** to the consumer's
> module graph. If afmpeg imports any package from go-tool-base, go-tool-base
> "provides an imported package", so its full requires — AWS SDK, Viper,
> OpenTelemetry, charmbracelet, etc. — land in **afmpeg's `go.mod`/`go.sum` and
> module graph**, even though none of it compiles into the binary.

Two of the three gtb deps are also transitively heavy on their own: `pkg/logger`
pulls the charmbracelet terminal stack, and `pkg/http` pulls Viper +
OpenTelemetry + `pkg/config`/`pkg/authn`/`pkg/tls`.

**Conclusion:** the request is valid and worthwhile, but an in-module extraction
does not achieve "no go-tool-base weight for consumers". Only a **separate
module** keeps go-tool-base out of a consumer's `go.mod` entirely. We therefore
target a standalone module.

## 3. Goals / non-goals

**Goals**

- A standalone, independently-versioned Go module exposing the release-signature
  **verification** surface, depending only on `go-crypto` (+ `x/crypto`) and
  `cockroachdb/errors`.
- A consumer (`afmpeg`) can `import` it and its `go.mod`/module graph contains
  **no go-tool-base**, no `pkg/config`/`pkg/vcs`, no OpenTelemetry/Viper/AWS.
- go-tool-base consumes the new module and behaves identically (`gtb update`
  unchanged); source compatibility preserved via re-export aliases in `pkg/setup`.

**Non-goals**

- New signing/verification *logic* — this is a packaging/decoupling change.
- A signing (signature-producing) library API (signing stays pipeline-side).
- Changing the trust model, WKD endpoint, or key material.

## 4. Design

### 4.1 New module

A new repository/module — proposed `gitlab.com/phpboyscout/releasetrust` (name is
an open question, §7). Contents:

- The verification surface, moved verbatim where possible:
  `KeyResolver`, `KeyResolverConfig`, `BuildKeyResolver`,
  `TrustSet`, `LoadTrustSet`, `VerifyManifestSignature` / `VerifyManifestSigner`,
  `NewEmbeddedResolver`, `CompositeResolver{RequireAll}`, the WKD resolver, and the
  sentinel errors (`ErrKeyResolverMismatch`, …).
- **`openpgpkey` moved into the new module** (e.g. `releasetrust/openpgpkey`). The
  verifier imports it, so it must live here — otherwise the new module would
  re-require go-tool-base and defeat the purpose. It is self-contained, so the
  move is clean.

### 4.2 Decoupling from `pkg/http` and `pkg/logger`

- **HTTP:** `KeyResolverConfig.HTTPClient *http.Client` already exists and WKD
  uses it. The only `pkg/http` use is the nil-default `gtbhttp.NewClient()` in
  `BuildKeyResolver`. Replace the default with a stdlib `*http.Client` (sane
  timeout). Consumers that want gtb's hardened client still inject it. Drops the
  `pkg/http` dependency.
- **Logging:** `logger.Logger` is used for exactly one fail-open `Warn` in the
  composite resolver. Replace with a **minimal local interface** in the new
  module, e.g.:

  ```go
  // Logger is the tiny sink the composite resolver uses for fail-open
  // warnings. gtb's logger.Logger satisfies it; nil is allowed (no-op).
  type Logger interface { Warn(msg string, keyvals ...any) }
  ```

  Drops the `pkg/logger` (charmbracelet) dependency. gtb's `logger.Logger`
  structurally satisfies this interface, so go-tool-base passes its logger
  through unchanged.

### 4.3 Resulting dependency footprint

New module `go.mod`: `github.com/ProtonMail/go-crypto`,
`github.com/cockroachdb/errors` (+ `golang.org/x/crypto` transitively). Nothing
else. (Open question §7: drop `cockroachdb/errors` for stdlib `errors` to make
the module fully crypto-only.)

### 4.4 go-tool-base integration

- go-tool-base adds the new module as a dependency.
- The five `pkg/setup/signing*.go` files are removed; `pkg/setup` re-exports the
  surface via **type aliases + function wrappers** for source compatibility
  (existing imports of `setup.BuildKeyResolver`, `setup.TrustSet`, etc. keep
  compiling). The `SelfUpdater` is unchanged; it constructs the resolver with
  gtb's hardened `*http.Client` and `logger.Logger` as today.
- All in-gtb importers of `pkg/openpgpkey` switch to the new module's path
  (audit: verify which packages import openpgpkey before the move).
- `gtb update` behaviour and the signed-release verification path are unchanged
  (covered by the existing tests, which move with the code).

### 4.5 New module CI/repo

Mirror the established org pattern (`images/dev-tools`): releaser-pleaser for
versioning, the `cicd/go-*` components on the dev-tools image, govulncheck. The
module is small and pure-Go (no goreleaser/binaries needed). Open question §7:
whether the module's own releases should be signed (dogfooding) — likely yes,
later, but not blocking.

## 5. Migration & compatibility

- **Source compat for go-tool-base:** re-export aliases mean no churn for
  existing `pkg/setup` callers. (Optional: mark the aliases `// Deprecated:` to
  nudge internal callers to the new import over time.)
- **Pre-1.0:** both modules are v0.x; the new module starts at v0.1.0.
- A migration note in `docs/reference/migration/` documents the new import path
  for any downstream already using `setup.*` verification symbols directly.

## 6. Testing strategy

- Move the existing `signing_*_test.go` suite into the new module (it already
  covers embedded/WKD/composite/subkey/script-integration paths) — strong
  inherited coverage; target ≥90%.
- **Dependency-footprint guard:** a CI check in the new module asserting
  `go list -deps ./...` contains no `go-tool-base`, `opentelemetry`, `viper`,
  `aws`, or `charmbracelet` — locking the "stays light" acceptance criterion so a
  future careless import can't regress it.
- go-tool-base: existing `pkg/setup` signing tests pass against the re-exported
  surface; `gtb update` e2e unchanged.
- A consumer smoke (in afmpeg, or a tiny example) proving `BuildKeyResolver →
  Resolve → VerifyManifestSignature` works with a minimal `go.mod`.

## 7. Open questions

1. **Module name.** `gitlab.com/phpboyscout/releasetrust`? Alternatives:
   `sigverify`, `opgpverify`, `release-verify`. (Proposed: `releasetrust`.)
2. **Drop `cockroachdb/errors`?** Using stdlib `errors`/`fmt.Errorf` would make
   the module dependency-free beyond crypto. Trade-off: lose the org's standard
   error wrapping/hints. (Proposed: keep `cockroachdb/errors` — it's light and
   keeps error style consistent; revisit if a consumer objects.)
3. **openpgpkey placement.** Sub-package of the new module
   (`releasetrust/openpgpkey`) vs a third tiny module. (Proposed: sub-package.)
4. **Re-export vs hard break in `pkg/setup`.** Keep deprecated aliases for a
   transition window, or cut over internal callers immediately and drop them?
   (Proposed: aliases now, deprecate, remove later.)
5. **Sign the new module's own releases?** (Proposed: defer; not blocking afmpeg.)

## 8. Work items (once approved)

1. Create the `releasetrust` repo + module + CI (releaser-pleaser, go-* cicd
   components, govulncheck, the dependency-footprint guard test).
2. Move `openpgpkey` + the five verification files into it; decouple HTTP
   (stdlib default client) and logging (minimal `Logger` interface).
3. Cut `releasetrust v0.1.0`.
4. In go-tool-base: depend on it, delete the moved files, add re-export aliases
   in `pkg/setup`, repoint openpgpkey importers; verify `gtb update` + tests.
5. afmpeg: import `releasetrust`, verify `ffmpeg-wasi` manifests; confirm its
   module graph excludes go-tool-base.
6. *(Separate, pipeline-side)* ffmpeg-wasi: adopt the GoReleaser `signs:` block +
   a signing key so its wasm releases carry the detached signature afmpeg verifies.
