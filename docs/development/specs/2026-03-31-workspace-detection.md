---
title: "Workspace / Project Detection"
description: "Utility for detecting project boundaries by walking up from CWD to find marker files."
date: 2026-03-31
status: IMPLEMENTED
tags:
  - specification
  - workspace
  - project
  - utility
  - feature
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
---

# Workspace / Project Detection

Authors
:   Matt Cockayne

Date
:   31 March 2026

Status
:   DRAFT

---

## Overview

Many CLI tools are scoped to "the current project" — like `git` scopes to the repository root, or `go` scopes to the module root. Tool authors repeatedly implement "walk up from CWD and find a marker file" logic.

This spec adds a small `pkg/workspace` utility that resolves the project root by searching for configurable marker files. It's a helper, not a framework feature — no integration with Props or lifecycle management.

---

## Design

### API

```go
// Workspace represents a detected project boundary.
type Workspace struct {
    Root   string // absolute path to the project root
    Marker string // the marker file that was found (e.g. ".gtb/manifest.yaml")
}

// Detect walks up from startDir looking for any of the given marker files.
// Returns the first match. Returns ErrNotFound if no marker is found
// before reaching the filesystem root.
func Detect(fs afero.Fs, startDir string, markers ...string) (*Workspace, error)

// DetectFromCWD is a convenience that calls Detect with os.Getwd().
func DetectFromCWD(fs afero.Fs, markers ...string) (*Workspace, error)
```

### Default Markers

```go
var DefaultMarkers = []string{
    ".gtb/manifest.yaml", // GTB-generated project
    "go.mod",             // Go module root
    ".git",               // Git repository root
}
```

### Behaviour

1. Start at `startDir`
2. Check if any marker file/directory exists at the current level
3. If found, return the `Workspace` with `Root` set to the current directory
4. Move to the parent directory and repeat
5. If the filesystem root is reached without finding a marker, return `ErrNotFound`

Markers are checked in order — the first match wins. This means `.gtb/manifest.yaml` takes precedence over `go.mod` when using `DefaultMarkers`.

---

## Usage Example

```go
ws, err := workspace.DetectFromCWD(props.FS, workspace.DefaultMarkers...)
if err != nil {
    return errors.Wrap(err, "not inside a project")
}

fmt.Println("Project root:", ws.Root)
fmt.Println("Detected via:", ws.Marker)
```

---

## Resolved Questions

1. **Max depth**: Yes — `Detect` accepts a max depth with a default of 100. This prevents runaway scanning on deeply nested paths or symlink loops.
2. **Glob patterns**: No — markers are exact filenames/directory names only. Tool authors needing globs can implement their own marker check.
