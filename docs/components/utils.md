---
title: Utilities
description: General-purpose utility functions for path resolution, system checks, and terminal interactivity.
date: 2026-02-16
tags: [components, utils, helpers, paths]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Utilities

The `utils` package contains general-purpose helper functions used across the `gtb` framework for environment discovery, path resolution, and terminal interactivity checks.

## Overview

These utilities provide consistent behavior for common system operations, ensuring that the library behaves predictably across different operating systems and environment configurations.

## Core Functions

### Path Resolution

#### GracefulGetPath

`GracefulGetPath` attempts to find the absolute path of an executable on the system's `PATH`. If the executable is not found, it logs helpful installation instructions rather than failing immediately.

```go
func GracefulGetPath(name string, l logger.Logger, instructions ...string) (string, error)
```

**Usage:**

```go
path, err := utils.GracefulGetPath("kubectl", l)
if err != nil {
    // Handle error (instructions have already been logged as warnings)
}
```

#### GetPath (Deprecated)

`GetPath` is a wrapper around `GracefulGetPath` that terminates the program with a fatal error if the executable is not found. **New code should use `GracefulGetPath` to handle errors gracefully.**

```go
// Deprecated: Use GracefulGetPath instead.
func GetPath(name string, l logger.Logger, instructions ...string) string
```

### System Checks

#### IsInteractive

`IsInteractive` determines if the current process is running in an interactive terminal (TTY). This is useful for deciding whether to prompt the user or use a TUI.

```go
func IsInteractive() bool
```

**Usage:**

```go
if utils.IsInteractive() {
    // Launch interactive TUI
} else {
    // Use standard CLI output
}
```

## Built-in Instructions

The package includes a set of predefined installation instructions for common tools:

| Tool | Variable | Description |
| :--- | :--- | :--- |
| `kubectl` | `InstructionKubectl` | Kubernetes CLI |
| `az` | `InstructionAz` | Azure CLI |
| `kubelogin` | `InstructionKubelogin` | Azure Kubernetes login helper |
| `terraform` | `InstructionTerraform` | Infrastructure as Code tool |
| `terragrunt` | `InstructionTerragrunt` | Terraform wrapper |
| `aws` | `InstructionAws` | AWS CLI |
| `git` | `InstructionGit` | Version control |
| `gh` | `InstructionGh` | GitHub CLI |

These instructions are automatically used by `GracefulGetPath` when the tool is missing.

## Implementation Details

The path resolution utilizes Go's standard `os/exec.LookPath`, while interactivity checks use `os.Stdin.Stat()` to inspect the file mode of standard input.
