---
title: "Extract pkg/credentials into a standalone go/credentials module"
description: "Extract the credential-storage abstraction (env-ref / OS-keychain / literal modes, the pluggable Backend registry, the opt-in keychain subpackage, the credtest in-memory helper, and the setup-wizard/CI-guard helpers) out of go-tool-base into gitlab.com/phpboyscout/go/credentials. The core is already framework-free — its only non-stdlib dependency is cockroachdb/errors and it imports zero go-tool-base packages — so this is a clean git-mv extraction with a direct caller repoint (no facade, no Props adapter). The one seam to resolve is the huh-returning StorageModeOptions helper: the module replaces it with a UI-agnostic Prompter abstraction (stdlib default, overridable by a tool's themed TUI) so no TUI dependency travels into the framework-free module. Keychain stays an opt-in subpackage per the standing decision-log rejection of a bundled vendor-backend family. Extracting credentials unblocks a clean go/vcs / go/releases extraction, which depends on it."
date: 2026-07-18
status: IMPLEMENTED
tags:
  - specification
  - credentials
  - keychain
  - secret-storage
  - module-extraction
  - reuse
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Extract `pkg/credentials` into a standalone `go/credentials` module

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   2026-07-18

Status
:   IMPLEMENTED (2026-07-18 — go/credentials v0.1.0 published; GTB cut over)

Related
:   [module-extraction playbook](2026-07-12-go-module-extraction-playbook.md) (§5 procedure, §10 chat findings, §11 controls findings),
    [credential-storage hardening](2026-04-02-credential-storage-hardening.md) (IMPLEMENTED — the trust model this package encodes),
    [credential phase-3 OAuth/SSH/keychain](2026-06-21-credential-phase3-oauth-ssh-keychain.md) (DRAFT roadmap — separate, not blocked by this),
    [extraction report](../reports/2026-07-07-package-extraction-report.md)

---

## Summary

`pkg/credentials` is the credential-storage abstraction shared by every GTB setup
wizard, resolver, and doctor check. It defines the three storage **modes**
(`ModeEnvVar` / `ModeKeychain` / `ModeLiteral`), a pluggable `Backend` registry
with an always-compiled no-op stub, an opt-in `keychain/` subpackage that registers
a go-keyring backend via a blank import, a `credtest/` in-memory backend for
downstream tests, and a set of wizard/CI-guard helpers (CI detection, env-var-name
validation, the literal-under-CI refusal, and the storage-mode selector).

This spec extracts it to `gitlab.com/phpboyscout/go/credentials` following the
[module-extraction playbook](2026-07-12-go-module-extraction-playbook.md). Like
`controls` and `authn` — and unlike `chat` — the **core is already framework-free**:
production `pkg/credentials` imports **zero** go-tool-base packages, and its only
non-stdlib dependency is `github.com/cockroachdb/errors`. There is no
`config.Containable` coupling and no `*FromProps` adapter to leave behind. This is
therefore a **clean-break, git-mv extraction with a direct caller repoint** — no
facade — with **one** decoupling task: the `StorageModeOptions` helper returns
`charm.land/huh/v2` options, and that TUI dependency should not travel into the
framework-free module (see [§Seams](#seams-and-decoupling)).

## Motivation

- **Reuse.** Env-ref / keychain / literal storage with a pluggable backend and an
  auditable opt-out surface is broadly useful to any Go CLI that handles secrets,
  independent of the rest of GTB.
- **Unblocks the VCS/release stack.** `go/releases` / `go/vcs` is the highest
  aggregate-value extraction still outstanding, and `pkg/vcs` depends on
  `credentials` (both the core and the keychain subpkg). Extracting `credentials`
  as a module is a prerequisite for a clean `go/vcs` that does not reach back into
  GTB. Per the [wave-2 handover](../reports/2026-07-07-package-extraction-report.md),
  credentials is the recommended next pick: clean, high value, unblocking.
- **Low risk, high signal.** The core is decoupled and the tests are pure (none
  import other GTB packages), so this exercises the playbook on a clean package
  with a single, well-contained seam to resolve.

## Target module

- **Module path:** `gitlab.com/phpboyscout/go/credentials` (bare name in the `go`
  subgroup, per playbook §2).
- **Package name:** stays `credentials`.
- **Subpackages that travel with it:**
  - `credentials/keychain` — the opt-in go-keyring backend (blank-import
    activation). Stays a subpackage of the same module (see
    [Open question 3](#open-questions)).
  - `credentials/credtest` — the in-memory `Backend` test helper. It is a normal
    buildable package (not `_test.go`), so it ships in the module and downstream
    tests import it (see [Open question 4](#open-questions)).
- **Docs microsite:** `credentials.go.phpboyscout.uk` (core module ⇒ full Diátaxis
  microsite, like `signing`/`chat`/`controls`; not README-only).
- **Local dir:** `~/workspace/phpboyscout/go/credentials`.

## Current structure & coupling

Source (1275 LOC incl. tests; **~469 LOC production**):

| File | LOC | Role | Destination |
|---|---|---|---|
| `backend.go` | 85 | `Backend` interface + atomic registry + `RegisterBackend` | module core |
| `mode.go` | 156 | `Mode` consts, `Store`/`Retrieve`/`Delete`/`Probe`, sentinels | module core |
| `default_backend.go` | 42 | always-compiled `stubBackend` | module core |
| `wizard.go` | 89 | `IsCI`, `ValidateEnvVarName`, `RefuseLiteralUnderCI`, `KeychainOpTimeout`, **`StorageModeOptions` (huh)** | core, but `StorageModeOptions` is **replaced** by a UI-agnostic `ModeChoices` + a `Prompter` seam — see Seams |
| _(new)_ `prompter.go` | ~90 | `Prompter` interface, stdlib `DefaultPrompter`, `ModeChoice`, `ModeChoices` | module core (new) |
| `clear_keys.go` | 44 | `KeyWriter` + `ClearKeysExcept` | module core |
| `doc.go` | 53 | package doc | module core (rewrite import paths) |
| `keychain/keychain.go` | 102 | go-keyring `Backend` + `init()` registration | `credentials/keychain` |
| `credtest/memory.go` | 156 | `MemoryBackend` + `Install` | `credentials/credtest` |
| `*_test.go` (5 files) | ~548 | unit tests — **all pure** (verified: none import other GTB pkgs) | move with their source |

**External dependencies (today):** `github.com/cockroachdb/errors` (core),
`charm.land/huh/v2` (only `StorageModeOptions`), `github.com/zalando/go-keyring`
(only `keychain/`). Zero `go-tool-base` imports in production code — confirmed.

**External dependencies (module, after the Prompter seam below):**
`github.com/cockroachdb/errors` + `golang.org/x/term` (core, the latter for the
stdlib prompter's no-echo secret entry — a light, non-framework dep that passes the
depfootprint guard); `github.com/zalando/go-keyring` (`keychain/` only). **No
`charm.land/huh`** anywhere in the module.

### Consumers to repoint

**Core `credentials` (7 non-test files):**
`pkg/chat/config_adapter.go`, `pkg/cmd/config/migrate.go`, `pkg/setup/ai/ai.go`,
`pkg/setup/bitbucket/bitbucket.go`, `pkg/setup/github/github.go`,
`pkg/vcs/auth.go`, `pkg/vcs/bitbucket/release.go`.

**`credentials/keychain` blank imports (6 files):**
`cmd/e2e/keychain.go`, `cmd/gtb/keychain.go`, `pkg/cmd/config/migrate.go`,
`pkg/vcs/auth.go`, `pkg/vcs/bitbucket/release.go`, and
**`internal/generator/templates/skeleton_keychain.go`** — a scaffolding template
whose emitted import string must be updated so newly-generated tools blank-import
`go/credentials/keychain`, not the old in-tree path.

**`credtest` (8 test files):** `pkg/cmd/config/{migrate_test,migrate_coverage_test}.go`,
`pkg/setup/ai/{ai_coverage_test,ai_keychain_test}.go`,
`pkg/setup/bitbucket/{bitbucket_test,bitbucket_more_test}.go`,
`pkg/setup/github/github_auth_test.go`,
`pkg/vcs/bitbucket/credentials_keychain_test.go` — import-path repoint only; these
stay in GTB.

## Seams and decoupling

Only **one** seam prevents a straight `git mv`: the module must not import a TUI
library. Today `StorageModeOptions(...) []huh.Option[Mode]` (called by the three
setup wizards `setup/ai`, `setup/github`, `setup/bitbucket`) drags `charm.land/huh/v2`
(Bubble Tea) into the package. Carrying that into a framework-free module
contradicts the playbook's depfootprint guard (which excludes `charmbracelet`/TUI
libraries) and forces every downstream — including non-interactive ones — to pull
Bubble Tea transitively.

**Resolution (agreed 2026-07-18): a `Prompter` seam with a stdlib default,
overridable by a tool's themed TUI.** Rather than merely relocating the huh helper
to GTB, the module owns a UI-agnostic *credential-capture* abstraction so simple
downstream tools work out of the box while rich tools (GTB) override the look.

```go
// ModeChoice pairs a storage mode with a human-facing label.
type ModeChoice struct {
    Mode  Mode
    Label string
}

// ModeChoices is the UI-agnostic replacement for StorageModeOptions:
// plain data (no huh). Keychain is included only when keychainUsable;
// literal is omitted under CI.
func ModeChoices(ci, keychainUsable bool, envLabel, keychainLabel, literalLabel string) []ModeChoice

// Prompter captures the interactive input the credential flow needs.
// The module ships a stdlib default (stdio + golang.org/x/term for
// no-echo secret entry); a tool overrides it with a themed TUI (huh)
// for visual consistency. All methods honour ctx cancellation.
type Prompter interface {
    SelectMode(ctx context.Context, title string, choices []ModeChoice) (Mode, error)
    InputEnvVarName(ctx context.Context, title, placeholder string, validate func(string) error) (string, error)
    InputSecret(ctx context.Context, title string) (string, error) // never echoes
}

// DefaultPrompter returns the stdlib stdio/term implementation.
func DefaultPrompter() Prompter
```

**Boundary:**

- **Module owns:** `Prompter`, `DefaultPrompter` (stdlib), `ModeChoice`,
  `ModeChoices`, and the pure `wizard.go` helpers (`IsCI`, `ValidateEnvVarName`,
  `RefuseLiteralUnderCI`, `KeychainOpTimeout`). `StorageModeOptions` is **removed**.
- **GTB overrides:** the three wizards keep their rich huh forms (warning notes,
  provider-specific descriptions, dynamic masking) driven off the module's
  plain-data helpers (`ModeChoices`, `ValidateEnvVarName`, `AvailableModes`) — i.e.
  GTB *is* the themed-TUI override. GTB may optionally wrap this behind a
  huh-backed `Prompter` implementation, but is not obliged to route its multi-field
  forms through the narrow interface; both satisfy the override contract.
- **Simple tools** that don't want a TUI get a working interactive flow for free via
  `DefaultPrompter`.

This keeps the module free of `charm.land/huh` while still shipping a usable
interactive capture path. `golang.org/x/term` is the only added core dependency (a
light extended-stdlib package for password masking; it is not a framework/TUI/cloud
SDK, so the depfootprint guard permits it). If a strictly zero-extra-dep core is
preferred, the fallback is a plain unmasked line read with masking delegated to an
injected `Prompter` — but masked secret entry is the better default UX and
`x/term` is the standard way to get it.

No other decoupling is required: there is no config schema, no `*FromProps`
adapter, and no GTB-owned state on top of the package — so per playbook §11.1 this
is a **repoint, not a facade**.

### Incidental cleanups to fold in

- **Rewrite the GTB import paths** baked into `doc.go`, the `Backend` doc comment,
  and the `ErrCredentialUnsupported` message (all name
  `gitlab.com/phpboyscout/go-tool-base/pkg/credentials/keychain`) to the new module
  path.
- **`probeService = "gtb-keychain-probe"`** is a GTB-branded literal. In a general
  module a neutral default (or a configurable service prefix) reads better; decide
  as part of the move (Open question 5).

## Migration procedure

Follows playbook §5, in the clean-repoint shape proven by `controls`/`authn`:

1. **Bootstrap** `phpboyscout/go/credentials` to playbook §3 (cicd components at the
   version GTB currently tracks, `.golangci.yaml` local prefix, `depfootprint_test.go`,
   `requirements-lock.txt` for docs, `enable_e2e: false`, shared toolkit README
   header). No `goreleaser` (library).
2. **Move code** — `backend.go`, `mode.go`, `default_backend.go`, `clear_keys.go`,
   the pure parts of `wizard.go`, `doc.go`, plus `keychain/` and `credtest/`.
   Rewrite in-tree import paths and the branded literals above.
3. **Tests + guards** — carry all pure `*_test.go`; keep **≥90% coverage** (the
   package is already there); add the `depfootprint_test.go` guard. Do **not** ship
   mockery mocks unless the module's own tests need them (playbook §11.4) — they do
   not today.
4. **Docs (Diátaxis) + Pages** — author `credentials.go.phpboyscout.uk`
   (getting-started, how-tos for env-ref/keychain/literal + a custom-backend guide,
   an explanation page carrying the **threat model / trust model** from the
   hardening spec, and the regulated-build opt-out story). Reference → pkg.go.dev.
   Enable the custom domain + Pages public visibility (playbook §11.9).
5. **Cut `v0.1.0`** via the Release-MR flow (human-gated).
6. **GTB cut-over (single MR):** add the dependency; **delete `pkg/credentials/`**;
   repoint the 7 core consumers, the 6 keychain blank-imports (incl. the generator
   template string), and the 8 `credtest` test imports to the new module — **no
   aliases**; migrate the 3 wizards from `StorageModeOptions` to the module's
   `ModeChoices` (they keep their existing huh forms — GTB is the themed-TUI
   override); run `just ci`; add a `docs/reference/migration/` note.
7. **GTB docs cross-reference** the microsite (playbook §4.1): stub the components
   page, sweep the whole `docs/` tree for the old path (playbook §11.6), and audit
   the deleted content for loss.
8. **Downstream adopters** (afmpeg et al.) repoint as needed.

## Test redistribution

All five `*_test.go` files under `pkg/credentials` (and `keychain_test.go`) are
**pure** — verified to import only the package under test, stdlib, and
testify/errors. They move with their source; none need to stay in GTB and none need
vendored cross-GTB helpers (contrast controls §11.2). The `credtest` package is
buildable (non-test) and ships in the module. After the cut-over, GTB's 8
`credtest`-consuming test files simply import the new path.

Because there is **no GTB adapter package** left behind (repoint, not facade),
playbook §10.9's "re-cover the adapter" does not apply. The only GTB coverage to
watch is that the relocated `StorageModeOptions` helper and its call sites stay
covered by the existing wizard tests.

## Acceptance criteria

- `go/credentials` resolves and builds under the new path; core dep graph is
  `cockroachdb/errors` only; `keychain/` adds `go-keyring`; `depfootprint_test.go`
  green and asserts the core (non-keychain) packages do **not** import go-keyring or
  any TUI/framework/cloud SDK.
- Module coverage ≥90%; docs live at `credentials.go.phpboyscout.uk` with a working
  LE cert.
- GTB builds and `just ci` is green consuming `go/credentials@v0.1.0`; `pkg/credentials/`
  is deleted; the regulated-build opt-out still holds (a gtb built without the
  keychain blank import links no go-keyring — verify via SBOM on the binary).
- Newly-scaffolded tools (`gtb generate`) blank-import `go/credentials/keychain`.
- Migration note added; component docs cross-reference the microsite.

## Resolutions (confirmed with user 2026-07-18)

- **R1 — huh seam → `Prompter`.** RESOLVED. The module defines a UI-agnostic
  `Prompter` seam with a stdlib `DefaultPrompter` (masking via `golang.org/x/term`),
  plus the plain-data `ModeChoices`; `StorageModeOptions` is removed and its huh
  dependency stays on the GTB side. Simple tools use the stdlib default; a tool
  (GTB) overrides with a thematically consistent TUI. See [Seams](#seams-and-decoupling).
- **R2 — `keychain/` stays a subpackage.** RESOLVED, and by *standing decision*, not
  merely convenience. `docs/development/feature-decisions.md` (31 March 2026) rejects
  a `SecretsProvider` plugin registry with bundled vendor implementations; its April
  update states the opt-in `keychain` subpackage (go-keyring) is the **only** adapter
  shipped in-tree, and Vault / AWS SSM / 1Password / custom stores are **tool-author
  responsibility** (they implement `Backend` themselves). There is therefore **no
  future first-party backend family to make consistent** — unlike chat, where the
  heavy-SDK providers were deliberately first-party sibling modules. Keychain remains
  a subpackage of `go/credentials`; go-keyring appears in the module graph but is
  linker-DCE'd from binaries that don't import it (identical to today's GTB
  behaviour). Scope the `depfootprint_test.go` guard to the **core** packages so the
  intentional go-keyring dependency in `keychain/` is asserted, not forbidden.
- **R3 — clean break, no facade.** RESOLVED. Repoint every caller directly to
  `go/credentials`; delete `pkg/credentials/` with no re-export facade, matching
  `authn`/`controls`.
- **R4 — `credtest` ships in the module.** RESOLVED. It travels as
  `credentials/credtest`; GTB's 8 consumers (and downstream tools) import the new
  path. (Differs from `controls`, which shipped no test helpers.)
- **R5 — de-brand the probe literal.** RESOLVED. Replace `"gtb-keychain-probe"` with
  a neutral default (e.g. `"credentials-keychain-probe"`) during the move; keep it an
  internal constant.
- **R6 — downstream repoint deferred.** RESOLVED. Do **not** repoint afmpeg or other
  downstream consumers in this wave. Downstreams upgrade at their own pace once GTB
  ships the completed extraction work; adopting the new import paths is the
  **downstream consumer's responsibility** at that point. This wave's scope ends at a
  green GTB consuming `go/credentials@v0.1.0`.

## Status

IMPLEMENTED (2026-07-18). All six open questions resolved (R1–R6 above). Delivered:
`go/credentials v0.1.0` published (repo, DNS/Pages at `credentials.go.phpboyscout.uk`,
landing card, release) with the `Prompter` seam + `ModeChoices` (R1), keychain +
`credtest` subpackages, de-branded probe (R5), and a depfootprint guard isolating
go-keyring to `keychain/` (R2). GTB cut over clean-break: `pkg/credentials` deleted,
consumers repointed, the three setup wizards migrated to `ModeChoices`, generator
template updated, docs cross-referenced ([migration note](../../reference/migration/v0.x-credentials-extracted.md)).
Downstream (afmpeg) intentionally deferred (R6).
