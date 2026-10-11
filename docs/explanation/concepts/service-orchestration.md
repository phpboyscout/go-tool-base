---
title: Service Orchestration & Control
description: Managing the lifecycle, health, and graceful shutdown of background services.
date: 2026-02-17
tags: [concepts, controls, orchestration, lifecycle, concurrency]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Service Orchestration & Control

!!! info "The supervisor lives in the standalone `go/controls` module"
    The service-lifecycle supervisor was **extracted into
    [`gitlab.com/phpboyscout/go/controls`](https://controls.go.phpboyscout.uk)**. Unlike
    some extracted components it has **no GTB adapter**: it is framework-free (its only
    seam is a nil-safe `*slog.Logger`), so go-tool-base consumes it **directly**, callers
    `import "gitlab.com/phpboyscout/go/controls"` and use its functional-options API as-is.
    The concepts below are unchanged; only the import path moved. See the
    [Controls component page](../components/controls/index.md) and the
    [migration note](../../reference/migration/v0.x-controls-extracted.md).

The `go/controls` package provides a standardized way to manage long-running background services. It handles the complexities of concurrent execution, health monitoring, and graceful shutdowns, ensuring that your CLI application remains stable and responsive.

## The Controller Pattern

The `Controller` is the central orchestrator that manages a collection of `Services`. It abstracts away the manual management of goroutines, wait groups, and shutdown sequencing.

### Key Components

- **`Controllable` Interface**: Defines the contract for any component that can be managed by the controller.
- **`Service` Struct**: A simple wrapper around three primary lifecycle functions: `Start`, `Stop`, and `Status`.
- **Channels**: The "nervous system" of the controller, using typed channels for:
    - **`Messages`**: Processing control signals (e.g., `Stop`, `Status`).
    - **`Health`**: Streaming host/port status and heartbeat messages.
    - **`Errors`**: every terminal service failure, and the latest of each run of retries. The controller is its only sender: read it until it closes, and never send on it. `WithOnEvent` delivers the same failures to a callback as typed `ServiceEvent`s.
    - **`Signals`**: Handling OS-level signals like `SIGINT` and `SIGTERM`:
      **opt-in only**, via `controls.WithSignals()`. Inside a GTB command the
      root command already owns signals, so the controller observes the
      cancellation of `cmd.Context()` instead of registering a competing handler.

## Specialized Server Controls

For common server types, GTB provides specialized sub-packages that simplify integration:

- **`pkg/http`**: Standardized HTTP/TLS server with production timeouts and security defaults.
- **`pkg/grpc`**: Standardized gRPC server with reflection support.

These packages provide `Start` and `Stop` functions that return the `StartFunc` and `StopFunc` types required by the `Controller.Register` method.

Cross-cutting request concerns on these servers (logging, auth, rate limiting, circuit breaking) are configured as middleware/interceptor chains at registration time (`WithMiddleware`/`WithInterceptors`), not in the lifecycle hooks. See [Transport Middleware & Resilience](transport-middleware.md).

## Lifecycle Management

The `Controller` manages a service's state through a clean lifecycle flow:

```mermaid
stateDiagram-v2
    [*] --> NeverStarted
    NeverStarted --> Running: Start()
    Running --> Stopping: Stop() / SIGINT
    Running --> UnableToStart: a service exhausts its restart policy without ever starting cleanly
    UnableToStart --> Stopping: Stop() / SIGINT
    Stopping --> Stopped: All Services Cleaned Up
    Stopped --> [*]
```

The controller reports **ready only while `Running`**. Before `Start()`, during
shutdown, and in `UnableToStart`, `Readiness()` is not overall healthy even when
every registered check passes, so an orchestrator routes no traffic to a process
that cannot yet, or can no longer, do its job. `Unknown` is not part of the
sequence: it is what a zero-valued controller (one built without
`NewController`) reports. Registration of services and health checks is only
honoured in `NeverStarted`.

### Run outcomes and restart semantics

Each supervised run of a service's `Start` is classified into one of three explicit outcomes, and **only an error triggers a restart**:

- **Clean start**: `Start` returned `nil`. This is the common case for a server that spawns its listener in a background goroutine: returning `nil` means "started successfully", *not* "exited". The controller never restarts a clean start; it supervises such a service via its `Status`/health check (when a `RestartPolicy` with a `HealthFailureThreshold` is configured) and otherwise simply waits for shutdown.
- **Context cancelled**: the run ended because the controller context was cancelled (graceful shutdown), or `Start` returned a *valid* terminal error (see below). Never restarts.
- **Error**: `Start` returned a genuine error while the context was still live. This is the only outcome that may trigger a restart, subject to the `RestartPolicy`.

The restart counter measures **consecutive failures**, not lifetime restarts: after a service has run healthily for the `RestartResetInterval` (default 30 s, configurable via `WithRestartResetInterval`), the counter resets to zero. The controller **never sends `nil`** on the error channel.

`WithValidError(func(error) bool)` registers a predicate identifying expected terminal errors (e.g. `http.ErrServerClosed`, `context.Canceled`). A matching error is treated as a graceful end-of-run: it neither counts toward the restart total nor is forwarded as a failure.

### Graceful Shutdown

Handling shutdowns correctly is critical, especially when services hold file locks or open network connections. A stop has one of five triggers, and the controller records which one as its **stop cause**:

1.  **Context cancellation** (`CauseParent`): the parent `context.Context` ended. Inside a GTB command this is how a signal arrives: the root command owns `SIGINT` and `SIGTERM` and cancels `cmd.Context()`.
2.  **OS signals** (`CauseSignal`): only with the opt-in `controls.WithSignals()`, for a standalone `main` with no CLI framework above it. Never inside a GTB command, where it would race the root's handler.
3.  **An explicit `Stop()`** (`CauseStop`).
4.  **A give-up** (`CauseGaveUp`): with `WithStopOnGiveUp()`, a service that fails for good stops the controller. That covers exhausting its restart policy, failing to start with no policy, and, through the death watch, a server whose `Status` reports it dead after a clean start.
5.  **A completion** (`CauseCompleted`): a service registered with `WithRunToCompletion()` returned `nil`. `WithCompletion(CompleteOnAll)` waits for every such service.

`Done()` closes once shutdown has finished, and `Outcome()` then reports the cause, the service behind a give-up or completion, and any shutdown step that didn't finish. A GTB command reads it to choose what to return, so the root can set the exit code: see [How to manage background services](../../how-to/manage-background-services.md#5-turn-the-outcome-into-the-commands-result).

A panic in a service's start, a status poll or a health check is that service's failure, wrapping `controls.ErrPanicked`. It doesn't crash the process. `WithOnPanic` hands each one to a callback.

When a stop is triggered, the controller stops services in **reverse registration order**, one at a time, so the last-started service stops first (respecting startup dependencies). Each `StopFunc` runs under the shutdown deadline: a context-ignoring stop is **abandoned** when the deadline elapses rather than hanging `Wait()` forever. The controller then waits for all goroutines to finish via a `sync.WaitGroup`.

`Start` is **idempotent**: it transitions `NeverStarted → Running` under a compare-and-set, so a second `Start()` is a safe no-op that does not double-start services or double-count the wait group. Services registered without a `Start` or `Stop` function default to no-ops, so they never panic at start or shutdown.

The controller's internal goroutines (error/context handler, signal handler, message processor) all terminate when shutdown completes: none busy-spins on a cancelled context or leaks past `Wait()`.

## State & Thread Safety

To prevent race conditions during lifecycle transitions, the `Controller` uses a `sync.Mutex` to protect its internal `State`. State transitions use an internal `compareAndSetState` method that atomically checks the current state and sets the new state, preventing check-then-act races. For example, concurrent `Stop()` calls are safe: only the first caller performs the shutdown; subsequent calls are no-ops. This allows other components of the application to safely query `IsRunning()`, `IsStopping()`, or `IsStopped()`.

## Best Practices

- **Context-Aware Functions**: `StartFunc` and `StopFunc` receive a `context.Context` parameter. Use this context for cancellation and timeout handling rather than creating your own.
- **Channel Buffering**: Use appropriate buffering for error and signal channels to prevent background services from blocking during high-volume events.
- **Logging**: The controller accepts an optional `*slog.Logger`. Always provide a logger to ensure service transitions and background errors are visible to the user.
- **Context Awareness**: Background services should always respect the `context.Context` provided by the controller for internal task cancellation.

**See also:** the signal-aware execution lifecycle that wires `SIGINT`/`SIGTERM` into the root command's run context, documented in [Commands: Root § Signal Handling](../../reference/cli/root.md#signal-handling-and-exit-codes).
