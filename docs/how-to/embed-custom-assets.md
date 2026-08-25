---
title: Embed and Register Custom Assets
description: How to ship default configs, templates, and data files with your tool using pkg/props Assets.
date: 2026-03-25
tags: [how-to, assets, embed, configuration, templates]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Embed and Register Custom Assets

GTB's `Assets` system lets you bundle files (YAML configs, templates, CSV data, etc.) directly into your binary using Go's `embed` package. Files from multiple packages are automatically merged at read time, so framework defaults and your tool's overrides coexist without conflicts.

---

## How Merging Works

The `Assets` interface maintains a named, ordered registry of `fs.FS` values:

- **Structured files** (`.yaml`, `.yml`, `.json`, `.toml`, `.csv`, etc.): merged in registration order, with later registrations overriding earlier ones (forward merge).
- **Static files** (everything else): last registered wins (shadowing).

This means you can ship sane defaults and let users or feature packages override only the keys they care about.

---

## Step 1: Create the Embedded Filesystem

In your package, declare an embedded FS and annotate it for `go embed`:

```
myfeature/
├── assets/
│   ├── config/
│   │   └── defaults.yaml
│   └── templates/
│       └── report.tmpl
└── feature.go
```

```go
// myfeature/assets.go
package myfeature

import "embed"

//go:embed assets
var embeddedAssets embed.FS
```

---

## Step 2: Register the Assets

Register your FS on `props.Assets` during tool initialisation. The name you give it is used for scoped access later:

```go
// In your feature initialiser or main.go
p.Assets.Register("myfeature", embeddedAssets)
```

Register the framework's core assets first, then your tool's assets, so that your defaults take precedence:

```go
// main.go
p.Assets.Register("core", coreAssets)       // framework defaults (registered by GTB)
p.Assets.Register("myfeature", myAssets)   // your defaults — override core where keys overlap
```

---

## Step 3: Read Assets

Once registered, use `p.Assets` as a standard `fs.FS`:

```go
// Open a file (YAML files are automatically merged across all registered FSes)
f, err := p.Assets.Open("config/defaults.yaml")
if err != nil {
    return err
}
defer f.Close()

data, err := io.ReadAll(f)
```

For templates and other static files:

```go
tmplData, err := fs.ReadFile(p.Assets, "templates/report.tmpl")
t, err := template.New("report").Parse(string(tmplData))
```

Glob across all registered filesystems:

```go
matches, err := p.Assets.Glob("templates/*.tmpl")
// Returns all .tmpl files from all registered FSes, deduplicated
```

---

## Step 4: Scoped Access with `For`

When you only want assets from specific registered packages (e.g. to avoid accidentally picking up another package's config):

```go
// Only access myfeature's assets
featureOnly := p.Assets.For("myfeature")
f, err := featureOnly.Open("config/defaults.yaml")
```

---

## Step 5: Shipping config defaults (the `assets/config.yaml` convention)

You almost never hand-wire embedded defaults into the store. GTB uses a
filename convention so defaults are picked up automatically:

- **`assets/config.yaml`**: your package's config **defaults**. Every bundle's
  copy is merged and declared as the store's lowest-precedence layer, so a key
  absent from the user's file resolves to the shipped default. These *always
  apply*.
- **`assets/init/config.yaml`**: the **init template**: the document (comments
  included) written to the user's config file by `init`. Seed only meaningful
  values here; an empty placeholder like `api.key: ""` reads as "configured but
  blank" and is noise.

A **command** feature's bundle rides along whenever the feature is enabled, no
extra wiring. A **non-command** feature package announces its bundle with
`setup.RegisterAssets`, and the root command applies it for enabled features at
construction (the framework baseline registers first, via `props.NewAssets`):

```go
//go:embed assets/*
var assets embed.FS

func init() {
    // Contributes assets/config.yaml (defaults) and assets/init/config.yaml
    // (init template) for props.MyFeatureCmd when the feature is enabled.
    setup.RegisterAssets(props.MyFeatureCmd, "myfeature", &assets)
}
```

The root pre-run then builds the store, merged `assets/config.yaml` defaults at
the bottom, then files, env, and flags, with the full
`flags > env > file > embedded > defaults` precedence. See
[Configuration Precedence](../explanation/components/config/index.md) and the
[segregated-defaults spec](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0138-segregated-default-config).
Reach for a manual `config.WithReaders(config.NamedSource{…})` layer only when
you construct a store yourself, outside the framework's root wiring.

---

## Mounting a Subdirectory

Use `Mount` to expose a filesystem at a virtual prefix path without restructuring your source tree:

```go
//go:embed templates
var templateFS embed.FS

// Accessible as "assets/templates/..." in the merged view
p.Assets.Mount(templateFS, "assets/templates")
```

---

## Merging Assets from Multiple Packages

If your tool is composed of feature packages that each bring their own assets, merge them at the top level:

```go
coreAssets := props.NewAssets()
coreAssets.Register("core", coreEmbedFS)

featureAssets := props.NewAssets()
featureAssets.Register("myfeature", featureEmbedFS)
featureAssets.Register("otherfeature", otherEmbedFS)

// Merge all into props.Assets
p.Assets = coreAssets.Merge(featureAssets)
```

---

## YAML Merge Example

Given two registered filesystems both containing `config/defaults.yaml`:

**core/config/defaults.yaml:**
```yaml
log:
  level: info
database:
  host: localhost
  port: 5432
```

**myfeature/config/defaults.yaml:**
```yaml
database:
  host: db.prod.example.com  # overrides core
myfeature:
  enabled: true              # new key
```

Reading `config/defaults.yaml` from `p.Assets` produces the merged result:

```yaml
log:
  level: info
database:
  host: db.prod.example.com
  port: 5432
myfeature:
  enabled: true
```

---

## Testing

In tests, use a simple `fstest.MapFS` instead of an embedded FS:

```go
import "testing/fstest"

testFS := fstest.MapFS{
    "config/defaults.yaml": &fstest.MapFile{
        Data: []byte("log:\n  level: debug\n"),
    },
    "templates/report.tmpl": &fstest.MapFile{
        Data: []byte("Report: {{.Name}}"),
    },
}

p.Assets.Register("test", testFS)
```

---

## Related Documentation

- **[Universal Asset Management](../explanation/components/assets.md)**: merging strategy and design rationale
- **[Props component](../explanation/components/props.md)**: how `Assets` fits into the Props container
