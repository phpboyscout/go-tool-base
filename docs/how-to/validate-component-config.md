---
title: Define and Validate Config for a Component
description: How to define config defaults via embedded assets and validate them at runtime using per-package schema validation.
date: 2026-03-27
tags: [how-to, config, validation, schema, components]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Define and Validate Config for a Component

When building a new feature package for a GTB-based tool, you need to handle two concerns:

1. **Config defaults**: what values should exist if the user doesn't provide them
2. **Config validation**: catching typos, missing required fields, and invalid values at startup

GTB separates these responsibilities deliberately. Defaults live in embedded assets. Validation lives in struct tags. This guide shows how to wire both.

---

## How It Fits Together

```
Embedded defaults               User config file        Environment variables
(assets/config.yaml,                  ↓                         ↓
merged across bundles)     →     layered Store          ←   env backend
        ↓                    (precedence = layer order)
        └──────────────────────────↓
                          store.View() (pinned snapshot)
                                    ↓
                      Package calls ValidateStruct[T]
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

1. **Edit the `Config` struct** in `config.go`: replace the TODO comments with your actual config fields and tags
2. **Add your config defaults** to `assets/config.yaml` (seeded by the generator beside the `assets/init/` template)
3. **Call `ValidateConfig`** from your command's `RunE` or initialiser (see [Step 4](#step-4-call-validation-at-the-right-time) below)

The generated `config.go` is yours to customise. Subsequent `regenerate` runs will **never overwrite** it. Your changes are preserved. The rest of this guide explains each piece in detail.

---

## Step 1: Define Config Defaults in Embedded Assets

Create an `assets/config.yaml` file in your package with sensible defaults.
This is the **defaults document**; the sibling `assets/init/config.yaml` is a
different thing. The human-facing template `init` writes into the user's
config file:

```
pkg/myfeature/
├── assets/
│   ├── config.yaml        # defaults — always applied as the lowest layer
│   └── init/
│       └── config.yaml    # init template — written to the user's file
├── config.go
├── feature.go
└── assets.go
```

**pkg/myfeature/assets/config.yaml:**

```yaml
myfeature:
  endpoint: https://api.example.com
  log_level: info
  timeout: 30s
```

Embed the assets and register the bundle. A **command package** registers in
its constructor (the generator emits this for you):

```go
// pkg/myfeature/assets.go
package myfeature

import "embed"

//go:embed assets/*
var assets embed.FS
```

```go
func NewCmdMyFeature(p *props.Props) *setup.Command {
    p.Assets.Register("myfeature", &assets)
    // ...
}
```

A **non-command feature package** announces its bundle through the setup
registry from `init()`; the root command applies the bundles of *enabled*
features during construction, so a disabled feature's defaults never leak into
the resolved configuration:

```go
func init() {
    setup.RegisterAssets(props.FeatureID("myfeature"), "myfeature", &assets)
    setup.Register(props.FeatureID("myfeature"), /* initialisers, subcommands, flags */)
}
```

`props.Assets` merges `assets/config.yaml` across every registered bundle, and
the root bootstrap loads that merged document as the store's lowest layer.
These defaults **always apply**: a key omitted from the user's file resolves
to your default rather than a zero value, and users override them in their
config file, environment, or flags. **Do not duplicate these values in struct
tags**. The `default` tag is for documentation and hints only.

---

## Step 2: Define the Config Struct with Validation Tags

Create a struct that describes the config keys your package consumes:

```go
// pkg/myfeature/config.go
package myfeature

import "gitlab.com/phpboyscout/go/config"

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
| `default:"info"` | Appears in error hints: does **not** set the value |
| `config:"-"` | Skips the field entirely |

---

## Step 3: Add a Validation Function

Expose a function that validates the config slice your package cares about. This
is exactly what the generator scaffolds when you pass `--with-config-validation`:

```go
// ValidateConfig checks that all required myfeature config keys are present
// and that constrained values are within their allowed sets.
func ValidateConfig(cfg config.Reader) error {
    return config.ValidateStruct[Config](cfg)
}
```

`ValidateStruct[T]` derives the schema from `T`'s struct tags (caching it per
type), runs it against the resolved configuration, and returns a formatted
error if anything fails. It takes the `config.Reader` interface, so callers
pass `props.Config.View()` in production and the published `MockReader` in
tests. Never a concrete store type.

If you need the `ValidationResult` itself (to inspect warnings, say) build the
schema with `SchemaOf[T]` and validate by hand:

```go
func ValidateConfig(cfg config.Reader) error {
    schema, err := config.SchemaOf[Config]()
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
func NewCmdMyFeature(p *props.Props) *setup.Command {
    return setup.Wrap("myfeature", &cobra.Command{
        Use:   "myfeature",
        Short: "Do something with myfeature",
        RunE: func(cmd *cobra.Command, args []string) error {
            if err := myfeature.ValidateConfig(p.Config); err != nil {
                return err
            }

            // Config is valid — proceed
            return run(cmd.Context(), p)
        },
    })
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
schema, err := config.SchemaOf[Config]()
if err != nil {
    return err
}

// Attach the schema at construction with WithSchema. The store then validates
// every reload against it and rejects an invalid one — observers keep the last
// valid configuration rather than being handed a broken reload.
store, err := config.NewStore(ctx,
    config.WithSchema(schema),
    config.WithFiles(fsys, paths...),
)
```

See [React to Configuration Changes at Runtime](config-hot-reload.md) for the full hot-reload pattern.

---

## Step 6: Strict Mode (Optional)

By default, unknown keys produce warnings. If your package needs tighter control (for example, a user-facing config file where typos should be caught) enable strict mode by passing the option straight through:

```go
if err := config.ValidateStruct[Config](cfg, config.WithStrictMode()); err != nil {
    return err
}
```

In strict mode, `myfeature.endpont` (typo) would produce an error instead of a warning.

---

## Testing

Build a store over an in-memory config file and pass a pinned `View` (a
`config.Reader`) to your validator:

```go
func TestValidateConfig_Valid(t *testing.T) {
    fs := afero.NewMemMapFs()

    err := afero.WriteFile(fs, "/config.yaml", []byte(`
myfeature:
  api_key: "secret"
  endpoint: "https://api.example.com"
  log_level: info
`), 0o600)
    require.NoError(t, err)

    store, err := config.NewStore(t.Context(),
        config.WithFiles(configafero.Wrap(fs), "/config.yaml"))
    require.NoError(t, err)

    err = myfeature.ValidateConfig(store.View())
    assert.NoError(t, err)
}

func TestValidateConfig_MissingRequired(t *testing.T) {
    fs := afero.NewMemMapFs()

    err := afero.WriteFile(fs, "/config.yaml", []byte(`
myfeature:
  log_level: info
`), 0o600)
    require.NoError(t, err)

    store, err := config.NewStore(t.Context(),
        config.WithFiles(configafero.Wrap(fs), "/config.yaml"))
    require.NoError(t, err)

    err = myfeature.ValidateConfig(store.View())
    require.Error(t, err)
    assert.Contains(t, err.Error(), "myfeature.api_key")
}
```

`configafero.Wrap` bridges an `afero.Fs` to the store's filesystem interface.
See [Test Configuration](test-configuration.md) for the reader-based variant.

---

## What NOT to Do

**Don't define defaults in struct tags AND in embedded assets.** Pick one source of truth. Embedded assets are the correct place for defaults; the `default` tag is documentation only.

**Don't create a single global schema for the whole config.** Each package validates its own slice. A global schema would need to know which features are active and would couple packages together.

**Don't reach for the concrete store to validate.** `ValidateStruct[T]` and `View.Validate` both work through the `config.Reader` interface, so a package never needs the concrete `*config.Store`. A pinned view (or a mock) is enough.

---

## Related Documentation

- **[Configuration component](../explanation/components/config/index.md)**: the Store, views, layers, and schema validation reference
- **[Embed and Register Custom Assets](embed-custom-assets.md)**: how to ship config defaults with your package
- **[React to Configuration Changes at Runtime](config-hot-reload.md)**: hot-reload and observer patterns
- **[Add an Initialiser](add-initialiser.md)**: the full feature registration pattern including `IsConfigured` checks
