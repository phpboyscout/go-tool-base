---
title: Observe Typed Config Sections
description: Use config.ObserveSection to rehydrate typed settings and react only when a config struct changes.
date: 2026-07-08
tags: [how-to, config, hot-reload, typed-config]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Observe Typed Config Sections

Use `config.ObserveSection` when a long-lived component needs settings from GTB
config but the reusable package should not depend on `pkg/config`.

The pattern is:

1. Define a package-owned settings struct.
2. Let the package accept that struct, or a tiny settings-source interface.
3. In GTB adapter code, bind the config section with `ObserveSection`.
4. Reconfigure from `SectionChange.Current` only when the typed struct changes.

## Define Package Settings

```go
type ServerSettings struct {
    Port           int `mapstructure:"port"`
    MaxHeaderBytes int `mapstructure:"max_header_bytes"`
}

type SettingsSource interface {
    Current() *ServerSettings
    Version() uint64
}
```

The package can accept `SettingsSource` without importing `pkg/config`.
`*config.ObservedSection[ServerSettings]` satisfies that shape from the GTB
adapter layer.

## Bind the Config Section

```go
settings, err := config.ObserveSection[ServerSettings](
    props.Config,
    "server.http",
    config.WithSectionDefaults(defaultServerSettings(), mergeServerSettings),
    config.WithSectionValidator(func(next ServerSettings) error {
        return next.Validate()
    }),
    config.WithSectionApply(func(change config.SectionChange[ServerSettings]) error {
        return server.Reconfigure(&change.Current.Value)
    }),
)
if err != nil {
    return err
}

server.SetSettingsSource(settings)
```

`ObserveSection` performs the initial unmarshal, registers an observer, validates
fresh snapshots on successful reloads, and publishes immutable snapshots through
`Current`.

## React Only to Real Changes

The binding compares the previous and current typed section as whole structs.
Unrelated config reloads do not increment `Version` and do not call
`WithSectionApply`.

When the section changes, the callback receives:

| Field | Meaning |
|-------|---------|
| `Previous` | Last valid typed section snapshot. |
| `Current` | New validated typed section snapshot. |
| `Changed` | True when the binding published a new version. |
| `Version` | Monotonic version after the change was published. |

Packages should reconfigure from `change.Current.Value` instead of watching
individual config keys.

## Custom Equality

Use `WithSectionEqual` when a package needs to ignore derived or operationally
irrelevant fields:

```go
config.WithSectionEqual(func(previous, current config.Section[ServerSettings]) bool {
    return previous.Exists == current.Exists &&
        previous.Value.Port == current.Value.Port &&
        previous.Value.MaxHeaderBytes == current.Value.MaxHeaderBytes
})
```

Keep the equality function package-owned and based on complete settings
semantics, not scattered call-site field checks.

## Dynamic Defaults

Use `WithSectionDefaultFunc` when defaults come from another config location and
must rehydrate on reload too. For example, a transport-specific port may fall
back to a shared `server.port` key:

```go
config.WithSectionDefaultFunc(func(cfg config.Containable) ServerSettings {
    return ServerSettings{Port: cfg.GetInt("server.port")}
}, mergeServerSettings)
```

The default function runs during the initial bind and on every successful
reload before equality is checked.

## HTTP Server Settings

`pkg/http` provides a package-specific helper for its server settings:

```go
settings, err := gtbhttp.ObserveServerSettingsFromConfig(
    cfg,
    "server.http",
    config.WithSectionApply(func(change config.SectionChange[gtbhttp.ServerSettings]) error {
        log.Info("http settings changed", "version", change.Version)

        return nil
    }),
)
if err != nil {
    return err
}

var source gtbhttp.ServerSettingsSource = settings
_ = source.Current()
```

The helper keeps the existing GTB config shape intact. It reads
`server.http.*`, keeps `server.port` as the shared port fallback, and rehydrates
that fallback on reload. The returned source exposes `Version()` so code can
quickly tell whether the whole typed settings snapshot has changed.

## Failure Behaviour

If a reload cannot be decoded or validation fails, the binding keeps the last
valid snapshot:

- `Current` continues to return the previous settings.
- `Version` does not increment.
- `WithSectionApply` is not called.
- The observer returns the decode or validation error so the container logs it.

This mirrors GTB's fail-closed config reload behaviour while keeping package
constructors independent of the framework config container.
