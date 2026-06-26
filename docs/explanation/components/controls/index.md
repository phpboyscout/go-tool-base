---
title: Controls
description: Service lifecycle management for coordinating concurrent services with graceful shutdown.
date: 2026-02-16
tags: [components, controls, lifecycle, services, shutdown]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Controls

The Controls component provides a sophisticated service lifecycle management system for GTB applications. It enables centralized control of multiple concurrent services with shared communication channels for errors, signals, health monitoring, and control messages.

## Overview

The Controls package is built around the `Controllable` interface and the `Controller` struct, providing a unified API for managing service lifecycles. The component adds several key benefits:

**Centralized Service Management**: Coordinate multiple services (HTTP servers, background workers, schedulers) from a single controller with consistent start/stop behavior.

**Shared Communication Channels**: All registered services share common channels for errors, OS signals, health monitoring, and control messages, enabling coordinated responses to system events.

**Graceful Shutdown**: Built-in support for graceful shutdown with proper cleanup ordering and timeout handling.

**Health Monitoring**: Integrated health check system that services can use to report their status and respond to health requests.

**Concurrent Safety**: Thread-safe service registration and lifecycle management with proper synchronization primitives.

## Quick Start

Get started quickly with a simple HTTP server managed by the controls system:

```go
package main

import (
    "context"
    "http"
    "log/slog"
    "os"

    "gitlab.com/phpboyscout/go-tool-base/pkg/controls"
)

func createStartFunc(srv *http.Server) controls.StartFunc {
    return func(ctx context.Context) error {
        err := srv.ListenAndServe()
        if err != nil && err != http.ErrServerClosed {
            return err
        }
        return nil
    }
}

func createStopFunc(srv *http.Server) controls.StopFunc {
    return func(ctx context.Context) {
        if err := srv.Shutdown(ctx); err != nil {
            // Log error but don't panic during shutdown
            slog.Error("Server shutdown error", "error", err)
        }
    }
}

func main() {
    // Setup context for graceful shutdown
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // Create logger
    l := logger.NewCharm(os.Stderr)

    // Create controller
    controller := controls.NewController(ctx,
        controls.WithLogger(l),
    )

    // Create HTTP server
    mux := http.NewServeMux()
    mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte("Hello from controlled service!"))
    })

    srv := &http.Server{
        Addr:    ":8080",
        Handler: mux,
    }

    // Register the HTTP server as a controlled service
    controller.Register(
        "http-server",
        controls.WithStart(createStartFunc(srv)),
        controls.WithStop(createStopFunc(srv)),
    )

    // Start all registered services
    controller.Start()

    // Wait for services to complete (blocks until shutdown)
    controller.Wait()

    logger.Info("Application shutdown complete")
}
```

This example demonstrates:

- Creating a controller with proper context and logger
- Registering an HTTP server as a managed service
- Defining start and stop functions that handle the service lifecycle
- Using the controller to coordinate service startup and shutdown

The controller automatically handles OS signals (SIGINT, SIGTERM) and will gracefully shutdown the HTTP server when these signals are received.

## Core Interfaces

The controls package uses focused role-based interfaces. Prefer the narrower interfaces where possible; use `Controllable` only when the full method set is needed.

### Runner

Provides service lifecycle operations:


> [!NOTE]
> See [pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/controls](https://pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/controls) for the full API definition.


### HealthReporter

Provides read access to aggregated service health. Use this interface when a
component only needs to query health — it does not need the full `Controllable`:


> [!NOTE]
> See [pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/controls](https://pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/controls) for the full API definition.


### StateAccessor

Provides read access to controller state and context:


> [!NOTE]
> See [pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/controls](https://pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/controls) for the full API definition.


### Configurable

Provides controller configuration setters:


> [!NOTE]
> See [pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/controls](https://pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/controls) for the full API definition.


### ChannelProvider

Provides access to controller channels:


> [!NOTE]
> See [pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/controls](https://pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/controls) for the full API definition.


### Controllable (composed)

The full controller interface, composed of all role-based interfaces:


> [!NOTE]
> See [pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/controls](https://pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/controls) for the full API definition.


`ControllerOpt` functions accept the `Configurable` interface since options only need setter methods.

## Controller Implementation

The `Controller` struct is the primary implementation of the `Controllable` interface. Engineers should use this concrete type rather than the interface directly, except for testing and dependency injection. Its fields are internal — construct it via the options-pattern factory:

```go
func NewController(ctx context.Context, opts ...ControllerOpt) *Controller

// Available controller options
func WithoutSignals() ControllerOpt
func WithLogger(l logger.Logger) ControllerOpt
func WithShutdownTimeout(d time.Duration) ControllerOpt
func WithValidError(fn ValidErrorFunc) ControllerOpt
```

> [!NOTE]
> See [pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/controls](https://pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/controls) for the full `Controller` API.

OS-signal handling is registered only **after** all options are applied, so
`WithoutSignals` genuinely leaves `SIGINT`/`SIGTERM` with their default
disposition (no orphaned `signal.Notify`). The registration is detached with
`signal.Stop` when the signal channel is replaced via `SetSignalsChannel` and at
shutdown.

## In this section

- **[Service Types & States](service-types.md)** — Service definitions, status, and the lifecycle states a service moves through.
- **[Usage](usage.md)** — Registering and running services — basic and advanced patterns.
- **[Testing](testing.md)** — Testing controllers and services, and the Controllable interface for mocking.
- **[Built-in Server Controls](server-controls.md)** — The HTTP and gRPC server controls that register against the controller.
- **[Best Practices & Integration](best-practices.md)** — Recommended patterns and how controls integrates with the GTB lifecycle.
