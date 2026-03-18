---
title: Universal Asset Management
description: Leveraging multi-filesystem merging and structured data parsing for lifecycle management.
date: 2026-02-17
tags: [concepts, assets, embed, merging, filesystem]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Universal Asset Management

GTB provides a sophisticated asset management layer that allows applications to treat embedded resources, local files, and remote configurations as a single, unified filesystem.

## The Assets Interface

The `Assets` interface (found in `pkg/props/assets.go`) extends the standard Go `fs.FS` interface with powerful capabilities for merging and mounting.

```go
type Assets interface {
    fs.FS // Standard Go filesystem interface
    Merge(others ...Assets) Assets
    Mount(f fs.FS, prefix string)
    Register(name string, fs fs.FS)
    Exists(name string) (fs.FS, error)
}
```

## Advanced Capabilities

### 1. Multi-Filesystem Merging

The `Merge` method allows you to combine multiple `embed.FS` (or any `fs.FS`) instances into a single hierarchy. If a file exists in multiple filesystems, the `Assets` layer provides intelligent conflict resolution.

This is particularly useful for features that need to "drop in" default configurations or templates into the main application.

```go
// In a feature's command constructor
p.Assets.Register("my-feature", &myFeaturedAssets)
```

### 2. Virtual File Mounting

The `Mount` method allows you to attach a filesystem at a specific virtual path. This is useful for exposing external resources (like a temporary directory or a mapped network drive) as if they were part of the application's internal asset tree.

### 3. Structured Data Merging

The most powerful feature of the `Assets` layer is its ability to automatically merge and parse structured data. When you call `Open` on a file with a supported extension (`.json`, `.yaml`, `.yml`, `.csv`):

1.  **Discovery**: The framework finds all instances of that file across all merged filesystems.
2.  **Parsing**: It unmarshals the content of each file.
3.  **Merging**: It performs a deep merge of the data (using `mergo`).
4.  **Re-serialization**: It returns a single `fs.File` reader containing the combined, merged data.

This allows for a "patch-like" pattern where features can contribute additional settings to a global `config.yaml` or add rows to a shared `commands.csv` without needing to edit the original source files.

## Project Usage

In `als`, this pattern is used to allow individual commands (like `train` or `pipeline`) to register their own assets during their constructor call. When the `init` command runs, it opens the merged `config.yaml` from the `Assets` layer, which now contains all the default values contributed by every registered feature.

## Best Practices

- **Namespaced Assets**: Always register feature assets with a unique name to ensure they can be identified and audited.
- **Prefer YAML for Config**: While JSON is supported, YAML provides better readability for merged configuration templates.
- **Lazy Registration**: Only register assets when they are actually needed (e.g., inside the command constructor) to keep the initial memory footprint low.
