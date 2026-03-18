---
title: Setup Package
description: Tool initialization and self-updating capabilities, including GitHub auth and SSH key setup.
date: 2026-02-16
tags: [components, setup, initialization, bootstrapping]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Setup Package

The setup package provides comprehensive functionality for tool initialization and self-updating capabilities within the GTB framework. This package enables CLI applications to bootstrap their configuration, manage SSH keys, authenticate with GitHub and GitLab, and maintain themselves through automated updates from pluggable release providers.

## Overview

The setup package implements three core functionalities:

**Tool Initialization**
: Automated creation and configuration of default settings, GitHub authentication, and SSH key management for new tool installations.

**Self-Update System**
: Complete binary update mechanism that downloads, validates, and installs new versions from pluggable release providers (GitHub/GitLab) with proper configuration migration.

**Version Management**
: Semantic version comparison utilities and development version detection for proper update handling.

## Quick Start

Initialize a new tool configuration:

```go
package main

import (
    "os"
    "time"

    "github.com/charmbracelet/log"
    "github.com/phpboyscout/gtb/pkg/setup"
    "github.com/phpboyscout/gtb/pkg/props"
)

func main() {
    // Create props with tool information
    props := &props.Props{
        Tool: props.Tool{
            Name: "mytool",
        },
        Logger: log.NewWithOptions(os.Stdout, log.Options{
            ReportTimestamp: true,
            TimeFormat:      time.Kitchen,
            Level:           log.InfoLevel,
        }),
    }

    // Get default configuration directory
    configDir := setup.GetDefaultConfigDir("mytool")

    // Initialize configuration (interactive setup)
    configFile, err := setup.Initialise(props, setup.InitOptions{Dir: configDir})
    if err != nil {
        props.Logger.Error("Failed to initialize", "error", err)
        return
    }

    props.Logger.Info("Configuration initialized", "file", configFile)
}
```

## Setup & Initialization

The Setup component is designed to be modular and extensible. While it handles core tasks like creating the configuration directory and file, it delegates specific configuration tasks to **Initialisers**.

### The Initialise Function

The entry point for bootstrapping a tool is the `Initialise` function:

```go
func Initialise(props *props.Props, opts InitOptions) (string, error)
```

**InitOptions:**

- `Dir` - Target directory for configuration file creation
- `Clean` - Force overwrite existing configuration (true) or merge (false)
- `SkipLogin` - Skip GitHub authentication setup
- `SkipKey` - Skip SSH key configuration
- `Initialisers` - Additional `Initialiser` implementations to run

**Process Flow:**

1.  **Directory Creation**: Creates target directory structure with proper permissions (0755).
2.  **Asset Loading**: Loads embedded default configuration from `assets/init/config.yaml`.
3.  **Config Merging**: Merges existing configuration if present (unless `Clean=true`).
4.  **Registration**: Discovers registered Initialisers (including built-ins like GitHub and AI).
5.  **Execution**: Runs each Initialiser that reports it is not yet configured.
6.  **Persistence**: Writes the final merged configuration to the target file.

### Initialisers

To keep the setup process modular, GTB uses the **Initialiser Pattern**.

*   **Conceptual Overview**: For a high-level understanding of the pattern, see [Initialisers Concept Documentation](../../concepts/initialisers.md).
*   **Technical Reference**: For implementation details and built-in initialisers, see [Initialisers Technical Reference](initialisers.md).

## Self-Update System

The `SelfUpdater` struct provides comprehensive binary update capabilities:

```go
type SelfUpdater struct {
    ctx            context.Context
    Tool           props.Tool
    force          bool
    version        string
    logger         *log.Logger
    releaseClient  release.Provider
    CurrentVersion string
    NextRelease    release.Release
}
```

**Factory Function:**
```go
func NewUpdater(ctx context.Context, props *props.Props, version string, force bool) (*SelfUpdater, error)
```

**Key Methods:**

#### Version Checking
```go
func (s *SelfUpdater) IsLatestVersion() (bool, string, error)
```

Compares current version against latest release from the configured provider:

- Returns `(true, message, nil)` if already latest or development version
- Returns `(false, message, nil)` if update available with descriptive message
- Handles development versions (v0.0.0) requiring --force flag

#### Binary Update
```go
func (s *SelfUpdater) Update() (string, error)
```

Downloads and installs the target version:

1. Detects current executable path via `os.Executable()`
2. Handles multiple installation detection with user selection
3. Downloads appropriate platform-specific release asset (.tar.gz)
4. Extracts binary with decompression bomb protection
5. Atomically replaces current binary via temporary file
6. Updates last-checked timestamps

#### Release Information
```go
func (s *SelfUpdater) GetReleaseNotes(from string, to string) (string, error)
func (s *SelfUpdater) GetLatestVersionString() (string, error)
func (s *SelfUpdater) GetLatestRelease() (release.Release, error)
```

## Version Management

#### Version Comparison
```go
func CompareVersions(v, w string) int
```

Semantic version comparison using `golang.org/x/mod/semver`:

- Returns `-1` if v < w (upgrade needed)
- Returns `0` if v == w (same version)
- Returns `1` if v > w (downgrade needed)

Automatically handles "v" prefix normalization.

#### Version Formatting
```go
func FormatVersionString(version string, prefixWanted bool) string
```

Standardizes version string format:
```go
// Add v prefix
version := FormatVersionString("1.2.3", true)  // "v1.2.3"

// Remove v prefix
version := FormatVersionString("v1.2.3", false) // "1.2.3"
```

## Configuration Management

#### Directory Utilities
```go
func GetDefaultConfigDir(name string) string
```

Creates and returns the standard configuration directory:

- Linux/macOS: `~/.toolname/`
- Creates directory with 0700 permissions if missing
- Returns empty string if home directory unavailable

#### SSH Key Management
```go
func ConfigureSSHKey(props *props.Props, cfg *viper.Viper) (string, string, error)
```

Interactive SSH key configuration:

1. Scans `~/.ssh/` directory for existing keys
2. Validates key types (RSA, Ed25519, ECDSA, DSA)
3. Offers key generation options if none found
4. Prompts user for key selection via charmbracelet/huh
5. Returns key type and path for configuration

## Integration Patterns

### CLI Command Integration

The setup package integrates seamlessly with cobra commands:

```go
// In cmd/init/init.go
func NewCmdInit(props *props.Props) *cobra.Command {
    return &cobra.Command{
        Use:   "init",
        Short: "Initialize tool configuration",
        Run: func(cmd *cobra.Command, args []string) {
            dir, _ := cmd.Flags().GetString("dir")
            clean, _ := cmd.Flags().GetBool("clean")

            if dir == "" {
                dir = setup.GetDefaultConfigDir(props.Tool.Name)
            }

            configFile, err := setup.Initialise(props, setup.InitOptions{
                Dir: dir,
                Clean: clean,
            })
            if err != nil {
                props.Logger.Error("Initialization failed", "error", err)
                return
            }

            props.Logger.Info("Configuration created", "file", configFile)
        },
    }
}
```

### Automatic Update Checking

Integration with root command for periodic update checks:

```go
// In cmd/root/root.go PreRunE
func checkForUpdates(ctx context.Context, cmd *cobra.Command, props *props.Props) error {
    if setup.SkipUpdateCheck(props.Tool.Name, cmd) {
        return nil
    }

    updater, err := setup.NewUpdater(props, "", false)
    if err != nil {
        return err
    }

    isLatest, message, err := updater.IsLatestVersion()
    if err != nil {
        props.Logger.Warn("Update check failed", "error", err)
        return nil
    }

    if !isLatest {
        props.Logger.Warn(message)
        // Prompt user for update...
    }

    setup.SetTimeSinceLast(props.Tool.Name, setup.CheckedKey)
    return nil
}
```

## Security Considerations

### VCS Authentication
- Supports environment variable and direct token configuration for GitHub and GitLab
- Tokens are stored in user's config directory with restricted permissions
- Enterprise URL support for private installations (GitHub Enterprise, GitLab Self-Managed)

### SSH Key Handling
- Keys are read but never logged or transmitted
- Only key metadata (type, path) stored in configuration
- User prompted for key selection with clear descriptions

### Binary Updates
- Downloads verified against release assets from the configured provider
- Atomic binary replacement prevents corruption
- Decompression bomb protection during extraction
- Executable permission preservation

## Best Practices

### Initialization
- Always use `GetDefaultConfigDir()` for consistent configuration placement
- Implement clean and merge modes for different installation scenarios
- Provide skip options for automated/CI environments
- Include proper error handling with user-friendly messages

### Updates
- Implement periodic update checking in root command PreRunE
- Respect user preferences for update frequency
- Display release notes after successful updates
- Handle multiple installation scenarios gracefully
