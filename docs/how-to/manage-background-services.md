---
title: How to Manage Background Services
description: A step-by-step guide to using the Controller and Service Orchestration patterns.
date: 2026-02-17
tags: [how-to, controls, services, background, lifecycle]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# How to Manage Background Services

Background services (like API listeners, file watchers, or long-running workers) require careful lifecycle management. This guide shows how to use the `pkg/controls` package to orchestrate these services.

## 1. Define Your Service

A service must implement the `Start`, `Stop`, and `Status` functions.

```go
type MyService struct {
    stop chan struct{}
}

func (s *MyService) Start() error {
    s.stop = make(chan struct{})
    go func() {
        for {
            select {
            case <-s.stop:
                return
            default:
                // Perform background work
                time.Sleep(1 * time.Second)
            }
        }
    }()
    return nil
}

func (s *MyService) Stop() {
    close(s.stop)
}

func (s *MyService) Status() {
    fmt.Println("MyService is healthy")
}
```

## 2. Initialize the Controller

Create a new `Controller` in your command's execution block.

```go
import "github.com/phpboyscout/gtb/pkg/controls"

func runServer(ctx context.Context) {
    ctrl := controls.NewController(ctx)
    
    // Register your service
    svc := &MyService{}
    ctrl.Register("worker-01", svc.Start, svc.Stop, svc.Status)
    
    // Start all services
    ctrl.Start()
    
    // Wait for the waitgroup (blocks until all services stop)
    ctrl.Wait()
}
```

## 3. Monitor Health and Errors

You can listen to the controller's channels in a separate goroutine to log errors or update a UI.

```go
go func() {
    for {
        select {
        case err := <-ctrl.Errors():
            log.Errorf("Background Error: %v", err)
        case msg := <-ctrl.Health():
            log.Infof("Health Check: %s on %s:%d", msg.Message, msg.Host, msg.Port)
        }
    }
}()
```

## 4. Triggering Shutdown

The controller automatically handles `SIGINT` and `SIGTERM`. If you need to trigger a shutdown programmatically (e.g., after a specific task is complete), call:

```go
ctrl.Stop()
```

This will transition the controller to the `Stopping` state and call the `Stop()` function of all registered services.
