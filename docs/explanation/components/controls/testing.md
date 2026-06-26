---
title: Testing
description: Testing controllers and services, and the Controllable interface for mocking.
date: 2026-02-16
tags: [components, controls, lifecycle, services, shutdown]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Testing

## Testing

### Using Mock Controllers

The GTB library includes auto-generated mocks for testing:

```go
import (
    "testing"

    "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/controls"
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
