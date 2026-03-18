---
title: Configuration System
description: The configuration system: precedence, environment variables, files, and the observer pattern.
date: 2026-02-16
tags: [concepts, config, viper, validation]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Configuration System

GTB provides a robust, opinionated wrapper around [Viper](https://github.com/spf13/viper). While Viper is the engine, our configuration component adds a layer of type safety, easy testability, and a powerful observer pattern for reactive application design.

## Why use the Wrapper?

Instead of industrializing Viper directly in your application code, GTB provides the `Containable` interface. This allows us to:

- **Enforce Consistency**: Methods like `NewFilesContainer` ensure that every CLI tool follows the same logic for loading and merging configuration files.
- **Abstract the Filesystem**: We integrate natively with `afero`, meaning your configuration can be loaded from the OS, an in-memory test buffer, or embedded assets through the same interface.
- **Automate Environment Mapping**: We pre-configure environment variable replacement (e.g., `server.port` becomes `SERVER_PORT`) so you don't have to.

## Precedence Order

When a configuration value is requested, the framework looks through these layers in order of priority (highest first):

1. **Environment Variables**: Prefixed automatically with your tool name. These always take highest priority.
2. **Local .env Files**: Automatically loaded from the current working directory during initialization.
3. **Flags**: Command-line arguments explicitly bound to configuration keys.
4. **Local Configuration Files**: Files located in your tool's config directory (~/.mytool/) or provided via `--config`.
5. **Default Configuration**: Embedded assets provided by the framework or your tool's `assets/` directory.

### Default Asset Convention
GTB follows a naming convention for modular default configuration. By placing a file at **`assets/init/config.yaml`** within your embedded filesystem, you register it as a "sane default" for your module.

During the `init` workflow, the framework iterates through **all** registered asset filesystems and collects these files. This allows your CLI to bootstrap itself with a complete configuration that is the aggregate of all its individual components, without requiring a single monolithic config file in the root.

---

## The Observer Pattern

One of the most powerful features of our configuration system is the ability to react to changes at runtime. Instead of polling for changes, you can register **Observers**.

### Use Cases
- **Dynamic Logging**: Update log levels without restarting the service.
- **Hot-Reloading**: Refresh API clients or database connections when a token or URL changes.
- **Feature Toggles**: Enable or disable features mid-execution by modifying a local config file.

### How to Implement
You can register an observer either as an object implementing the `Observable` interface or as a simple function:

```go
// Using a function
props.Config.AddObserverFunc(func(c config.Containable, errs chan error) {
    newLevel := c.GetString("log.level")
    props.Logger.SetLevel(log.ParseLevel(newLevel))
})
```

When the underlying configuration file is modified on disk, the framework detects the change via `fsnotify`, reloads the values, and executes all registered observers in parallel.

---

## Debugging Configuration

Understanding where a value is coming from can be difficult in a multi-layered system. We provide helper methods to inspect the current state:

### ToJSON & Dump
The `ToJSON()` and `Dump()` methods provide a snapshot of the current "merged" configuration state. This is invaluable when debugging precedence issues.

```go
// print the entire merged config to stdout as JSON
props.Config.Dump()
```

### Path Traceability
Every container is assigned an `ID` that tracks which files or readers were merged to create the current state. This allows you to verify that your specific configuration file was actually picked up by the framework.

## Initialiser Integration

The `config.Containable` interface is also the standard for [Tool Initialisers](initialisers.md). When creating a custom initialiser, you will use this interface to check for existing configuration (`IsConfigured`) and to write new values (`Set`), ensuring a consistent API across the entire lifecycle of the application.
