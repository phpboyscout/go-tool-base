---
title: React to Configuration Changes at Runtime
description: How to use config.Observable and AddObserver to trigger logic when the config file is modified.
date: 2026-03-25
tags: [how-to, config, hot-reload, observer, watch]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# React to Configuration Changes at Runtime

GTB's configuration container watches the config file for changes using `fsnotify`. When a change is detected, all registered observers are called with the updated `Containable`. This lets long-running services (daemons, servers) reconfigure themselves without restarting.

---

## How It Works

The `watchConfig` loop starts automatically when the container is loaded from a config file. Observers are invoked synchronously in the watch goroutine. Errors returned via the `chan error` are logged by the framework — they do not abort subsequent observers.

---

## Step 1: Implement the `Observable` Interface

Create a struct that implements `config.Observable`:

```go
import "gitlab.com/phpboyscout/go-tool-base/pkg/config"

type DatabaseReconfigurer struct {
    pool *sql.DB
    log  logger.Logger
}

func (r *DatabaseReconfigurer) Run(cfg config.Containable, errs chan error) {
    newDSN := cfg.GetString("database.dsn")
    if newDSN == "" {
        errs <- errors.New("database.dsn is required")
        return
    }

    if err := r.pool.Reconnect(newDSN); err != nil {
        errs <- errors.Wrap(err, "failed to reconfigure database pool")
        return
    }

    r.log.Info("Database pool reconfigured", "dsn", maskDSN(newDSN))
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

If you don't need a named struct, register a function directly:

```go
p.Config.AddObserverFunc(func(cfg config.Containable, errs chan error) {
    newLevel, err := logger.ParseLevel(cfg.GetString("log.level"))
    if err != nil {
        errs <- err
        return
    }

    p.Logger.SetLevel(newLevel)
    p.Logger.Info("Log level updated", "level", newLevel)
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

Errors from one observer do not prevent subsequent observers from running.

---

## Example: Log Level Hot-Reload

A complete pattern for runtime log level changes — useful for toggling debug output on a running daemon:

```go
// Register once during startup
p.Config.AddObserverFunc(func(cfg config.Containable, errs chan error) {
    levelStr := cfg.GetString("log.level")
    if levelStr == "" {
        return  // key absent — no change
    }

    level, err := logger.ParseLevel(levelStr)
    if err != nil {
        errs <- errors.WithHintf(err,
            "Valid levels are: debug, info, warn, error")
        return
    }

    current := p.Logger.GetLevel()
    if level == current {
        return  // no change
    }

    p.Logger.SetLevel(level)
    p.Logger.Info("Log level changed",
        "from", current,
        "to", level,
    )
})
```

Now, changing `log.level: debug` in the config file takes effect immediately on the running process.

---

## Important Constraints

**Observers run in the watch goroutine.** Keep handlers fast and non-blocking. For expensive operations (e.g. re-establishing a connection pool), dispatch to a separate goroutine and signal a channel:

```go
type AsyncReconfigurer struct {
    triggerCh chan config.Containable
}

func (r *AsyncReconfigurer) Run(cfg config.Containable, _ chan error) {
    // Non-blocking send; drop the update if the channel is busy
    select {
    case r.triggerCh <- cfg:
    default:
    }
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
p.Config.AddObserverFunc(func(cfg config.Containable, errs chan error) {
    if err := reconfigure(cfg); err != nil {
        errs <- err
    }
})
```

---

## Testing

In tests, call `observer.Run(mockCfg, errCh)` directly — no file watching needed:

```go
func TestDatabaseReconfigurer(t *testing.T) {
    mockCfg := mocks_config.NewMockContainable(t)
    mockCfg.On("GetString", "database.dsn").Return("postgres://test/db")

    mockPool := &mockDB{}
    observer := &DatabaseReconfigurer{pool: mockPool, log: logger.NewNoop()}

    errCh := make(chan error, 1)
    observer.Run(mockCfg, errCh)

    select {
    case err := <-errCh:
        t.Fatalf("unexpected error: %v", err)
    default:
        assert.True(t, mockPool.ReconnectCalled)
    }
}
```

---

## Related Documentation

- **[Configuration component](../components/config.md)** — `Containable`, `Observable`, `AddObserver` API reference
- **[Configuration Precedence](../concepts/config.md)** — how file watching fits into the config loading lifecycle
