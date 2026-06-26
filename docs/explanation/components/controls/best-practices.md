---
title: Best Practices & Integration
description: Recommended patterns and how controls integrates with the GTB lifecycle.
date: 2026-02-16
tags: [components, controls, lifecycle, services, shutdown]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Best Practices & Integration

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
