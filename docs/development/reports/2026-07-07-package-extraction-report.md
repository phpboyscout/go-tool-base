# GTB Package Extraction Report

_Assessment date: 2026-07-07_

This report reviews `pkg/` and relevant reusable subpackages for suitability as
independently versioned Go modules. It intentionally excludes packages scored 5
or lower in extraction value, including GTB command wrappers, setup glue,
`props`, and the internal generator.

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
`workspace`, and parts of `vcs`.

`chat` remains one of the highest-value extractions, but it needs deliberate
adapter work because it currently imports `props`, GTB HTTP helpers, GTB
credentials/config abstractions, and `logger`. The same pattern applies to
`http`, `grpc`, `tls`, `gateway`, and telemetry: these should be extracted as
coherent stacks, not as isolated leaf packages that still depend on GTB.

Recommended extraction sequence:

1. Extract low-coupling leaf utilities: `redact`, `regexutil`, `browser`,
   `workspace`.
2. Extract user-facing CLI helper libraries: `output`, `forms`, `logger`,
   `changelog`.
3. Extract security/runtime foundations: `credentials`, `authn`, `tls`.
4. Extract `controls`, then move `http`/`grpc` onto it as optional transport
   adapters.
5. Extract `vcs/release` and provider adapters.
6. Extract `chat` after replacing GTB-specific config/credentials/props seams
   with local interfaces and adapter packages.

## Candidate Packages

### `pkg/chat`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 9 | 10 | 7 | 4 |

`pkg/chat` has very high standalone value: multi-provider AI chat, structured
responses, tool calling, persistence, fallback policy, streaming, media input,
and provider-specific adapters are all useful outside GTB.

Current GTB coupling:

- Imports `pkg/props` in provider constructors and convenience flows.
- Imports `pkg/config` and `pkg/credentials` for runtime credential resolution.
- Imports `pkg/http` for HTTP client behaviour.
- Imports `pkg/logger` for fallback and filestore logging.

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
| 8 | 8 | 7 | 6 |

`pkg/tls` centralizes hardened TLS configuration, certificate pair handling, and
client cert-pool helpers. It is valuable outside GTB, especially if the HTTP and
gRPC transport modules are extracted.

Current GTB coupling:

- Imports `pkg/config`.

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
| 8 | 9 | 8 | 7 |

`pkg/vcs/release` is the most extractable part of VCS: a provider registry,
release metadata, asset APIs, and source configuration. It maps well to a
standalone release-source module.

Current GTB coupling:

- Imports `pkg/config`.
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
| 7 | 8 | 7 | 5 |

`pkg/http` provides hardened HTTP server/client helpers, middleware chains,
retry, rate limiting, circuit breaker, auth, TLS, logging, security headers, and
OpenTelemetry instrumentation.

Current GTB coupling:

- Imports `internal/circuitbreaker` and `internal/ratelimit`.
- Imports `pkg/authn`, `pkg/config`, `pkg/controls`, `pkg/logger`,
  `pkg/redact`, and `pkg/tls`.

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
| 7 | 8 | 7 | 5 |

`pkg/grpc` provides gRPC server wiring, health, reflection, TLS, auth,
interceptors, logging, rate limiting, circuit breaker, and OpenTelemetry
instrumentation.

Current GTB coupling:

- Imports `internal/circuitbreaker` and `internal/ratelimit`.
- Imports `pkg/authn`, `pkg/config`, `pkg/controls`, `pkg/logger`,
  `pkg/redact`, and `pkg/tls`.

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
| 6 | 7 | 7 | 5 |

`pkg/gateway` wires grpc-gateway into GTB's HTTP/gRPC server model.

Current GTB coupling:

- Imports `pkg/config`, `pkg/controls`, `pkg/grpc`, `pkg/http`, and
  `pkg/logger`.

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
| 7 | 8 | 7 | 6 |

These packages provide reusable OpenTelemetry configuration and exporters for
logs, metrics, and traces. They are cleaner extraction candidates than root
`pkg/telemetry`, which mixes product analytics, consent, deletion requests,
machine identity, and GTB integration.

Current GTB coupling:

- `otelcore` imports `pkg/config`.
- `logs`, `metrics`, and `tracing` import `otelcore`.

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
| 6 | 7 | 6 | 4 |

Root `pkg/telemetry` handles opt-in product analytics, buffering/spill,
redaction, deletion requests, machine identity, and backend orchestration.

Current GTB coupling:

- Imports `pkg/browser`, `pkg/controls`, `pkg/http`, `pkg/logger`, `pkg/osinfo`,
  `pkg/props`, `pkg/redact`, and OTel subpackages.

Extraction shape:

- Do not extract root telemetry before splitting observability from product
  analytics.
- Replace `props` with explicit app metadata and consent/config interfaces.
- Move GTB-specific data directory, tool metadata, and config binding into GTB.
- Ensure redaction happens at event ingestion boundaries before standalone use.

This package can be extracted, but only after significant boundary cleanup.

### `pkg/telemetry/posthog`, `pkg/telemetry/datadog`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 6 | 6 | 7 | 5 |

These are backend adapters for root telemetry.

Current GTB coupling:

- Import `pkg/http`, `pkg/logger`, and root `pkg/telemetry`.

Extraction shape:

- Move only after root telemetry contracts are extracted.
- Keep each backend as an optional adapter package.
- Avoid importing GTB HTTP if the extracted telemetry module can accept a
  standard `*http.Client`.

These are not independent first-wave candidates.

### `pkg/vcs`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 6 | 7 | 7 | 6 |

`pkg/vcs` contains shared VCS auth/config abstractions used by provider
packages. It is useful as the common layer for extracted release and repository
modules.

Current GTB coupling:

- Imports `pkg/config` and `pkg/credentials`.

Extraction shape:

- Extract after or alongside `credentials`.
- Replace GTB config containers with explicit provider auth configuration.
- Keep only provider-neutral types here; avoid setup/update concerns.

The stronger move is to extract `vcs/release` first, then pull this common layer
only as needed.

### `pkg/vcs/github`, `pkg/vcs/gitlab`, `pkg/vcs/gitea`, `pkg/vcs/bitbucket`, `pkg/vcs/direct`

| Extraction | Package value | Code quality | Ease of decoupling |
|---:|---:|---:|---:|
| 7 | 8 | 7 | 5 |

These packages are release-provider adapters and platform-specific helpers. They
have clear reuse value for tools that need release discovery and asset download
across forges.

Current GTB coupling:

- Import `pkg/config`, `pkg/http`, `pkg/vcs`, and/or `pkg/vcs/release`.
- Some providers import `pkg/credentials`, `pkg/browser`, or `pkg/regexutil`.

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
| 6 | 7 | 7 | 5 |

`pkg/vcs/repo` wraps go-git repository operations, safe repo access, and worktree
filesystem helpers.

Current GTB coupling:

- Imports `pkg/props`, `pkg/vcs`, `pkg/vcs/release`, and
  `pkg/vcs/repo/aferobilly`.

Extraction shape:

- Remove `props` from constructors and auth flows.
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

Current GTB coupling:

- Imports `pkg/logger`.
- Is a foundational dependency for many other packages.

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
