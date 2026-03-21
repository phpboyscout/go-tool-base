---
title: "Props Interface Narrowing Specification"
description: "Add narrow role-based interfaces intersecting the Props god object so consumers can declare minimal dependencies."
date: 2026-03-21
status: IMPLEMENTED
tags:
  - specification
  - props
  - interfaces
  - refactor
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-opus-4-6)
    role: AI drafting assistant
---

# Props Interface Narrowing Specification

Authors
:   Matt Cockayne, Claude (claude-opus-4-6) *(AI drafting assistant)*

Date
:   21 March 2026

Status
:   DRAFT

---

## Overview

`Props` is an intentional god object — a single struct carrying all cross-cutting dependencies (Logger, Config, Assets, FS, Version, ErrorHandler, Tool). This design is deliberate and will not change. However, consumers currently accept `*props.Props` even when they only need one or two of its fields, which:

1. Makes it impossible to tell from a function signature which dependencies are actually used
2. Makes unit testing harder — you must construct a full `Props` even if you only need a logger
3. Prevents compile-time enforcement of minimal dependency contracts

The solution is to add narrow interfaces that `*Props` satisfies, allowing consumers to gradually adopt them while the god object remains intact.

---

## Design Decisions

**Getter methods on Props**: Props currently uses public fields (`Props.Logger`, `Props.Config`, etc.). To satisfy interfaces, we add getter methods (`GetLogger()`, `GetConfig()`, etc.). The public fields remain for backwards compatibility.

**Interface-per-role, not interface-per-field**: Each interface groups related getters that commonly appear together, rather than one interface per field. This keeps the interface count manageable.

**Compound interfaces for common combinations**: Some consumers need two or three capabilities together. Pre-defined compound interfaces prevent consumers from having to declare their own multi-interface parameters.

**Incremental migration**: This spec only adds the interfaces and getter methods. It does not mandate migrating all consumers — that happens organically as files are touched for other reasons.

---

## Public API Changes

### New Getter Methods on `Props`

```go
func (p *Props) GetLogger() *slog.Logger        { return p.Logger }
func (p *Props) GetConfig() config.Containable   { return p.Config }
func (p *Props) GetAssets() Assetter             { return p.Assets }
func (p *Props) GetFS() afero.Fs                 { return p.FS }
func (p *Props) GetVersion() version.Version     { return p.Version }
func (p *Props) GetErrorHandler() errorhandling.Handler { return p.ErrorHandler }
func (p *Props) GetTool() Tool                   { return p.Tool }
```

### New Narrow Interfaces

```go
// LoggerProvider provides access to the application logger.
type LoggerProvider interface {
    GetLogger() *slog.Logger
}

// ConfigProvider provides access to the application configuration.
type ConfigProvider interface {
    GetConfig() config.Containable
}

// FileSystemProvider provides access to the application filesystem.
type FileSystemProvider interface {
    GetFS() afero.Fs
}

// AssetProvider provides access to embedded assets.
type AssetProvider interface {
    GetAssets() Assetter
}

// VersionProvider provides access to version information.
type VersionProvider interface {
    GetVersion() version.Version
}

// ErrorHandlerProvider provides access to the error handler.
type ErrorHandlerProvider interface {
    GetErrorHandler() errorhandling.Handler
}

// ToolMetadataProvider provides access to tool configuration and metadata.
type ToolMetadataProvider interface {
    GetTool() Tool
}
```

### Compound Interfaces

```go
// LoggingConfigProvider is the most common combination — logging and configuration.
type LoggingConfigProvider interface {
    LoggerProvider
    ConfigProvider
}

// CoreProvider provides the three most commonly needed capabilities.
type CoreProvider interface {
    LoggerProvider
    ConfigProvider
    FileSystemProvider
}
```

---

## Internal Implementation

### Compile-Time Satisfaction Checks

Add to `props.go`:

```go
var (
    _ LoggerProvider       = (*Props)(nil)
    _ ConfigProvider       = (*Props)(nil)
    _ FileSystemProvider   = (*Props)(nil)
    _ AssetProvider        = (*Props)(nil)
    _ VersionProvider      = (*Props)(nil)
    _ ErrorHandlerProvider = (*Props)(nil)
    _ ToolMetadataProvider = (*Props)(nil)
    _ LoggingConfigProvider = (*Props)(nil)
    _ CoreProvider         = (*Props)(nil)
)
```

### Example Consumer Migration

Before:

```go
func generatePackageDocs(p *props.Props, pkg Package) error {
    p.Logger.Info("generating docs", "package", pkg.Name)
    cfg := p.Config.GetString("docs.output_dir")
    // ...
}
```

After:

```go
func generatePackageDocs(p props.LoggingConfigProvider, pkg Package) error {
    p.GetLogger().Info("generating docs", "package", pkg.Name)
    cfg := p.GetConfig().GetString("docs.output_dir")
    // ...
}
```

This is an example only — consumer migration is not required in this spec.

---

## Project Structure

```
pkg/props/
├── props.go           ← MODIFIED: getter methods, compile-time checks
├── interfaces.go      ← NEW: narrow interface definitions
├── interfaces_test.go ← NEW: interface satisfaction tests
```

---

## Testing Strategy

| Test | Scenario |
|------|----------|
| `TestProps_SatisfiesLoggerProvider` | Compile-time: `var _ LoggerProvider = (*Props)(nil)` |
| `TestProps_SatisfiesConfigProvider` | Compile-time check |
| `TestProps_SatisfiesFileSystemProvider` | Compile-time check |
| `TestProps_SatisfiesAssetProvider` | Compile-time check |
| `TestProps_SatisfiesVersionProvider` | Compile-time check |
| `TestProps_SatisfiesErrorHandlerProvider` | Compile-time check |
| `TestProps_SatisfiesToolMetadataProvider` | Compile-time check |
| `TestProps_SatisfiesLoggingConfigProvider` | Compound interface check |
| `TestProps_SatisfiesCoreProvider` | Compound interface check |
| `TestGetLogger_ReturnsField` | `GetLogger()` returns same pointer as `Props.Logger` |
| `TestGetConfig_ReturnsField` | `GetConfig()` returns same instance as `Props.Config` |
| `TestGetFS_ReturnsField` | `GetFS()` returns same instance as `Props.FS` |

### Coverage
- Target: 100% for getter methods (trivial, but must be covered).
- Target: 90%+ for `pkg/props/`.

---

## Linting

- `golangci-lint run --fix` must pass.
- No new `nolint` directives.
- New interfaces should satisfy `interfacebloat` limits (all under 5 methods).

---

## Documentation

- Godoc for each narrow interface explaining when to use it.
- Godoc for compound interfaces explaining the combination rationale.
- Godoc for getter methods (minimal — they're self-explanatory).
- Add guidance to `Props` struct godoc: "When writing functions that accept Props, consider whether a narrow interface (LoggerProvider, ConfigProvider, etc.) would suffice."
- Update `docs/components/props.md` with interface hierarchy and migration guidance.

---

## Backwards Compatibility

- **No breaking changes**. Public fields remain accessible. Getter methods are purely additive.
- Existing code using `*props.Props` continues to work — `*Props` satisfies all new interfaces.
- Consumer migration is optional and incremental.

---

## Future Considerations

- **Automatic migration tool**: A `go fix`-style tool could identify functions that only use one or two `Props` fields and suggest narrowing the parameter type.
- **Mock generation**: Once consumers use narrow interfaces, mocks become simpler — mock only the interface needed, not all of Props.
- **Additional compound interfaces**: As patterns emerge in consumer code, new compound interfaces can be added without breaking changes.

---

## Implementation Phases

### Phase 1 — Getter Methods
1. Add getter methods to `Props` struct
2. Verify all return correct values

### Phase 2 — Interface Definitions
1. Create `interfaces.go` with all narrow and compound interfaces
2. Add compile-time satisfaction checks
3. Add godoc

### Phase 3 — Tests & Documentation
1. Add interface satisfaction tests
2. Add getter return value tests
3. Update documentation

---

## Verification

```bash
go build ./...
go test -race ./pkg/props/...
go test ./...
golangci-lint run --fix

# Verify all interfaces are satisfied
grep -c 'var _' pkg/props/props.go  # count compile-time checks

# Verify getter methods exist
grep -n 'func (p \*Props) Get' pkg/props/props.go
```
