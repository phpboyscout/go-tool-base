---
title: "Post-Gemini Implementation Review"
description: "Cross-reference of v1.5.0→HEAD implementation against feature specifications. Identifies deviations, missing items, and quality regressions introduced during Gemini-assisted development."
date: 2026-03-25
status: approved
tags:
  - review
  - quality
  - remediation
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude Sonnet 4.6
    role: AI review assistant
---

# Post-Gemini Implementation Review

Authors
:   Matt Cockayne, Claude Sonnet 4.6 *(AI review assistant)*

Date
:   25 March 2026

Status
:   APPROVED

## Overview

The work implemented between v1.5.0 and HEAD was carried out by Gemini. This document records a cross-reference of the eleven commits in that range against the feature specifications that drove them. Eleven implementation commits touched 89 files with a net addition of ~9,200 lines.

**Specs covered by this review:**

| Spec | Status at Review |
|------|-----------------|
| `2026-03-24-code-quality-hardening` | IMPLEMENTED |
| `2026-03-24-controls-health-integration` | IMPLEMENTED |
| `2026-03-24-controls-liveness-readiness-probes` | IMPLEMENTED |
| `2026-03-24-controls-self-healing-restarts` | IMPLEMENTED |
| `2026-03-24-secure-http-client` | IMPLEMENTED |
| `2026-03-24-security-server-hardening` | IMPLEMENTED |
| `2026-03-24-command-middleware-system` | IMPLEMENTED |
| `2026-03-21-controllable-interface-narrowing` | IMPLEMENTED |
| `2026-03-23-unified-logger-abstraction` | IMPLEMENTED |

## Spec Process Observations

None of the specs in this cycle passed through an `APPROVED` gate before implementation began. All were committed at `DRAFT` or `IN PROGRESS` status and implemented in the same working session, bypassing the review-before-implementation intent of the process described in `docs/development/specs/index.md`.

This is noted as a process concern but does not affect the validity of the implementation work itself.

## Corrections to Initial Review

Two findings from the preliminary review were incorrect:

1. **Generator template** — `internal/generator/templates/skeleton_config.go` **does** contain `server.grpc.reflection: true` at line 8. No action required.
2. **`mapLogLevel` in `pkg/cmd/root/root.go`** — The unified-logger spec stated this function would be eliminated. It cannot be: the MCP (ophis) library requires a `*slog.LevelVar` for its `SloggerOptions`, which necessitates a bridge from `logger.Level` to `slog.Level`. The function is correctly retained. The unified-logger spec is updated separately (Commit 14) to acknowledge this constraint.

## Issue Registry

The following issues were confirmed by reading implementation files against spec requirements. Each has a corresponding remediation commit.

| # | Severity | Package | Location | Description | Commit |
|---|----------|---------|----------|-------------|--------|
| 1 | Medium | `pkg/cmd/doctor` | `checks.go:107` | `//nolint:mnd` introduced by the code quality hardening spec, which itself prohibits new nolint directives | 1 |
| 2 | Medium | `pkg/controls`, `pkg/grpc` | `controller.go:185,210`, `server.go:103` | `fmt.Sprintf` calls embedded inside structured logger method calls | 2 |
| 3 | Medium | `pkg/controls` | `controls.go` | `Status()`, `Liveness()`, `Readiness()`, `GetServiceInfo()` added to `Runner` interface — violates the `controllable-interface-narrowing` spec which defined `Runner` as containing only lifecycle methods | 3 |
| 4 | High | `pkg/controls` | `services.go` | No panic recovery in `status()`, `liveness()`, `readiness()` methods — required by `controls-health-integration` spec §7. A panicking `StatusFunc` crashes the server | 4 |
| 5 | High | `pkg/setup` | `middleware.go:32,50` | Sealed registry silently ignores post-seal registrations during test runs (`flag.Lookup("test.v")` bypass). Defeats the integrity guarantee the spec requires | 5 |
| 6 | Medium | `pkg/setup` | `middleware.go` | `AddCommandWithMiddleware` and `ApplyMiddlewareRecursively` have 0% test coverage. `Seal()` has 0% test coverage. Overall `pkg/setup` coverage ~65.8% vs 95% target | 6 |
| 7 | Low | `pkg/cmd/doctor` | `doctor_test.go:209` | `TestCheckPermissions_EmptyDir` is stubbed with an empty body — mandated by the code quality hardening spec | 7 |
| 8 | Low | `pkg/controls` | `controls_test.go:436` | `TestController_Supervisor_HealthTriggered` uses `time.Sleep(150ms)` — flake risk under load | 8 |
| 9 | Medium | `pkg/controls` | `controls_test.go` | `TestControllerErrorHandler_ExitsOnClose` and `TestControllerErrorHandler_ExitsOnCancel` absent — both mandated by the code quality hardening spec | 9 |
| 10 | Medium | `pkg/http` | `server_test.go` | `TestNewServer_BaseContext`, `TestHTTPPortConfig_Specific`, `TestHTTPPortConfig_Fallback` absent — all mandated by the code quality hardening spec | 10 |
| 11 | Medium | `pkg/http` | `tls_test.go`, `client_test.go` | `TestDefaultTLSConfig_ServerAndClientMatch`, `TestNewServer_NoPreferServerCipherSuites`, `TestNewClient_WithMaxRedirects_Zero` absent — all mandated by the secure-http-client spec | 11 |
| 12 | Medium | `pkg/http`, `pkg/grpc` | `server_test.go` | `TestHTTPServer_MaxHeaderBytes_Zero`, `TestHTTPServer_RejectsOversizedHeaders`, `TestGRPCServer_ReflectionDefaultOff` absent — mandated by the security server hardening spec | 12 |
| 13 | Low | cross-package | — | `TestHTTPAndGRPC_SeparatePorts` integration test absent — mandated by the code quality hardening spec | 13 |

## Resolved / Correctly Implemented

The following areas were reviewed and found to be correctly implemented:

- `pkg/controls`: `ServiceStatus`, `HealthReport`, `RestartPolicy`, `ServiceInfo` types — correct
- `pkg/controls`: Exponential backoff in `runWithRestartPolicy` — correct
- `pkg/controls`: Liveness/readiness fallback to `Status` when probe is nil — correct
- `pkg/grpc`: gRPC reflection gated on `server.grpc.reflection` config key — correct
- `pkg/http`: `MaxHeaderBytes` with 1MB default fallback — correct
- `pkg/http`: HTTPS-to-HTTP downgrade protection in `redirectPolicy` — correct
- `pkg/http`: TLS config shared between server and client via `defaultTLSConfig()` — correct
- `pkg/setup`: `RegisterMiddleware`, `RegisterGlobalMiddleware`, `Chain`, `Seal` — functionally correct (seal bypass is the issue, not the chain logic)
- `pkg/setup`: Built-in middleware `WithTiming`, `WithRecovery`, `WithAuthCheck` — correct
- `internal/generator/templates/skeleton_config.go` — contains `server.grpc.reflection: true`
- VCS packages (`pkg/vcs/github`, `pkg/vcs/gitlab`) migrated to `gtbhttp.NewClient()` — correct
- AI chat packages (`pkg/chat/claude`, `openai`, `gemini`) injecting secure HTTP client — correct
- All production code uses `github.com/cockroachdb/errors` (not `go-errors/errors` or `fmt.Errorf` wrapping) — correct
- `pkg/cmd/root/root.go`: `mapLogLevel` retained for MCP `*slog.LevelVar` bridge — correct (see Corrections above)

## Remediation

Work to address all issues in the registry above proceeds as fourteen discrete commits, each addressing a single coherent concern. See the plan at `/home/matt/.claude/plans/eager-zooming-globe.md` for the full ordered list.
