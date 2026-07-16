# GTB Package Extraction Report

_Assessment date: 2026-07-07 — revised 2026-07-12 (post config + slog-first
decoupling), progress-updated 2026-07-14 (Phase 1 leaves complete)._

!!! success "2026-07-14 progress — Phase 1 leaves complete"

    The three low-coupling leaf utilities from the
    [transport-stack extraction plan](../specs/2026-07-13-transport-stack-extraction-plan.md)
    are **extracted, published, and consumed back**:

    - **`redact`** → `go/redact` v0.1.0 (pure stdlib; needed a scoped
      `.gitleaks.toml` allowlist for its example-token docs/tests).
    - **`regexutil`** → `go/regexutil` v0.1.0 (needed `x/sys@v0.46.0` to clear
      GO-2026-5024 inherited via `cockroachdb/errors`).
    - **`browser`** → `go/browser` v0.1.0 (`x/sys` pre-bumped at creation).

    All three are **pure repoints** (no GTB adapter): GTB imports the new module
    path directly. GTB `main` is clean of `pkg/{redact,regexutil,browser}`;
    component pages are stubs pointing at the microsites, each with a migration
    note under `docs/reference/migration/`. This follows **`controls`**
    (`go/controls` v0.1.0) and **`chat`** (the greenfield validation) — see
    [playbook §10/§11](../specs/2026-07-12-go-module-extraction-playbook.md).

    **Microsite / DNS learning (codified):** each `<name>.go.phpboyscout.uk`
    docs CNAME must be created **Cloudflare DNS-only (un-proxied)** — Cloudflare's
    Universal SSL covers `*.phpboyscout.uk` but **not** the two-level
    `*.go.phpboyscout.uk`, so a proxied domain fails TLS and its GitLab LE cert
    never provisions (the ACME challenge can't reach GitLab). Also set
    `pages_access_level=enabled` at repo creation or Pages returns 403. All six
    live microsites (`chat`/`controls`/`redact`/`regexutil`/`signing`/`browser`)
    were un-proxied to fix this.

    **Phase 2 started:** **`tls`** → `go/tls` v0.1.0 **DONE (2026-07-16)** — the
    first **facade** cut-over: the hardened core moved, while the config-key
    `Resolve`/`SharedPrefix` adapter stays in `pkg/tls`, giving **zero
    transport-consumer churn**. Docs live at `tls.go.phpboyscout.uk` (DNS-only +
    `pages_access_level=public` + `pages_primary_domain` set so the `gitlab.io` URL
    308-redirects to the custom domain). **`authn`** → `go/authn` v0.1.0 **DONE
    (2026-07-16)** — a **pure repoint** (no facade; zero GTB imports, no logger
    seam): `pkg/{grpc,http}/auth.go` repointed, `pkg/authn/` + the unused
    `mocks/pkg/authn/` deleted; docs live at `authn.go.phpboyscout.uk`. **Next:**
    `observability` (brought forward ahead of the transport clients/servers) — the
    last Phase 2 foundation.

This report reviews `pkg/` and relevant reusable subpackages for suitability as
independently versioned Go modules. It intentionally excludes packages scored 5
or lower in extraction value, including GTB command wrappers, setup glue,
`props`, and the internal generator.

!!! note "2026-07-12 revision — what changed"

    Three framework refactors shipped in **v0.30.0** (MR !200) and are reflected
    throughout this revision:

    - **Typed config section adapters**
      ([`2026-07-07-config-section-adapters-for-extraction.md`](../specs/2026-07-07-config-section-adapters-for-extraction.md),
      IMPLEMENTED). Every package that read config now owns typed settings
      structs; GTB reads them through `*FromContainable` / `*FromProps` adapters
      confined to a single `*config_adapter.go` file per package.
    - **slog-first logging**
      ([`2026-07-07-slog-first-extraction-seams.md`](../specs/2026-07-07-slog-first-extraction-seams.md),
      IMPLEMENTED). Package seams take `*slog.Logger`; `pkg/logger` no longer
      appears in first-wave cores.
    - **Telemetry props-decoupling**
      ([`2026-07-10-telemetry-props-decoupling.md`](../specs/2026-07-10-telemetry-props-decoupling.md),
      IMPLEMENTED). Shared telemetry value types moved to the dependency-free
      `pkg/telemetrytypes` leaf, so root `pkg/telemetry`'s core no longer imports
      `pkg/props`.

    A source-level guard (`test/architecture/boundary_test.go`) now fails CI if
    any first-wave core (`chat`, `tls`, `http`, `grpc`, `gateway`, `vcs`,
    `telemetry`) re-imports `pkg/props` or `pkg/logger`. The net effect: the
    `pkg/props` / `pkg/config` / `pkg/logger` coupling vectors — the dominant
    "ease of decoupling" drag in the original assessment — are **resolved for the
    entire first wave**, so their readiness scores rise below. Residual coupling
    is now sibling-package (e.g. transport-stack) and third-party, not
    composition-layer. Per-package notes are tagged **Since v0.30.0**, and a
    consolidated [Extraction Readiness Checklist](#extraction-readiness-checklist)
    is in the footer.

The goal is not to break GTB apart. The goal is to identify components with
clear reuse value outside the framework, then describe the broad decoupling work
required to extract them without hollowing out GTB's integrated experience.

## Scoring

| Score | Meaning |
|---:|---|
| Extraction | Overall recommendation to extract as a standalone module. |
| Package value | Value to downstream projects if available without GTB. |
| Code quality | Current cohesion, testability, API shape, and implementation maturity. |
| Ease of decoupling | How little framework-specific coupling must be removed first. |

All scores are out of 10.

## Executive Summary

The strongest extraction candidates are packages that already model a clear
domain and have low framework coupling: `redact`, `regexutil`, `controls`,
`credentials`, `authn`, `changelog`, `browser`, `forms`, `output`,
`workspace`, and parts of `vcs`. As of the 2026-07-12 revision these are joined
by `tls`, `logger`, `telemetry/otelcore`, and `vcs/release`, whose framework
coupling was removed by the config + slog-first work — they are now
**ready-to-extract** with only a `git mv` plus a GTB-side adapter left behind.

`chat` — the highest-value extraction — is **DONE (2026-07-13)**. It shipped as
`go/chat` (SDK-free core + `claude-local`) plus per-provider modules
`chat-anthropic` / `chat-openai` / `chat-gemini`, all `v0.1.0`; go-tool-base
consumes them through the `pkg/chat` facade (cut-over merged). It was the **first
greenfield extraction** and validated the whole playbook — the concrete,
reusable lessons are codified in
[playbook §10](../specs/2026-07-12-go-module-extraction-playbook.md#10-findings-from-the-first-greenfield-extraction-chat-2026-07-13)
(provider-authoring-API export, registry inversion for SDK-type logic, test
inventory + per-module vendored helpers, CI gotchas, facade-vs-repoint cut-over,
etc.). The transport stack
(`http`, `grpc`, `gateway`) is similarly free of composition-layer coupling now,
but should still be extracted as one coherent stack (with `internal/circuitbreaker`
and `internal/ratelimit` promoted alongside it) rather than as isolated leaves.

**Done so far:** `signing` + `signing-aws-kms` (the validation dry-run),
**`chat`** (the first greenfield extraction, 2026-07-13), **`controls`**
(→ `go/controls` v0.1.0, 2026-07-13 — the first pure-repoint cut-over, no adapter),
and the **Phase 1 leaves** — **`redact`**, **`regexutil`**, and **`browser`**
(each → `go/<name>` v0.1.0, complete 2026-07-14; pure repoints). Remaining strategy:

1. Extract low-coupling leaf utilities: `redact`, `regexutil`, `browser`,
   `workspace`.
2. Extract user-facing CLI helper libraries: `output`, `forms`, `logger`,
   `changelog`.
3. Extract security/runtime foundations: `credentials`, `authn`, `tls`.
4. Extract `controls`, then move `http`/`grpc` onto it as optional transport
   adapters.
5. Extract `vcs/release` and provider adapters.

### Recommended next (post-`chat`, 2026-07-13)

Two sensible directions, pick by appetite:

- **Quick warm-ups — `redact`, `regexutil`, `browser`** (ease 10, no GTB
  imports). Near-mechanical `git mv` + adapter; a good way to exercise the
  playbook §10 procedure on trivial packages before a chunkier one, and each is
  independently useful.
- **`controls` — the recommended substantial next pick.** High value (ease 8),
  **ready now**: its only GTB coupling is `pkg/logger`, already slog-first, so
  the seam is a nil-safe `*slog.Logger`. It's a clean standalone lifecycle
  supervisor and a **foundation the transport stack (`http`/`grpc`/`gateway`)
  will later sit on**, so extracting it early unblocks that stack. **Caveat:** do
  the lifecycle-correctness hardening first (idempotent start/stop, nil handling,
  shutdown-timeout and signal-cleanup semantics, health-check readiness) — see
  the `pkg/controls` entry above — since those are easier to fix in-tree than
  across a module boundary. Being SDK-free and framework-light, `controls` should
  be markedly simpler than `chat` was (no per-provider modules, no vendor SDKs,
  no security-scanner surprises).

## Candidate Packages

### `pkg/chat`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 9 | 10 | 7 | 6 _(was 4)_ |

`pkg/chat` has very high standalone value: multi-provider AI chat, structured
responses, tool calling, persistence, fallback policy, streaming, media input,
and provider-specific adapters are all useful outside GTB.

!!! success "Since v0.30.0"

    `pkg/props`, `pkg/config`, and `pkg/logger` coupling is **resolved** — all
    confined to `chat/config_adapter.go`, with `*slog.Logger` at every seam and
    guard-enforced. The dedicated
    [`2026-07-05-chat-module-extraction.md`](../specs/2026-07-05-chat-module-extraction.md)
    spec (DRAFT) covers the module split. The **one** residual framework
    dependency is `pkg/http` (`chat/httpclient.go` uses the hardened GTB
    transport); replace with an injected `*http.Client` / `HTTPClientFactory` at
    extraction.

Current GTB coupling (residual):

- Imports `pkg/http` for hardened HTTP client behaviour (`httpclient.go`).
- ~~`pkg/props`~~, ~~`pkg/config`~~, ~~`pkg/credentials`~~, ~~`pkg/logger`~~ —
  now adapter-confined (`config_adapter.go`), not in the core.

Extraction shape:

- Create a standalone `chat` module with provider-neutral core types:
  `Message`, `Tool`, `ToolCall`, `Client`, `StreamingClient`, `Store`,
  `FallbackPolicy`, usage accounting, media payloads, and schemas.
- Replace `*props.Props` entry points with explicit `Options` and narrow
  interfaces such as `CredentialResolver`, `Logger`, `HTTPClientFactory`, and
  `ConfigSource`.
- Keep provider packages either in the same module as subpackages
  (`chat/openai`, `chat/anthropic`, `chat/gemini`) or as optional files behind
  normal imports, not build tags.
- Move GTB integration back into this repository as adapter code that maps
  `props`, GTB config, and GTB credential storage into the standalone chat
  interfaces.
- Ensure persistence and filestore paths do not assume GTB project layout.

Main risk: provider contracts have real behavioural differences, so extraction
should be used as an opportunity to tighten shared interface guarantees and
document provider variance explicitly.

### `pkg/controls`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 9 | 9 | 7 | 8 |

`pkg/controls` is a strong standalone lifecycle supervisor for long-running
services. It has a clear domain: startup order, shutdown coordination, health
checks, restart policy, signal handling, and service state.

Current GTB coupling:

- Imports only `pkg/logger` from GTB.
- Transport packages (`pkg/http`, `pkg/grpc`, `pkg/gateway`) depend on it, but
  controls itself does not depend on transport code.

Extraction shape:

- Define a minimal local logging interface or use no-op hooks by default.
- Keep core service orchestration transport-agnostic.
- Provide small compatibility adapters in GTB for `pkg/logger`.
- Consider moving health check abstractions with the module, while keeping HTTP
  and gRPC health endpoints in transport modules.

Pre-extraction hardening:

- Resolve lifecycle correctness issues identified in prior audits: idempotent
  start/stop semantics, nil start/stop handling, shutdown timeout behaviour,
  signal registration cleanup, and health check readiness semantics.

### `pkg/redact`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 9 | 9 | 8 | 10 |

`pkg/redact` is a small, independent secret-redaction utility with broad reuse
value across telemetry, logs, diagnostics, and bug reports.

Current GTB coupling: none.

Extraction shape:

- Move as-is into a standalone module.
- Keep GTB importing it for telemetry, HTTP logging, doctor reports, and agent
  tools.
- Expand pattern coverage before or shortly after extraction, especially GitLab
  tokens, AWS secret formats, bare JWTs, and JSON-form API keys.

This is a low-risk, high-signal extraction candidate.

### `pkg/changelog`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 8 | 8 | 8 | 9 |

`pkg/changelog` parses conventional commits and generates categorized release
notes. It is already domain-specific and has no GTB imports.

Current GTB coupling: none at package level.

Extraction shape:

- Move the library to a standalone module.
- Keep `pkg/cmd/changelog` or the GTB CLI wrapper in GTB as an integration
  layer.
- Keep git archive/repo helpers if the module's identity is "changelog from git
  history"; split pure formatting/parsing only if downstreams need a smaller
  dependency surface.

Main tradeoff: dependency weight from go-git is acceptable for a changelog
module, but may be heavy for consumers that only need formatting.

### `pkg/authn`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 8 | 8 | 8 | 9 |

`pkg/authn` provides API key, JWT/JWKS, mTLS identity, context propagation, and
authorization seams. It has no GTB imports and is valuable for HTTP and gRPC
services beyond GTB.

Current GTB coupling: none.

Extraction shape:

- Extract as a standalone auth module.
- Keep HTTP and gRPC middleware in transport modules, depending on this module.
- Keep context key and principal APIs stable and transport-neutral.
- Preserve verifier construction as explicit options rather than config-bound
  helpers.

This could be extracted before the transport stack and consumed by both GTB and
future `http`/`grpc` modules.

### `pkg/credentials`, `pkg/credentials/keychain`, `pkg/credentials/credtest`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 8 | 9 | 7 | 8 |

The credentials package defines storage modes, backend interfaces, migration
helpers, and a keychain backend. This is useful outside GTB for CLI tools that
need secure secret storage.

Current GTB coupling:

- Core `pkg/credentials` has no GTB imports.
- `keychain` imports only `pkg/credentials`.
- `credtest` imports only `pkg/credentials`.
- The interactive wizard depends on `huh`, which is fine but may deserve a
  subpackage.

Extraction shape:

- Extract the core module with `Backend`, storage mode taxonomy, env/keychain
  selection helpers, and memory test backend.
- Keep `keychain` as a subpackage or optional companion module.
- Consider moving interactive wizard flows into `credentials/wizard` so
  non-interactive consumers can avoid TUI dependencies.
- Keep GTB setup/config migration code as adapters in GTB.

Main risk: credential semantics are central to GTB setup. Preserve import-path
compatibility via a staged migration or adapter aliases during pre-1.0.

### `pkg/regexutil`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 8 | 8 | 9 | 10 |

`pkg/regexutil` provides bounded, timeout-aware regex compilation for
user-supplied patterns. It is small, security-relevant, fuzz-tested, and has no
GTB imports.

Current GTB coupling: none.

Extraction shape:

- Move as-is into a standalone module.
- Keep GTB importing it where external patterns are compiled.
- Document threat model and default limits in the extracted module README.

This is one of the easiest extractions.

### `pkg/tls`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 8 | 8 | 7 | 9 _(was 6)_ |

`pkg/tls` centralizes hardened TLS configuration, certificate pair handling, and
client cert-pool helpers. It is valuable outside GTB, especially if the HTTP and
gRPC transport modules are extracted.

!!! success "Since v0.30.0"

    **Ready to extract.** The `pkg/config` binding is fully confined to
    `tls/config_adapter.go`; the core owns a typed `Config` struct and imports no
    GTB packages. Extraction is a `git mv` of the core plus leaving
    `config_adapter.go` behind in GTB.

Current GTB coupling:

- ~~`pkg/config`~~ — now confined to `tls/config_adapter.go`; core is
  GTB-import-free.

Extraction shape:

- Split pure TLS construction from GTB config loading.
- Define a local `Config` struct for TLS options.
- Move current `pkg/config` binding into a GTB adapter or a `tlsconfig/viper`
  helper if desired.
- Ensure HTTP/gRPC modules consume the pure TLS API, not GTB config directly.

Extraction should happen before or alongside the transport stack.

### `pkg/vcs/release`, `pkg/vcs/release/releasetest`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 8 | 9 | 8 | 8 _(was 7)_ |

`pkg/vcs/release` is the most extractable part of VCS: a provider registry,
release metadata, asset APIs, and source configuration. It maps well to a
standalone release-source module.

!!! success "Since v0.30.0"

    **Ready to extract.** `release` received a narrowed registry factory signature
    rather than a full typed adapter (its providers own the typed settings), and
    its core now imports no GTB `config`/`props`/`http`. Extract `release` first,
    then move provider adapters.

Current GTB coupling:

- ~~`pkg/config`~~ — removed from the core via the narrowed registry factory.
- Provider adapters under `pkg/vcs/{github,gitlab,gitea,bitbucket,direct}` build
  on it.

Extraction shape:

- Replace `pkg/config.Containable` use with a local narrow interface or explicit
  provider option structs.
- Extract `release` first, then move provider adapters as subpackages or
  companion modules.
- Keep GTB setup/update logic in GTB, consuming the extracted release module.
- Keep `releasetest` with the release module as downstream test support.

This extraction aligns with GTB's registry-over-hard-coding architecture.

### `pkg/vcs/repo/aferobilly`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 8 | 7 | 8 | 10 |

`aferobilly` bridges `afero.Fs` and go-git's `billy.Filesystem`. It is focused,
independent, and reusable anywhere go-git and afero meet.

Current GTB coupling: none.

Extraction shape:

- Extract as a small standalone module, or place it under a broader VCS module.
- Keep `pkg/vcs/repo` consuming it.
- Document supported billy/afero behaviours, especially path, stat, and locking
  semantics.

This is technically easy, though less strategically important than `vcs/release`.

### `pkg/browser`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 7 | 7 | 8 | 10 |

`pkg/browser` is a secure URL-opening wrapper with scheme allowlisting, length
checks, and control-character rejection.

Current GTB coupling: none.

Extraction shape:

- Extract as-is.
- Keep GTB policy that all URL opening goes through this package.
- Consider exposing a configurable allowlist for consumers while keeping the
  current secure defaults.

This is a good low-effort extraction, but it is small enough that it can also
remain internal to GTB until another module needs it.

### `pkg/forms`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 7 | 7 | 7 | 10 |

`pkg/forms` provides interactive terminal form helpers built on Bubble Tea and
Huh. It has no GTB imports and is reusable by CLI projects.

Current GTB coupling: none.

Extraction shape:

- Extract as a terminal form helper module.
- Keep GTB setup/generator code importing the extracted module.
- Document non-interactive behaviour and test helpers clearly, because form
  packages often become hard to test downstream.

Main tradeoff: dependency weight is UI-specific but appropriate for the package.

### `pkg/logger`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 7 | 7 | 8 | 10 |

`pkg/logger` offers a unified logger interface with Charm, slog, buffer, and noop
implementations.

!!! success "Since v0.30.0"

    **Ready to extract.** The slog-first work
    ([`2026-07-07-slog-first-extraction-seams.md`](../specs/2026-07-07-slog-first-extraction-seams.md))
    redefined the logging boundary to mirror `*slog.Logger`, so `pkg/logger` is now
    consumed only as a thin facade / `logger.ToSlog` adapter at composition edges,
    not threaded through package cores. A dedicated extraction-boundary guard keeps
    it that way. Decide only whether the extracted module stays Charm-opinionated or
    becomes a generic facade with Charm as one adapter.

Current GTB coupling: none.

Extraction shape:

- Extract as a small logging facade module.
- Keep GTB packages depending on a narrow logger interface, not a full concrete
  logger package where possible.
- Decide whether the extracted module should remain opinionated around Charm
  output or become a generic facade with Charm as one adapter.

Main risk: logging facades are only valuable if their interface stays small and
stable. Avoid expanding it to satisfy every downstream logging preference.

### `pkg/output`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 7 | 8 | 7 | 9 |

`pkg/output` contains structured output formatting, table rendering, status,
spinner, progress, and interactive helpers. It is valuable for scriptable CLI
tools outside GTB.

Current GTB coupling: none, though it imports Cobra for some command-output
integration.

Extraction shape:

- Extract core renderers and structured output helpers.
- Consider isolating Cobra-specific helpers in `output/cobra`.
- Keep terminal-dependent progress/spinner pieces optional through package
  boundaries, not build tags.
- Fix known Unicode/table truncation and spinner cancellation issues before a
  standalone release if possible.

This is a good candidate after the low-level utilities.

### `pkg/workspace`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 7 | 7 | 8 | 10 |

`pkg/workspace` detects project roots by walking up from a starting directory and
checking marker files. It is small, focused, and useful outside GTB for CLI
tools and developer automation.

Current GTB coupling: none.

Extraction shape:

- Extract as-is, preserving the afero-based filesystem seam.
- Keep GTB generator and command code importing the extracted module.
- Document marker precedence and failure behaviour clearly.

This is technically easy, though it can also be grouped with broader CLI helper
modules if avoiding very small repositories is preferred.

### `pkg/http`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 7 | 8 | 7 | 6 _(was 5)_ |

`pkg/http` provides hardened HTTP server/client helpers, middleware chains,
retry, rate limiting, circuit breaker, auth, TLS, logging, security headers, and
OpenTelemetry instrumentation.

!!! success "Since v0.30.0"

    `pkg/config` binding is confined to `http/config_adapter.go` and `pkg/logger`
    is gone (seams take `*slog.Logger`), both guard-enforced. Residual coupling is
    now **sibling-package and internal**, not composition-layer: `internal/*`
    support libs plus the extracted-together transport siblings. Extract as one
    transport stack, not a leaf.

Current GTB coupling (residual):

- Imports `internal/circuitbreaker` and `internal/ratelimit` (promote alongside
  the transport module).
- Imports `pkg/authn`, `pkg/controls`, `pkg/redact`, and `pkg/tls` (sibling
  modules in the same wave).
- ~~`pkg/config`~~ (adapter-confined), ~~`pkg/logger`~~ (now `*slog.Logger`).

Extraction shape:

- Extract only as part of a transport module or transport stack.
- Promote `internal/circuitbreaker` and `internal/ratelimit` into the extracted
  transport module or standalone support packages.
- Replace `pkg/config` access with explicit server/client option structs.
- Depend on extracted `authn`, `tls`, `redact`, `logger`, and `controls`
  modules, or define narrow interfaces where full dependencies are unnecessary.
- Keep GTB-specific config-prefix binding in GTB adapters.

Pre-extraction hardening:

- Resolve redirect credential leakage, rate limit concurrency semantics, server
  status reporting, TLS startup error visibility, and security-header defaults.

### `pkg/grpc`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 7 | 8 | 7 | 6 _(was 5)_ |

`pkg/grpc` provides gRPC server wiring, health, reflection, TLS, auth,
interceptors, logging, rate limiting, circuit breaker, and OpenTelemetry
instrumentation.

!!! success "Since v0.30.0"

    Same as `pkg/http`: `pkg/config` confined to `grpc/config_adapter.go`,
    `pkg/logger` replaced by `*slog.Logger`, guard-enforced. Residual coupling is
    sibling-package and internal. Extract alongside `http`.

Current GTB coupling (residual):

- Imports `internal/circuitbreaker` and `internal/ratelimit`.
- Imports `pkg/authn`, `pkg/controls`, `pkg/redact`, and `pkg/tls`.
- ~~`pkg/config`~~ (adapter-confined), ~~`pkg/logger`~~ (now `*slog.Logger`).

Extraction shape:

- Extract alongside `pkg/http`, not independently.
- Share extracted support packages for auth, TLS, rate limiting, circuit
  breaking, redaction, and logging.
- Replace config container usage with explicit transport options.
- Keep GTB controller registration helpers as adapters if they depend on
  framework assumptions.

The package is useful, but it should follow `controls`, `authn`, and `tls`
extraction rather than lead.

### `pkg/gateway`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 6 | 7 | 7 | 6 _(was 5)_ |

`pkg/gateway` wires grpc-gateway into GTB's HTTP/gRPC server model.

!!! success "Since v0.30.0"

    `pkg/config` confined to `gateway/config_adapter.go` and `pkg/logger` replaced
    by `*slog.Logger`, guard-enforced. It still depends on `http`/`grpc`/`controls`
    (the transport siblings), so it remains a **stack member**, not an independent
    first-wave module — extract last within the transport stack.

Current GTB coupling (residual):

- Imports `pkg/controls`, `pkg/grpc`, and `pkg/http` (transport siblings).
- ~~`pkg/config`~~ (adapter-confined), ~~`pkg/logger`~~ (now `*slog.Logger`).

Extraction shape:

- Move only after HTTP and gRPC are extracted.
- Treat it as an adapter package in the transport stack.
- Replace GTB config binding with explicit options.

This is not a first-wave module, but it belongs with a transport extraction.

### `pkg/openapi`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 6 | 7 | 7 | 8 |

`pkg/openapi` serves an OpenAPI spec and embedded Stoplight Elements UI.

Current GTB coupling:

- Imports `pkg/http`.

Extraction shape:

- Extract after or with the HTTP transport module.
- Keep the package focused on static OpenAPI serving and UI embedding.
- Avoid taking GTB config dependencies; accept explicit options and `http.ServeMux`
  style integration.

This is easy technically, but its value is higher as an HTTP module companion.

### `pkg/telemetry/otelcore`, `pkg/telemetry/logs`, `pkg/telemetry/metrics`, `pkg/telemetry/tracing`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 7 | 8 | 7 | 8 _(was 6)_ |

These packages provide reusable OpenTelemetry configuration and exporters for
logs, metrics, and traces. They are cleaner extraction candidates than root
`pkg/telemetry`, which mixes product analytics, consent, deletion requests,
machine identity, and GTB integration.

!!! success "Since v0.30.0"

    **Ready to extract** as the `observability` module. `otelcore`'s `pkg/config`
    binding is confined to `otelcore/config_adapter.go`; `logs`/`metrics`/`tracing`
    only import `otelcore`. This is the destination for the observability half of
    the [telemetry analytics/observability split](../specs/2026-07-12-telemetry-analytics-observability-split.md).

Current GTB coupling:

- ~~`otelcore` imports `pkg/config`~~ — now confined to
  `otelcore/config_adapter.go`.
- `logs`, `metrics`, and `tracing` import `otelcore` (intra-group only).

Extraction shape:

- Extract a pure observability module around explicit endpoint/resource options.
- Move `pkg/config` binding into GTB adapters or a separate config helper.
- Keep product analytics and consent-gated event tracking out of this module.
- Let `pkg/http` and `pkg/grpc` depend on this module for instrumentation where
  useful.

This should be treated as "observability", separate from GTB's product telemetry.

### `pkg/telemetry`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 6 | 7 | 6 | 5 _(was 4)_ |

Root `pkg/telemetry` handles opt-in product analytics, buffering/spill,
redaction, deletion requests, machine identity, and backend orchestration.

!!! warning "Since v0.30.0 — partial"

    The `pkg/props` **type** coupling is severed (`EventType`/`DeliveryMode` moved
    to the `pkg/telemetrytypes` leaf), config reads are confined to
    `*_config_adapter.go`, and the core is in the boundary guard. But the package
    still mixes two products, so it is **not** yet extractable. The blocking work
    — separating consent-gated product analytics from service observability — is
    now specced in
    [`2026-07-12-telemetry-analytics-observability-split.md`](../specs/2026-07-12-telemetry-analytics-observability-split.md)
    (DRAFT). Ease stays low until that split lands.

Current GTB coupling (residual):

- Imports `pkg/browser`, `pkg/controls`, `pkg/http`, `pkg/osinfo`, `pkg/redact`,
  and OTel subpackages.
- ~~`pkg/props`~~ type coupling removed via `pkg/telemetrytypes`; config now
  adapter-confined.

Extraction shape:

- Do not extract root telemetry before splitting observability from product
  analytics (see the split spec above).
- Replace remaining `props`-adapter reach-through with explicit app metadata and
  consent/config interfaces.
- Move GTB-specific data directory, tool metadata, and config binding into GTB.
- Ensure redaction happens at event ingestion boundaries before standalone use.

This package can be extracted, but only after the analytics/observability split.

### `pkg/telemetry/posthog`, `pkg/telemetry/datadog`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 6 | 6 | 7 | 5 |

These are backend adapters for root telemetry.

!!! note "Since v0.30.0"

    Unchanged and still gated: these move only after root telemetry's
    analytics/observability split and contract extraction (see the
    [split spec](../specs/2026-07-12-telemetry-analytics-observability-split.md)).
    `pkg/logger` seams are now `*slog.Logger`, but the ordering dependency on root
    telemetry keeps readiness at 5.

Current GTB coupling:

- Import `pkg/http` and root `pkg/telemetry`.

Extraction shape:

- Move only after root telemetry contracts are extracted.
- Keep each backend as an optional adapter package.
- Avoid importing GTB HTTP if the extracted telemetry module can accept a
  standard `*http.Client`.

These are not independent first-wave candidates.

### `pkg/vcs`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 6 | 7 | 7 | 7 _(was 6)_ |

`pkg/vcs` contains shared VCS auth/config abstractions used by provider
packages. It is useful as the common layer for extracted release and repository
modules.

!!! success "Since v0.30.0"

    `pkg/config` binding is confined to `vcs/config_adapter.go`; the core's only
    residual GTB dependency is `pkg/credentials` (itself an extraction candidate
    with no GTB coupling). Extract after or alongside `credentials`.

Current GTB coupling (residual):

- Imports `pkg/credentials`.
- ~~`pkg/config`~~ — now confined to `vcs/config_adapter.go`.

Extraction shape:

- Extract after or alongside `credentials`.
- Replace GTB config containers with explicit provider auth configuration.
- Keep only provider-neutral types here; avoid setup/update concerns.

The stronger move is to extract `vcs/release` first, then pull this common layer
only as needed.

### `pkg/vcs/github`, `pkg/vcs/gitlab`, `pkg/vcs/gitea`, `pkg/vcs/bitbucket`, `pkg/vcs/direct`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 7 | 8 | 7 | 6 _(was 5)_ |

These packages are release-provider adapters and platform-specific helpers. They
have clear reuse value for tools that need release discovery and asset download
across forges.

!!! success "Since v0.30.0"

    Provider cores no longer import `pkg/config` (auth/config resolves through the
    `vcs` adapter seam). Residual coupling is `pkg/http` (hardened client) and, for
    Bitbucket, `pkg/credentials` — both injectable at extraction. Move as
    subpackages of the release module.

Current GTB coupling (residual):

- Import `pkg/http`, `pkg/vcs`, and/or `pkg/vcs/release`.
- Some providers import `pkg/credentials`, `pkg/browser`, or `pkg/regexutil`.
- ~~`pkg/config`~~ — no longer in provider cores.

Extraction shape:

- Extract `vcs/release` first.
- Move providers as subpackages of the release module or as separate adapter
  modules if dependency weight becomes a concern.
- Replace config-container constructors with explicit options.
- Use standard `*http.Client` or an extracted HTTP helper, not GTB HTTP directly.
- Keep setup wizard and update command logic in GTB.

Pre-extraction hardening:

- Fix token forwarding to arbitrary asset hosts.
- Ensure browser opening routes through the extracted browser package or an
  injected opener.

### `pkg/vcs/repo`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 6 | 7 | 7 | 7 _(was 5)_ |

`pkg/vcs/repo` wraps go-git repository operations, safe repo access, and worktree
filesystem helpers.

!!! success "Since v0.30.0"

    `pkg/props` is **removed from the core** and config is confined to
    `vcs/repo/config_adapter.go`. Residual coupling is only the sibling VCS
    modules (`vcs`, `release`, `aferobilly`). Trails the release/common VCS work
    but is otherwise clean.

Current GTB coupling (residual):

- Imports `pkg/vcs`, `pkg/vcs/release`, and `pkg/vcs/repo/aferobilly` (sibling
  modules).
- ~~`pkg/props`~~ removed from constructors/auth flows; config now
  adapter-confined.

Extraction shape:

- Depend on extracted `vcs`, `release`, and `aferobilly` modules.
- Use explicit auth and filesystem options.
- Keep GTB-specific provider resolution in GTB adapters.

This is extractable but should trail the release/common VCS work.

### `pkg/config`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 6 | 7 | 7 | 7 |

`pkg/config` wraps Viper with file/env/default merging, embedded assets,
validation, schema helpers, and hot reload.

!!! note "Since v0.30.0"

    `pkg/config` gained the typed section-decode primitives (`Unmarshal`,
    `UnmarshalKey`, `SectionExists`, and the generic `UnmarshalSection[T]` /
    `ObserveSection[T]` helpers) that every first-wave adapter now builds on. This
    hardens its identity as an *opinionated Viper wrapper* usable as a lightweight
    standalone dependency — and importantly, the extraction pattern deliberately
    does **not** require other modules to import it (they own typed structs; GTB
    adapters do the decoding). Its own `pkg/logger` dependency is now a
    `*slog.Logger` seam.

Current GTB coupling:

- Imports `pkg/logger` (as a `*slog.Logger` seam).
- Is a foundational dependency for many other packages, but no longer a *required*
  one for extracted modules.

Extraction shape:

- Define whether the module's identity is "opinionated Viper wrapper" or a
  GTB-flavoured config system. The former is extractable; the latter should
  remain in GTB.
- Replace `pkg/logger` with a local narrow logger interface or functional hooks.
- Fix hot-reload semantics before extraction: multi-file reload, validation
  rollback, observer synchronization, and missing-file contract.
- Keep GTB defaults/assets conventions in GTB adapters.

Extraction is possible, but it should not be a prerequisite for every other
module. Prefer explicit option structs in extracted modules instead of dragging
the config module along.

### `pkg/errorhandling`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 6 | 7 | 7 | 6 |

`pkg/errorhandling` provides user-facing hints, help channel configuration,
debug stack handling, and exit-code semantics.

Current GTB coupling:

- Imports `pkg/logger`.
- Imports Cobra for command-facing error handling.

Extraction shape:

- If extracted, make it a CLI error UX module rather than generic error
  handling.
- Split pure hint/exit-code helpers from Cobra integration.
- Replace `pkg/logger` with a narrow interface.
- Keep GTB root execution and telemetry-flush behaviour in GTB.

This is useful, but less urgent than leaf utilities and runtime modules.

## Cross-Cutting Decoupling Work

Several changes would make extraction smoother across multiple packages:

- Replace `*props.Props` in reusable libraries with narrow local interfaces or
  explicit option structs. Keep `props` as GTB's composition mechanism, not a
  dependency of extracted modules.
- Move config binding to adapters. Extracted modules should accept typed config
  structs and options; GTB can continue reading Viper/embedded assets and
  translating them.
- Promote internal support libraries only when their public contracts are clear.
  `internal/circuitbreaker` and `internal/ratelimit` belong with the transport
  extraction, not as accidental dependencies.
- Keep command wrappers in GTB. `pkg/cmd/*` should consume extracted libraries
  but remain part of the framework.
- Prefer adapter packages over compatibility shims. For example, GTB can expose
  `chatFromProps(p *props.Props)` internally while the extracted chat module
  exposes clean constructors.
- Treat docs as part of extraction. Each extracted module needs its own README,
  examples, API stability statement, and migration note from the old GTB import
  path.

## Proposed Module Map

| Module | Packages |
|---|---|
| `gitlab.com/phpboyscout/redact` | `pkg/redact` |
| `gitlab.com/phpboyscout/regexutil` | `pkg/regexutil` |
| `gitlab.com/phpboyscout/browser` | `pkg/browser` |
| `gitlab.com/phpboyscout/workspace` | `pkg/workspace` |
| `gitlab.com/phpboyscout/cli-output` | `pkg/output` |
| `gitlab.com/phpboyscout/forms` | `pkg/forms` |
| `gitlab.com/phpboyscout/credentials` | `pkg/credentials`, `keychain`, `credtest` |
| `gitlab.com/phpboyscout/controls` | `pkg/controls` |
| `gitlab.com/phpboyscout/chat` | `pkg/chat` after adapter split |
| `gitlab.com/phpboyscout/transport` | `pkg/http`, `pkg/grpc`, `pkg/gateway`, `pkg/openapi`, support code |
| `gitlab.com/phpboyscout/authn` | `pkg/authn` |
| `gitlab.com/phpboyscout/tlsconfig` | `pkg/tls` after config split |
| `gitlab.com/phpboyscout/releases` | `pkg/vcs/release`, provider adapters, `releasetest` |
| `gitlab.com/phpboyscout/vcsrepo` | `pkg/vcs/repo`, `aferobilly` |
| `gitlab.com/phpboyscout/observability` | `pkg/telemetry/otelcore`, `logs`, `metrics`, `tracing` |

This map is intentionally conservative. Some very small modules could instead
be grouped into a shared `cli-kit` or `security-kit`, but separate modules give
cleaner dependency boundaries and avoid forcing consumers to take unrelated
dependency weight.

## Extraction Readiness Checklist

_Snapshot: 2026-07-12. A quick-lookup tracker for future extraction work. Tick a
box when the module is extracted and consumed back via a GTB adapter. "Ease"
is the ease-of-decoupling score from each package's table above._

**Readiness tiers**

- ✅ **Ready now** — core has no GTB composition-layer coupling (or it is fully
  confined to a `*config_adapter.go`); extraction is roughly `git mv` the core +
  leave the adapter behind in GTB.
- 🔗 **Ready, pending siblings** — core is clean of `props`/`logger` and config is
  adapter-confined, but the package depends on other `pkg/` modules that should
  extract together or first (chiefly the transport stack and the VCS tree).
- 🚧 **Blocked** — needs design/refactor work before extraction is sound.

### ✅ Ready now

| Done | Package | Target module | Ease | Note |
|:--:|---|---|:--:|---|
| ✅ | `pkg/redact` | `redact` | 10 | **DONE (2026-07-13)** → `go/redact` v0.1.0. Pure-stdlib repoint (Phase 1); needed a scoped gitleaks allowlist for its example-token docs/tests. |
| ✅ | `pkg/regexutil` | `regexutil` | 10 | **DONE (2026-07-13)** → `go/regexutil` v0.1.0. Phase 1 leaf, pure repoint. |
| ✅ | `pkg/browser` | `browser` | 10 | **DONE (2026-07-13)** → `go/browser` v0.1.0. Phase 1 leaf, pure repoint. |
| ☐ | `pkg/workspace` | `workspace` | 10 | No GTB imports. Preserve afero seam. |
| ☐ | `pkg/forms` | `forms` | 10 | No GTB imports. Document non-interactive/test behaviour. |
| ☐ | `pkg/logger` | (facade) | 10 | **slog-first**: consumed as `*slog.Logger` + `ToSlog` adapter only. |
| ☐ | `pkg/output` | `cli-output` | 9 | Isolate Cobra helpers in `output/cobra`; fix Unicode/spinner issues first. |
| ☐ | `pkg/changelog` | (own module) | 9 | No GTB imports. go-git weight is acceptable. |
| ✅ | `pkg/authn` | `authn` | 9 | **DONE (2026-07-16)** → `go/authn` v0.1.0. Phase 2 #2; **pure repoint** (no facade — zero GTB imports, no logger seam); repointed `pkg/{grpc,http}/auth.go`, deleted `pkg/authn/` + unused `mocks/pkg/authn/`. |
| ☐ | `pkg/credentials` (+`keychain`,`credtest`) | `credentials` | 8 | No GTB imports in core. Consider a `wizard` subpackage. |
| ✅ | `pkg/controls` | `controls` | 8 | **DONE (2026-07-13)** → `go/controls` v0.1.0. Direct repoint (no adapter); D8/D9 hardening landed first; 3 transport-coupled integration tests relocated to `test/integration/controls/`. |
| ✅ | `pkg/tls` | `tls` | 9 | **DONE (2026-07-16)** → `go/tls` v0.1.0. First Phase 2 module; **facade** cut-over (core moved; `Resolve`/`SharedPrefix` config adapter stays in `pkg/tls`), zero transport-consumer churn. |
| ☐ | `pkg/telemetry/otelcore` (+`logs`,`metrics`,`tracing`) | `observability` | 8 | **Newly ready** — config confined; the observability half of the telemetry split. |
| ☐ | `pkg/vcs/release` (+`releasetest`) | `releases` | 8 | **Newly ready** — narrowed registry factory; extract first within VCS. |
| ☐ | `pkg/vcs/repo/aferobilly` | `vcsrepo` (or standalone) | 10 | No GTB imports. afero↔billy bridge. |

### 🔗 Ready, pending sibling / stack extraction

| Done | Package | Target module | Ease | Blocking dependency |
|:--:|---|---|:--:|---|
| ☑ | `pkg/chat` | `go/chat` (+`chat-anthropic`/`openai`/`gemini`) | 6 | **EXTRACTED v0.1.0 (2026-07-13)** — HTTP client injected; per-provider modules; GTB consumes via the `pkg/chat` facade (MR !215). First greenfield extraction — see playbook §10. |
| ☐ | `pkg/http` | `transport` | 6 | Promote `internal/circuitbreaker`+`internal/ratelimit`; needs `authn`/`tls`/`redact`/`controls`. |
| ☐ | `pkg/grpc` | `transport` | 6 | Extract alongside `http`. |
| ☐ | `pkg/gateway` | `transport` | 6 | After `http`+`grpc`+`controls`. |
| ☐ | `pkg/openapi` | `transport` | 8 | HTTP-module companion; inject `http.ServeMux`. |
| ☐ | `pkg/vcs` | `releases`/`vcsrepo` common | 7 | After/with `credentials`. |
| ☐ | `pkg/vcs/{github,gitlab,gitea,bitbucket,direct}` | `releases` | 6 | Subpackages of the release module; inject `*http.Client`. |
| ☐ | `pkg/vcs/repo` | `vcsrepo` | 7 | After `vcs`/`release`/`aferobilly`. |
| ☐ | `pkg/config` | (opinionated Viper wrapper) | 7 | Optional foundational extraction; swap `pkg/logger` for a narrow seam. Not a prerequisite for others. |
| ☐ | `pkg/errorhandling` | (CLI error UX) | 6 | Split Cobra integration from pure hint/exit-code helpers. |

### 🚧 Blocked on design/refactor work

| Done | Package | Target module | Ease | Blocker |
|:--:|---|---|:--:|---|
| ☐ | `pkg/telemetry` (root) | (analytics) | 5 | **Product-analytics ↔ observability split** — [2026-07-12 spec](../specs/2026-07-12-telemetry-analytics-observability-split.md) (DRAFT). |
| ☐ | `pkg/telemetry/{posthog,datadog}` | (backend adapters) | 5 | Follows root telemetry contract extraction. |

**Suggested order:** ✅ `chat` (done, out of sequence — highest value) → leaf
utilities (redact, regexutil, browser, workspace, forms, logger, aferobilly) →
CLI helpers (output, changelog) → security/runtime (authn, credentials, tls,
**controls** — the recommended next substantial pick, harden lifecycle first) →
observability (`telemetry/otelcore` group) → transport stack (http → grpc →
gateway/openapi; sits on `controls`) → VCS (release → providers → vcs/repo) →
telemetry split → root telemetry + backends.
