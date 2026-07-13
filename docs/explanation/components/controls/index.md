---
title: Controls
description: go-tool-base consumes the standalone go/controls service-lifecycle supervisor directly.
date: 2026-07-13
tags: [components, controls, lifecycle, service-orchestration]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Service Controls

The service-lifecycle supervisor has been **extracted into the standalone
[`gitlab.com/phpboyscout/go/controls`](https://gitlab.com/phpboyscout/go/controls)
module**. Its full documentation — the `Controller` API, service registration and
startup ordering, health probes (liveness/readiness), graceful shutdown, signal
handling, and the self-healing restart policy — now lives at:

> **[controls.go.phpboyscout.uk](https://controls.go.phpboyscout.uk)**

Unlike some extracted components, `controls` has **no GTB adapter**: it is
framework-free (its only seam is a nil-safe `*slog.Logger`), so go-tool-base
consumes it **directly** — callers import `gitlab.com/phpboyscout/go/controls`
and use its functional-options API as-is. See the
[migration note](../../../reference/migration/v0.x-controls-extracted.md) for the
import-path change.

## How go-tool-base uses it

GTB's transport servers are themselves controllable services registered with a
`controls.Controller`:

- **`pkg/http`** and **`pkg/grpc`** servers register `Start`/`Stop` lifecycle
  functions and expose health probes; the controller orchestrates their startup
  order and drives graceful shutdown on `SIGINT`/`SIGTERM`.
- **`pkg/gateway`** composes the HTTP and gRPC servers under the same controller.
- The servers surface the controller's `Status()` / `Liveness()` / `Readiness()`
  `HealthReport`s through their own health endpoints — that endpoint wiring is a
  GTB transport concern and is documented with those packages, not here.

For the supervisor's own concepts and API, follow the microsite above.
