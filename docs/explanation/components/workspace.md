---
title: Workspace
description: Project root detection by walking up from the current directory to find marker files.
date: 2026-04-01
tags: [components, workspace, project, utility]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Workspace

**Package:** `pkg/workspace`

Detects project boundaries by walking up from a starting directory to find marker files. Used internally by the generator commands to auto-resolve the project root when running from subdirectories.

## Usage

```go
ws, err := workspace.Detect(afero.NewOsFs(), ".", workspace.DefaultMarkers)
if err != nil {
    return errors.Wrap(err, "not inside a project")
}

fmt.Println("Project root:", ws.Root)
fmt.Println("Detected via:", ws.Marker)
```

### From Current Working Directory

```go
ws, err := workspace.DetectFromCWD(afero.NewOsFs(), workspace.DefaultMarkers)
```

## Default Markers

Checked in order — the first match wins:

| Marker | Detects |
|--------|---------|
| `.gtb/manifest.yaml` | GTB-generated project |
| `go.mod` | Go module root |
| `.git` | Git repository root |

## Custom Markers

```go
ws, err := workspace.Detect(fs, startDir, []string{"package.json", "Cargo.toml"})
```

## Max Depth

Default: 100 levels. Override to limit scanning:

```go
ws, err := workspace.Detect(fs, startDir, markers, workspace.WithMaxDepth(10))
```

## Generator Integration

All generator commands (`regenerate`, `generate command/docs/flag`, `remove`) automatically resolve the workspace root when `--path` is `"."` (the default). This means you can run `gtb regenerate project` from any subdirectory within the project.

## Related Documentation

- [Workspace Detection Specification](../development/specs/2026-03-31-workspace-detection.md)
