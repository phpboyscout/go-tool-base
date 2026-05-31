---
title: Components
description: Overview of the reusable library components available in the pkg directory.
date: 2026-02-16
tags: [components, overview, libraries]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Components

The `pkg` directory contains the reusable library components that power `gtb` applications. These packages are designed to be modular, testable, and strictly typed.

## Core Components

| Component | Package | Description |
| :--- | :--- | :--- |
| **[Props](props.md)** | `pkg/props` | The dependency injection container. Holds global state like configuration, logger, and filesystem interfaces. |
| **[Config](config.md)** | `pkg/config` | Robust configuration management wrapping generic Viper usage with type safety and interface-based testability. |
| **[Logger](logger.md)** | `pkg/logger` | Unified logging abstraction with charmbracelet, slog, and noop backends. |
| **[Commands](commands/index.md)** | `cmd/` | Built-in Cobra commands for configuration (`init`), updates (`version`, `update`), interactive browser (`docs`), and agentic workflows (`mcp`). |
| **[Error Handling](error-handling.md)** | `pkg/errorhandling` | Centralized error reporting and formatting, ensuring consistent exit codes and log output. |
| **[Output](output.md)** | `pkg/output` | Dual-format (text/JSON) structured output for scriptable CLI commands. |
| **[Version](version.md)** | `pkg/version` | Semantic version parsing, comparison, and development-build detection. |

## Advanced Features

| Component | Package | Description |
| :--- | :--- | :--- |
| **[Controls](controls.md)** | `pkg/controls` | Service orchestration and lifecycle management for long-running processes (e.g., servers, watchers). |
| **[Setup](setup/index.md)** | `pkg/setup` | bootstrapping logic for tool initialization, including GitHub authentication and self-updates. |
| **[VCS](vcs/index.md)** | `pkg/vcs/...` | Git operations, GitHub/GitLab API clients, and backend-agnostic release provider. |
| **[Chat](chat.md)** | `pkg/chat` | Multi-provider AI client (OpenAI, Anthropic, Gemini) for building intelligent features. |
| **[Docs](docs.md)** | `pkg/docs` | Logic for the interactive TUI documentation browser. |
| **[Forms](forms.md)** | `pkg/forms` | Multi-step interactive CLI form helpers with Escape-to-go-back navigation, built on charmbracelet/huh. |
| **[Utils](utils.md)** | `pkg/utils` | General-purpose utility functions for path resolution and system checks. |

## Web Service

Components for running a CLI as a long-lived service. See also **[Controls](controls.md)** for the lifecycle management they register against.

| Component | Package | Description |
| :--- | :--- | :--- |
| **[gRPC](grpc.md)** | `pkg/grpc` | gRPC server wired to the controller, with health, reflection, interceptors and TLS — plus `DialLocal` and client credentials for in-process callers. |
| **[HTTP](http.md)** | `pkg/http` | Hardened HTTP server and client, health endpoints, middleware chains, and per-server config prefixes. |
| **[TLS](tls.md)** | `pkg/tls` | Shared hardened TLS config, the typed `Pair`, shared/per-transport resolution, and client cert-pool helpers. |
| **[Gateway](gateway.md)** | `pkg/gateway` | grpc-gateway as a first-class transport: REST-to-gRPC, mounted or as its own server. |
| **[OpenAPI](openapi.md)** | `pkg/openapi` | Serve an OpenAPI spec and an embedded Stoplight Elements docs site. |

## Testing Support

| Component | Package | Description |
| :--- | :--- | :--- |
| **[Mocks](mocks.md)** | `pkg/mocks` | Auto-generated Mockery definitions for all core interfaces to simplify unit testing. |

## Internal Development

- **[Internal Packages](internal/index.md)**: Documentation for the private `internal/` packages that power the CLI generator itself. (Contributors Only)
