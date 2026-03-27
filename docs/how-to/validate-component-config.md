---
title: Define and Validate Config for a Component
description: How to define config defaults via embedded assets and validate them at runtime using per-package schema validation.
date: 2026-03-27
tags: [how-to, config, validation, schema, components]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Define and Validate Config for a Component

When building a new feature package for a GTB-based tool, you need to handle two concerns:

1. **Config defaults** — what values should exist if the user doesn't provide them
2. **Config validation** — catching typos, missing required fields, and invalid values at startup

GTB separates these responsibilities deliberately. Defaults live in embedded assets. Validation lives in struct tags. This guide shows how to wire both.

---

## How It Fits Together

```
Embedded assets (defaults)     User config file        Environment variables
        ↓                           ↓                         ↓
    Props.Assets.Open()    →   Viper merge hierarchy   ←   AutomaticEnv
                                    ↓
                          Container (merged config)
                                    ↓
                      Package calls Validate(schema)
                                    ↓
                    ✓ pass → use config    ✗ fail → actionable error
```

Each package owns its slice of the config. No centralised schema is needed.

---

## Quick Start: Scaffolding with the Generator

If you are creating a new command, the `gtb generate command` tool can scaffold the config validation boilerplate for you:

```bash
gtb generate command --name myfeature --assets --with-config-validation
```

This creates a `config.go` file in your command package containing:

- A `Config` struct stub with example `config` struct tags
- A `ValidateConfig` function wired to the schema validation engine

**After scaffolding, you need to:**

1. **Edit the `Config` struct** in `config.go` — replace the TODO comments with your actual config fields and tags
2. **Add your config defaults** to `assets/init/config.yaml` (created by `--assets`)
3. **Call `ValidateConfig`** from your command's `RunE` or initialiser (see [Step 4](#step-4-call-validation-at-the-right-time) below)

The generated `config.go` is yours to customise. Subsequent `regenerate` runs will **never overwrite** it — your changes are preserved. The rest of this guide explains each piece in detail.

---

## Step 1: Define Config Defaults in Embedded Assets

Create an `assets/init/config.yaml` file in your package with sensible defaults:

```
pkg/myfeature/
├── assets/
│   └── init/
│       └── config.yaml
├── config.go
├── feature.go
└── assets.go
```

**pkg/myfeature/assets/init/config.yaml:**

```yaml
myfeature:
  endpoint: https://api.example.com
  log_level: info
  timeout: 30s
```

Embed and register the assets:

```go
// pkg/myfeature/assets.go
package myfeature

import "embed"

//go:embed assets/*
var assets embed.FS
```

Register during initialisation so the merge hierarchy picks up your defaults:

```go
func init() {
    setup.Register(props.FeatureCmd("myfeature"),
        []setup.InitialiserProvider{
            func(p *props.Props) setup.Initialiser {
                p.Assets.Mount(assets, "pkg/myfeature")
                return &Initialiser{}
            },
        },
        // ...
    )
}
```

These defaults are now the baseline. Users override them in their config file or via environment variables. **Do not duplicate these values in struct tags** — the `default` tag is for documentation and hints only.

---

## Step 2: Define the Config Struct with Validation Tags

Create a struct that describes the config keys your package consumes:

```go
// pkg/myfeature/config.go
package myfeature

import (
    "github.com/cockroachdb/errors"
    "github.com/phpboyscout/go-tool-base/pkg/config"
)

// Config describes the configuration keys consumed by myfeature.
type Config struct {
    APIKey   string `config:"myfeature.api_key" validate:"required"`
    Endpoint string `config:"myfeature.endpoint" validate:"required"`
    LogLevel string `config:"myfeature.log_level" enum:"debug,info,warn,error" default:"info"`
    Timeout  string `config:"myfeature.timeout"`
}
```

**Tag reference:**

| Tag | Effect |
|-----|--------|
| `config:"myfeature.api_key"` | Maps to the dot-separated config key |
| `validate:"required"` | Fails if the key is absent or zero-valued |
| `enum:"debug,info,warn,error"` | Fails if the value is not in the allowed set |
| `default:"info"` | Appears in error hints — does **not** set the value |
| `config:"-"` | Skips the field entirely |

---

## Step 3: Add a Validation Function

Expose a function that validates the config slice your package cares about:

```go
// ValidateConfig checks that all required myfeature config keys are present
// and that constrained values are within their allowed sets.
func ValidateConfig(cfg *config.Container) error {
    schema, err := config.NewSchema(config.WithStructSchema(Config{}))
    if err != nil {
        return err
    }

    result := cfg.Validate(schema)
    if !result.Valid() {
        return errors.New(result.Error())
    }

    // Optionally log warnings (e.g., unknown keys under myfeature.*)
    for _, w := range result.Warnings {
        // log warning
    }

    return nil
}
```

---

## Step 4: Call Validation at the Right Time

Validate in your command's `RunE` or `PersistentPreRunE`, after config has been loaded:

```go
func NewCmdMyFeature(p *props.Props) *cobra.Command {
    return &cobra.Command{
        Use:   "myfeature",
        Short: "Do something with myfeature",
        RunE: func(cmd *cobra.Command, args []string) error {
            container, ok := p.Config.(*config.Container)
            if !ok {
                return errors.New("config container required for validation")
            }

            if err := myfeature.ValidateConfig(container); err != nil {
                return err
            }

            // Config is valid — proceed
            return run(cmd.Context(), p)
        },
    }
}
```

If validation fails, the user sees actionable output:

```
config validation failed:
  myfeature.api_key: required field is missing (hint: add myfeature.api_key to your config file or set the MYFEATURE_API_KEY environment variable)
  myfeature.log_level: value "verbose" is not allowed (hint: allowed values: debug, info, warn, error)
```

---

## Step 5: Gate Hot-Reloads (Optional)

For long-running services, attach the schema to the container to prevent invalid config reloads from reaching observers:

```go
schema, err := config.NewSchema(config.WithStructSchema(Config{}))
if err != nil {
    return err
}

container.SetSchema(schema)

// Now if the config file changes and validation fails,
// observers are NOT notified and the previous valid config stays in effect.
```

See [React to Configuration Changes at Runtime](config-hot-reload.md) for the full hot-reload pattern.

---

## Step 6: Strict Mode (Optional)

By default, unknown keys produce warnings. If your package needs tighter control — for example, a user-facing config file where typos should be caught — enable strict mode:

```go
schema, err := config.NewSchema(
    config.WithStructSchema(Config{}),
    config.WithStrictMode(),
)
```

In strict mode, `myfeature.endpont` (typo) would produce an error instead of a warning.

---

## Testing

Test validation using in-memory config containers:

```go
func TestValidateConfig_Valid(t *testing.T) {
    l := logger.NewNoop()
    fs := afero.NewMemMapFs()

    err := afero.WriteFile(fs, "/config.yaml", []byte(`
myfeature:
  api_key: "secret"
  endpoint: "https://api.example.com"
  log_level: info
`), 0o644)
    require.NoError(t, err)

    c, err := config.LoadFilesContainer(l, fs, "/config.yaml")
    require.NoError(t, err)

    container := c.(*config.Container)
    err = myfeature.ValidateConfig(container)
    assert.NoError(t, err)
}

func TestValidateConfig_MissingRequired(t *testing.T) {
    l := logger.NewNoop()
    fs := afero.NewMemMapFs()

    err := afero.WriteFile(fs, "/config.yaml", []byte(`
myfeature:
  log_level: info
`), 0o644)
    require.NoError(t, err)

    c, err := config.LoadFilesContainer(l, fs, "/config.yaml")
    require.NoError(t, err)

    container := c.(*config.Container)
    err = myfeature.ValidateConfig(container)
    require.Error(t, err)
    assert.Contains(t, err.Error(), "myfeature.api_key")
}
```

---

## What NOT to Do

**Don't define defaults in struct tags AND in embedded assets.** Pick one source of truth. Embedded assets are the correct place for defaults; the `default` tag is documentation only.

**Don't create a single global schema for the whole config.** Each package validates its own slice. A global schema would need to know which features are active and would couple packages together.

**Don't add `Validate` to the `Containable` interface.** It lives on `*Container` deliberately. Tests that mock config use `Containable`; validation runs against the real container in production.

---

## Related Documentation

- **[Configuration component](../components/config.md)** — `Containable`, `Container`, factory functions, schema validation reference
- **[Embed and Register Custom Assets](embed-custom-assets.md)** — how to ship config defaults with your package
- **[React to Configuration Changes at Runtime](config-hot-reload.md)** — hot-reload and observer patterns
- **[Add an Initialiser](add-initialiser.md)** — the full feature registration pattern including `IsConfigured` checks
