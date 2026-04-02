---
title: "Config Environment Variable Prefix"
description: "Add configurable environment variable prefix to pkg/config to prevent config pollution in shared environments."
date: 2026-04-02
status: DRAFT
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

### Modified Constructor Signatures

The following public functions gain a variadic `...ContainerOption` parameter:

```go
// NewFilesContainer — existing signature + options
func NewFilesContainer(l logger.Logger, fs afero.Fs, configFiles ...string) *Container
// becomes:
func NewFilesContainer(l logger.Logger, fs afero.Fs, opts []ContainerOption, configFiles ...string) *Container

// LoadFilesContainer — existing signature + options
func LoadFilesContainer(l logger.Logger, fs afero.Fs, configFiles ...string) (Containable, error)
// becomes:
func LoadFilesContainer(l logger.Logger, fs afero.Fs, opts []ContainerOption, configFiles ...string) (Containable, error)

// LoadFilesContainerWithSchema — existing signature + options
func LoadFilesContainerWithSchema(l logger.Logger, fs afero.Fs, schema *Schema, opts []ContainerOption, configFiles ...string) (Containable, error)

// NewReaderContainer — existing signature + options
func NewReaderContainer(l logger.Logger, format string, configReaders ...io.Reader) *Container
// becomes:
func NewReaderContainer(l logger.Logger, format string, opts []ContainerOption, configReaders ...io.Reader) *Container
```

**API stability note**: These are **breaking changes** to four public functions in `pkg/config/`. The `configFiles` and `configReaders` variadic parameters make it impossible to add `...ContainerOption` without changing the signature. The `opts []ContainerOption` slice parameter is inserted before the variadic to maintain clarity.

Since `pkg/config` is a Stable-tier API under the v1.10.0+ stability guarantee, this change requires:

1. A `BREAKING CHANGE:` footer in the commit to trigger a major version bump consideration.
2. Alternatively, a **non-breaking approach** using a separate constructor set (see Open Questions).

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

The `initContainer` function is the single point where `AutomaticEnv` and `SetEnvKeyReplacer` are called. The change is minimal:

```go
func initContainer(l logger.Logger, fs afero.Fs, opts ...ContainerOption) *Container {
    o := &containerOptions{}
    for _, opt := range opts {
        opt(o)
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

### `pkg/cmd/root/root.go`

The `loadAndMergeConfig` function builds the options slice from `Props.Tool.EnvPrefix` and passes it through to `config.Load`, `config.NewReaderContainer`, and `config.LoadEmbed`.

### `pkg/config/load.go`

The `Load` and `LoadEmbed` functions propagate the options to the underlying container constructors. Their signatures gain an `opts []ContainerOption` parameter.

---

## Project Structure

| File | Action | Description |
|------|--------|-------------|
| `pkg/config/options.go` | **New** | `ContainerOption` type, `containerOptions` struct, `WithEnvPrefix` |
| `pkg/config/config.go` | Modify | `initContainer` accepts `...ContainerOption`; all constructors updated |
| `pkg/config/load.go` | Modify | `Load`, `LoadEmbed` accept and propagate options |
| `pkg/config/container.go` | No change | No changes needed |
| `pkg/config/options_test.go` | **New** | Unit tests for option application |
| `pkg/config/config_test.go` | Modify | Update constructor calls with `nil` options |
| `pkg/config/load_test.go` | Modify | Update constructor calls with `nil` options |
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

The interactive wizard gains a step (after tool name input) where the user can confirm or override the default env prefix. The default is the tool name upper-cased (e.g., tool `my-app` defaults to `MY_APP`). Hyphens are replaced with underscores to produce valid env var prefixes.

### Regeneration

The `regenerate` command must detect the existing `EnvPrefix` from the manifest or AST and preserve it, avoiding overwrite on regeneration.

---

## Error Handling

No new error types are introduced. Invalid prefix values (e.g., containing spaces or lowercase) are not validated by the config package itself — Viper accepts any string prefix. The generator wizard validates that the prefix contains only `[A-Z0-9_]` characters and provides a warning if the user enters an unusual value.

---

## Testing Strategy

### Unit Tests

| Test | File | Description |
|------|------|-------------|
| `TestWithEnvPrefix_Applied` | `pkg/config/options_test.go` | Verify option populates `containerOptions.envPrefix` |
| `TestInitContainer_WithPrefix` | `pkg/config/config_test.go` | Set prefix, set env var with prefix, confirm config resolves |
| `TestInitContainer_WithoutPrefix` | `pkg/config/config_test.go` | No prefix set, confirm all env vars still resolve (backward compat) |
| `TestInitContainer_PrefixWithDotKey` | `pkg/config/config_test.go` | Verify `GTB_AI_PROVIDER` resolves `ai.provider` with prefix `GTB` |
| `TestNewFilesContainer_WithPrefix` | `pkg/config/config_test.go` | End-to-end: file + env override with prefix |
| `TestNewReaderContainer_WithPrefix` | `pkg/config/config_test.go` | End-to-end: reader + env override with prefix |
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
| `TestSkeletonRoot_EnvPrefix_Empty` | Verify `EnvPrefix` is omitted when empty |

---

## Migration & Compatibility

### Breaking Change Assessment

The constructor signature changes are breaking for any downstream consumer calling `NewFilesContainer`, `LoadFilesContainer`, `LoadFilesContainerWithSchema`, or `NewReaderContainer`. All callers must add a `nil` (or populated) options slice parameter.

**Mitigation options** (to be resolved before implementation):

1. **Accept the break**: Bump to v2.0.0. This is heavy-handed for a single parameter addition.
2. **New constructors**: Keep existing signatures unchanged; add `NewFilesContainerWithOptions`, `LoadFilesContainerWithOptions`, etc. This avoids any breaking change but adds API surface.
3. **Options on the container after creation**: Add a `Container.SetEnvPrefix(prefix string)` method that must be called before first use. This avoids signature changes entirely but has a temporal coupling issue — if called after `AutomaticEnv`, Viper may not apply the prefix correctly.
4. **Wrapper approach**: Add the options parameter only to `initContainer` (unexported) and thread the prefix through `Props.Tool.EnvPrefix` at the `pkg/cmd/root` layer. The public constructors remain unchanged; only consumers using `pkg/cmd/root` (which is the expected integration point) get the prefix automatically. Direct constructor callers who need a prefix use `GetViper().SetEnvPrefix()` after construction. This is the least disruptive approach.

**Recommended approach**: Option 4 (wrapper approach). The prefix is applied at the `pkg/cmd/root` level via `Props.Tool.EnvPrefix`. The `initContainer` function (unexported) accepts options internally. Public constructor signatures remain unchanged. Consumers who create containers directly and need a prefix can call `container.GetViper().SetEnvPrefix("PREFIX")` — this works because `AutomaticEnv` in Viper respects prefix changes made after the call. This preserves full backward compatibility with zero breaking changes.

If the recommended approach is adopted, the `ContainerOption` type and `WithEnvPrefix` function are still added to the public API (as new additions, not modifications), providing a clean path for a future minor release that adds options to the constructors.

### Migration Guide

For the recommended (non-breaking) approach, no migration is required. Existing code continues to work unchanged. Tools that want env prefix support add `EnvPrefix` to their `props.Tool` struct — a purely additive change.

---

## Future Considerations

1. **Per-container prefix**: A future enhancement could allow different prefixes for different config containers (e.g., shared library config vs. application config). The `ContainerOption` pattern is designed to accommodate this.
2. **Env var allowlist**: Beyond prefixing, a future spec could add an explicit allowlist of env var names that are permitted to override config, for maximum security in sensitive environments.
3. **Constructor signature migration**: In a future major version (v2.0.0), the constructors could be updated to accept `...ContainerOption` natively, retiring the current variadic-file-path signatures in favor of an options-based API.
4. **Config doctor check**: The `doctor` command could verify that env vars matching config keys (with and without prefix) are intentional, warning about potential pollution.

---

## Implementation Phases

### Phase 1: Core Prefix Support (pkg/config)

- Add `ContainerOption` type and `WithEnvPrefix` in `pkg/config/options.go`.
- Modify `initContainer` to accept and apply options.
- Add `EnvPrefix` field to `props.Tool`.
- Wire prefix in `pkg/cmd/root` from `Props.Tool.EnvPrefix`.
- Unit tests for prefix resolution, backward compatibility, and dot-key interaction.

### Phase 2: GTB CLI Integration

- Set `EnvPrefix: "GTB"` in `internal/cmd/root/root.go`.
- Add E2E Gherkin scenarios for prefix behavior.
- Update `docs/components/config.md` with env prefix documentation.

### Phase 3: Generator Support

- Add `EnvPrefix` to `SkeletonRootData` and `buildToolDict`.
- Add wizard step for env prefix configuration.
- Update regeneration to preserve existing prefix.
- Generator unit tests.

---

## Open Questions

1. **Breaking vs. non-breaking**: Should we go with the recommended non-breaking wrapper approach (option 4), or accept a breaking change to the constructor signatures? The wrapper approach is less pure but avoids a major version bump.
2. **Prefix validation**: Should the config package validate that the prefix contains only valid env var characters (`[A-Z0-9_]`), or leave that to the caller / generator wizard?
3. **Default prefix for new tools**: Should the generator default to an upper-cased tool name as the prefix, or should no prefix be the default (matching current behavior) with prefix as an opt-in wizard question?
