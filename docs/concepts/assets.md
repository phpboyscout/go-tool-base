---
title: Asset Management
description: Management of embedded assets, virtual filesystems, and configuration merging.
date: 2026-02-16
tags: [concepts, assets, embed, vfs]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Asset Management

GTB makes heavy use of Go's `embed` package to ship default configurations, templates, and documentation directly within the binary.

## Why Embed Assets?

Shipping assets inside the binary ensures that:

- The tool works immediately after installation without needing external files.
- Default settings are always available as a fallback.
- Migration logic can compare older local files against the newest "embedded" defaults.

## Asset Merging

The framework supports a hierarchical, variadic asset system. This allows different parts of your application—from the root entry point to individual sub-plugins—to contribute their own embedded files.

### 1. Root Initialization
In your `main.go`, you typically initialize the global `Assets` container with your tool's base configuration or templates by providing an `AssetMap`:

```go
//go:embed assets/*
var assets embed.FS

func main() {
    // ...
    p := &props.Props{
        // NewAssets accepts AssetMap for named initialisation
        Assets: props.NewAssets(props.AssetMap{"root": &assets}),
        // ...
    }
}
```

### 2. Subcommand Contribution
Subcommands can then "register" their own domain-specific assets into the shared `Props` container using the `Register` method. Each registration requires a unique name, which allows for explicit identification and retrieval.

```go
//go:embed assets/*
var assets embed.FS

func NewCmdSub(p *props.Props) *cobra.Command {
    // Register subcommand assets with a unique name
    p.Assets.Register("sub", &assets)

    return &cobra.Command{
        Use: "sub",
        // ...
    }
}
```

### Discovery and Filtering

The new map-based storage provides enhanced discovery capabilities:

- **`Get(name string) fs.FS`**: Retrieves a specific filesystem by its registered name.
- **`Names() []string`**: Returns the list of all registered names in their registration order.
- **`For(names ...string) Assets`**: Creates a new `Assets` container containing only the specified named filesystems. This is useful for scoped operations where you only want to work with a subset of the available assets.

### Smart Search & Merging

The `props.Assets` container doesn't just find files; it understands them. When you call `Open(path)`, the framework applies different logic based on the file type:

- **Static Assets (Shadowing)**: For binary files, images, or raw text, we use **Reverse Search (Last-Registered wins)**. If multiple filesystems contain the same file, the one registered latest is returned.
- **Structured Data (Automatic Merging)**: For all structured formats, we perform a **Forward Merge (Aggregate)**. The framework collects every instance of the file across all registered modules (in registration order) and merges them into a single virtual file.
    - **Deep Merge (Maps)**: `.yaml`, `.yml`, `.json`, `.toml`, `.hcl`, `.tf`, and `.xml`.
    - **Key-Value Merge**: `.properties` and `.env`.
    - **Header-Aware Append**: `.csv` (collects and appends all rows).

### Union Filesystem

The container implements `fs.ReadDirFS` and `fs.GlobFS` as a **Union Filesystem**:

- **ReadDir**: returns a deduplicated list of all files in a directory across every registered module.
- **Glob**: allows you to find files across the entire tool-base (e.g., `props.Assets.Glob("**/init/*.yaml")`).

### Advanced Extensibility

- **Mounting**: You can attach any `fs.FS` to a specific virtual prefix using `props.Assets.Mount(myFS, "plugins/my-plugin")`.
- **Generic Support (Afero)**: While we use Go's `embed` package for defaults, the system supports any `fs.FS` implementation. You can easily wrap an `afero.Fs` using `afero.NewIOFS(fs)` and register it.

This architecture enables a truly modular CLI where each component is both a consumer and a contributor to a unified virtual environment.

## Filesystem Abstraction

The `props.Assets` manager wraps these embedded files and presents them through an interface compatible with `spf13/afero`. This means your code can treat embedded files and local files almost identically, greatly simplifying logic that needs to "sync" or "copy" defaults to a user's machine.
