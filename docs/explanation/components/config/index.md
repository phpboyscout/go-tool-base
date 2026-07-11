---
title: Configuration
description: Configuration management extending Viper with testability, change observation, and multiple sources.
date: 2026-02-16
tags: [components, config, configuration, viper]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Configuration

The Configuration component provides a flexible and powerful abstraction over the `spf13/viper` configuration library. It delivers enhanced functionality for configuration loading and management while adding crucial testability features that are not available with viper directly.

## Overview

The configuration system is built around the `Containable` interface and the `Container` struct, providing a unified API for accessing configuration values regardless of their source. The package adds several key improvements over raw viper usage:

**Enhanced Testability**: Unlike viper, which is difficult to mock effectively, the `Containable` interface enables clean dependency injection and comprehensive testing strategies.

**Observer Pattern**: Adds filesystem watching with an observer pattern for configuration changes, allowing your application to react to configuration updates automatically.

**Typed Section Boundaries**: Decodes resolved config sections into package-owned structs with `UnmarshalSection`, so reusable packages can receive ordinary Go data instead of depending on GTB's config container.

**Observed Settings Snapshots**: Binds long-lived components to typed settings with `ObserveSection`, which performs the initial decode, registers a reload observer, validates new snapshots, detects whole-struct changes, and publishes the latest immutable settings through `ObservedSection`.

**Simplified API**: Provides convenience methods for common configuration tasks while maintaining access to the underlying viper instance when needed.

**Multiple Source Support**: Handles configuration loading from files, embedded resources, environment variables, and command-line flags with automatic merging and type conversion.

## Why use the Wrapper?

Instead of industrializing Viper directly in your application code, GTB provides the `Containable` interface. This allows us to:

- **Enforce Consistency**: Methods like `NewFilesContainer` ensure that every CLI tool follows the same logic for loading and merging configuration files.
- **Abstract the Filesystem**: We integrate natively with `afero`, meaning your configuration can be loaded from the OS, an in-memory test buffer, or embedded assets through the same interface.
- **Automate Environment Mapping**: We pre-configure environment variable replacement (e.g., `server.port` becomes `SERVER_PORT`) so you don't have to.

## Core Interface

The `Containable` interface provides the primary API for configuration access:


> [!NOTE]
> See [pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/config](https://pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/config) for the full API definition.


## Container Implementation

The `Container` struct is the primary implementation of the `Containable` interface. Engineers should use this concrete type rather than the interface directly, except for testing and dependency injection. Its fields are internal — construct it via the options-pattern factory functions:

```go
func NewFilesContainer(fs afero.Fs, opts ...ContainerOption) *Container
func NewReaderContainer(fs afero.Fs, opts ...ContainerOption) *Container
```

> [!NOTE]
> See [pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/config](https://pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/config) for the full `Container` API.

## Typed Sections for Package Boundaries

Reusable packages should define the settings struct they need and accept that
struct in their constructors. GTB adapter code is responsible for loading and
observing the framework config:

```go
type ServerSettings struct {
    Port int           `mapstructure:"port"`
    ReadTimeout time.Duration `mapstructure:"read_timeout"`
}

section, err := config.UnmarshalSection[ServerSettings](cfg, "server.http")
if err != nil {
    return err
}

server, err := http.NewServer(ctx, section.Value, handler)
```

For long-lived components, prefer `ObserveSection` so reloads rehydrate typed
settings automatically:

```go
settings, err := config.ObserveSection[ServerSettings](
    cfg,
    "server.http",
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

Packages that may be extracted should depend on a tiny local interface such as
`interface { Current() *ServerSettings }` when they need reload-aware access.
That lets `*config.ObservedSection[ServerSettings]` satisfy the package
contract without the extracted module importing `pkg/config`.

`ObservedSection.Version()` increments only when the typed section changes after
a successful reload. `WithSectionApply` receives a `SectionChange[T]` with the
previous and current snapshots, so packages can reconfigure from whole settings
objects instead of watching individual config keys.

### Container Options

All factory functions accept functional options to configure container behavior. The only required argument is `fs afero.Fs`:

```go
config.WithLogger(l *slog.Logger)              // Logger (default: noop)
config.WithEnvPrefix(prefix string)            // Env var prefix (default: none)
config.WithConfigFiles(files ...string)        // Config file paths
config.WithConfigFormat(format string)         // "yaml", "json", "toml"
config.WithConfigReaders(readers ...io.Reader) // io.Reader config sources
config.WithSchema(schema *Schema)              // Validation schema
```

---

## In this section

- **[Creating Containers](creating-containers.md)** — factory functions and choosing the right one
- **[Sources & Precedence](sources-and-precedence.md)** — file, embedded, environment, dotenv, and how they merge
- **[Schema Validation](validation.md)** — validate configuration against a schema
- **[Hot-Reload & Observers](hot-reload.md)** — react to live configuration changes
- **[Best Practices & Integration](best-practices.md)** — patterns, GTB integration, sensitive-value masking

For test recipes (in-memory containers, the generated mocks, testing observers), see the **[Test Code That Uses Configuration](../../../how-to/test-configuration.md)** how-to guide.
