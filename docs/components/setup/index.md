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
: Complete binary update mechanism that downloads, validates, and installs new versions from pluggable release providers (GitHub, GitLab, Bitbucket, Gitea, Codeberg, Direct HTTP, or custom) with proper configuration migration.

**Version Management**
: Semantic version comparison utilities and development version detection for proper update handling.

**Command Middleware**
: A functional chain pattern for injecting cross-cutting concerns (auth, timing, recovery) into CLI commands.

## Quick Start

Initialize a new tool configuration:

```go
package main

import (
    "os"

    "github.com/phpboyscout/go-tool-base/pkg/logger"
    "github.com/phpboyscout/go-tool-base/pkg/setup"
    "github.com/phpboyscout/go-tool-base/pkg/props"
)

func main() {
    // Create props with tool information
    props := &props.Props{
        Tool: props.Tool{
            Name: "mytool",
        },
        Logger: logger.NewCharm(os.Stdout,
            logger.WithTimestamp(),
            logger.WithLevel(logger.InfoLevel),
        ),
    }

    // Get default configuration directory
    configDir := setup.GetDefaultConfigDir(props.FS, "mytool")

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
    logger         logger.Logger
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

#### Offline Update (Air-Gapped Environments)

For environments without network access, `UpdateFromFile` installs a binary from a local `.tar.gz` release archive:

```go
updater := setup.NewOfflineUpdater(props.Tool, props.Logger, props.FS)
targetPath, err := updater.UpdateFromFile("/path/to/tool_Linux_x86_64.tar.gz")
```

If a `.sha256` sidecar file exists alongside the tarball (e.g., `tool_Linux_x86_64.tar.gz.sha256`), the checksum is verified automatically before extraction. If no sidecar is present, a warning is logged and installation proceeds.

**CLI usage:**
```bash
# Standard offline update
mytool update --from-file /path/to/mytool_Linux_x86_64.tar.gz

# With sidecar checksum (auto-detected)
mytool update --from-file /path/to/mytool_Linux_x86_64.tar.gz
# expects: mytool_Linux_x86_64.tar.gz.sha256 alongside the tarball
```

The `--from-file` flag is mutually exclusive with `--version`. No VCS client or network access is required.

**Checksum verification:**
```go
err := setup.VerifyChecksum(fs, "/path/to/file.tar.gz.sha256", fileData)
```

`VerifyChecksum` accepts the standard `sha256sum` sidecar format (`<hex-hash>  <filename>`) and GoReleaser checksums.txt entries.

#### Release Information
```go
func (s *SelfUpdater) GetReleaseNotes(from string, to string) (string, error)
func (s *SelfUpdater) GetLatestVersionString() (string, error)
func (s *SelfUpdater) GetLatestRelease() (release.Release, error)
```

## Version Management

Version comparison and formatting utilities live in `pkg/version`, not in
`pkg/setup`. The self-updater uses them internally:

```go
import ver "github.com/phpboyscout/go-tool-base/pkg/version"

// Compare two version strings — returns -1, 0, or 1
result := ver.CompareVersions("v1.2.3", "v1.3.0") // -1 (upgrade available)

// Normalise v prefix
ver.FormatVersionString("1.2.3", true)   // "v1.2.3"
ver.FormatVersionString("v1.2.3", false) // "1.2.3"
```

See the [Version component documentation](../version.md) for the full API.

## Command Middleware

The Setup package provides a comprehensive middleware system for wrapping CLI commands with cross-cutting concerns.

*   **Conceptual Overview**: For a high-level understanding of how middleware works in GTB, see [Command Middleware Concept Documentation](../../concepts/command-middleware.md).
*   **Technical Reference**: For the full API and built-in middleware details, see [Command Middleware Technical Reference](middleware.md).

### Core Features
- **Functional Chain Pattern**: Middleware "wraps" the execution, allowing for logic before and after the command runs.
- **Global & Feature Scopes**: Register middleware globally for all commands, or specifically for a feature.
- **Built-ins**: Includes `WithTiming`, `WithRecovery` (panic protection), and `WithAuthCheck` (config validation).
- **Thread-Safe Registry**: Secure registration during initialization with a "sealing" mechanism to prevent runtime modifications.

## Configuration Management

#### Directory Utilities
```go
func GetDefaultConfigDir(fs afero.Fs, name string) string
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
                dir = setup.GetDefaultConfigDir(props.FS, props.Tool.Name)
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

## Release Provider Registry

`NewUpdater` resolves the `release.Provider` from `props.Tool.ReleaseSource.Type` via the provider registry (`pkg/vcs/release`). All built-in providers are pre-registered by the blank imports in `pkg/setup/providers.go` — no manual wiring is needed.

### Supported source types

| `Type` value | Provider | Auth env var |
|---|---|---|
| `"github"` | GitHub / GitHub Enterprise | `GITHUB_TOKEN` |
| `"gitlab"` | GitLab / self-managed | `GITLAB_TOKEN` |
| `"bitbucket"` | Bitbucket Cloud Downloads | `BITBUCKET_USERNAME` + `BITBUCKET_APP_PASSWORD` |
| `"gitea"` | Gitea / Forgejo | `GITEA_TOKEN` |
| `"codeberg"` | Codeberg (Forgejo) | `CODEBERG_TOKEN` |
| `"direct"` | Arbitrary HTTP / S3 / CDN | `DIRECT_TOKEN` |

### Provider-specific parameters

The `props.ReleaseSource.Params` field (`map[string]string`) passes provider-specific configuration:

```go
ReleaseSource: props.ReleaseSource{
    Type: "direct",
    Repo: "mytool",
    Params: map[string]string{
        "url_template": "https://dl.example.com/{tool}/{version}/{tool}_{os}_{arch}.{ext}",
        "version_url":  "https://dl.example.com/latest.json",
    },
},
```

See the [Release Provider component](../vcs/release.md) for a full `Params` reference for each built-in provider.

### Custom providers

Register a custom `release.Provider` factory before calling `NewUpdater`:

```go
import "github.com/phpboyscout/go-tool-base/pkg/vcs/release"

func main() {
    release.Register("s3", func(src release.ReleaseSourceConfig, cfg config.Containable) (release.Provider, error) {
        return myS3Provider(src, cfg)
    })
    // ...
}
```

See [How to add a custom release source](../../how-to/custom-release-source.md) for a step-by-step guide.

---

## Security Considerations

### VCS Authentication
- Supports environment variable and direct token configuration for all release providers
- Tokens are stored in user's config directory with restricted permissions
- Enterprise URL support for private installations (GitHub Enterprise, GitLab Self-Managed, self-hosted Gitea)

### Credential Storage Modes

The `gtb init ai` and `gtb init github` wizards now present a credential storage mode selector backed by [`pkg/credentials`](../credentials.md). Users choose how their secret is persisted, with sensible defaults:

| Mode | Config output | When offered |
|------|---------------|--------------|
| Env-var reference (default) | `{provider}.api.env: ENV_NAME` / `github.auth.env: ENV_NAME` | Always. Selected by default. |
| OS keychain | `{provider}.api.keychain: service/account` | Only when the binary is built with `-tags keychain` and the OS keychain probe succeeds. Phase 2. |
| Literal | `{provider}.api.key: sk-...` / `github.auth.value: ghp_...` | Hidden entirely under `CI=true`; the wizard refuses to persist a plaintext credential into a config file that will almost certainly leak via CI artefacts or logs. |

The AI wizard then prompts for an env var name (defaulting to the provider standard — `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`). The literal key is never written to disk in env-var mode.

The GitHub wizard:

1. **Short-circuits** when a credential is already configured at any resolution layer — env-var reference, literal config (including prefix-aware env via Viper's `AutomaticEnv`), or the unprefixed `GITHUB_TOKEN` ecosystem fallback. Re-running `init` after a successful prior run no longer overwrites env-var mode with a fresh OAuth token.
2. **Refuses literal mode under `CI=true`** with a hint directing the user to the CI platform's secret-injection mechanism.
3. **Falls back to manual token entry** when the OAuth device flow cannot launch a browser — common on dev servers, containers, and SSH-only hosts. The wizard prints a personal-access-token creation URL with the required scopes (`repo,read:org,gist`) pre-populated and reads the pasted token via a hidden input. The captured token is persisted under the same `github.auth.value` key the OAuth flow would have used.

Related surfaces that rely on the same taxonomy:

- **`pkg/chat`** — `resolveAPIKey` honours `{provider}.api.env` before `{provider}.api.key` before the unprefixed ecosystem env. See [Chat > Credential Resolution](../chat.md#credential-resolution).
- **`pkg/vcs/bitbucket`** — dual-credential resolver (`username` + `app_password`) reads the `.env` variant of each field before literals.
- **`pkg/cmd/doctor`** — the `credentials.no-literal` check warns when any literal credential remains in config, with a migration hint.
- **`pkg/cmd/config`** — the sensitive masker now matches mid-path segments so `github.auth.value`, `bitbucket.username`, and `bitbucket.app_password` are rendered as `****<tail>` in `config list` / `config get`.

See the end-user guide at [How to configure credentials](../../how-to/configure-credentials.md) for practical examples and the [Credential Storage Hardening spec](../../development/specs/2026-04-02-credential-storage-hardening.md) for the full design.

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
