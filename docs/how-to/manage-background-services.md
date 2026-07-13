---
title: How to Manage Background Services
description: A step-by-step guide to using the Controller and Service Orchestration patterns.
date: 2026-02-17
tags: [how-to, controls, services, background, lifecycle]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# How to Manage Background Services

Background services (API listeners, file watchers, long-running workers) need
ordered start-up, health monitoring, and graceful shutdown. This guide uses the
standalone [`gitlab.com/phpboyscout/go/controls`](https://controls.go.phpboyscout.uk)
module — GTB consumes it directly — to orchestrate them, with GTB-specific glue
(`Props`, `logger.ToSlog`). For the full supervisor API, interface contracts, and
lifecycle states, see the module docs at
[controls.go.phpboyscout.uk](https://controls.go.phpboyscout.uk).

## 1. Create the controller

```go
import (
    "context"

    "gitlab.com/phpboyscout/go/controls"
)

func setupController(ctx context.Context, l logger.Logger) *controls.Controller {
    return controls.NewController(ctx, controls.WithLogger(logger.ToSlog(l)))
}
```

## 2. Register a service

Each service is registered by id with functional options: `WithStart` runs it
(blocking work returns an error), `WithStop` shuts it down with the shutdown
context, and `WithStatus` is a `func() error` health probe — return `nil` for
healthy, or an error describing the failure.

```go
func registerHTTPServer(controller *controls.Controller, props *props.Props) {
    mux := http.NewServeMux()
    mux.HandleFunc("/health", healthHandler)
    server := &http.Server{Addr: props.Config.GetString("server.addr"), Handler: mux}

    controller.Register("http-server",
        controls.WithStart(func(ctx context.Context) error {
            props.Logger.Info("Starting HTTP server", "addr", server.Addr)
            if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
                return errors.Wrap(err, "HTTP server failed")
            }
            return nil
        }),
        controls.WithStop(func(ctx context.Context) {
            props.Logger.Info("Stopping HTTP server")
            if err := server.Shutdown(ctx); err != nil {
                props.Logger.Error("HTTP server shutdown error", "error", err)
            }
        }),
        controls.WithStatus(func() error {
            // Return nil while healthy; a non-nil error marks the service
            // unhealthy in the controller's aggregated Status()/Liveness()/
            // Readiness() reports.
            return healthCheck(server)
        }),
    )
}
```

## 3. Register a background worker

A worker doing periodic work derives its own cancellable context from the
controller and reports failures on the error channel.

```go
func registerBackgroundWorker(controller *controls.Controller, props *props.Props) {
    workerCtx, workerCancel := context.WithCancel(controller.GetContext())

    controller.Register("background-worker",
        controls.WithStart(func(ctx context.Context) error {
            props.Logger.Info("Starting background worker")
            go func() {
                ticker := time.NewTicker(30 * time.Second)
                defer ticker.Stop()
                for {
                    select {
                    case <-workerCtx.Done():
                        props.Logger.Info("Background worker shutting down")
                        return
                    case <-ticker.C:
                        if err := doBackgroundWork(props); err != nil {
                            controller.Errors() <- errors.Wrap(err, "background work failed")
                        }
                    }
                }
            }()
            return nil
        }),
        controls.WithStop(func(ctx context.Context) {
            props.Logger.Info("Stopping background worker")
            workerCancel()
        }),
    )
}
```

## 4. Monitor health and errors

Listen on the controller's channels in their own goroutines. Trigger a shutdown
on a critical error; request a status sweep on a timer.

```go
func handleErrors(controller *controls.Controller, props *props.Props) {
    for {
        select {
        case <-controller.GetContext().Done():
            return
        case err := <-controller.Errors():
            if isCriticalError(err) {
                props.Logger.Error("Critical error, initiating shutdown", "error", err)
                controller.Stop()
                return
            }
            props.Logger.Warn("Non-critical error, continuing", "error", err)
        }
    }
}

func handleHealthChecks(controller *controls.Controller, props *props.Props) {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-controller.GetContext().Done():
            return
        case <-ticker.C:
            // Ask the controller for an aggregated report — Status()/Liveness()/
            // Readiness() each return a controls.HealthReport built from every
            // service's probe. (Health is read from these reports, not a channel.)
            if report := controller.Readiness(); !report.OverallHealthy {
                props.Logger.Warn("service not ready", "report", report)
            }
        }
    }
}
```

## 5. Handle signals and shut down

The controller traps `SIGINT`/`SIGTERM` itself. To react to additional signals —
or to trigger a programmatic shutdown — read the signal channel:

```go
func handleSignals(controller *controls.Controller, props *props.Props, cancel context.CancelFunc) {
    for {
        select {
        case <-controller.GetContext().Done():
            return
        case sig := <-controller.Signals():
            switch sig {
            case syscall.SIGINT, syscall.SIGTERM:
                props.Logger.Info("Graceful shutdown")
                controller.Stop()
                cancel()
                return
            case syscall.SIGUSR1:
                props.Logger.Info("status report", "report", controller.Status())
            }
        }
    }
}
```

`controller.Stop()` transitions to the `Stopping` state and runs every service's
stop function; `controller.Wait()` blocks until they have all stopped.

## Complete application

```go
func main() {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    props, err := setupProps()
    if err != nil {
        log.Fatal("failed to set up props:", err)
    }

    controller := controls.NewController(ctx, controls.WithLogger(logger.ToSlog(props.Logger)))

    registerHTTPServer(controller, props)
    registerBackgroundWorker(controller, props)

    go handleErrors(controller, props)
    go handleHealthChecks(controller, props)
    go handleSignals(controller, props, cancel)

    controller.Start() // start all services in registration order
    controller.Wait()  // block until every service has stopped

    props.Logger.Info("Application shutdown complete")
}
```

## Related

- [Controls component](../explanation/components/controls/index.md) — how GTB consumes the external module
- [controls.go.phpboyscout.uk](https://controls.go.phpboyscout.uk) — the full supervisor API, service types, health model, and testing guide
- [Register health checks](register-health-checks.md)
