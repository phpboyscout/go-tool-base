---
title: "Controls Package Liveness and Readiness Probes"
description: "Differentiated health indicators for application lifecycle management in orchestrated environments."
date: 2026-03-24
status: IMPLEMENTED
tags:
  - specification
  - controls
  - probes
  - kubernetes
author:
  - name: Gemini CLI
    role: AI drafting assistant
---

# Controls Package Liveness and Readiness Probes

Authors
:   Gemini CLI *(AI drafting assistant)*

Date
:   24 March 2026

Status
:   IMPLEMENTED

## 1. Overview

While a general "health" check is useful, modern orchestration platforms (like Kubernetes) distinguish between **Liveness** (is the process alive or should it be killed?) and **Readiness** (is the process ready to accept traffic or should it be removed from the load balancer?).

This specification extends the `controls` package to allow services to define separate liveness and readiness checks, and provides standardized endpoints for each.

## 2. Problem Statement

The basic health check (`/healthz`) is an aggregate of all services. However:
1. A service might be "alive" but not yet "ready" (e.g., still loading a large cache or waiting for a database migration).
2. Killing a process because it's temporarily not ready can lead to "crash looping" that prevents it from ever becoming ready.
3. Consumers need a way to define which checks are critical for "liveness" vs. "readiness".

## 3. Goals & Non-Goals

### Goals
- Allow services to register separate `LivenessFunc` and `ReadinessFunc`.
- Provide `/livez` and `/readyz` HTTP handlers.
- Support gRPC health check sub-services for different probe types.
- Maintain backward compatibility with the general `StatusFunc`.

### Non-Goals
- Defining the specific business logic of what makes a service "ready" (this is implementation-defined).
- Handling "Startup" probes (can be added later if needed).

## 4. Public API

### 4.1 `pkg/controls`

Update `Service` and `ServiceOption`:

```go
type ProbeFunc func() error

type Service struct {
    Name      string
    Start     StartFunc
    Stop      StopFunc
    Status    StatusFunc // Backward compatibility / general health
    Liveness  ProbeFunc
    Readiness ProbeFunc
}

func WithLiveness(fn ProbeFunc) ServiceOption
func WithReadiness(fn ProbeFunc) ServiceOption
```

Update `Controller` to expose differentiated reports:

```go
func (c *Controller) Liveness() HealthReport
func (c *Controller) Readiness() HealthReport
```

### 4.2 `pkg/controls/http`

Add specific handlers:

```go
func LivenessHandler(controller controls.Controllable) http.HandlerFunc
func ReadinessHandler(controller controls.Controllable) http.HandlerFunc
```

### 4.3 `pkg/controls/grpc`

The gRPC health protocol supports a "service" string. We can map these:
- Service `""` (empty): Aggregate Status
- Service `"liveness"`: Liveness status
- Service `"readiness"`: Readiness status

## 5. Internal Implementation

### 5.1 Default Behavior
- If `Liveness` is not provided, it defaults to calling `Status()`.
- If `Readiness` is not provided, it defaults to calling `Status()`.
- This ensures that if a user only provides a single health check, both probes use it.

### 5.2 Aggregation Logic
- **Liveness**: If any *critical* service (to be defined) fails its liveness check, the whole report is unhealthy.
- **Readiness**: If any service fails its readiness check, the report is unhealthy (removing the pod from traffic).

## 6. Testing Strategy

Implementation must follow the **Test-Driven Development (TDD)** approach.

### 6.1 Unit Tests
- **Package `pkg/controls`**:
    - Verify `WithLiveness` and `WithReadiness` options correctly populate the `Service` struct.
    - Test `Liveness()` and `Readiness()` methods on `Controller` with mixed service states.
    - Verify fallback logic (Liveness/Readiness falling back to Status if not provided).
- **Package `pkg/controls/http`**:
    - Test `LivenessHandler` and `ReadinessHandler` independently.
    - Assert correct status codes and JSON payloads for various scenarios.
- **Package `pkg/controls/grpc`**:
    - Verify gRPC health service returns correct status for named services `"liveness"` and `"readiness"`.

### 6.2 Integration Tests
- Deploy a mock service with separate liveness and readiness logic.
- Verify through HTTP requests that `/livez` returns 200 while `/readyz` returns 503 during "warm-up" phases.

### 6.3 Quality Gates
- **Code Coverage**: At least **90% coverage** for new code.
- **Race Detection**: All tests must pass with `go test -race ./...`.
- **Linting**: Clean `golangci-lint run --fix` output.

## 7. Documentation Maintenance

- **Library Documentation**: Update `docs/components/controls.md` to include `WithLiveness`, `WithReadiness`, and the new handlers.
- **Concept Documentation**: Update `docs/concepts/service-orchestration.md` to explain the difference between health, liveness, and readiness in the context of `go-tool-base`.
- **API Reference**: Ensure all new exported functions and types are documented with GoDoc comments.

## 8. Leveraged Workflows

Implementation MUST leverage:
- `/gtb-library-contribution`
- `/gtb-verify`
- `/gtb-docs`

## 9. Implementation Phases

### Phase 1: Core Extensions
- Update `Service` struct and options.
- Implement `Liveness()` and `Readiness()` methods on `Controller`.

### Phase 2: HTTP Integration
- Add handlers to `pkg/controls/http`.

### Phase 3: gRPC Integration
- Update gRPC health service to handle named sub-services.
