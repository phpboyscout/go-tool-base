---
title: "Config Schema Validation Specification"
description: "Add a decentralised validation layer to pkg/config that checks configuration values against a schema, catching typos, missing required fields, and enum violations before they cause runtime errors."
date: 2026-03-26
status: IMPLEMENTED
tags:
  - specification
  - config
  - validation
  - feature
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-opus-4-6)
    role: AI drafting assistant
---

# Config Schema Validation Specification

Authors
:   Matt Cockayne, Claude (claude-opus-4-6) *(AI drafting assistant)*

Date
:   26 March 2026

Status
:   IMPLEMENTED

---

## Overview

`pkg/config` supports hierarchical merging from multiple sources (files, embedded assets, environment variables, CLI flags) but performs no structural validation. A typo in a config key (e.g., `github.tokne` instead of `github.token`) silently produces an empty value, discovered only at runtime when an API call fails. Missing required fields and unrecognised keys go undetected.

This specification adds a **decentralised** schema validation layer designed to work with GTB's existing config architecture. Validation is performed per-package at the point of consumption, not as a global schema applied to the entire config tree. This aligns with the existing patterns where each feature package owns its config defaults (via embedded assets) and its initialiser logic.

---

## Design Decisions

**Decentralised, per-package validation**: GTB's config system is intentionally modular — each feature package registers its own embedded defaults via `Props.Assets` and manages its config slice via initialisers. A centralised schema would fight this architecture. Instead, each package defines a struct describing the config keys it consumes and validates its own slice.

**No default injection**: Default values are the responsibility of embedded assets (`assets/init/config.yaml`) and the merge hierarchy (embedded → user file → env → flags). The `default` struct tag is retained for documentation and error hint purposes but the validation layer does not mutate config values. Adding a second source of defaults would create divergence between the struct tag and the embedded YAML.

**No JSON Schema input**: JSON Schema as an input mechanism was considered and rejected. The decentralised nature of GTB's config means there is no single document that describes the full config shape — it depends on which feature packages are active. JSON Schema has future value as a *generated output* (struct tags → JSON Schema for IDE completion or CI validation) but not as a validation input.

**Validation on the concrete `Container` type, not the `Containable` interface**: Adding `Validate` to the interface would be a breaking change (all implementations and mocks would need updating). Since validation is an opt-in feature used by package authors, placing it on `*Container` is sufficient. Test code uses `Containable` for mocking; validation runs against the real container.

**Struct tags as the schema source**: Go struct tags (`config`, `validate`, `enum`) are the natural fit for per-package validation. The package author defines a struct matching the config keys they consume, and the schema is derived automatically.

**Warnings vs errors**: Unknown keys produce warnings (logged, not fatal) to support forward-compatible config files where multiple packages contribute keys. Missing required fields and enum violations produce errors. Strict mode upgrades unknown-key warnings to errors for packages that need tighter control.

---

## Public API Changes

### New Types in `pkg/config`

```go
// Schema defines the expected structure and constraints for configuration values.
type Schema struct {
    fields   map[string]FieldSchema
    strict   bool
}

// FieldSchema describes a single configuration field.
type FieldSchema struct {
    // Type is the expected Go type: "string", "int", "float64", "bool", "duration".
    Type        string
    // Required indicates the field must be present and non-zero.
    Required    bool
    // Description is used in validation error messages.
    Description string
    // Default is the default value for documentation and hints only.
    // The validation layer does not inject defaults — use embedded assets for that.
    Default     any
    // Enum restricts the field to a set of allowed values.
    Enum        []any
    // Children defines nested fields for map/object types.
    Children    map[string]FieldSchema
}

// ValidationError contains details about a single validation failure.
type ValidationError struct {
    Key     string // dot-separated config key
    Message string // human-readable description
    Hint    string // actionable fix suggestion
}

// ValidationResult holds the outcome of schema validation.
type ValidationResult struct {
    Errors   []ValidationError
    Warnings []ValidationError
}

// Valid returns true if no errors were found. Warnings do not affect validity.
func (r *ValidationResult) Valid() bool

// Error returns a formatted multi-line error string, or empty string if valid.
func (r *ValidationResult) Error() string
```

### Schema Construction

```go
// SchemaOption configures schema validation behaviour.
type SchemaOption func(*schemaConfig)

// WithStrictMode treats unknown keys as errors instead of warnings.
func WithStrictMode() SchemaOption

// WithStructSchema derives a schema from a tagged Go struct.
// Supported tags: `config:"key" validate:"required" enum:"a,b,c" default:"value"`.
func WithStructSchema(v any) SchemaOption

// NewSchema creates a Schema from the provided options.
func NewSchema(opts ...SchemaOption) (*Schema, error)
```

### Validation on Container

```go
// On *Container (not on Containable interface):

// Validate checks the current configuration against the provided schema.
// Returns a ValidationResult; callers should check result.Valid().
func (c *Container) Validate(schema *Schema) *ValidationResult

// SetSchema attaches a validation schema to the container for hot-reload gating.
func (c *Container) SetSchema(schema *Schema)
```

### Integration with Load Functions

```go
// LoadFilesContainerWithSchema loads config files and validates against the schema.
// Returns an error wrapping all validation errors if the config is invalid.
func LoadFilesContainerWithSchema(l logger.Logger, fs afero.Fs, schema *Schema, configFiles ...string) (Containable, error)
```

### Usage Example — Per-Package Validation

```go
// pkg/myfeature/config.go — each package validates its own config slice

type MyFeatureConfig struct {
    APIKey   string `config:"myfeature.api_key" validate:"required"`
    Endpoint string `config:"myfeature.endpoint" validate:"required"`
    LogLevel string `config:"myfeature.log_level" enum:"debug,info,warn,error" default:"info"`
}

func ValidateConfig(cfg *config.Container) error {
    schema, err := config.NewSchema(config.WithStructSchema(MyFeatureConfig{}))
    if err != nil {
        return err
    }

    result := cfg.Validate(schema)
    if !result.Valid() {
        return errors.New(result.Error())
    }

    return nil
}
```

### Usage Example — Load-Time Validation

```go
// For CLI tools that load config and want upfront validation:

schema, err := config.NewSchema(config.WithStructSchema(AppConfig{}))
if err != nil {
    return err
}

cfg, err := config.LoadFilesContainerWithSchema(logger, fs, schema, "config.yaml")
if err != nil {
    // err contains actionable messages:
    // "config validation failed:
    //   myfeature.api_key: required field is missing (hint: ... set the MYFEATURE_API_KEY environment variable)"
    return err
}
```

---

## Design Rationale: Why Not Centralised Schema Validation?

GTB's config architecture is intentionally decentralised:

1. **Defaults live in embedded assets**, not in a schema. Each feature package ships its own `assets/init/config.yaml` which is merged by `Props.Assets` during startup. A centralised schema adding default injection would create two sources of truth.

2. **Initialisers handle setup**, including `IsConfigured()` checks. A centralised "required" check would duplicate this logic and couldn't account for feature flags — `github.token` is only required if the GitHub feature is enabled.

3. **The config shape is dynamic**. It depends on which feature packages are registered via `init()`. No single schema document can describe all valid configurations without knowing the feature set at compile time.

4. **JSON Schema as input doesn't fit**. A single JSON Schema document assumes a fixed config shape. With multiple packages each contributing their own keys, a centralised schema becomes either incomplete or overly permissive. JSON Schema has value as a *generated output* for IDE/CI tooling — this is documented as a future consideration.

The per-package `Validate()` pattern gives each package control over its own config contract without coupling to other packages' config keys.

---

## Internal Implementation

### Schema Compilation

`WithStructSchema` compiles to the internal `Schema` struct. Struct tag parsing uses `reflect` to walk the struct and extract `config`, `validate`, `enum`, and `default` tags. Nested structs without a `config` tag are recursed into with the lowercased field name as the key prefix.

### Validation Engine

```go
func (c *Container) Validate(schema *Schema) *ValidationResult {
    result := &ValidationResult{}

    for key, field := range schema.fields {
        value := c.viper.Get(key)
        validateField(key, field, value, result)
    }

    detectUnknownKeys(c.viper.AllKeys(), schema.fields, result, schema.strict)

    return result
}
```

Checks performed:
- **Required**: field must be present and non-zero
- **Enum**: value must be in the allowed set
- **Unknown keys**: keys present in config but absent from schema produce warnings (or errors in strict mode)

### Hot-Reload Integration

The existing `watchConfig` method in `Container` is extended: if a `Schema` has been set on the container via `SetSchema`, validation runs on the updated config before observers are notified. If validation fails, the error is logged and the reload is rejected — observers are not called.

```go
func (c *Container) watchConfig() {
    c.viper.OnConfigChange(func(e fsnotify.Event) {
        if c.schema != nil {
            result := c.Validate(c.schema)
            if !result.Valid() {
                c.logger.Error("config reload rejected: validation failed", "errors", result.Error())
                return // do not notify observers
            }
        }
        // ... existing observer notification ...
    })
    c.viper.WatchConfig()
}
```

---

## Project Structure

```
pkg/config/
├── config.go          ← MODIFIED: add LoadFilesContainerWithSchema
├── container.go       ← MODIFIED: add schema field, Validate method, SetSchema, watchConfig gate
├── observer.go        ← UNCHANGED
├── schema.go          ← NEW: Schema, FieldSchema, SchemaOption, NewSchema, struct tag parsing
├── validate.go        ← NEW: validation engine, ValidationResult, ValidationError
├── validate_test.go   ← NEW: validation tests
├── schema_test.go     ← NEW: schema construction tests
```

---

## Testing Strategy

| Test | Scenario |
|------|----------|
| `TestValidate_RequiredFieldPresent` | Required field exists and is non-zero → no error |
| `TestValidate_RequiredFieldMissing` | Required field absent → error with hint |
| `TestValidate_RequiredFieldEmpty` | Required string field is empty string → error |
| `TestValidate_EnumValid` | Value is in allowed set → no error |
| `TestValidate_EnumInvalid` | Value not in allowed set → error listing allowed values |
| `TestValidate_UnknownKey_Warning` | Key not in schema (non-strict) → warning only |
| `TestValidate_UnknownKey_Strict` | Key not in schema (strict mode) → error |
| `TestValidate_NestedFields` | Nested config objects validated recursively |
| `TestWithStructSchema_Tags` | Struct tags correctly parsed into Schema |
| `TestWithStructSchema_NestedStruct` | Nested structs with prefix derivation |
| `TestWithStructSchema_SkipDash` | Fields tagged `config:"-"` excluded |
| `TestNewSchema_StrictMode` | Strict flag propagates |
| `TestNewSchema_NoFields` | Empty schema produces error |
| `TestLoadFilesContainerWithSchema_Valid` | End-to-end load + validate |
| `TestLoadFilesContainerWithSchema_Invalid` | Missing required field → error |
| `TestLoadFilesContainerWithSchema_FileNotFound` | Missing file → nil, nil |
| `TestValidationResult_Error` | Multi-error formatting matches expected output |
| `TestValidationResult_ValidEmpty` | Empty result → valid |

### Coverage

- Target: 90%+ for all new files in `pkg/config/`.

---

## Linting

- `golangci-lint run --fix` must pass.
- No new `nolint` directives.

---

## Documentation

- Godoc for all new public types and functions.
- Update `docs/components/config.md` with schema validation usage and struct tag reference.
- Add `docs/how-to/validate-component-config.md` showing per-package config definition AND validation pattern.

---

## Backwards Compatibility

- **No breaking changes**. The `Containable` interface is unchanged. `Validate` and `SetSchema` are methods on `*Container` only.
- `LoadFilesContainer` and `Load` remain unchanged. Schema validation is opt-in via `LoadFilesContainerWithSchema` or explicit `Validate()` calls.
- Hot-reload validation only activates when a schema is attached to the container.

---

## Future Considerations

- **JSON Schema generation**: A `gtb config schema` command that generates a JSON Schema from struct tags for distribution alongside the tool. This enables IDE autocompletion and CI validation against the generated schema.
- **Deprecation warnings**: Mark config keys as deprecated with a migration hint, easing version upgrades.
- **Cross-field validation**: Rules like "if provider is gitlab, then gitlab.token is required" using conditional schema logic.
- **Composable schemas**: Merge multiple per-package schemas into a combined schema for tools that want whole-config validation at startup.

---

## Implementation Phases

### Phase 1 — Schema Definition
1. Define `Schema`, `FieldSchema`, `SchemaOption` types
2. Implement `NewSchema` with functional options
3. Implement `WithStructSchema` (struct tag parsing)
4. Add unit tests for schema construction

### Phase 2 — Validation Engine
1. Implement `ValidationResult`, `ValidationError`
2. Implement `Container.Validate()` with required and enum checks
3. Implement unknown-key detection (strict and non-strict modes)
4. Add unit tests for all validation paths

### Phase 3 — Load Integration
1. Add `LoadFilesContainerWithSchema`
2. Integrate validation into hot-reload (`watchConfig`)
3. Add integration tests for load + validate flow

---

## Verification

```bash
go build ./...
go test -race ./pkg/config/...
go test ./...
golangci-lint run --fix

# Verify new types exist
grep -n 'type Schema struct' pkg/config/schema.go
grep -n 'func.*Validate' pkg/config/validate.go
grep -n 'LoadFilesContainerWithSchema' pkg/config/config.go
```
