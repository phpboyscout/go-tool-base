---
title: "Config Environment Variable Prefix"
description: "Add configurable environment variable prefix to pkg/config to prevent config pollution in shared environments."
date: 2026-04-02
status: IMPLEMENTED
tags:
  - specification
  - config
  - security
  - feature
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
---

# Config Environment Variable Prefix

Authors
:   Matt Cockayne

Date
:   2 April 2026

Status
:   DRAFT

---

## Overview

A security audit identified that `pkg/config/config.go` calls `viper.AutomaticEnv()` with a dot-to-underscore replacer (`SetEnvKeyReplacer(strings.NewReplacer(".", "_"))`), which means **any** environment variable matching the config key pattern can silently override configuration values. In shared environments such as CI runners, containers, or multi-tenant hosts, this creates a config pollution risk: an unrelated process or user setting `AI_PROVIDER=malicious` would override the `ai.provider` config key in every GTB-based tool running in that environment.

The fix is to support an optional **environment variable prefix** that scopes env-based overrides. When a prefix such as `GTB` is configured, only environment variables beginning with `GTB_` are considered (e.g., `GTB_AI_PROVIDER` resolves to config key `ai.provider`). This leverages Viper's native `SetEnvPrefix()` method.

Backward compatibility is preserved: when no prefix is set, the current unprefixed behavior continues unchanged.

---

## Design Decisions

**Functional options pattern**: The `initContainer` function already serves as the single point of container initialisation. Adding a variadic `...ContainerOption` parameter to this function (and propagating it through the public constructors) aligns with Go idioms and the existing codebase style. This is preferred over adding a prefix field to the `Container` struct or requiring callers to call `SetEnvPrefix` on the Viper instance after construction.

**Prefix does not include the trailing underscore**: Viper's `SetEnvPrefix("GTB")` automatically adds `_` as the separator when resolving env vars. Consumers pass `"GTB"` not `"GTB_"`. This matches Viper's convention and avoids double-underscore bugs.

**Empty prefix means no prefix (backward compatible)**: If `WithEnvPrefix("")` is called or no option is provided, `SetEnvPrefix` is not called, preserving the existing behavior where all env vars are candidates.

**Interaction with `SetEnvKeyReplacer`**: Viper applies the prefix *before* the key replacer. For config key `ai.provider` with prefix `GTB`, Viper looks up `GTB_AI_PROVIDER` — the prefix is prepended, then dots are replaced with underscores. This is Viper's documented behavior and requires no custom logic.

**Propagation through `Props`**: The env prefix is a property of the tool being built, not of individual config files. It is set once at tool startup and applies to all config containers created via the standard constructors. Adding a `EnvPrefix` field to `props.Tool` is the natural home, since the prefix is derived from the tool name.

**Generator derives prefix from tool name**: The generator will upper-case the tool name and use it as the default env prefix (e.g., tool `myapp` gets prefix `MYAPP`). This is a sensible default that can be overridden by the user during the generation wizard.

---

## Public API Changes

### New Option Type in `pkg/config`

```go
// ContainerOption configures optional behavior for config containers.
type ContainerOption func(*containerOptions)

type containerOptions struct {
    envPrefix string
}

// WithEnvPrefix sets the environment variable prefix for automatic env binding.
// When set to "GTB", the config key "ai.provider" resolves from the
// environment variable "GTB_AI_PROVIDER". An empty string disables prefixing
// (the default, preserving backward compatibility).
func WithEnvPrefix(prefix string) ContainerOption {
    return func(o *containerOptions) {
        o.envPrefix = prefix
    }
}
```

### Options-Pattern Constructors (Breaking Change)

The existing constructors have been replaced with a clean options-pattern API. The `logger.Logger` parameter has been removed from constructor signatures — logging is now provided via the `WithLogger` option. All constructors accept `(fs afero.Fs, opts ...ContainerOption)`:

```go
// NewFilesContainer creates a container from config files with options.
func NewFilesContainer(fs afero.Fs, opts ...ContainerOption) *Container

// LoadFilesContainer loads a container from config files with options.
func LoadFilesContainer(fs afero.Fs, opts ...ContainerOption) (Containable, error)

// LoadFilesContainerWithSchema loads a container with schema validation and options.
func LoadFilesContainerWithSchema(fs afero.Fs, schema *Schema, opts ...ContainerOption) (Containable, error)

// NewReaderContainer creates a container from readers with options.
func NewReaderContainer(opts ...ContainerOption) *Container
```

Available options:

```go
WithLogger(l logger.Logger)         // Provide a logger (optional, defaults to noop)
WithEnvPrefix(prefix string)        // Set env var prefix for automatic env binding
WithConfigFiles(files ...string)    // Specify config file paths
WithConfigFormat(format string)     // Specify config format (for reader-based containers)
WithConfigReaders(readers ...io.Reader) // Provide config readers
WithSchema(schema *Schema)          // Provide a validation schema
```

The `Props.Tool.EnvPrefix` field threads the prefix through `pkg/cmd/root`, which passes `config.WithEnvPrefix(props.Tool.EnvPrefix)` to the config constructors when the prefix is non-empty.

This is a **breaking change** to the `pkg/config` constructor signatures. The API stability guarantee is being moved from v1.10.0 to v1.11.0 to accommodate this migration. See [Migration & Compatibility](#migration--compatibility) for details.

### New Field in `props.Tool`

```go
type Tool struct {
    // ... existing fields ...

    // EnvPrefix is the environment variable prefix used by the config package.
    // When set, only env vars starting with this prefix (e.g., "GTB_") are
    // considered for config overrides. Empty means no prefix (all env vars match).
    EnvPrefix string
}
```

### Changes to `pkg/cmd/root`

The `loadAndMergeConfig` function and related callers will pass `[]config.ContainerOption{config.WithEnvPrefix(props.Tool.EnvPrefix)}` to the config constructors when `props.Tool.EnvPrefix` is non-empty.

---

## Internal Implementation

### `pkg/config/config.go`

The `initContainer` function (unexported) is the single point where `AutomaticEnv` and `SetEnvKeyReplacer` are called. It accepts `(fs afero.Fs, opts ...ContainerOption)` and resolves all options internally:

```go
func initContainer(fs afero.Fs, opts ...ContainerOption) *Container {
    o := &containerOptions{}
    for _, opt := range opts {
        opt(o)
    }

    l := o.logger
    if l == nil {
        l = logger.NewNoop()
    }

    c := Container{
        ID:        "",
        viper:     viper.New(),
        logger:    l,
        observers: make([]Observable, 0),
    }

    c.viper.SetFs(fs)
    LoadEnv(fs, l)

    if o.envPrefix != "" {
        c.viper.SetEnvPrefix(o.envPrefix)
    }

    c.viper.AutomaticEnv()
    c.viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
    c.viper.SetTypeByDefaultValue(true)

    return &c
}
```

Note: `SetEnvPrefix` must be called **before** `AutomaticEnv()` for Viper to apply the prefix during automatic env resolution.

The public constructors delegate to `initContainer` with the clean options-pattern signature:

```go
func NewFilesContainer(fs afero.Fs, opts ...ContainerOption) *Container {
    c := initContainer(fs, opts...)
    // ... configure file paths from WithConfigFiles option ...
}

func NewReaderContainer(opts ...ContainerOption) *Container {
    c := initContainer(afero.NewMemMapFs(), opts...)
    // ... configure readers from WithConfigReaders option ...
}
```

### `pkg/cmd/root/root.go`

The `loadAndMergeConfig` function builds the options slice from `Props.Tool.EnvPrefix` and passes `config.WithEnvPrefix(props.Tool.EnvPrefix)` along with `config.WithLogger(...)` and `config.WithConfigFiles(...)` to the config constructors. When `Props.Tool.EnvPrefix` is empty, the `WithEnvPrefix` option is omitted (no-op).

### `pkg/config/load.go`

The `Load` and `LoadEmbed` functions use the same options-pattern signatures, propagating options to the underlying container constructors.

---

## Project Structure

| File | Action | Description |
|------|--------|-------------|
| `pkg/config/options.go` | **New** | `ContainerOption` type, `containerOptions` struct, `WithEnvPrefix` |
| `pkg/config/config.go` | Modify | `initContainer` accepts `(fs afero.Fs, opts ...ContainerOption)`; refactor all constructors to options-pattern signatures |
| `pkg/config/load.go` | Modify | Update `Load`, `LoadEmbed` to use options-pattern constructors |
| `pkg/config/options_test.go` | **New** | Unit tests for option application |
| `pkg/config/config_test.go` | Modify | Add tests for options-pattern constructors with env prefix |
| `pkg/config/load_test.go` | Modify | Add tests for options-pattern load variants |
| `pkg/props/props.go` | Modify | Add `EnvPrefix` field to `Tool` |
| `pkg/cmd/root/root.go` | Modify | Wire `Props.Tool.EnvPrefix` into config options |
| `pkg/cmd/root/root_test.go` | Modify | Update tests, add env prefix coverage |
| `internal/cmd/root/root.go` | Modify | Set `EnvPrefix: "GTB"` in `props.Tool` |
| `internal/generator/templates/skeleton_root.go` | Modify | Emit `EnvPrefix` in generated `Tool` struct |
| `docs/components/config.md` | Modify | Document env prefix behavior |

---

## Generator Impact

### `internal/generator/templates/skeleton_root.go`

The `SkeletonRootData` struct gains an `EnvPrefix` field. The generated `Tool` struct literal includes the prefix:

```go
type SkeletonRootData struct {
    // ... existing fields ...
    EnvPrefix string // e.g. "MYAPP"
}
```

The `buildToolDict` function emits the `EnvPrefix` field when non-empty:

```go
if data.EnvPrefix != "" {
    toolDict[jen.Id("EnvPrefix")] = jen.Lit(data.EnvPrefix)
}
```

### Generation Wizard

Environment variable prefix is an **opt-out feature** — enabled by default on `generate` and `regenerate`. The wizard flow is:

1. After tool name input, the wizard shows the env prefix step.
2. Default: **enabled**, with the prefix auto-derived from the tool name upper-cased (e.g., tool `my-app` defaults to `MY_APP`). Hyphens are replaced with underscores.
3. The user can override the derived prefix with a custom value (validated against `[A-Z0-9_]+`).
4. The user can explicitly **disable** the prefix by toggling the feature off, in which case `EnvPrefix` is left empty (unprefixed behaviour, matching pre-feature behaviour).
5. If enabled, the prefix is **required** — the form rejects empty input.

This ensures new tools get prefix protection by default while allowing opt-out for tools that intentionally need unprefixed env var resolution.

### Regeneration

The `regenerate` command must detect the existing `EnvPrefix` from the manifest or AST and preserve it, avoiding overwrite on regeneration. If the existing project has no prefix (pre-feature), regeneration should prompt the user to adopt one (opt-out).

---

## Error Handling

No new error types are introduced. The config package is intentionally permissive — Viper accepts any string prefix, and the config layer does not validate format. Prefix validation is the responsibility of the caller. The generator wizard enforces that the prefix matches `[A-Z0-9_]+` and rejects invalid input with a descriptive form validation error.

---

## Testing Strategy

### Unit Tests

| Test | File | Description |
|------|------|-------------|
| `TestWithEnvPrefix_Applied` | `pkg/config/options_test.go` | Verify option populates `containerOptions.envPrefix` |
| `TestInitContainer_WithPrefix` | `pkg/config/config_test.go` | Set prefix, set env var with prefix, confirm config resolves |
| `TestInitContainer_WithoutPrefix` | `pkg/config/config_test.go` | No prefix set, confirm all env vars still resolve (backward compat) |
| `TestInitContainer_PrefixWithDotKey` | `pkg/config/config_test.go` | Verify `GTB_AI_PROVIDER` resolves `ai.provider` with prefix `GTB` |
| `TestNewFilesContainer_WithPrefix` | `pkg/config/config_test.go` | End-to-end: constructor with file + env override with prefix |
| `TestNewReaderContainer_WithPrefix` | `pkg/config/config_test.go` | End-to-end: constructor with reader + env override with prefix |
| `TestLoadFilesContainer_WithPrefix` | `pkg/config/config_test.go` | End-to-end: load + env override with prefix |
| `TestEnvWithoutPrefix_DoesNotResolve` | `pkg/config/config_test.go` | With prefix `GTB`, bare `AI_PROVIDER` does not override `ai.provider` |

### Integration / E2E

| Test | Description |
|------|-------------|
| Gherkin: env prefix scenario | Given a built binary with prefix "GTB", when `GTB_LOG_LEVEL=debug` is set, then debug logging is active |
| Gherkin: unprefixed env ignored | Given a built binary with prefix "GTB", when `LOG_LEVEL=debug` is set (without prefix), then debug logging is NOT active |

### Generator Tests

| Test | Description |
|------|-------------|
| `TestSkeletonRoot_EnvPrefix` | Verify generated code includes `EnvPrefix` in `Tool` struct |
| `TestSkeletonRoot_EnvPrefix_Disabled` | Verify `EnvPrefix` is omitted when feature is opted out |
| `TestSkeletonRoot_EnvPrefix_Derived` | Verify prefix auto-derived from tool name (`my-app` → `MY_APP`) |
| `TestWizard_EnvPrefix_Validation` | Verify wizard rejects invalid prefix (spaces, lowercase, empty when enabled) |

---

## Migration & Compatibility

### Strategy: Clean Options-Pattern Migration (Breaking Change)

Rather than a three-tier deprecation approach, the implementation takes a clean break: constructor signatures are updated to the options pattern directly. The API stability guarantee is moved from v1.10.0 to v1.11.0 to permit this breaking change in the v1.10.x to v1.11.0 transition.

This means:
- **Breaking change** in this release — existing code using the old constructor signatures must be updated.
- **Cleaner API** — no deprecated constructors, no `*WithOptions` variants, no `SetEnvPrefix` method. One idiomatic API surface.
- **v1.11.0** marks the start of the guaranteed API stability period. From v1.11.0 onwards, breaking changes require a major version bump (v2.0.0+).

### Migration Guide

**Required migration (v1.10.x to v1.11.0):**

```go
// Before:
c := config.NewFilesContainer(logger, fs, "config.yaml")

// After:
c := config.NewFilesContainer(fs,
    config.WithLogger(logger),
    config.WithConfigFiles("config.yaml"),
    config.WithEnvPrefix("MYAPP"),
)

// Before:
c := config.NewReaderContainer(logger, "yaml", reader1, reader2)

// After:
c := config.NewReaderContainer(
    config.WithLogger(logger),
    config.WithConfigFormat("yaml"),
    config.WithConfigReaders(reader1, reader2),
    config.WithEnvPrefix("MYAPP"),
)
```

---

## Future Considerations

1. **Per-container prefix**: A future enhancement could allow different prefixes for different config containers (e.g., shared library config vs. application config). The `ContainerOption` pattern is designed to accommodate this.
2. **Env var allowlist**: Beyond prefixing, a future spec could add an explicit allowlist of env var names that are permitted to override config, for maximum security in sensitive environments.
3. **Config doctor check**: The `doctor` command could verify that env vars matching config keys (with and without prefix) are intentional, warning about potential pollution.

---

## Implementation Phases

### Phase 1: Core Prefix Support (pkg/config)

- Add `ContainerOption` type and `WithEnvPrefix` in `pkg/config/options.go`.
- Modify `initContainer` (unexported) to accept and apply `...ContainerOption`.
- Refactor all four public constructors to the options-pattern signature `(fs afero.Fs, opts ...ContainerOption)`.
- Add `EnvPrefix` field to `props.Tool`.
- Wire prefix in `pkg/cmd/root` from `Props.Tool.EnvPrefix` via `WithEnvPrefix` option.
- Unit tests for prefix resolution, backward compatibility, and dot-key interaction.

### Phase 2: GTB CLI Integration

- Set `EnvPrefix: "GTB"` in `internal/cmd/root/root.go`.
- Add E2E Gherkin scenarios for prefix behavior.
- Update `docs/components/config.md` with env prefix documentation.

### Phase 3: Generator Support

- Add `EnvPrefix` to `SkeletonRootData` and `buildToolDict`.
- Add wizard step for env prefix configuration (opt-out, enabled by default, auto-derived from tool name).
- Add `[A-Z0-9_]+` validation to the wizard form input.
- Update regeneration to detect and preserve existing prefix; prompt adoption for pre-feature projects.
- Generator unit tests.

---

## Resolved Decisions

1. **Clean options-pattern migration (breaking change):** Replace constructor signatures with a clean `(fs afero.Fs, opts ...ContainerOption)` pattern instead of the three-tier deprecation approach. This produces a simpler, more idiomatic API at the cost of a breaking change. The API stability guarantee is moved from v1.10.0 to v1.11.0 to accommodate this migration. No deprecated constructors, no `*WithOptions` variants, no `SetEnvPrefix` method.

2. **Prefix validation at the caller (Option C):** The config package is permissive — it accepts any string prefix. Validation (`[A-Z0-9_]+`) is enforced by the generator wizard at input time. This keeps the config package simple and avoids coupling it to format rules.

3. **Opt-out feature, enabled by default:** The env prefix feature is enabled by default on `generate` and `regenerate`. The prefix is auto-derived from the tool name (upper-cased, hyphens to underscores). Users can override the value or explicitly disable the feature. When enabled, a non-empty prefix is required.
