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

    "github.com/phpboyscout/gtb/pkg/controls"
)

func createStartFunc(srv *http.Server) func() error {
    return func() error {
        err := srv.ListenAndServe()
        if err != nil && err != http.ErrServerClosed {
            return err
        }
        return nil
    }
}

func createStopFunc(ctx context.Context, srv *http.Server) func() {
    return func() {
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
    logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

    // Create controller
    controller := controls.NewController(ctx,
        controls.WithLogger(logger),
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
        "http-server",                    // Service ID
        createStartFunc(srv),             // Start function
        createStopFunc(ctx, srv),         // Stop function
        func() {},                        // Status function (empty for this example)
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


## Core Interface

The `Controllable` interface provides the primary API for service control:

```go
type Controllable interface {
    // Channel access
    Messages() chan Message
    Health() chan HealthMessage
    Errors() chan error
    Signals() chan os.Signal

    // Channel configuration
    SetErrorsChannel(errs chan error)
    SetMessageChannel(control chan Message)
    SetSignalsChannel(sigs chan os.Signal)
    SetHealthChannel(health chan HealthMessage)

    // Lifecycle management
    Start()
    Stop()
    SetWaitGroup(wg *sync.WaitGroup)

    // Context and state management
    GetContext() context.Context
    SetState(state State)
    GetState() State

    // State queries
    IsRunning() bool
    IsStopped() bool
    IsStopping() bool

    // Logging
    SetLogger(logger *slog.Logger)
    GetLogger() *slog.Logger

    // Service registration
    Register(id string, start StartFunc, stop StopFunc, status StatusFunc)
}
```

## Controller Implementation

The `Controller` struct is the primary implementation of the `Controllable` interface. Engineers should use this concrete type rather than the interface directly, except for testing and dependency injection:

```go
type Controller struct {
    ctx        context.Context
    logger     *slog.Logger
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
func WithLogger(logger *slog.Logger) ControllerOpt
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

// Function types for service lifecycle
type StartFunc func() error
type StopFunc func()
type StatusFunc func()
type ValidErrorFunc func(error) bool
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

    "github.com/phpboyscout/gtb/pkg/controls"
)

func setupController(ctx context.Context, logger *slog.Logger) *controls.Controller {
    controller := controls.NewController(ctx,
        controls.WithLogger(logger),
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
    startFunc := func() error {
        props.Logger.Info("Starting HTTP server", "addr", server.Addr)
        err := server.ListenAndServe()
        if err != nil && err != http.ErrServerClosed {
            return errors.WrapPrefix(err, "HTTP server failed", 0)
        }
        return nil
    }

    stopFunc := func() {
        props.Logger.Info("Stopping HTTP server")
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()

        if err := server.Shutdown(ctx); err != nil {
            props.Logger.Error("HTTP server shutdown error", "error", err)
        }
    }

    statusFunc := func() {
        // Report service status to health channel
        controller.Health() <- controls.HealthMessage{
            Host:    "localhost",
            Port:    8080,
            Status:  200,
            Message: "HTTP server healthy",
        }
    }

    // Register the service
    controller.Register("http-server", startFunc, stopFunc, statusFunc)
}
```

### Background Worker Service

```go
func registerBackgroundWorker(controller *controls.Controller, props *props.Props) {
    workerCtx, workerCancel := context.WithCancel(controller.GetContext())

    startFunc := func() error {
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

    stopFunc := func() {
        props.Logger.Info("Stopping background worker")
        workerCancel()
    }

    statusFunc := func() {
        controller.Health() <- controls.HealthMessage{
            Host:    "localhost",
            Port:    0,
            Status:  200,
            Message: "Background worker healthy",
        }
    }

    controller.Register("background-worker", startFunc, stopFunc, statusFunc)
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

    "github.com/phpboyscout/gtb/mocks/pkg/controls"
    "github.com/stretchr/testify/assert"
)

func TestServiceRegistration(t *testing.T) {
    mockController := controls.NewMockControllable(t)

    // Set up expectations
    mockController.EXPECT().Register("test-service", mock.Anything, mock.Anything, mock.Anything).Return()
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

    logger := slog.New(slog.NewTextHandler(io.Discard, nil))
    controller := controls.NewController(ctx, controls.WithLogger(logger))

    // Create test HTTP server
    server := &http.Server{
        Addr:    ":0", // Use random port for testing
        Handler: http.NewServeMux(),
    }

    started := false
    stopped := false

    startFunc := func() error {
        started = true
        // Simulate server start without actually binding
        return nil
    }

    stopFunc := func() {
        stopped = true
    }

    statusFunc := func() {
        controller.Health() <- controls.HealthMessage{
            Status:  200,
            Message: "test server healthy",
        }
    }

    // Register and test
    controller.Register("test-server", startFunc, stopFunc, statusFunc)

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

The `Controllable` interface is primarily used for testing and when working with provided mocks. In production code, use the concrete `Controller` type:

```go
type Controllable interface {
    Messages() chan Message
    Health() chan HealthMessage
    Errors() chan error
    Signals() chan os.Signal
    SetErrorsChannel(errs chan error)
    SetMessageChannel(control chan Message)
    SetSignalsChannel(sigs chan os.Signal)
    SetHealthChannel(health chan HealthMessage)
    SetWaitGroup(wg *sync.WaitGroup)
    Start()
    Stop()
    GetContext() context.Context
    SetState(state State)
    GetState() State
    SetLogger(logger *slog.Logger)
    GetLogger() *slog.Logger
    IsRunning() bool
    IsStopped() bool
    IsStopping() bool
    Register(id string, start StartFunc, stop StopFunc, status StatusFunc)
}
```

## Built-in Server Controls

GTB provides pre-configured server controls for HTTP and gRPC that integrate seamlessly with the `controls` lifecycle management.

### HTTP Server Control

The `pkg/controls/http` package provides a standard HTTP/TLS server that follows best practices for timeouts and security.

#### Functions

- **`NewServer(ctx context.Context, cfg config.Containable, handler http.Handler) (*http.Server, error)`**: Returns a pre-configured `*http.Server` with production-ready timeouts (Read: 5s, Write: 10s, Idle: 120s) and secure TLS settings.
- **`Start(cfg config.Containable, logger *log.Logger, srv *http.Server) controls.StartFunc`**: Returns a start function that handles both HTTP and HTTPS based on configuration.
- **`Stop(logger *log.Logger, srv *http.Server) controls.StopFunc`**: Returns a stop function that performs a graceful shutdown.

#### Configuration

Expected configuration keys:

| Key | Type | Description |
|-----|------|-------------|
| `server.port` | `int` | The port to listen on. |
| `server.tls.enabled` | `bool` | Whether to enable TLS. |
| `server.tls.cert` | `string` | Path to the TLS certificate file. |
| `server.tls.key` | `string` | Path to the TLS key file. |

#### Usage Example

```go
import (
    "github.com/phpboyscout/gtb/pkg/controls"
    "github.com/phpboyscout/gtb/pkg/controls/http"
)

// In your application setup
srv, _ := http.NewServer(ctx, props.Config, myHandler)

controller.Register(
    "http-api",
    http.Start(props.Config, props.Logger, srv),
    http.Stop(props.Logger, srv),
    http.Status,
)
```

### gRPC Server Control

The `pkg/controls/grpc` package provides a standard gRPC server with reflection enabled by default.

#### Functions

- **`NewServer(cfg config.Containable, opt ...grpc.ServerOption) (*grpc.Server, error)`**: Returns a new `*grpc.Server` with reflection registered.
- **`Start(cfg config.Containable, logger *log.Logger, srv *grpc.Server) controls.StartFunc`**: Returns a start function that listens on the configured port.
- **`Stop(logger *log.Logger, srv *grpc.Server) controls.StopFunc`**: Returns a stop function that performs a `GracefulStop`.

#### Configuration

Expected configuration keys:

| Key | Type | Description |
|-----|------|-------------|
| `server.port` | `int` | The port to listen on. |

#### Usage Example

```go
import (
    "github.com/phpboyscout/gtb/pkg/controls"
    "github.com/phpboyscout/gtb/pkg/controls/grpc"
)

// In your application setup
srv, _ := grpc.NewServer(props.Config)
// Register your gRPC services here: pb.Register*Server(srv, myService)

controller.Register(
    "grpc-api",
    grpc.Start(props.Config, props.Logger, srv),
    grpc.Stop(props.Logger, srv),
    grpc.Status,
)
```

## Best Practices

### 1. Use Concrete Types in Production

- Use `*controls.Controller` for production service management
- Use `controls.Controllable` interface for testing and dependency injection
- Reserve the interface for mocking and testing scenarios

### 2. Service Design Patterns

```go
// Recommended: Services should respect the controller's context
func createDatabaseService(controller *controls.Controller) (StartFunc, StopFunc, StatusFunc) {
    var db *sql.DB

    start := func() error {
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

    stop := func() {
        if db != nil {
            db.Close()
        }
    }

    status := func() {
        if db == nil {
            controller.Health() <- controls.HealthMessage{
                Status:  503,
                Message: "Database not initialized",
            }
            return
        }

        ctx, cancel := context.WithTimeout(controller.GetContext(), 2*time.Second)
        defer cancel()

        err := db.PingContext(ctx)
        if err != nil {
            controller.Health() <- controls.HealthMessage{
                Status:  503,
                Message: fmt.Sprintf("Database unhealthy: %v", err),
            }
        } else {
            controller.Health() <- controls.HealthMessage{
                Status:  200,
                Message: "Database healthy",
            }
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
func createGracefulService(timeout time.Duration) (StartFunc, StopFunc) {
    var cancel context.CancelFunc

    start := func() error {
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

    stop := func() {
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
        controls.WithLogger(slog.New(props.Logger)),
    )

    // Services can access shared Props
    registerApplicationServices(controller, props)

    // Start and manage lifecycle
    controller.Start()
    controller.Wait()
}
```

This controls component provides the foundation for robust service lifecycle management in GTB applications, enabling coordinated startup, shutdown, and monitoring of multiple concurrent services with shared communication channels and proper error handling.
