---
title: Components
description: Overview of the reusable library components a gtb application is built from — those in this repository's pkg directory, and the standalone modules GTB wires in.
date: 2026-02-16
tags: [components, overview, libraries]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Components

These are the reusable library components that power `gtb` applications — modular, testable and strictly typed.

**Read the Package column carefully: it names two different kinds of thing.**

- **`pkg/…`** — a package in *this* repository, versioned with GTB and covered by its
  [API stability policy](../../reference/api-stability.md).
- **`go/…`** — a **standalone module** with its own repository, release cadence and
  documentation microsite. GTB consumes it like any other dependency and is not the
  authority on its API; the pages here describe **how GTB wires it**, and each links out
  to the module for the API itself.

Many components began life in `pkg/` and were later extracted. The
[migration guides](../../reference/migration/index.md) record what moved and when.

## Core Components

| Component | Package | Description |
| :--- | :--- | :--- |
| **[Props](props.md)** | `pkg/props` | The dependency injection container. Holds global state like configuration, logger, and filesystem interfaces. |
| **[Config](config/index.md)** | `go/config` | GTB's wiring of the standalone layered config store — embedded defaults, project-local layer, env prefix, flag binding, and masking. |
| **[Logger](logger.md)** | `pkg/logger` | Unified logging abstraction with charmbracelet, slog, and noop backends. |
| **[Commands](../../reference/cli/index.md)** | `cmd/` | Built-in Cobra commands for configuration (`init`), updates (`version`, `update`), interactive browser (`docs`), and agentic workflows (`mcp`). |
| **[Error Handling](error-handling.md)** | `go/errorhandling` | Centralized error reporting and formatting, ensuring consistent exit codes and log output. |
| **[Output](output.md)** | `go/output` | Structured CLI output (text/JSON/YAML/CSV/TSV/Markdown), tables, spinners, progress and the JSON envelope behind one `Renderer` façade — now a standalone module; GTB wires it in via the opt-in `go/output/cobra` subpackage. |
| **[Version](version.md)** | `pkg/version` | Semantic version parsing, comparison, and development-build detection. |
| **[Errors](errors.md)** | `pkg/...` | Catalogue of sentinel errors defined across GTB packages, with descriptions and handling guidance. |
| **[Changelog](changelog.md)** | `go/changelog` | Framework-free Conventional-Commits changelog generation (via go-git) and parsing, now a standalone module; GTB wires it into the `changelog` command, the generator tool, and self-update. |

## Advanced Features

| Component | Package | Description |
| :--- | :--- | :--- |
| **[Controls](controls/index.md)** | `go/controls` | Service orchestration and lifecycle management for long-running processes (e.g., servers, watchers). |
| **[Setup](setup/index.md)** | `pkg/setup` | bootstrapping logic for tool initialization, including GitHub authentication and self-updates. |
| **[VCS](vcs/index.md)** | `pkg/vcs/...` | Git operations, GitHub/GitLab API clients, and backend-agnostic release provider. (See also the [Version Control](version-control.md) redirect page.) |
| **[Chat](chat/index.md)** | `pkg/chat` | Multi-provider AI client (OpenAI, Anthropic, Gemini) for building intelligent features. |
| **[Telemetry](telemetry/index.md)** | `pkg/telemetry` | Opt-in, consent-gated product analytics with pluggable backends (OTLP, PostHog, Datadog), bounded buffering and GDPR deletion. Distinct from web-service **[Observability](observability.md)**. |
| **[Docs](docs.md)** | `pkg/docs` | Logic for the interactive TUI documentation browser. |
| **[Utils](utils.md)** | `pkg/utils` | General-purpose utility functions for path resolution and system checks. |
| **[Workspace](workspace.md)** | `go/workspace` | Framework-free project-root detection — a marker-file walk over an injected `afero.Fs`, now a standalone module. |
| **[OS Info](osinfo.md)** | `pkg/osinfo` | Human-readable OS-version string; the single shared implementation behind the telemetry OS field and the doctor support bundle. |

## Security & Credentials

| Component | Package | Description |
| :--- | :--- | :--- |
| **[Credentials](credentials.md)** | `go/credentials` | Storage-mode taxonomy for user-supplied secrets (API keys, VCS tokens), shared by the setup wizard, config masking, doctor checks, and runtime resolvers. |
| **[Redact](redact.md)** | `go/redact` | Pattern-based credential stripping for strings shipped to telemetry, distributed logs, and third-party observability surfaces. |
| **[Browser](browser.md)** | `go/browser` | The single validated entry point for opening URLs — enforces a scheme allowlist, URL-length bound, and control-character rejection before invoking the OS handler. |
| **[Regexutil](regexutil.md)** | `go/regexutil` | DoS-safe wrappers around `regexp.Compile` for user- or config-supplied patterns, with a byte-length cap and compile timeout. |

## Release Signing

These packages were extracted into the standalone, independently-versioned [signing module](https://signing.phpboyscout.uk) (v0.1.0); go-tool-base now consumes them as dependencies. The **`sign` and `keys` command builders** were likewise extracted into `go/signing-cli`, so go-tool-base and the standalone `sigillum` CLI share one command surface. The `gtb` CLI behaviour is unchanged — only the Go import paths moved.

| Component | Module | Description |
| :--- | :--- | :--- |
| **[Signing](signing.md)** | `gitlab.com/phpboyscout/go/signing` | Backend registry letting `gtb keys mint` and downstream tools target arbitrary HSM/KMS/keyring back-ends through a single CLI-agnostic `Backend` interface. |
| **[OpenPGP Key](openpgpkey.md)** | `gitlab.com/phpboyscout/go/signing/openpgpkey` | OpenPGP packet assembly from a `crypto.Signer`, wrapping an HSM/KMS-held RSA key as an ASCII-armored OpenPGP public key. |
| **[Signing CLI](https://signing-cli.go.phpboyscout.uk)** | `gitlab.com/phpboyscout/go/signing-cli` | The shareable `sign` / `keys` Cobra command builders, props-decoupled behind a narrow `Logger` seam so both go-tool-base (which re-attaches them, unchanged) and the standalone `sigillum` CLI compose them without a module cycle. Backends are registered by the host binary. |

## Web Service

Components for running a CLI as a long-lived service. See also **[Controls](controls/index.md)** for the lifecycle management they register against.

| Component | Package | Description |
| :--- | :--- | :--- |
| **[gRPC](grpc.md)** | `pkg/grpc` | gRPC server wired to the controller, with health, reflection, interceptors and TLS — plus `DialLocal` and client credentials for in-process callers. |
| **[HTTP](http.md)** | `pkg/http` | Hardened HTTP server and client, health endpoints, middleware chains, and per-server config prefixes. |
| **[Auth](authn.md)** | `go/authn` | Opt-in credential verification (API-key, JWT/OIDC, mTLS) and a minimal authorization seam for the HTTP and gRPC transports. |
| **[TLS](tls.md)** | `pkg/tls` | Shared hardened TLS config, the typed `Pair`, shared/per-transport resolution, and client cert-pool helpers. |
| **[Gateway](gateway.md)** | `pkg/gateway` | grpc-gateway as a first-class transport: REST-to-gRPC, mounted or as its own server. |
| **[OpenAPI](openapi.md)** | `go/transport-openapi` | Serve an OpenAPI spec and an embedded Stoplight Elements docs site from one `Register` call — now a standalone companion module to `go/transport`. |
| **[Observability](observability.md)** | `pkg/telemetry/*` | OTel-native traces, metrics and logs over OTLP; one-line transport instrumentation in `pkg/http`/`pkg/grpc`; trace-correlated request logs. |

## Testing Support

| Component | Package | Description |
| :--- | :--- | :--- |
| **[Mocks](mocks.md)** | `mocks/` | Auto-generated Mockery definitions for GTB's core interfaces to simplify unit testing (config mocks ship with the `go/config` module). |

## Internal Development

- **[Internal Packages](internal/index.md)**: Documentation for the private `internal/` packages that power the CLI generator itself. (Contributors Only)
