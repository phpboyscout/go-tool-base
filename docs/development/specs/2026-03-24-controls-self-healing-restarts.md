---
title: "Controls Package Self-Healing and Automatic Restarts"
description: "A mechanism for detecting and recovering from service failures via automatic restarts."
date: 2026-03-24
status: IMPLEMENTED
tags:
  - specification
  - controls
  - self-healing
  - resilience
author:
  - name: Gemini CLI
    role: AI drafting assistant
---

# Controls Package Self-Healing and Automatic Restarts

Authors
:   Gemini CLI *(AI drafting assistant)*

Date
:   24 March 2026

Status
:   IMPLEMENTED

## 1. Overview

Long-running services occasionally encounter transient failures or enter unrecoverable states. While external orchestrators (Kubernetes) handle process-level restarts, internal self-healing can provide faster recovery for specific components without restarting the entire application.

This specification proposes a self-healing mechanism for the `controls` package that automatically restarts registered services if they stop unexpectedly or fail health checks repeatedly.

## 2. Problem Statement

1. If a registered service's `StartFunc` returns an error, the `Controller` currently logs it but does not attempt to restart the service.
2. If a service becomes "stuck" (failing health checks), it remains in that state until an external actor intervenes.
3. Consumers have to manually implement retry loops inside their `StartFunc`, which is repetitive and error-prone.

## 3. Goals & Non-Goals

### Goals
- Automatically restart a service if its `StartFunc` exits.
- Provide configurable retry policies (limit, backoff).
- Optionally restart a service if its `StatusFunc` reports a critical failure.
- Ensure restarts are isolated and do not impact other running services.

### Non-Goals
- Handling "process-level" restarts (this is handled by the OS/orchestrator).
- Distributed consensus for restarts (this is a local node feature).

## 4. Public API

### 4.1 `pkg/controls`

Update `ServiceOption` and add `RestartPolicy`:

```go
type RestartPolicy struct {
    MaxRestarts int
    Backoff     time.Duration
    MaxBackoff  time.Duration
}

func WithRestartPolicy(policy RestartPolicy) ServiceOption
```

Update `Controller` to track restart state:

```go
type ServiceInfo struct {
    Name         string
    RestartCount int
    LastStarted  time.Time
    LastStopped  time.Time
    Error        error
}

func (c *Controller) GetServiceInfo(name string) (ServiceInfo, bool)
```

## 5. Internal Implementation

### 5.1 Restart Loop
The `Controller`'s `startErrorAndContextHandler` or the `Services.start` method will be updated to wrap the `StartFunc` in a supervisor loop.

```go
go func(s Service) {
    for {
        err := s.Start(ctx)
        if err == nil || errors.Is(err, context.Canceled) {
            return // Clean exit
        }
        
        // Check policy
        if !c.shouldRestart(s, err) {
            c.logger.Error("Service failed and will not be restarted", "service", s.Name, "error", err)
            return
        }
        
        // Wait for backoff
        select {
        case <-time.After(c.getBackoff(s)):
            c.logger.Warn("Restarting service", "service", s.Name)
            continue
        case <-ctx.Done():
            return
        }
    }
}(service)
```

### 5.2 Health-Based Restarts
The `Controller` will periodically (configurable) check `Status()`. If a service fails its health check more than `N` times consecutively, the controller will signal the service to `Stop()` and then `Start()` it again.

## 6. Testing Strategy

Implementation must follow the **Test-Driven Development (TDD)** approach.

### 6.1 Unit Tests
- **Backoff Logic**: Test the exponential backoff calculation, ensuring it respects `MaxBackoff`.
- **Restart Policy**:
    - Verify service stops restarting after `MaxRestarts`.
    - Verify "clean exits" (nil error or context canceled) do not trigger restarts.
- **Service Info**: Verify `RestartCount`, `LastStarted`, and `LastStopped` are updated correctly.

### 6.2 Integration Tests
- **Failure Recovery**:
    - Start a service that fails immediately with an error.
    - Assert that it is restarted according to the policy.
- **Health-Triggered Restart**:
    - Start a service that starts successfully but subsequently reports unhealthy status.
    - Assert that the controller initiates a restart after the threshold is reached.
- **Concurrency**: Verify that multiple services restarting simultaneously do not cause deadlocks or race conditions.

### 6.3 Quality Gates
- **Code Coverage**: Minimum **90% coverage** for all new logic.
- **Race Detection**: Mandatory passing of `go test -race ./...`.
- **Linting**: Must be clean according to `golangci-lint run --fix`.

## 7. Documentation Maintenance

- **Library Documentation**: Update `docs/components/controls.md` to document `RestartPolicy` and `WithRestartPolicy`.
- **Concept Documentation**: Update `docs/concepts/service-orchestration.md` to include a section on "Self-Healing and Resilience".
- **API Reference**: Add GoDoc comments to all new public fields and methods.

## 8. Leveraged Workflows

Implementation MUST leverage:
- `/gtb-library-contribution`
- `/gtb-verify`
- `/gtb-lint`
- `/gtb-docs`

## 9. Implementation Phases

### Phase 1: Supervisor Loop
- Implement the basic retry loop in `pkg/controls/services.go`.
- Add `RestartPolicy` and related types.

### Phase 2: Observability
- Update `ServiceInfo` to track restart counts.
- Add logging for restart events.

### Phase 3: Health-Triggered Restarts
- Implement the "health check failure threshold" logic.
- Add options to `WithRestartPolicy` for health-triggered restarts.
