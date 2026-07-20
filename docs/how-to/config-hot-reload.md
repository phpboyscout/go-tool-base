---
title: React to Configuration Changes at Runtime
description: How to watch the config store and use observers or typed sections to trigger logic when configuration changes on disk.
date: 2026-03-25
tags: [how-to, config, hot-reload, observer, watch]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# React to Configuration Changes at Runtime

GTB's configuration is a layered [go/config](https://config.go.phpboyscout.uk)
Store. **Watching is explicit**: nothing reloads until someone calls
`Store.Watch`. For a GTB tool you don't have to — the root command's bootstrap
wires `Watch` (scoped to the command context) and `OnReloadError` for you, so
by the time your `RunE` runs, external file changes already reach the store.
What's left for your code is deciding how to *react*, and that is what
observers and typed sections are for.

On a change the store re-reads the file layers, builds a candidate snapshot,
validates it, and — only if the candidate is good — publishes it and notifies
observers. A rejected reload keeps the last-known-good snapshot and fires
`OnReloadError` (the root bootstrap logs it as a warning). The store's own
`Apply` writes never travel the watch path, so a write can't come back around
as a spurious reload.

---

## Step 1: Implement the `Observable` Interface

Create a struct that implements `config.Observable`. `Run` receives a
`config.Observed` — a read surface **pinned to the snapshot that triggered the
notification**, so every value you read belongs to the same coherent
configuration:

```go
import "gitlab.com/phpboyscout/go/config"

type DatabaseReconfigurer struct {
    pool *sql.DB
    log  logger.Logger
}

func (r *DatabaseReconfigurer) Run(cfg config.Observed) error {
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
            pool, err := sql.Open("postgres", p.Config.View().GetString("database.dsn"))
            if err != nil {
                return err
            }

            // Called whenever a config change is applied.
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
p.Config.AddObserverFunc(func(cfg config.Observed) error {
    var newLevel slog.Level
    if err := newLevel.UnmarshalText([]byte(cfg.GetString("log.level"))); err != nil {
        return err
    }

    logger.SetLevel(p.Logger, newLevel)
    p.Logger.Info("Log level updated", "level", newLevel)

    return nil
})
```

This is the idiomatic pattern for simple, stateless reconfiguration.

---

## Typed Sections That Stay Current

For a component that consumes a whole configuration section, prefer
`config.ObserveSection` over a hand-rolled observer: it decodes the section
into your struct once per applied change, in a single operation against a
single snapshot, and hands out the latest value on demand:

```go
type ServerSettings struct {
    Port int `config:"port"`
}

section, err := config.ObserveSection[ServerSettings](p.Config, "server.http")
if err != nil {
    return err
}

// Anywhere later — always the settings from the latest applied snapshot:
current := section.Value()
```

The transport adapters (`pkg/http`, `pkg/grpc`, `pkg/gateway`) and the
observability adapter in `pkg/telemetry` are built on exactly this — see
[Observe Typed Config](observe-typed-config.md).

---

## Multiple Observers

Observers execute in registration order. Register independent concerns
separately:

```go
p.Config.AddObserverFunc(updateLogLevel)
p.Config.AddObserverFunc(updateRateLimit)
p.Config.AddObserver(&DatabaseReconfigurer{pool: pool})
p.Config.AddObserver(&CacheReconfigurer{cache: cache})
```

An error returned by one observer is reported through the store's observer
error hook and does not prevent subsequent observers from running, nor does it
stall future reloads.

---

## Reacting to a Rejected Reload

Observers see **applied changes** only. When a reload is rejected — a parse
error, or a candidate that fails schema validation — the store keeps the
last-known-good snapshot and observers are not called, because nothing
changed.

The root bootstrap already registers `OnReloadError` and logs a rejected
reload as a warning. Register your own callback if a component needs to react
beyond the log line:

```go
p.Config.OnReloadError(func(err error) {
    metrics.Increment("config_reload_rejected")
})
```

---

## Watching Outside the Root Command

A library context that builds its own store (tests, a service embedding the
store directly) must start the watcher itself — it is never implicit:

```go
stop, err := store.Watch(ctx)
if err != nil {
    // No watchable sources (e.g. purely embedded config) or the platform
    // watcher failed: the store still works, it just won't see external
    // changes. Decide whether that is fatal for your component.
    log.Warn("config watching unavailable", "error", err)
}
defer stop()
```

`Watch` fails loudly when it cannot function rather than silently doing
nothing — an application that believes it will hear about changes and never
does is worse off than one that knows it must restart. Full watcher semantics
(poll fallback, settle window, injectable watchers for tests) are documented
in the [go/config module docs](https://config.go.phpboyscout.uk).
