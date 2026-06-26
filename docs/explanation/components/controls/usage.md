---
title: Usage
description: Registering and running services — basic and advanced patterns.
date: 2026-02-16
tags: [components, controls, lifecycle, services, shutdown]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Usage

## Basic Usage

### Creating a Controller

```go
import (
    "context"
    "log/slog"

    "gitlab.com/phpboyscout/go-tool-base/pkg/controls"
)

func setupController(ctx context.Context, l logger.Logger) *controls.Controller {
    controller := controls.NewController(ctx,
        controls.WithLogger(l),
    )

    return controller
}
```

### Registering Services

```go
func registerHTTPServer(controller *controls.Controller, props *props.Props) {
    // Create HTTP server
    mux := http.NewServeMux()
    mux.HandleFunc("/health", healthHandler)

    server := &http.Server{
        Addr:    props.Config.GetString("server.addr"),
        Handler: mux,
    }

    // Define service functions
    startFunc := func(ctx context.Context) error {
        props.Logger.Info("Starting HTTP server", "addr", server.Addr)
        err := server.ListenAndServe()
        if err != nil && err != http.ErrServerClosed {
            return errors.WrapPrefix(err, "HTTP server failed", 0)
        }
        return nil
    }

    stopFunc := func(ctx context.Context) {
        props.Logger.Info("Stopping HTTP server")
        if err := server.Shutdown(ctx); err != nil {
            props.Logger.Error("HTTP server shutdown error", "error", err)
        }
    }

    statusFunc := func() error {
        // Report service status to health channel
        controller.Health() <- controls.HealthMessage{
            Host:    "localhost",
            Port:    8080,
            Status:  200,
            Message: "HTTP server healthy",
        }
        return nil
    }

    // Register the service using functional options
    controller.Register("http-server",
        controls.WithStart(startFunc),
        controls.WithStop(stopFunc),
        controls.WithStatus(statusFunc),
    )
}
```

### Background Worker Service

```go
func registerBackgroundWorker(controller *controls.Controller, props *props.Props) {
    workerCtx, workerCancel := context.WithCancel(controller.GetContext())

    startFunc := func(ctx context.Context) error {
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
                    // Perform background work
                    err := doBackgroundWork(props)
                    if err != nil {
                        controller.Errors() <- errors.WrapPrefix(err, "background work failed", 0)
                    }
                }
            }
        }()

        return nil
    }

    stopFunc := func(ctx context.Context) {
        props.Logger.Info("Stopping background worker")
        workerCancel()
    }

    statusFunc := func() error {
        controller.Health() <- controls.HealthMessage{
            Host:    "localhost",
            Port:    0,
            Status:  200,
            Message: "Background worker healthy",
        }
        return nil
    }

    controller.Register("background-worker",
        controls.WithStart(startFunc),
        controls.WithStop(stopFunc),
        controls.WithStatus(statusFunc),
    )
}
```

## Advanced Usage

### Complete Application Setup

```go
func main() {
    // Setup context and cancellation
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // Initialize Props
    props, err := setupProps()
    if err != nil {
        log.Fatal("Failed to setup props:", err)
    }

    // Create controller
    controller := controls.NewController(ctx,
        controls.WithLogger(props.Logger),
    )

    // Register services
    registerHTTPServer(controller, props)
    registerBackgroundWorker(controller, props)
    registerDatabaseService(controller, props)

    // Setup error handling
    go handleErrors(controller, props)

    // Setup health monitoring
    go handleHealthChecks(controller, props)

    // Setup signal handling
    go handleSignals(controller, props, cancel)

    // Start all services
    controller.Start()

    // Wait for completion
    controller.Wait()

    props.Logger.Info("Application shutdown complete")
}
```

### Error Handling

```go
func handleErrors(controller *controls.Controller, props *props.Props) {
    for {
        select {
        case <-controller.GetContext().Done():
            return
        case err := <-controller.Errors():
            props.Logger.Error("Service error received", "error", err)

            // Implement error handling strategy
            if isCriticalError(err) {
                props.Logger.Error("Critical error detected, initiating shutdown")
                controller.Stop()
                return
            }

            // Log non-critical errors but continue
            props.Logger.Warn("Non-critical error, continuing operation", "error", err)
        }
    }
}

func isCriticalError(err error) bool {
    // Define what constitutes a critical error

    criticalPatterns := []string{
        "database connection lost",
        "authentication service unavailable",
        "configuration validation failed",
    }

    errStr := err.Error()
    for _, pattern := range criticalPatterns {
        if strings.Contains(errStr, pattern) {
            return true
        }
    }

    return false
}
```

### Health Monitoring

```go
func handleHealthChecks(controller *controls.Controller, props *props.Props) {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-controller.GetContext().Done():
            return
        case <-ticker.C:
            // Request status from all services
            controller.Messages() <- controls.Status
        case health := <-controller.Health():
            props.Logger.Info("Health check received",
                "host", health.Host,
                "port", health.Port,
                "status", health.Status,
                "message", health.Message)

            // Store health information or forward to monitoring system
            if health.Status >= 400 {
                props.Logger.Warn("Service reporting unhealthy status",
                    "status", health.Status,
                    "message", health.Message)
            }
        }
    }
}
```

### Signal Handling

```go
func handleSignals(controller *controls.Controller, props *props.Props, cancel context.CancelFunc) {
    for {
        select {
        case <-controller.GetContext().Done():
            return
        case sig := <-controller.Signals():
            props.Logger.Info("Received signal", "signal", sig)

            switch sig {
            case syscall.SIGINT, syscall.SIGTERM:
                props.Logger.Info("Initiating graceful shutdown")
                controller.Stop()
                cancel()
                return
            case syscall.SIGUSR1:
                // Custom signal handling - request status
                props.Logger.Info("Status requested via signal")
                controller.Messages() <- controls.Status
            }
        }
    }
}
```
