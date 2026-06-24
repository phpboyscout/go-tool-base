---
title: React to Configuration Changes at Runtime
description: How to use config.Observable and AddObserver to trigger logic when the config file is modified.
date: 2026-03-25
tags: [how-to, config, hot-reload, observer, watch]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# React to Configuration Changes at Runtime

GTB's file-backed configuration container watches **every** configured file for changes using a container-owned `fsnotify` watcher. When a change is detected, the container rebuilds and re-merges all files into a candidate config, validates it, and — only if the candidate is good — swaps it in and calls every registered observer with the updated `Containable`. This lets long-running services (daemons, servers) reconfigure themselves without restarting.

---

## How It Works

The watcher starts automatically once the container has finished loading from one or more files (single-file containers are watched too). On a change:

1. **Re-read and re-merge.** The container reads file `[0]` and merges files `[1:]` in order into a fresh candidate — exactly as on first load — so the merged multi-file view is preserved.
2. **Validate the candidate.** If a schema is attached, the candidate is validated *before* it is swapped in.
3. **Fail-closed.** If any file fails to parse (e.g. file `[0]` is valid but file `[2]` is malformed), or the candidate fails validation, the **entire reload is rejected**: the last-known-good config is retained, `Get*` keeps serving the previous values, and observers are **not** notified (nothing changed).
4. **Swap and notify.** On success the live config is swapped atomically under the container lock, then observers run.

A single save typically emits a burst of filesystem events (write, rename, chmod). These are **coalesced behind a debounce window** (default 250 ms, see [Tuning the debounce](#tuning-the-debounce-window)) so observers fire once per save. The watcher also re-establishes its watch on each path after every event so atomic-rename saves (used by many editors) are not missed.

Observers are invoked synchronously in the watch goroutine. An observer that **returns an error** has that error logged by the framework; it does not abort subsequent observers and never stalls future reloads.

---

## Step 1: Implement the `Observable` Interface

Create a struct that implements `config.Observable`. `Run` returns an `error`:

```go
import "gitlab.com/phpboyscout/go-tool-base/pkg/config"

type DatabaseReconfigurer struct {
    pool *sql.DB
    log  logger.Logger
}

func (r *DatabaseReconfigurer) Run(cfg config.Containable) error {
    newDSN := cfg.GetString("database.dsn")
    if newDSN == "" {
        return errors.New("database.dsn is required")
    }

    if err := r.pool.Reconnect(newDSN); err != nil {
        return errors.Wrap(err, "failed to reconfigure database pool")
    }

    r.log.Info("Database pool reconfigured", "dsn", maskDSN(newDSN))

    return nil
}
```

---

## Step 2: Register the Observer

Register during command setup, after the config has been loaded:

```go
func NewCmdServe(p *props.Props) *setup.Command {
    return setup.Wrap("serve", &cobra.Command{
        Use:  "serve",
        RunE: func(cmd *cobra.Command, args []string) error {
            pool, err := sql.Open("postgres", p.Config.GetString("database.dsn"))
            if err != nil {
                return err
            }

            // Register observer — called whenever the config file changes
            p.Config.AddObserver(&DatabaseReconfigurer{
                pool: pool,
                log:  p.Logger,
            })

            return runServer(cmd.Context(), p, pool)
        },
    })
}
```

---

## Using `AddObserverFunc` for Simple Cases

If you don't need a named struct, register a function directly. The function returns an `error`:

```go
p.Config.AddObserverFunc(func(cfg config.Containable) error {
    newLevel, err := logger.ParseLevel(cfg.GetString("log.level"))
    if err != nil {
        return err
    }

    p.Logger.SetLevel(newLevel)
    p.Logger.Info("Log level updated", "level", newLevel)

    return nil
})
```

This is the idiomatic pattern for simple, stateless reconfiguration.

---

## Multiple Observers

Observers execute in registration order. Register independent concerns separately:

```go
// Each observer handles one concern
p.Config.AddObserverFunc(updateLogLevel)
p.Config.AddObserverFunc(updateRateLimit)
p.Config.AddObserver(&DatabaseReconfigurer{pool: pool})
p.Config.AddObserver(&CacheReconfigurer{cache: cache})
```

An error returned by one observer is logged and does not prevent subsequent observers from running, nor does it stall future reloads.

---

## Reacting to a Rejected Reload

Observers see **changes**: they are called only after a reload succeeds — the candidate was built, validated, and swapped in. They are **never** called when a reload is *rejected*, because nothing changed (the returned-error contract means observers *return* errors; there is no channel to *push* a reload-time error to them).

To react to a **rejected** reload — a fail-closed parse/merge error, a missing primary file, or a schema-validation failure that caused the container to keep last-known-good — register an `OnReloadError` callback. It is additive to the container's own `ERROR` log:

```go
// Observer: fires on a reload that was APPLIED.
p.Config.AddObserverFunc(func(cfg config.Containable) error {
    return updateLogLevel(cfg)
})

// OnReloadError: fires on a reload that was REJECTED.
// The config is unchanged and last-known-good is retained.
p.Config.OnReloadError(func(err error) {
    p.Logger.Warn("config reload rejected; keeping last-known-good", "error", err)
    // raise an alert, bump a metric, surface a banner, etc.
})
```

`OnReloadError` callbacks run in registration order on the watch goroutine and follow the same locking discipline as observers (copied under the container lock, invoked outside it), so registering one concurrently with an active reload is race-safe. Keep them fast for the same reason observers should be fast.

---

## Tuning the Debounce Window

The default debounce is 250 ms, chosen to tolerate slow or networked filesystems. Tune it with `WithReloadDebounce`:

```go
c := config.NewFilesContainer(
    fs,
    config.WithConfigFiles("config.yml", "override.yml"),
    config.WithReloadDebounce(500*time.Millisecond),
)
```

A value `<= 0` falls back to `config.DefaultReloadDebounce`.

---

## Example: Log Level Hot-Reload

A complete pattern for runtime log level changes — useful for toggling debug output on a running daemon:

```go
// Register once during startup
p.Config.AddObserverFunc(func(cfg config.Containable) error {
    levelStr := cfg.GetString("log.level")
    if levelStr == "" {
        return nil // key absent — no change
    }

    level, err := logger.ParseLevel(levelStr)
    if err != nil {
        return errors.WithHintf(err, "Valid levels are: debug, info, warn, error")
    }

    current := p.Logger.GetLevel()
    if level == current {
        return nil // no change
    }

    p.Logger.SetLevel(level)
    p.Logger.Info("Log level changed", "from", current, "to", level)

    return nil
})
```

Now, changing `log.level: debug` in the config file takes effect immediately on the running process.

---

## Important Constraints

**Observers run in the watch goroutine.** Keep handlers fast and non-blocking. For expensive operations (e.g. re-establishing a connection pool), dispatch to a separate goroutine:

```go
type AsyncReconfigurer struct {
    triggerCh chan config.Containable
}

func (r *AsyncReconfigurer) Run(cfg config.Containable) error {
    // Non-blocking send; drop the update if the channel is busy
    select {
    case r.triggerCh <- cfg:
    default:
    }

    return nil
}
```

**Observers are not called on startup** — only on subsequent file changes. If you need the same logic at startup and on reload, extract it to a shared function:

```go
reconfigure := func(cfg config.Containable) error {
    return updateDatabasePool(cfg)
}

// Run once at startup
if err := reconfigure(p.Config); err != nil {
    return err
}

// And again on every config file change
p.Config.AddObserverFunc(reconfigure)
```

**Reader/embedded containers are not watched** — there is no backing file. Hot-reload applies only to file-backed containers (`NewFilesContainer`, `LoadFilesContainer`, `LoadFilesContainerWithSchema`).

**Stop the watcher on shutdown.** File-backed containers own an OS watcher; call `Close()` to release it when the container is no longer needed. `Close()` is safe to call more than once and on non-watching containers.

---

## Testing

In tests, call `observer.Run(mockCfg)` directly — no file watching needed:

```go
func TestDatabaseReconfigurer(t *testing.T) {
    mockCfg := mocks_config.NewMockContainable(t)
    mockCfg.On("GetString", "database.dsn").Return("postgres://test/db")

    mockPool := &mockDB{}
    observer := &DatabaseReconfigurer{pool: mockPool, log: logger.NewNoop()}

    err := observer.Run(mockCfg)
    require.NoError(t, err)
    assert.True(t, mockPool.ReconnectCalled)
}
```

For an end-to-end test, write to a real temp file, register an observer, and poll (do not hard-sleep less than the debounce) for the observed effect. Use a small `WithReloadDebounce` to keep the test fast.

---

## Related Documentation

- **[Configuration component](../explanation/components/config.md)** — `Containable`, `Observable`, `AddObserver` API reference
- **[Configuration Precedence](../explanation/concepts/config.md)** — how file watching fits into the config loading lifecycle
- **[Migration: observer signature change](../reference/migration/v0.16-hot-reload-observer.md)** — channel → returned error
