---
title: Service Types & States
description: Service definitions, status, and the lifecycle states a service moves through.
date: 2026-02-16
tags: [components, controls, lifecycle, services, shutdown]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Service Types & States

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
func WithRestartResetInterval(d time.Duration) ServiceOption
```

A service registered without a `Start` or `Stop` function defaults to a no-op for
the missing function, so it never panics at start or shutdown. `Start()` is
idempotent: a second call while already running is a safe no-op.

**Register before Start.** `Register` must be called before `Start()`. Once
`Start()` has run, the controller has already snapshotted the service set and
launched its supervisor goroutines, so a service registered afterwards is never
started, monitored, or stopped. `Register` has no error return (missing lifecycle
funcs default to no-ops), so a late call does not fail — instead it logs a
**WARNING** (`Register called after Start; service will not be supervised`) and
still records the service so status queries reflect it. Treat that warning as a
programming error to fix, not a supported pattern.

### Self-Healing and Automatic Restarts

The `controls` package includes an opt-in supervisor loop that can automatically restart failing services. By default, services are not restarted. To enable self-healing for a specific service, provide a `RestartPolicy` during registration:


> [!NOTE]
> See [pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/controls](https://pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/controls) for the full API definition.


Each supervised run is classified into one of three explicit outcomes, and **only an error triggers a restart**:

- **Clean start** — `StartFunc` returned `nil`. A server that spawns its listener in a background goroutine and returns `nil` has *started*, not *exited*. It is **never** restarted on that basis; it is supervised via its `StatusFunc` (when `HealthFailureThreshold > 0`) and otherwise simply runs until shutdown.
- **Context cancelled / valid error** — the run ended because the controller context was cancelled, or `StartFunc` returned an error matched by a `WithValidError` predicate (e.g. `http.ErrServerClosed`). Never restarts; never forwarded as a failure.
- **Error** — `StartFunc` returned a genuine error while the context was live, or the `StatusFunc` exceeded `HealthFailureThreshold`. This is the only restart-worthy outcome.

`MaxRestarts` counts **consecutive** failures. After a service runs healthily for `RestartResetInterval` (default 30 s; set per-service via `WithRestartResetInterval` or the policy field), the counter resets to zero. The backoff between restarts grows exponentially up to `MaxBackoff`. The controller **never sends `nil`** on the error channel.

To exempt expected terminal errors from the restart count for the whole controller, register a predicate with `WithValidError`:

```go
controller := controls.NewController(ctx,
    controls.WithValidError(func(err error) bool {
        return errors.Is(err, http.ErrServerClosed) || errors.Is(err, context.Canceled)
    }),
)
```

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

#### Registering checks

A check is registered with `controller.RegisterHealthCheck(controls.HealthCheck{…})`:
the `Check` func returns a `CheckResult`, `Timeout` bounds each run, and `Type`
(below) controls which endpoints it appears in. With an `Interval` set the check
runs **async** on that interval and caches its result; without one it runs **sync**,
inline on every health request.

```go
controller.RegisterHealthCheck(controls.HealthCheck{
    Name:    "database",
    Check:   func(ctx context.Context) controls.CheckResult { /* probe; return CheckHealthy / CheckUnhealthy */ },
    Timeout: 2 * time.Second,
    Type:    controls.CheckTypeReadiness,
})
```

!!! note "Readiness fails closed before the first async run"
    An async check has no cached result until its first interval run completes.
    For **readiness** gating (`/readyz`), an async readiness check with no result
    yet is reported as **not-ready** (HTTP 503) rather than defaulting to OK. This
    prevents a brief window at startup where traffic is admitted before the check
    has actually run. The same uninitialised result is treated as OK for `/healthz`
    and `/livez`, which are not traffic gates.

#### Check Types

| `CheckType` | Appears in |
|------------|------------|
| `CheckTypeReadiness` (default) | `/readyz` and `/healthz` |
| `CheckTypeLiveness` | `/livez` and `/healthz` |
| `CheckTypeBoth` | All endpoints |

For the full sync/async recipes and querying cached results, see the
**[Register Health Checks](../../../how-to/register-health-checks.md)** how-to.

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


> [!NOTE]
> See [pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/controls](https://pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/controls) for the full API definition.
