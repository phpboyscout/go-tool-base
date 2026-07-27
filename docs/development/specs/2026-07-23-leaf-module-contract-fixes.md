---
title: "Leaf-module contract fixes: errorhandling, credentials, output, authn"
description: "Batched MEDIUM/LOW findings from the architectural review where a leaf module's documented contract and its implementation disagree: errorhandling.Fatal never exits on special errors (usage errors exit 0), credentials.Probe can hang the setup wizard on a wedged keychain, output.ParseFormat is case-sensitive despite its doc, plus three small hardening/doc items."
date: 2026-07-23
status: IMPLEMENTED
tags:
  - specification
  - errorhandling
  - credentials
  - output
  - authn
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# Leaf-module contract fixes: errorhandling, credentials, output, authn

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   IMPLEMENTED — 2026-07-27 (all four modules fixed and merged)

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (cross-cutting leaf modules section); [redact: scheme-agnostic URL userinfo stripping](2026-07-23-redact-scheme-agnostic-userinfo.md) (covers the redact HIGH and JSON LOW findings from the same review section — excluded here); [setup credential-stage context scoping](2026-07-23-setup-credential-stage-context-scoping.md) (GTB-side context plumbing for the setup wizard — the Probe fix below complements it by making the deadline actually bite inside the module)

---

## 1. Context

The 2026-07-23 architectural review of the extracted leaf modules found the modules structurally sound, but flagged a cluster of places where a module's **documented contract and its implementation disagree** — a fatal handler that doesn't exit, a probe that can't be timed out, a parser that isn't case-insensitive as documented, plus three smaller hardening/doc items. None warrants a standalone spec; all are small, independent fixes to published `go/*` modules. This spec batches them so each module gets one coherent fix release and GTB picks them up via routine dependency bumps.

## 2. Items

### 2.1 go/errorhandling

#### [MEDIUM] `Fatal` on special errors never exits — process exits 0 on usage errors

`go/errorhandling/handling.go:92-101` — `Check` returns early when `handleSpecialErrors` handles `ErrRunSubCommand` / `ErrNotImplemented`, so the `LevelFatal` branch (`handling.go:176-181`) and `h.Exit` are never reached. Verified empirically: `h.Fatal(ErrRunSubCommand)` and `h.Fatal(NewErrNotImplemented(...))` do not call the exit func — a CLI whose main does `handler.Fatal(err)` on "subcommand required" prints usage and then exits **0**, so `if tool; then …` in a script treats an invalid invocation as success (cobra exits 1 in the same situation).

**Direction:** after the special-error presentation, still honour a fatal level with a non-zero exit (usage errors conventionally exit 2 or 64; see open question 1).

**Note — behaviour-breaking by design:** this changes observable exit codes: usage errors move from 0 to non-zero. That is the desired fix (the current behaviour contradicts universal CLI convention), but it is technically a behavioural break for any script that relied on exit 0 here; the commit body and a `docs/migration/` note must call it out.

**Acceptance:** `h.Fatal(ErrRunSubCommand)` and `h.Fatal(NewErrNotImplemented(...))` call the exit func with a non-zero code while still printing the special-error presentation; non-fatal levels remain non-exiting.

#### [LOW] `WithWriter` / the `Writer` field is a dead no-op

`go/errorhandling/options.go:18-22`, `handling.go:64,75` — `Writer` is initialised to `os.Stderr` and settable via `WithWriter`, but no production code path ever writes to it (all output goes through `h.Logger`; usage goes through the `Usage` seam). Callers configuring it get silent nothing.

**Direction:** remove the field and option (pre-1.0 clean break, per module convention), or route the special-error/usage presentation through it — whichever review prefers; do not leave a documented option that does nothing.

**Acceptance:** either `WithWriter` is gone, or a test proves output actually reaches an injected writer.

### 2.2 go/credentials

#### [MEDIUM] `Probe` can hang the setup wizard — the shipped keychain backend cannot honour its context

`go/credentials/mode.go:132-135` documents "pass a context with a short timeout … so a misbehaving remote backend cannot stall the setup flow", but `Probe` calls the backend's `Store`/`Retrieve`/`Delete` synchronously and the shipped go-keyring backend (`go/credentials/keychain/keychain.go:40`) ignores the context entirely (as its own doc admits). `KeychainOpTimeout` (`wizard.go:16`) is purely advisory. Concrete failure: headless Linux with a wedged D-Bus Secret Service, or a locked macOS keychain waiting on a GUI unlock dialog, blocks `Probe` — and therefore first-run setup — indefinitely, regardless of any deadline the caller derives.

**Direction:** enforce the deadline in the `credentials` package itself: run each backend call in a goroutine and `select` on the result channel vs `ctx.Done()`, accepting the abandoned-goroutine tradeoff exactly as `regexutil.CompileBounded` does and documents. On timeout, `Probe` returns false (keychain treated as unavailable — the safe wizard outcome). This complements the setup-credential-stage context-scoping spec: that spec plumbs a properly scoped context into the stage; this fix makes the deadline actually enforceable at the module boundary.

**Acceptance:** with a backend that blocks forever, `Probe(ctx)` with a 100 ms-deadline context returns false within the deadline (with sensible slack) instead of hanging.

#### [LOW] `RegisterBackend(nil)` poisons the process

`go/credentials/backend.go:78-80` — `RegisterBackend` stores `&b` unconditionally; a nil `Backend` interface value makes `currentBackend()` return nil and the next `Store`/`Retrieve`/`KeychainAvailable` call nil-panic, far from the registration site.

**Direction:** `if b == nil { return }` (ignore) or restore the unavailable stub on nil — either closes it; ignoring is the least-surprise choice.

**Acceptance:** `RegisterBackend(nil)` followed by `KeychainAvailable()` neither panics nor changes the previously active backend.

### 2.3 go/output

#### [MEDIUM] `ParseFormat` documents case-insensitivity but is exact-match

`go/output/format.go:27-37` — the doc says "maps a **case-insensitive** string to a `Format`", but the switch matches `Format(s)` exactly. Verified: `ParseFormat("JSON")` → `("text", false)`. Any CLI that trusts the doc for its `--output` flag silently degrades `--output JSON` to text output — breaking scripted consumers expecting JSON on stdout — instead of erroring or honouring it.

**Direction:** `switch Format(strings.ToLower(s))` — one line, plus table-driven case-variant tests.

**Acceptance:** `ParseFormat("JSON")`, `ParseFormat("Yaml")` etc. return the corresponding format with `true`; unknown strings still fall back to `(FormatText, false)`.

### 2.4 go/authn

#### [LOW] Package doc overstates its purity

`go/authn/authn.go:2` claims the package "carries no HTTP or gRPC imports" — but `jwt.go` and `jwks.go` import `net/http` for JWKS/OIDC fetching. The intended claim (no *server-transport* coupling; reusable from both transports) is true; the literal wording is not.

**Direction:** reword the doc comment to say it carries no server-transport (HTTP/gRPC server) coupling, noting the outbound JWKS/OIDC HTTP client as the one deliberate `net/http` use. Doc-only change.

**Acceptance:** the package doc no longer contains a claim contradicted by the package's own imports.

## 3. Scope & release plan

- **Modules:** `gitlab.com/phpboyscout/go/errorhandling`, `go/credentials`, `go/output`, `go/authn` — one MR per module, each fixing that module's items above.
- **Releases:** conventional `fix(<module>):` commits; releaser-pleaser cuts patch releases for credentials/output/authn. The errorhandling exit-code change is behaviour-breaking (desired); pre-1.0 that still ships as a **minor** bump with the break noted in the commit body — no `BREAKING CHANGE:` footer. `WithWriter` removal (if chosen) rides the same minor.
- **Docs:** each fix updates the module's own doc comments; the errorhandling change adds a migration note in GTB `docs/migration/`.
- **GTB pickup:** routine Renovate dependency bumps — no GTB code changes expected, except reviewing any GTB call site that relied on `Fatal`'s current exit-0 behaviour (none known) and updating `docs/components/` cross-references where behaviour is described.

## 4. Open questions

1. **Exit code for usage/special errors:** 1 (cobra parity), 2 (common Unix "misuse" convention), or 64 (`EX_USAGE`, sysexits)? Recommendation: 2 — conventional, portable, and distinct from generic failure 1; but this is a public-contract choice worth a deliberate call.
2. **Is `Probe`'s abandoned-goroutine tradeoff acceptable?** A timed-out backend call leaves a goroutine blocked until the underlying syscall/D-Bus call returns (possibly never). Recommendation: yes — it is bounded to one goroutine per probe attempt, `Probe` runs once per wizard step, and `regexutil` already sets the precedent of documenting exactly this tradeoff rather than pretending the deadline is free.
3. **`WithWriter`:** remove or wire up? Recommendation: remove (clean break, pre-1.0) unless review wants the special-error/usage presentation decoupled from the logger, in which case wiring it there is the natural home.

## 5. Resolution (2026-07-27)

All items implemented and merged, each red-first (a regression test that captured
the defect on unmodified `main` before the fix):

- **Q1 — exit code: 2.** `errorhandling.Fatal` on `ErrRunSubCommand` /
  `NewErrNotImplemented` now presents the special error **and** exits with code
  **2** (`ExitCodeUsage`), the conventional Unix "misuse" code; non-fatal levels
  stay non-exiting. Red: `TestErrorHandler_Fatal_SpecialErrors` (exit func not
  called on `main`). Migration note added at
  [`v0.x-errorhandling-fatal-exit-code`](../../reference/migration/v0.x-errorhandling-fatal-exit-code.md).
- **Q3 — `WithWriter`: removed** (clean break, pre-1.0). The dead `Writer`
  field + option are gone; all output flows through the logger/usage seam.
- **Q2 — Probe goroutine tradeoff: accepted.** `credentials.Probe` now enforces
  the caller's deadline itself (goroutine + `select` on `ctx.Done()`), returning
  `false` on timeout so a wedged keychain reads as unavailable — the safe wizard
  outcome. The bounded abandoned-goroutine tradeoff is documented on `Probe`,
  mirroring `regexutil.CompileBounded`. Red: `TestProbe_DeadlineHonoured`
  (blocked past 2s on `main`).
- **`RegisterBackend(nil)`** now ignored (`if b == nil { return }`); the
  previously active backend is preserved. Red: `TestRegisterBackend_NilIgnored`
  (SIGSEGV on `main`).
- **`output.ParseFormat`** is now case-insensitive (`strings.ToLower`); unknown
  strings still fall back to `(FormatText, false)`. Red: `TestParseFormat` case
  variants returned `(text, false)` on `main`.
- **`authn`** package doc reworded: it carries no *server-transport* coupling and
  is reusable from both transports, noting the outbound JWKS/OIDC HTTP client as
  the one deliberate `net/http` use (contradicted by `jwt.go`/`jwks.go` imports
  before).

Merged MRs: errorhandling !12, credentials !15, output !18, authn !13.
