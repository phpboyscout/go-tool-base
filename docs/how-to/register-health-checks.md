---
title: Register Custom Health Checks
description: How to register standalone health checks for external dependencies with the controls package.
date: 2026-03-26
tags: [how-to, controls, health, observability]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Register Custom Health Checks

Health checks verify external dependencies (databases, caches, third-party APIs) independently of service lifecycle. They feed into the existing `/healthz`, `/livez`, and `/readyz` HTTP endpoints and gRPC health service.

!!! important
    Health checks must be registered **before** calling `controller.Start()`.

## 1. Write a Check Function

A check function receives a `context.Context` (with timeout applied) and returns a `controls.CheckResult`:

```go
func checkDatabase(ctx context.Context) controls.CheckResult {
    if err := db.PingContext(ctx); err != nil {
        return controls.CheckResult{
            Status:  controls.CheckUnhealthy,
            Message: fmt.Sprintf("database unreachable: %v", err),
        }
    }

    return controls.CheckResult{
        Status:  controls.CheckHealthy,
        Message: "database connection OK",
    }
}
```

### Three-State Model

| Status | Meaning | Effect on `/healthz` |
|--------|---------|---------------------|
| `CheckHealthy` | All good | Reports `"OK"`, overall healthy |
| `CheckDegraded` | Needs attention but still serving | Reports `"DEGRADED"`, overall **still healthy** |
| `CheckUnhealthy` | Failed | Reports `"ERROR"`, overall **unhealthy** |

Use `CheckDegraded` for situations like connection pool saturation or elevated latency:

```go
func checkConnectionPool(ctx context.Context) controls.CheckResult {
    stats := db.Stats()
    usage := float64(stats.InUse) / float64(stats.MaxOpenConnections)

    switch {
    case usage > 0.9:
        return controls.CheckResult{
            Status:  controls.CheckUnhealthy,
            Message: fmt.Sprintf("pool exhausted: %.0f%% in use", usage*100),
        }
    case usage > 0.7:
        return controls.CheckResult{
            Status:  controls.CheckDegraded,
            Message: fmt.Sprintf("pool pressure: %.0f%% in use", usage*100),
        }
    default:
        return controls.CheckResult{Status: controls.CheckHealthy}
    }
}
```

## 2. Register a Synchronous Check

Sync checks run inline on every health request. Use these for fast, low-cost checks:

```go
controller := controls.NewController(ctx, controls.WithLogger(l))

err := controller.RegisterHealthCheck(controls.HealthCheck{
    Name:    "database",
    Check:   checkDatabase,
    Timeout: 2 * time.Second,
    Type:    controls.CheckTypeReadiness,
})
if err != nil {
    return err
}

controller.Start()
```

## 3. Register an Asynchronous Check

Async checks run on a background interval and cache their result. Use these for expensive checks (network round-trips, heavy queries) to avoid adding latency to every health request:

```go
err := controller.RegisterHealthCheck(controls.HealthCheck{
    Name:     "redis",
    Check:    checkRedis,
    Timeout:  3 * time.Second,
    Interval: 15 * time.Second, // Run every 15s, serve cached result between runs
    Type:     controls.CheckTypeBoth,
})
```

The first execution runs immediately on `controller.Start()`. Subsequent runs follow the interval. The async goroutine stops automatically on controller shutdown.

## 4. Choose a Check Type

The `Type` field controls which health endpoints include the check:

| Type | `/healthz` (status) | `/livez` (liveness) | `/readyz` (readiness) |
|------|:---:|:---:|:---:|
| `CheckTypeReadiness` (default) | Yes | No | Yes |
| `CheckTypeLiveness` | Yes | Yes | No |
| `CheckTypeBoth` | Yes | Yes | Yes |

**Guidelines:**

- **Readiness** — Use for dependencies that determine whether the service can accept traffic (database, downstream APIs).
- **Liveness** — Use for checks that determine whether the process should be restarted (deadlock detection, critical subsystem failure).
- **Both** — Use when the check is relevant to both decisions (e.g., a required cache that is both a startup dependency and a runtime health signal).

## 5. Query Check Results Programmatically

Retrieve the latest result for any named check:

```go
result, ok := controller.GetCheckResult("database")
if ok {
    fmt.Printf("Status: %d, Message: %s, At: %s\n",
        result.Status, result.Message, result.Timestamp)
}
```

For sync checks, the result is populated after the first call to `Status()`, `Liveness()`, or `Readiness()`. For async checks, it is populated immediately after `Start()`.

## Complete Example

```go
func setupHealthChecks(controller *controls.Controller, db *sql.DB, redis *redis.Client) error {
    // Fast sync check — runs on every readiness request
    if err := controller.RegisterHealthCheck(controls.HealthCheck{
        Name:    "database",
        Check:   func(ctx context.Context) controls.CheckResult {
            if err := db.PingContext(ctx); err != nil {
                return controls.CheckResult{Status: controls.CheckUnhealthy, Message: err.Error()}
            }
            return controls.CheckResult{Status: controls.CheckHealthy}
        },
        Timeout: 2 * time.Second,
        Type:    controls.CheckTypeReadiness,
    }); err != nil {
        return err
    }

    // Expensive async check — cached, runs every 30s
    if err := controller.RegisterHealthCheck(controls.HealthCheck{
        Name:    "redis",
        Check:   func(ctx context.Context) controls.CheckResult {
            if err := redis.Ping(ctx).Err(); err != nil {
                return controls.CheckResult{Status: controls.CheckUnhealthy, Message: err.Error()}
            }
            return controls.CheckResult{Status: controls.CheckHealthy}
        },
        Timeout:  3 * time.Second,
        Interval: 30 * time.Second,
        Type:     controls.CheckTypeBoth,
    }); err != nil {
        return err
    }

    return nil
}
```
