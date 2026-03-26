---
title: Controls
description: Service lifecycle management for coordinating multiple concurrent services with graceful shutdown.
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

    "github.com/phpboyscout/go-tool-base/pkg/controls"
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

```go
type Runner interface {
    Start()
    Stop()
    IsRunning() bool
    IsStopped() bool
    IsStopping() bool
    Register(id string, opts ...ServiceOption)
}
```

### HealthReporter

Provides read access to aggregated service health. Use this interface when a
component only needs to query health — it does not need the full `Controllable`:

```go
type HealthReporter interface {
    Status() HealthReport
    Liveness() HealthReport
    Readiness() HealthReport
    GetServiceInfo(name string) (ServiceInfo, bool)
}
```

### StateAccessor

Provides read access to controller state and context:

```go
type StateAccessor interface {
    GetState() State
    SetState(state State)
    GetContext() context.Context
    GetLogger() logger.Logger
}
```

### Configurable

Provides controller configuration setters:

```go
type Configurable interface {
    SetErrorsChannel(errs chan error)
    SetMessageChannel(control chan Message)
    SetSignalsChannel(sigs chan os.Signal)
    SetHealthChannel(health chan HealthMessage)
    SetWaitGroup(wg *sync.WaitGroup)
    SetShutdownTimeout(d time.Duration)
    SetLogger(l logger.Logger)
}
```

### ChannelProvider

Provides access to controller channels:

```go
type ChannelProvider interface {
    Messages() chan Message
    Health() chan HealthMessage
    Errors() chan error
    Signals() chan os.Signal
}
```

### Controllable (composed)

The full controller interface, composed of all role-based interfaces:

```go
type Controllable interface {
    Runner
    HealthReporter
    StateAccessor
    Configurable
    ChannelProvider
}
```

`ControllerOpt` functions accept the `Configurable` interface since options only need setter methods.

## Controller Implementation

The `Controller` struct is the primary implementation of the `Controllable` interface. Engineers should use this concrete type rather than the interface directly, except for testing and dependency injection:

```go
type Controller struct {
    ctx        context.Context
    logger     logger.Logger
    messages   chan Message
    health     chan HealthMessage
    errs       chan error
    signals    chan os.Signal
    wg         *sync.WaitGroup
    state      State
    stateMutex sync.Mutex
    services   Services
}

// Factory function with options
func NewController(ctx context.Context, opts ...ControllerOpt) *Controller

// Available options
func WithoutSignals() ControllerOpt
func WithLogger(l logger.Logger) ControllerOpt
```

## Service Types and States

### Service Definition

```go
type Service struct {
    Name   string
    Start  StartFunc
    Stop   StopFunc
    Status StatusFunc
}

type ServiceStatus struct {
    Name   string `json:"name"`
    Status string `json:"status"` // "OK", "DEGRADED", "ERROR"
    Error  string `json:"error,omitempty"`
}

type HealthReport struct {
    OverallHealthy bool            `json:"overall_healthy"`
    Services       []ServiceStatus `json:"services"`
}

// Function types for service lifecycle
type StartFunc func(context.Context) error
type StopFunc func(context.Context)
type StatusFunc func() error
type ValidErrorFunc func(error) bool

// ServiceOption is a functional option for configuring a Service.
type ServiceOption func(*Service)

func WithStart(fn StartFunc) ServiceOption
func WithStop(fn StopFunc) ServiceOption
func WithStatus(fn StatusFunc) ServiceOption

func WithLiveness(fn ProbeFunc) ServiceOption
func WithReadiness(fn ProbeFunc) ServiceOption

func WithRestartPolicy(policy RestartPolicy) ServiceOption
```

### Self-Healing and Automatic Restarts

The `controls` package includes an opt-in supervisor loop that can automatically restart failing services. By default, services are not restarted. To enable self-healing for a specific service, provide a `RestartPolicy` during registration:

```go
type RestartPolicy struct {
    MaxRestarts            int           // Maximum number of consecutive restarts (0 = infinite)
    InitialBackoff         time.Duration // Backoff before the first restart (default: 1s)
    MaxBackoff             time.Duration // Maximum backoff duration (default: 30s)
    HealthFailureThreshold int           // Number of consecutive health check failures before triggering a restart
    HealthCheckInterval    time.Duration // Interval between health checks (default: 10s)
}
```

When a policy is provided, the controller will automatically restart the service if its `StartFunc` returns an error, or if its `StatusFunc` reports errors exceeding the `HealthFailureThreshold`. The backoff between restarts grows exponentially up to `MaxBackoff`.

You can retrieve the runtime statistics for any service, including its current restart count, using the `GetServiceInfo` method on the `Controller`:

```go
info, ok := controller.GetServiceInfo("my-service")
if ok {
    fmt.Printf("Restarts: %d, Last Error: %v\n", info.RestartCount, info.Error)
}
```

### Health & Status Checks

The controls package supports health and status reporting through the
`WithStatus()` service option:

```go
controller.Register("my-service",
    controls.WithStart(startFn),
    controls.WithStop(stopFn),
    controls.WithStatus(func() error {
        // Return nil to indicate healthy, non-nil to indicate unhealthy.
        return nil
    }),
)
```

**Intended pattern:** each registered service provides a `StatusFunc` that the
controller calls when a `controls.Status` message is sent on the messages
channel. The function is responsible for reporting its health to the shared
health channel:

```go
statusFunc := func() error {
    controller.Health() <- controls.HealthMessage{
        Host:    "localhost",
        Port:    8080,
        Status:  200,
        Message: "service is healthy",
    }
    return nil
}
```

Returning a non-nil error signals that the service is unhealthy. When a
`WithRestartPolicy` is configured, repeated health failures trigger an
automatic restart (see [Self-Healing and Automatic Restarts](#self-healing-and-automatic-restarts)).

**Liveness vs readiness:** For Kubernetes-style probes, prefer the dedicated
`WithLiveness` and `WithReadiness` options (see below) over `WithStatus`. The
`WithStatus` mechanism is for internal controller health aggregation.

### Liveness and Readiness Probes

```go
controller.Register("my-service",
    controls.WithStart(startFn),
    controls.WithLiveness(func() error {
        // Return nil if the service is alive (i.e. should not be restarted).
        return nil
    }),
    controls.WithReadiness(func() error {
        // Return nil if the service can accept traffic.
        return nil
    }),
)
```

The HTTP and gRPC server implementations expose these probes at:

- **`/healthz`** — liveness check (returns 200 OK / 503 Service Unavailable)
- **`/readyz`** — readiness check (returns 200 OK / 503 Service Unavailable)

### Standalone Health Checks

In addition to service-bound probes, the controls package supports standalone health checks for external dependencies (databases, caches, third-party APIs) that are not tied to a service lifecycle.

Health checks use a three-state result model:

| Status | `ServiceStatus.Status` | `OverallHealthy` | Meaning |
|--------|----------------------|-------------------|---------|
| `CheckHealthy` | `"OK"` | `true` | Check passed |
| `CheckDegraded` | `"DEGRADED"` | `true` | Needs attention but still serving |
| `CheckUnhealthy` | `"ERROR"` | `false` | Check failed |

#### Registering a Sync Check

Sync checks run inline on every health request:

```go
controller.RegisterHealthCheck(controls.HealthCheck{
    Name: "database",
    Check: func(ctx context.Context) controls.CheckResult {
        if err := db.PingContext(ctx); err != nil {
            return controls.CheckResult{Status: controls.CheckUnhealthy, Message: err.Error()}
        }
        return controls.CheckResult{Status: controls.CheckHealthy}
    },
    Timeout: 2 * time.Second,
    Type:    controls.CheckTypeReadiness,
})
```

#### Registering an Async Check

Async checks run on a background interval and cache their result:

```go
controller.RegisterHealthCheck(controls.HealthCheck{
    Name: "redis",
    Check: func(ctx context.Context) controls.CheckResult {
        if err := redisClient.Ping(ctx).Err(); err != nil {
            return controls.CheckResult{Status: controls.CheckUnhealthy, Message: err.Error()}
        }
        return controls.CheckResult{Status: controls.CheckHealthy}
    },
    Timeout:  2 * time.Second,
    Interval: 10 * time.Second, // Run every 10s, cache result between requests
    Type:     controls.CheckTypeBoth,
})
```

#### Check Types

| `CheckType` | Appears in |
|------------|------------|
| `CheckTypeReadiness` (default) | `/readyz` and `/healthz` |
| `CheckTypeLiveness` | `/livez` and `/healthz` |
| `CheckTypeBoth` | All endpoints |

#### Querying Check Results

```go
result, ok := controller.GetCheckResult("database")
if ok {
    fmt.Printf("Status: %d, Message: %s\n", result.Status, result.Message)
}
```

### Controller States

```go
type State string
type Message string

const (
    Unknown  State = "unknown"
    Running  State = "running"
    Stopping State = "stopping"
    Stopped  State = "stopped"
)

const (
    Stop   Message = "stop"
    Status Message = "status"
)
```

### Health Monitoring

```go
type HealthMessage struct {
    Host    string `json:"host"`
    Port    int    `json:"port"`
    Status  int    `json:"status"`
    Message string `json:"message"`
}
```

## Basic Usage

### Creating a Controller

```go
import (
    "context"
    "log/slog"

    "github.com/phpboyscout/go-tool-base/pkg/controls"
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

## Testing

### Using Mock Controllers

The GTB library includes auto-generated mocks for testing:

```go
import (
    "testing"

    "github.com/phpboyscout/go-tool-base/mocks/pkg/controls"
    "github.com/stretchr/testify/assert"
)

func TestServiceRegistration(t *testing.T) {
    mockController := controls.NewMockControllable(t)

    // Set up expectations
    mockController.EXPECT().Register("test-service", mock.Anything, mock.Anything).Return()
    mockController.EXPECT().Start().Return()

    // Test service registration
    service := NewTestService(mockController)
    service.Initialize()

    // Verify expectations are met automatically
}
```

### Testing Service Functions

```go
func TestHTTPServerLifecycle(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    l := logger.NewNoop()
    controller := controls.NewController(ctx, controls.WithLogger(l))

    // Create test HTTP server
    server := &http.Server{
        Addr:    ":0", // Use random port for testing
        Handler: http.NewServeMux(),
    }

    started := false
    stopped := false

    startFunc := func(ctx context.Context) error {
        started = true
        // Simulate server start without actually binding
        return nil
    }

    stopFunc := func(ctx context.Context) {
        stopped = true
    }

    statusFunc := func() error {
        controller.Health() <- controls.HealthMessage{
            Status:  200,
            Message: "test server healthy",
        }
        return nil
    }

    // Register and test
    controller.Register("test-server",
        controls.WithStart(startFunc),
        controls.WithStop(stopFunc),
        controls.WithStatus(statusFunc),
    )

    // Test start
    controller.Start()
    assert.True(t, started)

    // Test status
    go func() {
        controller.Messages() <- controls.Status
    }()

    select {
    case health := <-controller.Health():
        assert.Equal(t, 200, health.Status)
        assert.Equal(t, "test server healthy", health.Message)
    case <-time.After(1 * time.Second):
        t.Fatal("Status response timeout")
    }

    // Test stop
    controller.Stop()
    assert.True(t, stopped)
}
```

## Controllable Interface (For Testing and Mocking)

The `Controllable` interface is primarily used for testing and when working with provided mocks. In production code, use the concrete `Controller` type. For narrower dependency declarations, prefer `Runner`, `Configurable`, `StateAccessor`, or `ChannelProvider` — see [Core Interfaces](#core-interfaces) above.

## Built-in Server Controls

GTB provides pre-configured server controls for HTTP and gRPC that integrate seamlessly with the `controls` lifecycle management. These components have been moved to their own packages to support both server and client use cases.

- [HTTP Documentation](http.md) - Hardened HTTP server and client factory.
- [gRPC Documentation](grpc.md) - Standard gRPC server with built-in observability.

## Best Practices

### 1. Use Concrete Types in Production

- Use `*controls.Controller` for production service management
- Use `controls.Controllable` interface for testing and dependency injection
- Reserve the interface for mocking and testing scenarios

### 2. Service Design Patterns

```go
// Recommended: Services should respect the controller's context
func createDatabaseService(controller *controls.Controller) (controls.StartFunc, controls.StopFunc, controls.StatusFunc) {
    var db *sql.DB

    start := func(ctx context.Context) error {
        var err error
        db, err = sql.Open("postgres", connectionString)
        if err != nil {
            return errors.WrapPrefix(err, "failed to open database", 0)
        }

        // Test the connection
        ctx, cancel := context.WithTimeout(controller.GetContext(), 5*time.Second)
        defer cancel()

        if err := db.PingContext(ctx); err != nil {
            return errors.WrapPrefix(err, "database ping failed", 0)
        }

        return nil
    }

    stop := func(ctx context.Context) {
        if db != nil {
            db.Close()
        }
    }

    status := func() error {
        if db == nil {
            controller.Health() <- controls.HealthMessage{
                Status:  503,
                Message: "Database not initialized",
            }
            return fmt.Errorf("database not initialized")
        }

        ctx, cancel := context.WithTimeout(controller.GetContext(), 2*time.Second)
        defer cancel()

        err := db.PingContext(ctx)
        if err != nil {
            controller.Health() <- controls.HealthMessage{
                Status:  503,
                Message: fmt.Sprintf("Database unhealthy: %v", err),
            }
            return err
        } else {
            controller.Health() <- controls.HealthMessage{
                Status:  200,
                Message: "Database healthy",
            }
            return nil
        }
    }

    return start, stop, status
}
```

### 3. Error Handling Strategy

- Use the shared error channel for all service errors
- Implement error categorization (critical vs non-critical)
- Consider implementing retry logic for transient errors
- Always wrap errors with context using `cockroachdb/errors`

### 4. Graceful Shutdown

```go
// Implement proper timeout handling
func createGracefulService(timeout time.Duration) (controls.StartFunc, controls.StopFunc) {
    var cancel context.CancelFunc

    start := func(ctx context.Context) error {
        ctx, c := context.WithCancel(context.Background())
        cancel = c

        go func() {
            // Service loop with context cancellation support
            for {
                select {
                case <-ctx.Done():
                    return
                default:
                    // Do work
                    time.Sleep(100 * time.Millisecond)
                }
            }
        }()

        return nil
    }

    stop := func(ctx context.Context) {
        if cancel != nil {
            cancel()
        }

        // Wait for graceful shutdown with timeout
        time.Sleep(timeout)
    }

    return start, stop
}
```

### 5. Health Check Implementation

- Implement meaningful health checks that verify actual service state
- Use appropriate HTTP status codes in health messages
- Include relevant diagnostic information in health messages
- Respond to status requests promptly

### 6. Channel Management

- Never close channels managed by the controller
- Use select statements with context cancellation
- Implement proper timeout handling for channel operations

## Integration with GTB

The controls component integrates seamlessly with other GTB components:

```go
// In your main application
func main() {
    // Setup Props
    props, err := setupProps()
    if err != nil {
        log.Fatal(err)
    }

    // Create controller with shared logger
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    controller := controls.NewController(ctx,
        controls.WithLogger(props.Logger),
    )

    // Services can access shared Props
    registerApplicationServices(controller, props)

    // Start and manage lifecycle
    controller.Start()
    controller.Wait()
}
```

This controls component provides the foundation for robust service lifecycle management in GTB applications, enabling coordinated startup, shutdown, and monitoring of multiple concurrent services with shared communication channels and proper error handling.
