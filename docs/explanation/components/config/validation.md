---
title: Schema Validation
description: Validate configuration against a schema, including generic struct-tag validation.
date: 2026-02-16
tags: [components, config, configuration, viper]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Schema Validation

## Advanced Features

### Schema Validation

The `Container` supports **decentralised, per-package** schema validation using struct tags. Each package defines a struct describing the config keys it consumes and validates its own slice of the config — there is no centralised schema for the entire config tree.

This design aligns with GTB's config architecture where defaults live in embedded assets and each feature package owns its config independently.

!!! note "Defaults vs Validation"
    Default values belong in your package's embedded `assets/init/config.yaml`, not in struct tags. The `default` tag is retained for documentation and error hints only — the validation layer does not inject defaults.

**Struct tag reference:**

| Tag | Purpose | Example |
|-----|---------|---------|
| `config:"key"` | Maps field to config key | `config:"github.token"` |
| `validate:"required"` | Field must be present and non-zero | `validate:"required"` |
| `enum:"a,b,c"` | Restricts to allowed values | `enum:"debug,info,warn,error"` |
| `default:"value"` | Documentation only (used in hints) | `default:"info"` |

**Per-package validation (recommended pattern):**

Each package defines a config struct and validates the keys it owns with the
generic `ValidateStruct[T]` helper, which derives the schema from the struct's
tags and runs it against the container:

```go
// pkg/myfeature/config.go
type Config struct {
    APIKey   string `config:"myfeature.api_key" validate:"required"`
    Endpoint string `config:"myfeature.endpoint" validate:"required"`
    LogLevel string `config:"myfeature.log_level" enum:"debug,info,warn,error" default:"info"`
}

func ValidateConfig(cfg config.Containable) error {
    return config.ValidateStruct[Config](cfg)
}
```

`ValidateStruct[T]` takes the `Containable` interface, so there is no need to
type-assert `Props.Config` down to the concrete `*Container`. Schema options pass
straight through, e.g. `config.ValidateStruct[Config](cfg, config.WithStrictMode())`.
When you need the `*Schema` itself (for hot-reload gating via `SetSchema`, say),
build it with `config.SchemaOf[Config]()`, which caches the schema per type.

**Load-time validation (for CLI tools):**

```go
schema, err := config.NewSchema(config.WithStructSchema(AppConfig{}))
if err != nil {
    return err
}

cfg, err := config.LoadFilesContainerWithSchema(fs, schema,
    config.WithLogger(logger.ToSlog(l)),
    config.WithConfigFiles("config.yaml"),
)
if err != nil {
    // "config validation failed:
    //   myfeature.api_key: required field is missing (hint: ... set the MYFEATURE_API_KEY environment variable)"
    return err
}
```

**Validation on an existing container:**

```go
container := config.NewFilesContainer(fs,
    config.WithLogger(logger.ToSlog(l)),
    config.WithConfigFiles("config.yaml"),
)
result := container.Validate(schema)
if !result.Valid() {
    fmt.Println(result.Error())
}
// Warnings (unknown keys) are available via result.Warnings
```

**Schema options:**

| Option | Description |
|--------|-------------|
| `WithStructSchema(v any)` | Derive schema from struct tags |
| `WithStrictMode()` | Treat unknown keys as errors (default: warnings) |

**Generic helpers:**

| Function | Description |
|----------|-------------|
| `SchemaOf[T](opts ...SchemaOption)` | Build a schema from `T`'s struct tags; caches the option-free result per type |
| `ValidateStruct[T](cfg Containable, opts ...SchemaOption)` | Validate `cfg` against `T`'s schema without a manual `*Container` cast |

**Hot-reload integration:** Attach a schema to a container via `container.SetSchema(schema)`. When config files change, validation runs before notifying observers. Invalid reloads are rejected and logged.

For a complete walkthrough of defining config defaults AND validation for a new component, see the [Validate Component Config](../../../how-to/validate-component-config.md) how-to guide.
