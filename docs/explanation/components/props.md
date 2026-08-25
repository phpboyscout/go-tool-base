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

!!! note "What's in a Name?"
    The name **Props** is not merely a shorthand for 'properties' (though we do shove plenty of those in there). It's a direct reference to a **prop**, the heavy-duty timber or steel beam that prevents a structure from an embarrassing collapse. For the sports fans, it's also a lovingly crafted nod to the rugby position: the broad-shouldered stalwarts who provide the primary structural support for the scrum. Much like its on-field namesake, our `Props` struct isn't here to score the flashy tries; it's here to do the unsung heavy lifting that keeps the entire framework from falling over.

## Design Rationale

Props is intentionally designed as a concrete dependency injection container rather than using Go's `context.Context` for passing dependencies. This design choice provides several key benefits:

### Type Safety and Compile-Time Checks

Unlike `context.Context` which stores values as `interface{}`, Props provides concrete types for all dependencies:

```go
// Props approach - Type safe, IDE-friendly
func NewCommand(props *props.Props) *cobra.Command {
    props.Logger.Info("Starting command")            // ✅ Compile-time type checking
    host := props.Config.View().GetString("db.host") // ✅ pinned snapshot read
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


> [!NOTE]
> See [pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/props](https://pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/props) for the full API definition.


!!! note "Collector is always non-nil"
    When telemetry is disabled, `Collector` is a noop implementation. Commands can safely call `p.Collector.Track(...)` without checking whether telemetry is enabled.

    The root bootstrap upholds this invariant automatically: building the command tree (`NewCmdRoot`) defaults the field to `props.NoopCollector{}`, and the resolved `*telemetry.Collector` replaces it once config loads. A `Props` constructed directly as a struct literal (for example in tests that exercise a command without going through the bootstrap) should set `Collector: props.NoopCollector{}` itself, or run the command via `root.Execute` (which also defaults it).

!!! note "ErrorHandler is an Interface"
    The `ErrorHandler` field is an interface type, not a pointer. This enables easy mocking and custom implementations for testing.

## Constants and Types

### Feature Commands

Feature commands are identifiers used to enable or disable built-in functionality:

```go
type FeatureID string

const (
    UpdateCmd = FeatureID("update") // Self-update functionality
    InitCmd   = FeatureID("init")   // Configuration initialisation
    McpCmd    = FeatureID("mcp")    // Model Context Protocol server
    DocsCmd   = FeatureID("docs")   // Interactive documentation browser
    AiCmd        = FeatureID("ai")        // AI-powered features (opt-in)
    DoctorCmd    = FeatureID("doctor")    // Environment health checks
    ConfigCmd    = FeatureID("config")    // Programmatic config access (opt-in)
    TelemetryCmd  = FeatureID("telemetry")  // Anonymous usage telemetry (opt-in)
    ChangelogCmd  = FeatureID("changelog")  // Embedded changelog display
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
- `changelog`

### The feature registry

Features are **registered**, not listed. `AllFeatures()` and `DefaultFeatures()` are derived from the registry, so a feature declares its identity, its kind and its default in one place and the two cannot drift.

```go
type FeatureDescriptor struct {
    ID           FeatureID   // identity, and the config/manifest name
    ConstName    string      // exported Go identifier, e.g. "AiCmd"
    ConstPackage string      // import path declaring ConstName
    Kind         FeatureKind // KindBuiltin | KindForge
    Default      bool        // builtin-only; see below
}
```

`ConstName` and `ConstPackage` exist because the generator **emits Go source** naming the constant. The identifier cannot be derived from the ID (`mcp` yields `McpCmd`), and a plugin's constant does not live in `props`. The forge features are declared by `pkg/setup/forge`.

A package registers its own features from `init`, so a blank import is the entire mechanism:

```go
func init() {
    props.RegisterFeature(props.FeatureDescriptor{
        ID:           GithubFeature,
        ConstName:    "GithubFeature",
        ConstPackage: PackagePath,
        Kind:         props.KindForge,
    })
}
```

!!! warning "Only builtin features may be default-enabled"
    `RegisterFeature` rejects a non-builtin descriptor with `Default: true` (`ErrPluginDefaultOn`). Adding a blank import must change what is **available**, never what is **on**: otherwise an import list becomes a behavioural file, and a downstream that deliberately omits a provider cannot reason about what its remaining imports switched on behind it.

#### Querying by kind

`FeaturesOfKind` turns "what forges are there?" into a question with one answer, rather than a list duplicated across the generator wizard, config validation and the doctor report:

```go
for _, id := range props.FeaturesOfKind(props.KindForge) {
    // ...
}
```

#### Ordering and sealing

Enumeration order never consults `init()` sequencing: built-ins hold their declared order and everything else sorts by `(kind, id)`. Go runs `init` in dependency-then-filename order, which is stable for one build but shifts with the import graph, and both the doctor report and the generator's golden files depend on this order.

Reading the registry **seals** it. A registration afterwards fails with `ErrRegistrySealed` rather than yielding a set that depends on when it was read.

!!! info "`AllFeatures()` reflects what this binary linked"
    A tool that blank-imports fewer providers enumerates fewer features. That is the correct *runtime* answer. The generator needs the complete **possible** set instead. Every feature a scaffolded project could choose, including adapters the generator itself does not link, and takes it from its own catalogue rather than from this registry.

The following features are **opt-in** (disabled by default):
- `ai`: AI provider configuration during `init`
- `config`: programmatic config access (`config get/set/list/validate`)
- `telemetry`: anonymous usage telemetry collection and CLI management commands

#### The `SetFeatures` Constructor

The preferred way to define a tool's feature set in code is using the `props.SetFeatures` constructor. It automatically applies all default features first, allowing you to only specify overrides:

```go
// Returns defaults (Update, Init, Mcp, Docs, Doctor, Changelog enabled)
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
| `TelemetryProvider` | `GetCollector()` | You only need the telemetry collector |
| `LoggingConfigProvider` | `GetLogger()`, `GetConfig()` | You need logging + config |
| `CoreProvider` | `GetLogger()`, `GetConfig()`, `GetFS()` | You need the three most common capabilities |

### Example

```go
// Before: opaque dependency on all of Props
func generateDocs(p *props.Props) error { ... }

// After: declares exactly what it needs
func generateDocs(p props.LoggingConfigProvider) error {
    p.GetLogger().Info("generating docs")
    dir := p.GetConfig().View().GetString("docs.output_dir")
    ...
}
```

Migration is optional and incremental, `*Props` continues to work everywhere.

## Components

### Tool Metadata

The `Tool` struct contains essential information about your CLI tool:

```go
type Tool struct {
    Name          string                   `json:"name" yaml:"name"`
    Summary       string                   `json:"summary" yaml:"summary"`
    Description   string                   `json:"description" yaml:"description"`
    Features      []Feature                `json:"features" yaml:"features"`
    Bootstrap     BootstrapPolicy          `json:"bootstrap,omitempty" yaml:"bootstrap,omitempty"`
    ReleaseSource ReleaseSource            `json:"release_source" yaml:"release_source"`
    Help          errorhandling.HelpConfig `json:"-" yaml:"-"`
    // InstallHint is shown when a feature needs a full release binary the
    // running binary lacks (e.g. embedded docs after `go install`). Set it to
    // your tool's recommended install command; empty falls back to a generic
    // message referencing Name.
    InstallHint string `json:"install_hint,omitempty" yaml:"install_hint,omitempty"`
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
    Cmd     FeatureID `json:"cmd" yaml:"cmd"`
    Enabled bool       `json:"enabled" yaml:"enabled"`
}

// FeatureState is a functional option used to mutate the feature list.
type FeatureState func([]Feature) []Feature
```

!!! info "Help Configuration"
    `Tool.Help` accepts any value that implements the `errorhandling.HelpConfig` interface (`SupportMessage() string`). Use `props.SlackHelp` or `props.TeamsHelp` for built-in support channel messages, or pass `nil` for no help message. The field is set programmatically. It is not read from YAML/JSON config files.

#### Bootstrap Policy

`Tool.Bootstrap` groups config-bootstrap lifecycle policy. Its zero value reproduces the historical behaviour: when the `init` feature is enabled, a missing configuration file is a hard error ("please run init"). Because the root bootstrap always runs first (see [command bootstrap ordering](../concepts/feature-setup.md)), that error aborts the invocation before any subcommand's own `PreRunE` can run, so these two opt-ins let a tool control first-run behaviour instead.

```go
type BootstrapPolicy struct {
    // AutoInitialise runs a non-interactive init (writing the default config)
    // when no config file is found, then continues — instead of erroring.
    AutoInitialise bool `json:"auto_initialise,omitempty" yaml:"auto_initialise,omitempty"`

    // SkipConfigCheck lists commands whose missing-config hard-fail is relaxed
    // to a tolerant load (embedded defaults), so the command's own PreRunE can
    // manage bootstrap. An entry matches a command by its Name() or full
    // CommandPath() ("studio" or "app studio").
    SkipConfigCheck []string `json:"skip_config_check,omitempty" yaml:"skip_config_check,omitempty"`
}
```

Neither knob skips the framework bootstrap itself (config load, telemetry, update check). They relax only the missing-config *outcome*, so `props.Config` is always populated. A command may alternatively be marked robustly (rename-safe) with the `setup.SkipConfigCheck(cmd)` annotation instead of naming it in the list; either mechanism relaxes the gate. `SkipConfigCheck` takes precedence over `AutoInitialise` for a given command. See [Auto-initialise configuration on first run](../../how-to/auto-initialise-config.md).

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
p.Tool.Help = props.SlackHelp{
    Channel: "#support",
    Team:    "myteam",
}
```

### Version Information

Version tracking for updates and display. The `Version` field on `Props` uses the `version.Version` interface from `pkg/version`:


> [!NOTE]
> See [pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/props](https://pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/props) for the full API definition.


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

    p.ErrorHandler = errorhandling.New(logger.ToSlog(l), p.Tool.Help)

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
    // Pin one view per logical operation: every value read from it belongs to
    // the same resolved configuration, and it will not shift under a hot reload
    // mid-read.
    view := props.Config.View()
    dbHost := view.GetString("database.host")
    dbPort := view.GetInt("database.port")

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

### `test.New` (recommended)

The `pkg/props/test` package (public, so tools built on GTB can use it too) distils the common "construct a fully-wired `*props.Props`" pattern into a single call. Every field gets a hermetic, safe default, so the documented invariants (notably non-nil `Collector` and a usable `Config`) hold without hand-assembly:

```go
import "gitlab.com/phpboyscout/go-tool-base/pkg/props/test"

func TestMyCommand(t *testing.T) {
    t.Parallel()

    p := test.New() // all fields wired with safe defaults

    // Override only what the test cares about:
    p = test.New(
        test.WithTool(props.Tool{Name: "mytool", EnvPrefix: "MYTOOL"}),
        test.WithFS(afero.NewMemMapFs()),
    )

    // ... drive code that needs a *props.Props ...
}
```

Defaults applied by `test.New`:

| Field | Default |
|-------|---------|
| `Logger` | `logger.NewNoop()` |
| `FS` | `afero.NewMemMapFs()` (in-memory, isolated) |
| `Collector` | `props.NoopCollector{}` (upholds the non-nil invariant) |
| `ErrorHandler` | `errorhandling.New(...)` with an inert `Exit` and `io.Discard` writer: a `Fatal` under test never terminates the process |
| `Tool` | benign valid metadata (`testtool`, `EnvPrefix: TESTTOOL`, a GitHub `ReleaseSource`) |
| `Version` | deterministic `version.NewInfo("v0.0.0-test", ...)` |
| `Assets` | empty-but-valid `props.NewAssets()` |
| `Config` | writable `*config.Store` over an empty in-memory file: reads via `.View()`, `Apply` is safe |

Each call returns a fresh, independent instance with no real filesystem, network, keychain or `os.Exit` side effects, so it is safe under `t.Parallel()`. Override options are: `WithTool`, `WithLogger`, `WithFS`, `WithCollector`, `WithVersion`, `WithAssets`, `WithConfig`, and `WithErrorHandler`.

### Manual construction

For full control you can still assemble a `Props` literal directly. Remember to set `Collector: props.NoopCollector{}` so the non-nil invariant holds:

```go
func createTestProps() *props.Props {
    l := logger.NewNoop()
    memFs := afero.NewMemMapFs()

    return &props.Props{
        Tool: props.Tool{
            Name:    "test-tool",
            Summary: "Test tool",
        },
        Logger:    l,
        FS:        memFs,
        Version:   version.NewInfo("0.0.0-test", "", ""),
        Collector: props.NoopCollector{},
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
p.Tool.Help = props.SlackHelp{Team: "Platform", Channel: "#help"}
p.ErrorHandler = errorhandling.New(logger.ToSlog(l), p.Tool.Help)
```

The Props component provides a robust foundation for building maintainable and testable CLI applications with GTB.
