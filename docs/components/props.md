---
title: Props
description: Dependency injection container identifying tool metadata and providing access to global services.
date: 2026-02-16
tags: [components, props, dependency-injection, architecture]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Props

## Overview

Props serves as the primary data structure that carries essential information about your tool and provides access to various services and configurations. It's designed to be passed to all major components and commands in your CLI application.

## Design Rationale

Props is intentionally designed as a concrete dependency injection container rather than using Go's `context.Context` for passing dependencies. This design choice provides several key benefits:

### Type Safety and Compile-Time Checks

Unlike `context.Context` which stores values as `interface{}`, Props provides concrete types for all dependencies:

```go
// Props approach - Type safe, IDE-friendly
func NewCommand(props *props.Props) *cobra.Command {
    props.Logger.Info("Starting command")     // ✅ Compile-time type checking
    host := props.Config.GetString("db.host") // ✅ Known interface methods
    return cmd
}

// Context approach - Runtime type assertions required
func NewCommand(ctx context.Context) *cobra.Command {
    l := ctx.Value("logger").(logger.Logger) // ❌ Runtime panic risk
    config := ctx.Value("config").(SomeInterface) // ❌ No compile-time guarantee
    return cmd
}
```

### Clear Dependency Declaration

Props makes dependencies explicit and discoverable:

- **Discoverability**: IDEs can provide accurate autocomplete and navigation
- **Documentation**: Each field is clearly documented with its purpose
- **Refactoring**: Changes to dependency interfaces are caught at compile time
- **Testing**: Easy to create test doubles with concrete interfaces

### Performance Benefits

- **No runtime type assertions**: All types are known at compile time
- **Reduced allocations**: No boxing/unboxing of interface{} values
- **Better inlining**: Compiler can optimize concrete type access

## Core Structure

```go
type Props struct {
    Tool         Tool                       // Tool metadata and settings
    Logger       logger.Logger              // Configured logger instance
    Config       config.Containable         // Configuration container
    Assets       Assets                     // Embedded assets wrapper interface
    FS           afero.Fs                   // Filesystem abstraction
    Version      version.Version            // Version information (pkg/version.Version interface)
    ErrorHandler errorhandling.ErrorHandler  // Error Handler interface
    Collector    TelemetryCollector          // Telemetry event collector (always non-nil)
}
```

!!! note "Collector is always non-nil"
    When telemetry is disabled, `Collector` is a noop implementation. Commands can safely call `p.Collector.Track(...)` without checking whether telemetry is enabled.

!!! note "ErrorHandler is an Interface"
    The `ErrorHandler` field is an interface type, not a pointer. This enables easy mocking and custom implementations for testing.

## Constants and Types

### Feature Commands

Feature commands are identifiers used to enable or disable built-in functionality:

```go
type FeatureCmd string

const (
    UpdateCmd = FeatureCmd("update") // Self-update functionality
    InitCmd   = FeatureCmd("init")   // Configuration initialisation
    McpCmd    = FeatureCmd("mcp")    // Model Context Protocol server
    DocsCmd   = FeatureCmd("docs")   // Interactive documentation browser
    AiCmd        = FeatureCmd("ai")        // AI-powered features (opt-in)
    DoctorCmd    = FeatureCmd("doctor")    // Environment health checks
    ConfigCmd    = FeatureCmd("config")    // Programmatic config access (opt-in)
    TelemetryCmd = FeatureCmd("telemetry") // Anonymous usage telemetry (opt-in)
)
```

### Default Behavior

`props.Tool` automatically handles default feature states. `IsEnabled` prioritizes configured features but falls back to built-in defaults if no explicit configuration is found.

`pkg/props` defines a standard set of features enabled by default:
- `update`
- `init`
- `mcp`
- `docs`
- `doctor`

The following features are **opt-in** (disabled by default):
- `ai` — AI provider configuration during `init`
- `config` — programmatic config access (`config get/set/list/validate`)
- `telemetry` — anonymous usage telemetry collection and CLI management commands

#### The `SetFeatures` Constructor

The preferred way to define a tool's feature set in code is using the `props.SetFeatures` constructor. It automatically applies all default features first, allowing you to only specify overrides:

```go
// Returns defaults (Update, Init, Mcp, Docs enabled)
Features: props.SetFeatures(),

// Starts with defaults, but disables 'init' and enables 'ai'
Features: props.SetFeatures(
    props.Disable(props.InitCmd),
    props.Enable(props.AiCmd),
),
```

!!! tip "Enabling vs Disabling Features"
    To disable default features or enable optional features (like `ai`), use the `SetFeatures` helper in your tool configuration:

    ```go
    Features: props.SetFeatures(
        props.Disable(props.InitCmd),
        props.Enable(props.AiCmd),
    ),
    ```

    You can check feature status using the helper methods:
    `props.Tool.IsEnabled(props.AiCmd)` or `props.Tool.IsDisabled(props.InitCmd)`.

## Narrow Interfaces

Props provides narrow role-based interfaces that `*Props` satisfies. When writing functions that only need a subset of Props, prefer these interfaces to declare minimal dependencies:

| Interface | Methods | Use When |
|-----------|---------|----------|
| `LoggerProvider` | `GetLogger()` | You only need logging |
| `ConfigProvider` | `GetConfig()` | You only need configuration |
| `FileSystemProvider` | `GetFS()` | You only need filesystem access |
| `AssetProvider` | `GetAssets()` | You only need embedded assets |
| `VersionProvider` | `GetVersion()` | You only need version info |
| `ErrorHandlerProvider` | `GetErrorHandler()` | You only need error handling |
| `ToolMetadataProvider` | `GetTool()` | You only need tool metadata |
| `LoggingConfigProvider` | `GetLogger()`, `GetConfig()` | You need logging + config |
| `CoreProvider` | `GetLogger()`, `GetConfig()`, `GetFS()` | You need the three most common capabilities |

### Example

```go
// Before: opaque dependency on all of Props
func generateDocs(p *props.Props) error { ... }

// After: declares exactly what it needs
func generateDocs(p props.LoggingConfigProvider) error {
    p.GetLogger().Info("generating docs")
    dir := p.GetConfig().GetString("docs.output_dir")
    ...
}
```

Migration is optional and incremental — `*Props` continues to work everywhere.

## Components

### Tool Metadata

The `Tool` struct contains essential information about your CLI tool:

```go
type Tool struct {
    Name          string                   `json:"name" yaml:"name"`
    Summary       string                   `json:"summary" yaml:"summary"`
    Description   string                   `json:"description" yaml:"description"`
    Features      []Feature                `json:"features" yaml:"features"`
    ReleaseSource ReleaseSource            `json:"release_source" yaml:"release_source"`
    Help          errorhandling.HelpConfig `json:"-" yaml:"-"`
}

// ReleaseSource identifies where the tool's releases are hosted.
type ReleaseSource struct {
    Type    string `json:"type" yaml:"type"`       // "github" or "gitlab"
    Host    string `json:"host" yaml:"host"`       // Custom host (e.g., self-hosted GitLab)
    Owner   string `json:"owner" yaml:"owner"`     // Organisation or user
    Repo    string `json:"repo" yaml:"repo"`       // Repository name
    Private bool   `json:"private" yaml:"private"` // Whether the repository is private
}

// Feature represents the configuration state of a feature (Enabled/Disabled).
type Feature struct {
    Cmd     FeatureCmd `json:"cmd" yaml:"cmd"`
    Enabled bool       `json:"enabled" yaml:"enabled"`
}

// FeatureState is a functional option used to mutate the feature list.
type FeatureState func([]Feature) []Feature
```

!!! info "Help Configuration"
    `Tool.Help` accepts any value that implements the `errorhandling.HelpConfig` interface (`SupportMessage() string`). Use `errorhandling.SlackHelp` or `errorhandling.TeamsHelp` for built-in support channel messages, or pass `nil` for no help message. The field is set programmatically — it is not read from YAML/JSON config files.

**Example:**
```go
p := &props.Props{
    Tool: props.Tool{
        Name:        "awesome-cli",
        Summary:     "An awesome command-line tool",
        Description: "A comprehensive CLI tool for managing awesome things",
        ReleaseSource: props.ReleaseSource{
            Type:  "github",
            Owner: "mycompany",
            Repo:  "awesome-cli",
        },
        Features: props.SetFeatures(
            props.Enable(props.AiCmd),
        ),
    },
    // ... other fields
}

// Set the help channel after constructing Props
p.Tool.Help = errorhandling.SlackHelp{
    Channel: "#support",
    Team:    "myteam",
}
```

### Version Information

Version tracking for updates and display. The `Version` field on `Props` uses the `version.Version` interface from `pkg/version`:

```go
// pkg/version
type Version interface {
    GetVersion() string
    GetCommit() string
    GetDate() string
    String() string
    Compare(other string) int
    IsDevelopment() bool
}
```

**Example:**
```go
Version: version.NewInfo("1.0.0", "abc123def456", "2024-01-15T10:30:00Z")
```

### Logger Configuration

Structured logging with configurable output via the unified `logger` package:

```go
l := logger.NewCharm(os.Stderr,
    logger.WithCaller(),
    logger.WithTimestamp(),
    logger.WithLevel(logger.InfoLevel),
)
```

**Log Levels:**

- `logger.DebugLevel` - Detailed debugging information
- `logger.InfoLevel` - General information
- `logger.WarnLevel` - Warning messages
- `logger.ErrorLevel` - Error messages
- `logger.FatalLevel` - Fatal messages

### Filesystem Abstraction

The `FS` field uses the afero library for filesystem abstraction, enabling easy testing:

```go
import "github.com/spf13/afero"

// Production: real filesystem
FS: afero.NewOsFs()

// Testing: in-memory filesystem
FS: afero.NewMemMapFs()
```

### Embedded Assets

The `Assets` field holds a wrapper for embedded filesystems (configurations, templates, etc.):

```go
//go:embed assets/*
var assets embed.FS

props := &props.Props{
    Assets: props.NewAssets(props.AssetMap{"root": &assets}),
}
```

Subcommands can register their own assets:

```go
func NewCmdSub(p *props.Props) *cobra.Command {
    p.Assets.Register("sub", &assets)
    // ...
}
```

## Usage Patterns

### Basic Initialization

```go
func NewCmdRoot(v version.Info) (*cobra.Command, *props.Props) {
    l := logger.NewCharm(os.Stderr,
        logger.WithTimestamp(),
        logger.WithLevel(logger.InfoLevel),
    )

    p := &props.Props{
        Tool: props.Tool{
            Name:        "mytool",
            Summary:     "My CLI tool",
            Description: "Does amazing things",
            ReleaseSource: props.ReleaseSource{
                Type:  "github",
                Owner: "myorg",
                Repo:  "mytool",
            },
        },
        Logger:  l,
        Assets:  props.NewAssets(props.AssetMap{"root": &assets}),
        FS:      afero.NewOsFs(),
        Version: v,
    }

    p.ErrorHandler = errorhandling.New(l, p.Tool.Help)

    rootCmd := root.NewCmdRoot(p)
    return rootCmd, p
}
```

### Passing to Custom Commands

```go
func NewCustomCommand(props *props.Props) *cobra.Command {
    cmd := &cobra.Command{
        Use:   "custom",
        Short: "A custom command",
        RunE: func(cmd *cobra.Command, args []string) error {
            return runCustomCommand(cmd.Context(), props)
        },
    }
    return cmd
}

func runCustomCommand(ctx context.Context, props *props.Props) error {
    props.Logger.Info("Running custom command")

    data, err := afero.ReadFile(props.FS, "data.txt")
    if err != nil {
        return errors.Wrap(err, "failed to read data file")
    }

    props.Logger.Info("Command completed successfully")
    return nil
}
```

### Configuration Integration

```go
func runDatabaseCommand(ctx context.Context, props *props.Props) error {
    dbHost := props.Config.GetString("database.host")
    dbPort := props.Config.GetInt("database.port")

    props.Logger.Info("Connecting to database", "host", dbHost, "port", dbPort)
    return nil
}

func NewDatabaseCommand(props *props.Props) *cobra.Command {
    return &cobra.Command{
        Use:   "database",
        Short: "Database operations",
        RunE: func(cmd *cobra.Command, args []string) error {
            return runDatabaseCommand(cmd.Context(), props)
        },
    }
}
```

## Advanced Configuration

### Conditional Features

```go
Tool: props.Tool{
    Name: "enterprise-tool",
    Features: props.SetFeatures(
        props.Disable(props.UpdateCmd), // Disable auto-updates in enterprise
    ),
}
```

### Copy-on-Write Filesystem

```go
import "github.com/spf13/afero"

baseFs := afero.NewReadOnlyFs(afero.NewOsFs())
overlayFs := afero.NewMemMapFs()
cowFs := afero.NewCopyOnWriteFs(baseFs, overlayFs)

props.FS = cowFs
```

## Testing with Props

```go
func createTestProps() *props.Props {
    l := logger.NewNoop()
    memFs := afero.NewMemMapFs()

    return &props.Props{
        Tool: props.Tool{
            Name:    "test-tool",
            Summary: "Test tool",
        },
        Logger:  l,
        FS:      memFs,
        Version: version.NewInfo("0.0.0-test", "", ""),
    }
}
```

## Best Practices

### 1. Use ReleaseSource for Repository Identity

`ReleaseSource` is the single source of truth for where the tool's releases are hosted. It supports both GitHub and GitLab:

```go
// GitHub
ReleaseSource: props.ReleaseSource{
    Type:  "github",
    Owner: "your-org",
    Repo:  "tool-name",
},

// GitLab (including self-hosted)
ReleaseSource: props.ReleaseSource{
    Type:    "gitlab",
    Host:    "gitlab.example.com", // Optional: defaults to gitlab.com
    Owner:   "your-group",
    Repo:    "tool-name",
    Private: true,                 // Set to true for private repositories
},
```

### 2. Consistent Tool Metadata

```go
Tool: props.Tool{
    Name:        "kebab-case-name",
    Summary:     "Brief description",
    Description: "Longer description that explains the tool's purpose and capabilities",
    ReleaseSource: props.ReleaseSource{
        Type:  "github",
        Owner: "your-org",
        Repo:  "tool-name",
    },
}
```

### 3. Set Help After Construction

Since `Tool.Help` is an interface (not serializable), assign it programmatically after building `Props`:

```go
p := &props.Props{Tool: props.Tool{...}}
p.Tool.Help = errorhandling.SlackHelp{Team: "Platform", Channel: "#help"}
p.ErrorHandler = errorhandling.New(l, p.Tool.Help)
```

The Props component provides a robust foundation for building maintainable and testable CLI applications with GTB.
