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

    "gitlab.com/phpboyscout/go-tool-base/pkg/logger"
    "gitlab.com/phpboyscout/go-tool-base/pkg/setup"
    "gitlab.com/phpboyscout/go-tool-base/pkg/props"
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

*   **Conceptual Overview**: For a high-level understanding of the pattern, see [Initialisers Concept Documentation](initialisers.md).
*   **Technical Reference**: For implementation details and built-in initialisers, see [Initialisers Technical Reference](initialisers.md).

## Self-Update System

The `SelfUpdater` struct provides comprehensive binary update capabilities:


> [!NOTE]
> See [pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/setup](https://pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/setup) for the full API definition.


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

#### Remote Checksum Verification (Phase 1)

Remote updates via `Update()` automatically verify the downloaded binary against the release's `checksums.txt` manifest before extraction. GoReleaser produces this file by default on every release, so no `.goreleaser.yaml` change is required.

**How it works:**

1. After downloading the target binary, `Update()` looks for a `checksums.txt` asset in the same release.
2. The manifest is downloaded (capped at `setup.MaxChecksumsSize`, default 1 MiB) and parsed line-by-line. A single malformed line rejects the whole manifest (`ErrChecksumManifestMalformed`), and a filename listed **more than once** rejects it too (`ErrChecksumManifestDuplicate`) — a duplicate is never silently resolved last-wins, since that would let a tampered manifest shadow the genuine hash with an attacker-chosen one.
3. The binary's SHA-256 is compared against the manifest entry in constant time.
4. A mismatch aborts the update; a match logs `"checksum verified"` at INFO and proceeds to extraction.

**Fail-open by default, fail-closed by opt-in:**

The library defaults to fail-open — a release without `checksums.txt` logs a warning and proceeds, preserving backward compatibility with legacy releases. Tool authors who want fail-closed verification from day one set:

```go
func main() {
    setup.DefaultRequireChecksum = true  // refuse unverified updates
    // ...
}
```

End users can override at runtime via config:

```yaml
update:
  require_checksum: true
  checksum_asset_name: ""    # override default "checksums.txt" if needed
```

Or via env var (respects the tool's env prefix): `MYTOOL_UPDATE_REQUIRE_CHECKSUM=true`.

**Non-standard asset layouts:**

Providers that don't publish `checksums.txt` as a release asset — notably the Direct HTTP provider and Bitbucket Downloads — opt in to the optional `release.ChecksumProvider` interface, retrieving the manifest via an alternate path (a URL template for Direct, an exact-name lookup in the downloads list for Bitbucket). The `Update()` flow prefers this interface when implemented and falls back to the asset-list scan otherwise.

See [Secure Releases How-To](../../../how-to/secure-releases.md) for the full setup and config story.

#### Signature Verification (Phase 2)

Phase 1 proves a download matches its manifest, but not that the manifest itself is authentic. Phase 2 adds OpenPGP signature verification of `checksums.txt` against a trust set whose anchor is diffused away from the VCS — an embedded key cross-checked against a Web Key Directory key. See [Signature Verification — Trust Anchors & Key Resolvers](signature-verification.md) for the `TrustSet` primitive, the minimum-strength policy, and the pluggable `KeyResolver` chain (embedded, WKD, composite).

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
import ver "gitlab.com/phpboyscout/go-tool-base/pkg/version"

// Compare two version strings — returns -1, 0, or 1
result := ver.CompareVersions("v1.2.3", "v1.3.0") // -1 (upgrade available)

// Normalise v prefix
ver.FormatVersionString("1.2.3", true)   // "v1.2.3"
ver.FormatVersionString("v1.2.3", false) // "1.2.3"
```

See the [Version component documentation](../version.md) for the full API.

## Command Middleware

The Setup package provides a comprehensive middleware system for wrapping CLI commands with cross-cutting concerns.

*   **Conceptual Overview**: For a high-level understanding of how middleware works in GTB, see [Command Middleware Concept Documentation](middleware.md).
*   **Technical Reference**: For the full API and built-in middleware details, see [Command Middleware Technical Reference](middleware.md).

### Core Features
- **Functional Chain Pattern**: Middleware "wraps" the execution, allowing for logic before and after the command runs.
- **Global & Feature Scopes**: Register middleware globally for all commands, or specifically for a feature.
- **Built-ins**: Includes `WithTiming`, `WithRecovery` (panic protection), `WithAuthCheck` (config validation), and `WithTelemetry`.
- **Thread-Safe Registry**: Secure registration during initialization with a "sealing" mechanism to prevent runtime modifications.
- **Composed `Command` type**: Since v0.5, command constructors return `*setup.Command` (`{*cobra.Command, Feature props.FeatureCmd}`). Parents attach children via `cmd.Register(child...)`, which wraps each child's `RunE` exactly once with global and feature-specific middleware — no separate middleware-wiring call required. (The former `AddCommandWithMiddleware` helper was removed in v0.20.)

## Configuration Management

#### Directory Utilities
```go
func GetDefaultConfigDir(fs afero.Fs, name string) string
```

Resolves and returns the standard configuration directory path:

- Linux/macOS: `~/.toolname/`
- **Pure** — computes and returns the path only; it never creates the directory. Building the command tree (`--help`, completions, default flag values) resolves this path, so a hidden `MkdirAll` here would create `~/.toolname` as a side effect of merely running `--help`. Directory creation is deferred to the writers that actually persist a file under it (the init flow, the update-timestamp marker, and the config writers in `pkg/cmd/config`), each of which `MkdirAll`s its parent at write time.
- Returns empty string if the home directory is unavailable (callers must treat this as "no config dir" and skip the read/write rather than joining a relative path).
- The `fs` parameter is retained for API compatibility and is unused.

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

The setup package integrates seamlessly with the GTB command composition pattern (`*setup.Command` returned from each constructor):

```go
// In cmd/init/init.go
func NewCmdInit(p *props.Props) *setup.Command {
    return setup.Wrap("init", &cobra.Command{
        Use:   "init",
        Short: "Initialize tool configuration",
        RunE: func(cmd *cobra.Command, args []string) error {
            dir, _ := cmd.Flags().GetString("dir")
            clean, _ := cmd.Flags().GetBool("clean")

            if dir == "" {
                dir = setup.GetDefaultConfigDir(p.FS, p.Tool.Name)
            }

            configFile, err := setup.Initialise(p, setup.InitOptions{
                Dir: dir,
                Clean: clean,
            })
            if err != nil {
                return err
            }

            p.Logger.Info("Configuration created", "file", configFile)
            return nil
        },
    })
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
import "gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"

func main() {
    release.Register("s3", func(src release.ReleaseSourceConfig, cfg config.Containable) (release.Provider, error) {
        return myS3Provider(src, cfg)
    })
    // ...
}
```

See [How to add a custom release source](../../../how-to/custom-release-source.md) for a step-by-step guide.

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
| OS keychain | `{provider}.api.keychain: service/account` | Only when the tool's `main` imports `gitlab.com/phpboyscout/go-tool-base/pkg/credentials/keychain` (or registers a custom [`Backend`](../credentials.md#backend-interface)) AND [`credentials.Probe`](../credentials.md#api) succeeds against that backend at wizard start. Phase 2. |
| Literal | `{provider}.api.key: sk-...` / `github.auth.value: ghp_...` | Hidden entirely under `CI=true`; the wizard refuses to persist a plaintext credential into a config file that will almost certainly leak via CI artefacts or logs. |

The AI wizard then prompts for an env var name (defaulting to the provider standard — `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`). The literal key is never written to disk in env-var mode.

The GitHub wizard:

1. **Short-circuits** when a credential is already configured at any resolution layer — env-var reference, literal config (including prefix-aware env via Viper's `AutomaticEnv`), keychain reference, or the unprefixed `GITHUB_TOKEN` ecosystem fallback. Re-running `init` after a successful prior run does not overwrite an existing mode with a fresh OAuth token.
2. **Refuses literal mode under `CI=true`** with a hint directing the user to the CI platform's secret-injection mechanism.
3. **Presents the same three-mode selector as the AI wizard**, gated on CI (hides literal) and on `credentials.Probe` (hides keychain when no backend is reachable).
4. **Env-var mode → OAuth + display-once.** The wizard prompts for an env var name (default `GITHUB_TOKEN`) then asks whether to run OAuth now. If yes, it captures a token via `gh auth login` (or the manual PAT entry fallback on headless hosts), displays the token once inside a protected note with instructions to `export GITHUB_TOKEN=<token>` in the shell profile, and waits for the user to acknowledge before continuing. Only the env-var reference is written to config — the token itself never hits disk.
5. **Keychain mode → Store + ref.** Runs OAuth (or manual fallback) to capture a token, writes it via `credentials.Store(ctx, <toolname>, "github.auth", token)`, and records `github.auth.keychain: <toolname>/github.auth` in the config. No plaintext on disk.
6. **Literal mode → legacy write.** Runs OAuth (or manual fallback) and writes the captured token to `github.auth.value`. Refused under CI.
7. **Falls back to manual token entry** when the OAuth device flow cannot launch a browser — common on dev servers, containers, and SSH-only hosts. The wizard prints a personal-access-token creation URL with the required scopes (`repo,read:org,gist`) pre-populated and reads the pasted token via a hidden input. The captured token is persisted via the mode chosen in step 3.

The Bitbucket wizard (`init bitbucket`) mirrors the same three modes but handles Bitbucket's dual-credential model natively:

- **Env-var mode** prompts for two env var names (defaults `BITBUCKET_USERNAME`, `BITBUCKET_APP_PASSWORD`) and writes both references — `bitbucket.username.env` and `bitbucket.app_password.env`.
- **Keychain mode** collects the username and app password in one form (app password input uses a hidden echo mode), serialises the pair as `{"username": "...", "app_password": "..."}`, and stores it under a single `bitbucket.keychain` entry via the registered backend.
- **Literal mode** collects both fields and writes them as plaintext (`bitbucket.username`, `bitbucket.app_password`). Refused under CI.

Related surfaces that rely on the same taxonomy:

- **`pkg/chat`** — `resolveAPIKey` honours `{provider}.api.env` before `{provider}.api.key` before the unprefixed ecosystem env. See [Chat > Credential Resolution](../chat.md#credential-resolution).
- **`pkg/vcs/bitbucket`** — dual-credential resolver (`username` + `app_password`) walks the full chain per field: `bitbucket.<field>.env` → shared `bitbucket.keychain` JSON blob (`{"username": ..., "app_password": ...}`) → literal `bitbucket.<field>` → well-known `BITBUCKET_<FIELD>` env. Corrupt or incomplete keychain blobs abort resolution rather than silently falling back to stale literals.
- **`pkg/cmd/doctor`** — the `credentials.no-literal` check warns when any literal credential remains in config, with a migration hint.
- **`pkg/cmd/config`** — the sensitive masker now matches mid-path segments so `github.auth.value`, `bitbucket.username`, and `bitbucket.app_password` are rendered as `****<tail>` in `config list` / `config get`.

See the end-user guide at [How to configure credentials](../../../how-to/configure-credentials.md) for practical examples, the [Custom credential backend how-to](../../../how-to/custom-credential-backend.md) for implementing a `Backend` against Vault, AWS SSM, or any other secret store, and the [Credential Storage Hardening spec](../../../development/specs/2026-04-02-credential-storage-hardening.md) for the full design.

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


## Constructors & Tree Building

# Command Constructor Pattern

In GTB, we consistently use the `NewCmd*` constructor pattern for instantiating commands. Since v0.5 the constructor returns `*setup.Command` — a typed wrapper around `*cobra.Command` that also carries the command's middleware feature key. This architectural choice is fundamental to the framework's goals of testability, modularity, and explicit dependency management, and it removes a class of regressions where the parent had to know how to wrap each child.

## The Pattern

A typical command constructor in GTB looks like this:

```go
func NewCmdExample(props *props.Props) *setup.Command {
    cmd := setup.Wrap("example", &cobra.Command{
        Use:   "example",
        Short: "An example command",
        RunE: func(cmd *cobra.Command, args []string) error {
            // Implementation logic using props
            props.Logger.Info("Executing example command")
            return nil
        },
    })

    // Add flags or subcommands
    return cmd
}
```

`setup.Wrap(feature, cobraCmd)` returns a `*setup.Command` that **embeds** `*cobra.Command`, so every cobra method — `cmd.Flags()`, `cmd.MarkFlagsMutuallyExclusive(…)`, `cmd.SetContext(…)` — keeps working through the embedded pointer. The `"example"` literal is implicitly converted to `props.FeatureCmd` (a named string type) by Go, so call sites stay readable.

## Composing the tree

Parents attach children by calling `Register(...)` on their own `*setup.Command`. There is **no** separate "wrap with middleware" step:

```go
func NewCmdRoot(p *props.Props) *setup.Command {
    cmd := setup.Wrap("", &cobra.Command{Use: "myapp"})

    cmd.Register(
        example.NewCmdExample(p),
        other.NewCmdOther(p),
    )

    return cmd
}
```

`Register` wires global middleware *and* any feature-specific middleware registered for the child's key. Each command is wrapped exactly once with **its own** feature — the parent's feature is never propagated downward.

## Rationale

### 1. Explicit Dependency Injection

By passing the `Props` container directly to the constructor, we make the command's dependencies explicit. The command has immediate access to core services like logging, configuration, and the filesystem without relying on global state or hidden package-level variables.

### 2. Improved Testability

Because dependencies are injected, they can be easily mocked during unit testing. For example, you can pass a `Props` object with an in-memory `afero.Fs` to verify file operations without touching the actual disk.

```go
func TestExampleCommand(t *testing.T) {
    mockFS := afero.NewMemMapFs()
    p := &props.Props{
        FS: mockFS,
        // ... other mocked props
    }

    cmd := NewCmdExample(p)
    // Execute command and assert on mockFS state
}
```

### 3. Encapsulation

The constructor provides a single place to define the command URI, description, flags, and execution logic. This encapsulation makes the codebase easier to navigate and maintain, as everything related to a specific command is contained within its own package and constructor.

### 4. Consistency Across the Framework

Using a standardized pattern ensures that all commands in a project behaving similarly. Whether it's a built-in framework command like `version` or a custom-implemented feature, the lifecycle and dependency management remain identical.

### 5. Seamless Generation

This pattern is natively supported by the [Framework CLI](../../../reference/cli/index.md) and its generation logic. When you add a new command via the manifest, the generator automatically scaffolds the `NewCmd*` constructor, ensuring your project remains aligned with framework standards.

## Best Practices

- **Avoid Global State**: Do not use `init()` functions to register commands globally. Use the constructor and register the command in the parent's constructor or the `Root` command.
- **Minimal Logic in Run**: Keep the `Run()` function focused on parsing arguments and calling service methods. Business logic should ideally reside in the `pkg/` directory, making it independently testable.
- **Pass Props Down**: If a command has subcommands, pass the `Props` pointer down to their respective constructors.
- **Wrap once, at the top of the constructor**: assign `cmd := setup.Wrap(...)` immediately so every later mutation (`cmd.Flags()`, `cmd.Register(child)`, …) operates on the composed type.
- **Use the empty feature `""` for non-feature-gated commands**: root and pure command-group containers (with no feature-specific middleware) typically pass `setup.Wrap("", &cobra.Command{...})`. Feature keys are middleware lookup keys, not display labels.

## Why the typed return matters

Returning `*setup.Command` (rather than `*cobra.Command`) is what lets `parent.Register(child)` be the single, idiomatic attachment call:

- It can wrap the child's `RunE` with the child's own feature middleware.
- It can stay idempotent on regeneration — re-running the generator does not double-wrap.
- It is type-checked at compile time: a caller cannot accidentally attach an unwrapped `*cobra.Command` and skip middleware.

The previous API exposed this via a free function (`setup.AddCommandWithMiddleware(parent, child, props.<Name>Cmd)`) that required the parent to know the child's feature key. That coupling is removed; the child owns its own identity. See the [v0.4-to-v0.5 migration guide](../../../reference/migration/v0.4-to-v0.5.md) for the before/after diff and the [command middleware concept](middleware.md) for how the wrapping interacts with the middleware registry.


## Root Command Architecture

## Lifecycle Hooks (PersistentPreRunE)

Before any subcommand is executed, the root command performs the following automated steps:

1. **Flag Extraction**: Validates and parses the global flags.
2. **Configuration Loading**: Merges embedded assets with local filesystem configuration.
3. **Logging Setup**: Configures the global `props.Logger` level and format based on flags and config.
4. **Update Checking**: Optionally performs a background check for newer versions (unless `--ci` is set or the check was done in the last 24 hours).

## Signal Handling

`root.Execute` runs the command tree with a **signal-aware execution context**: it derives a cancellable context watching `os.Interrupt` (SIGINT/Ctrl-C) and `syscall.SIGTERM`, and passes it to Cobra via `ExecuteContext`, so every command's `cmd.Context()` is cancelled on interruption.

The lifecycle mirrors `kubectl`/`docker`:

1. **First signal** — cancels `cmd.Context()`. Long-running commands observing `ctx.Done()` unwind gracefully; the deferred telemetry flush still runs (on a bounded background context, so cancellation cannot abort the flush itself).
2. **Second signal** — force-exits immediately, so a hung cleanup can never trap the user.
3. **Exit code** — a signal-terminated run exits `128 + signum` (`130` for SIGINT, `143` for SIGTERM), threaded through the `ErrorHandler`'s exit path via `errorhandling.WithExitCode` so it never conflicts with normal error exits.

An interrupt is a deliberate user choice, not a failure, so the `interrupted by signal: …` notice is logged at **debug**, not error (it routes through `errorhandling.LevelFatalQuiet`, which exits like `LevelFatal` but logs at debug). End users see a clean exit with the conventional code; `--debug` still surfaces the notice. The non-zero exit code is the signal.

On Windows only `os.Interrupt` is deliverable; the SIGTERM registration is harmless there, so no build tags are needed.

!!! note "Interactive prompts own Ctrl-C"
    While a TUI prompt (Huh/Bubble Tea) is active, the terminal is in raw mode, so Ctrl-C arrives as a *keystroke* — it aborts the current prompt and never raises SIGINT. The outer signal context therefore only acts when no TUI is reading the keyboard. An external `kill -INT`/`kill -TERM` still cancels the whole run, which is the desired semantic for supervisors.

Commands should treat `cmd.Context()` as the single cancellation source:

```go
RunE: func(cmd *cobra.Command, args []string) error {
    select {
    case <-cmd.Context().Done():
        return cmd.Context().Err() // graceful unwind on Ctrl-C / SIGTERM
    case result := <-work:
        return handle(result)
    }
},
```

## Implementation

The root command is implemented in `cmd/root/root.go` and created via the `root.NewCmdRoot(props)` entry point.

For more information on the dependency injection pattern used here, see the **[Props Documentation](../props.md)**.
