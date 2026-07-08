---
title: Hot-Reload & Observers
description: React to live configuration changes with the Observable interface and fail-closed reloads.
date: 2026-02-16
tags: [components, config, configuration, viper]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Hot-Reload & Observers

## Observer Pattern for Configuration Changes

The configuration system includes a built-in observer pattern. The file-backed
`Container` runs its own `fsnotify` watcher over **every** configured file. On a
change it rebuilds and re-merges all files into a candidate, validates the
candidate against the schema (if any), and — only on success — swaps the live
config atomically and notifies observers. A reload that fails to parse any file
or fails validation is rejected **fail-closed**: the last-known-good config is
retained, `Get*` keeps serving the previous values, and observers are not
notified. Save bursts are coalesced behind a configurable debounce window
(default 250 ms; see `WithReloadDebounce`).

### Observable Interface

`Run` returns an `error`. A returned error is logged by the framework; it does
not abort subsequent observers and never stalls future reloads. (This replaced
the previous `chan error` parameter — see the
[migration guide](../../../reference/migration/v0.16-hot-reload-observer.md).)


> [!NOTE]
> See [pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/config](https://pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/config) for the full API definition.


### Adding Observers

Register observers to react to configuration changes:

```go
// Using the Observable interface
type ConfigWatcher struct {
    name string
}

func (cw *ConfigWatcher) Run(cfg config.Containable) error {
    // React to configuration changes
    newPort := cfg.GetInt("app.port")
    fmt.Printf("Configuration updated - new port: %d\n", newPort)

    // Return any error; it is logged by the framework
    if newPort < 1024 {
        return fmt.Errorf("invalid port number: %d", newPort)
    }

    return nil
}

// Register the observer
watcher := &ConfigWatcher{name: "port-monitor"}
container.AddObserver(watcher)

// Or use a function directly
container.AddObserverFunc(func(cfg config.Containable) error {
    l.Info("Configuration reloaded", "timestamp", time.Now())

    return nil
})
```

### Observing Typed Sections

When a long-lived service, transport, client, or health check needs typed
settings, prefer `ObserveSection` over a hand-written `AddObserverFunc`.
`ObserveSection` performs the initial `UnmarshalSection`, registers a reload
observer, validates each fresh snapshot, preserves the last valid snapshot if a
reload cannot be decoded or validated, and optionally invokes an apply callback
after a successful rehydrate changes the typed section.

```go
type HTTPSettings struct {
    Port int `mapstructure:"port"`
}

settings, err := config.ObserveSection[HTTPSettings](
    container,
    "server.http",
    config.WithSectionValidator(func(next HTTPSettings) error {
        if next.Port <= 0 {
            return fmt.Errorf("port must be positive")
        }

        return nil
    }),
    config.WithSectionApply(func(change config.SectionChange[HTTPSettings]) error {
        return server.Reconfigure(&change.Current.Value)
    }),
)
if err != nil {
    return err
}

server.SetSettingsSource(settings)
```

Packages that should remain extractable can define their own local settings
source interface:

```go
type SettingsSource interface {
    Current() *HTTPSettings
}
```

`*config.ObservedSection[HTTPSettings]` satisfies that shape, but the package
itself does not need to import `pkg/config`.

The binding compares complete typed snapshots. Unrelated config reloads do not
increment `ObservedSection.Version()` and do not invoke the apply callback. When
the section does change, the callback receives `SectionChange[T]` with both
`Previous` and `Current`, so the component can reconfigure from whole settings
objects rather than tracking individual keys.

### Automatic File Watching

Every file-backed container is watched — single-file as well as multi-file —
once construction completes:

```go
// This enables file watching automatically
container := config.NewFilesContainer(fs,
    config.WithLogger(l),
    config.WithConfigFiles("config.yaml", "local.yaml"),
    config.WithReloadDebounce(500*time.Millisecond), // optional; default 250ms
)
defer container.Close() // stop the watcher and release OS resources

// File watching triggers observers when either file changes
container.AddObserverFunc(func(cfg config.Containable) error {
    // Called whenever config.yaml or local.yaml changes, after the merged
    // candidate has validated and been swapped in.
    newLogLevel := cfg.GetString("log.level")
    // Reconfigure logging, restart services, etc.
    _ = newLogLevel

    return nil
})
```

### Reacting to rejected reloads

Observers are notified only when a reload **succeeds** — the candidate config
was built, passed schema validation, and was swapped in. They are never handed
a rejected reload, because nothing changed and the returned-error contract has
no channel to push a reload-time error back to an observer.

To learn about a **rejected** reload programmatically, register an
`OnReloadError` callback. It fires whenever a reload is rejected and the
last-known-good config is retained — that is, when:

- the candidate failed to build (a fail-closed partial-merge / parse error, or
  the primary file went missing, honouring `ErrConfigFileNotFound`); or
- the candidate failed schema validation.

```go
container := config.NewFilesContainer(fs,
    config.WithLogger(l),
    config.WithConfigFiles("config.yaml", "local.yaml"),
    config.WithSchema(schema),
)

// Fires on a CHANGE that was applied.
container.AddObserverFunc(func(cfg config.Containable) error {
    // apply the new, validated configuration
    return nil
})

// Fires on a CHANGE that was REJECTED (config unchanged, last-known-good kept).
container.OnReloadError(func(err error) {
    l.Warn("config reload rejected; keeping last-known-good", "error", err)
    // e.g. raise an alert, bump a metric, surface a banner
})
```

**Guarantees and ordering**

- The container always logs the rejection at `ERROR`; `OnReloadError` callbacks
  are **additive** to that log, not a replacement.
- `OnReloadError` is **never** invoked for a successful reload; observers are.
- Callbacks are stored under the container mutex, copied under the lock, and
  invoked **outside** the lock (the same race-safe, deadlock-free discipline as
  observer notification), so registering a callback concurrently with an active
  reload is safe under `-race`.
- Callbacks run in registration order on the watcher goroutine; a slow callback
  delays subsequent reloads, so offload expensive work.

## Related

- [How to React to Configuration Changes](../../../how-to/config-hot-reload.md) — the step-by-step recipe for wiring observers and handling rejected reloads
- [Observe Typed Config Sections](../../../how-to/observe-typed-config.md) — bind typed settings, detect whole-struct changes, and reconfigure from `SectionChange`
- [How to Test Code That Uses Configuration](../../../how-to/test-configuration.md) — exercising observers without file watching
