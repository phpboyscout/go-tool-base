---
title: "Health Check Extensibility Specification"
description: "Add a registration API for custom health checks that feed into the existing HTTP health endpoints, supporting timeouts, async execution, and degraded states."
date: 2026-03-26
status: IMPLEMENTED
tags:
  - specification
  - controls
  - health
  - extensibility
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-opus-4-6)
    role: AI drafting assistant
---

# Health Check Extensibility Specification

Authors
:   Matt Cockayne, Claude (claude-opus-4-6) *(AI drafting assistant)*

Date
:   26 March 2026

Status
:   IMPLEMENTED

---

## Overview

The `pkg/controls` package provides health, liveness, and readiness probes for registered services via `ProbeFunc` callbacks on each `Service`. These probes report binary OK/ERROR status and are tightly coupled to the service registration -- there is no mechanism for downstream consumers to register standalone health checks that are not tied to a service lifecycle.

Real-world tools built on GTB need to verify external dependencies (database connectivity, third-party API reachability, cache availability, certificate expiry) and expose that information through the existing HTTP health endpoints. Currently, the only option is to embed these checks inside a service's `Status`/`Liveness`/`Readiness` probe functions, which conflates service lifecycle with dependency health.

This specification adds a `HealthChecker` registration API that allows consumers to register named health checks independent of services. These checks support configurable timeouts, async (cached) execution, and a three-state result model (healthy, degraded, unhealthy) that maps into the existing `HealthReport` structure.

---

## Design Decisions

**Separate from Service registration**: Health checks are not services -- they have no start/stop lifecycle. Registering them separately keeps the `Service` type focused on lifecycle management and avoids forcing consumers to create dummy services just to add a health check.

**Three-state result model**: Binary OK/ERROR is insufficient for real-world health. A database connection pool at 90% capacity is not healthy, but it is not failed either. The `degraded` state lets operators distinguish "needs attention" from "actively broken". For backwards compatibility, `degraded` maps to `OverallHealthy: true` in `HealthReport` (the system is still serving) but includes a status string that HTTP handlers can use to return different status codes.

**Async checks with caching**: Some health checks are expensive (network round-trips, database queries). Running them synchronously on every HTTP health request adds latency and load. Async checks run on a configurable interval and cache their last result. Sync checks run inline on each request. The consumer chooses per check.

**Timeout per check**: Each check has its own timeout, independent of the HTTP request timeout. A slow database check should not block reporting of other checks.

**Registration after controller creation, before Start()**: Checks are registered on the `Controller` after construction but before `Start()` is called. This follows the same pattern as `Register()` for services.

---

## Public API Changes

### New Types in `pkg/controls`

```go
// CheckResult represents the outcome of a health check.
type CheckResult struct {
    // Status is the health status.
    Status CheckStatus
    // Message provides human-readable detail about the check result.
    Message string
    // Timestamp is when this result was produced.
    Timestamp time.Time
}

// CheckStatus represents the health state of a check.
type CheckStatus int

const (
    // CheckHealthy indicates the check passed.
    CheckHealthy CheckStatus = iota
    // CheckDegraded indicates the check passed but with warnings.
    CheckDegraded
    // CheckUnhealthy indicates the check failed.
    CheckUnhealthy
)

// HealthCheck defines a named health check function.
type HealthCheck struct {
    // Name is the unique identifier for this check.
    Name string
    // Check is the function that performs the health check.
    // It receives a context with the check's timeout applied.
    Check func(ctx context.Context) CheckResult
    // Timeout is the maximum duration for a single check execution.
    // Default: 5s.
    Timeout time.Duration
    // Interval is the polling interval for async checks.
    // Zero means synchronous (run on every health request).
    Interval time.Duration
    // Type determines which health endpoints this check feeds into.
    // Default: CheckTypeReadiness.
    Type CheckType
}

// CheckType determines which health endpoint(s) a check contributes to.
type CheckType int

const (
    // CheckTypeReadiness contributes to the readiness endpoint.
    CheckTypeReadiness CheckType = iota
    // CheckTypeLiveness contributes to the liveness endpoint.
    CheckTypeLiveness
    // CheckTypeBoth contributes to both liveness and readiness endpoints.
    CheckTypeBoth
)
```

### New Methods on `Controller`

```go
// RegisterHealthCheck adds a standalone health check to the controller.
// Must be called before Start(). The check name must be unique across
// both services and health checks.
func (c *Controller) RegisterHealthCheck(check HealthCheck) error
```

### Updated `HealthReport`

```go
type ServiceStatus struct {
    Name   string `json:"name"`
    Status string `json:"status"` // "OK", "DEGRADED", "ERROR"
    Error  string `json:"error,omitempty"`
}
```

The `Status` field gains a new valid value: `"DEGRADED"`. Existing consumers that check `== "OK"` or `== "ERROR"` are unaffected.

### HealthReporter Extension

```go
// HealthCheckReporter extends HealthReporter with check-specific queries.
type HealthCheckReporter interface {
    HealthReporter
    // GetCheckResult returns the latest result for a named health check.
    GetCheckResult(name string) (CheckResult, bool)
}
```

---

## Internal Implementation

### Health Check Registry

```go
type healthCheckEntry struct {
    check      HealthCheck
    lastResult atomic.Pointer[CheckResult]
    cancel     context.CancelFunc
}
```

The `Controller` gains a `healthChecks` field:

```go
type Controller struct {
    // ... existing fields
    healthChecks map[string]*healthCheckEntry
}
```

### Async Check Loop

For checks with `Interval > 0`, a goroutine is started in `Controller.Start()`:

```go
func (c *Controller) startAsyncCheck(entry *healthCheckEntry) {
    c.wg.Add(1)
    go func() {
        defer c.wg.Done()
        ticker := time.NewTicker(entry.check.Interval)
        defer ticker.Stop()

        // Run immediately on start
        entry.runCheck(c.ctx)

        for {
            select {
            case <-c.ctx.Done():
                return
            case <-ticker.C:
                entry.runCheck(c.ctx)
            }
        }
    }()
}

func (e *healthCheckEntry) runCheck(parentCtx context.Context) {
    timeout := e.check.Timeout
    if timeout == 0 {
        timeout = 5 * time.Second
    }

    ctx, cancel := context.WithTimeout(parentCtx, timeout)
    defer cancel()

    result := e.check.Check(ctx)
    result.Timestamp = time.Now()
    e.lastResult.Store(&result)
}
```

### Integration with HealthReport

The `status()`, `liveness()`, and `readiness()` methods on `Services` are updated to include health check results. Health checks are appended to the `Services` slice in the report:

- `CheckTypeReadiness` checks appear in `readiness()` and `status()`.
- `CheckTypeLiveness` checks appear in `liveness()` and `status()`.
- `CheckTypeBoth` checks appear in all three.

`CheckDegraded` maps to `ServiceStatus{Status: "DEGRADED"}` and does not set `OverallHealthy` to false. `CheckUnhealthy` maps to `ServiceStatus{Status: "ERROR"}` and sets `OverallHealthy` to false.

---

## Project Structure

```
pkg/controls/
├── controls.go        <- MODIFIED: new types (CheckResult, CheckStatus, HealthCheck, CheckType, HealthCheckReporter)
├── controller.go      <- MODIFIED: healthChecks field, RegisterHealthCheck, startAsyncCheck
├── services.go        <- MODIFIED: include health checks in status/liveness/readiness reports
├── healthcheck.go     <- NEW: healthCheckEntry, runCheck, async loop
├── healthcheck_test.go <- NEW: health check tests
```

---

## Testing Strategy

| Test | Scenario |
|------|----------|
| `TestRegisterHealthCheck_Success` | Register a check before Start(), verify it appears in status |
| `TestRegisterHealthCheck_DuplicateName` | Registering two checks with the same name returns error |
| `TestRegisterHealthCheck_AfterStart` | Registering after Start() returns error |
| `TestSyncCheck_RunsOnRequest` | Sync check (Interval=0) executes on each Status() call |
| `TestAsyncCheck_CachesResult` | Async check result cached between status requests |
| `TestAsyncCheck_RefreshesOnInterval` | Result updates after interval elapses |
| `TestCheck_Timeout` | Check exceeding timeout receives cancelled context |
| `TestCheck_Healthy` | Healthy check produces "OK" in ServiceStatus |
| `TestCheck_Degraded` | Degraded check produces "DEGRADED", OverallHealthy remains true |
| `TestCheck_Unhealthy` | Unhealthy check produces "ERROR", OverallHealthy becomes false |
| `TestCheckType_Readiness` | ReadinessOnly check appears in readiness and status, not liveness |
| `TestCheckType_Liveness` | LivenessOnly check appears in liveness and status, not readiness |
| `TestCheckType_Both` | Both check appears in all endpoints |
| `TestAsyncCheck_StopsOnShutdown` | Async goroutine exits when controller context is cancelled |
| `TestGetCheckResult` | GetCheckResult returns latest cached result |
| `TestGetCheckResult_Unknown` | GetCheckResult for unregistered name returns false |
| `TestHealthReport_MixedServicesAndChecks` | Report includes both service probes and standalone checks |

### Coverage

- Target: 90%+ for `pkg/controls/` including new health check paths.

---

## Linting

- `golangci-lint run --fix` must pass.
- No new `nolint` directives.

---

## Documentation

- Godoc for all new types and methods.
- Update `docs/components/controls.md` with health check registration examples.
- Document the three-state model and its mapping to HTTP status codes.
- Add examples showing sync vs async checks and check type selection.

---

## Backwards Compatibility

- **No breaking changes to existing interfaces**. `HealthReporter` is unchanged. `HealthCheckReporter` is a new, separate interface.
- The `ServiceStatus.Status` field gains `"DEGRADED"` as a new value. Existing consumers comparing against `"OK"` and `"ERROR"` are unaffected.
- `HealthReport.OverallHealthy` semantics are preserved: it is false only when a check is `CheckUnhealthy` or a service probe returns an error.

---

## Future Considerations

- **Check groups**: Group related checks (e.g., all database checks) and allow enabling/disabling groups at runtime.
- **Check dependencies**: Express that check B is only meaningful if check A passes (e.g., check query latency only if connection check succeeds).
- **Metrics integration**: Expose check results as Prometheus metrics for external monitoring systems.
- **HTTP response code mapping**: Map degraded state to HTTP 207 or a custom header so load balancers can make routing decisions.

---

## Implementation Phases

### Phase 1 -- Types
1. Define `CheckResult`, `CheckStatus`, `CheckType`, `HealthCheck`
2. Define `HealthCheckReporter` interface
3. Add `"DEGRADED"` status to `ServiceStatus` documentation

### Phase 2 -- Registration and Sync Execution
1. Add `healthChecks` map to `Controller`
2. Implement `RegisterHealthCheck` with validation
3. Implement sync check execution in `status()`, `liveness()`, `readiness()`
4. Implement `GetCheckResult`

### Phase 3 -- Async Execution
1. Implement `healthCheckEntry` with `atomic.Pointer` cached result
2. Implement async check goroutine with ticker
3. Wire async check startup into `Controller.Start()`
4. Wire async check shutdown into controller context cancellation

### Phase 4 -- Tests
1. Unit tests for registration and validation
2. Unit tests for sync and async execution
3. Tests for timeout behaviour
4. Tests for three-state mapping in HealthReport
5. Run with race detector

---

## Verification

```bash
go build ./...
go test -race ./pkg/controls/...
go test ./...
golangci-lint run --fix

# Verify new types
grep -n 'CheckResult\|CheckStatus\|HealthCheck\b' pkg/controls/controls.go

# Verify registration method
grep -n 'RegisterHealthCheck' pkg/controls/controller.go
```
