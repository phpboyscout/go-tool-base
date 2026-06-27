---
title: Sources & Precedence
description: How file, embedded, environment, and dotenv sources merge, and the precedence order.
date: 2026-02-16
tags: [components, config, configuration, viper]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Configuration Sources & Precedence

## Configuration Sources

### 1. File-Based Configuration

Load configuration from YAML files using the simplified `Load` function or create containers directly:

```go
// Using the convenience Load function
fs := afero.NewOsFs()
paths := []string{"config.yaml", "config.yml", "/etc/myapp/config.yaml"}

container, err := config.Load(paths, fs, false,
    config.WithLogger(l),
)
if err != nil {
    log.Fatal(err)
}

// Or create a Container directly
container := config.NewFilesContainer(fs,
    config.WithLogger(l),
    config.WithConfigFiles("config.yaml", "local.yaml"),
)
```

**Example config.yaml:**
```yaml
app:
  name: "my-application"
  debug: false
  port: 8080

database:
  host: "localhost"
  port: 5432
  name: "myapp"
  timeout: "30s"

features:
  - "auth"
  - "logging"
  - "metrics"
```

### 2. Embedded Configuration

Load configuration from embedded files (useful for default configurations). The library supports loading and merging configurations from multiple embedded filesystem instances.

!!! warning "Naming Convention & Path Requirement"
    For automated configuration loading and merging (especially during `init`), the library expects the following structure within your `embed.FS`:

    *   **Path**: `assets/init/config.yaml`
    *   **Embed Directive**: `//go:embed assets/*` (ensure all subdirectories are included)

### Root Command Integration

When building a modular CLI where each subcommand manages its own configuration, you should collect all assets into a slice and pass them to the root command creator:

```go
// pkg/cmd/root/root.go

//go:embed assets/*
var assets embed.FS

func NewCmdRoot(props *props.Props) *cobra.Command {
    // 1. Initialize subcommands and collect their assets
    trainCmd, trainAssets := train.NewCmdTrain(props)
    kubeCmd, kubeAssets := kube.NewCmdKube(props)

    // 2. Aggregate all assets (root assets + subcommand assets)
    allAssets := []embed.FS{assets}
    for _, a := range []*embed.FS{trainAssets, kubeAssets} {
        if a != nil {
            allAssets = append(allAssets, *a)
        }
    }

    // 3. Create the root command with the full slice of assets
    // This allows the configuration system to search across ALL modules
    rootCmd := root.NewCmdRoot(props, allAssets)

    // 4. Add the subcommands to the root
    rootCmd.AddCommand(trainCmd)
    rootCmd.AddCommand(kubeCmd)

    return rootCmd
}
```

The library searches all provided assets for the `assets/init/config.yaml` path and merges them together during both application startup and the `init` command process.

### Project-local config layer (`.<tool>.yaml`)

At startup the root command also looks for a **project-local** config file named
`.<tool>.yaml` (e.g. `.myapp.yaml`), discovered by **walking up from the working
directory** to the filesystem root — a repo-root convention like `.editorconfig`. When
found, it is merged **last among the config files**, so it **deep-merges over and
overrides the global** `~/.<tool>/config.yaml`:

```
~/.myapp/config.yaml          # global, per-user
/path/to/repo/.myapp.yaml     # project — overrides the global (committed with the repo)
```

This lets a project keep its own config (themes, providers, non-secret settings) in its
repo — "config lives in the owning project". A tool **opts out simply by not having the
file**; it never errors when absent. Environment variables and flags still override the
project layer (it sits in the *file* tier of the precedence below). The filename derives
from `Props.Tool.Name`, so it is automatic and tool-specific.

### 3. Environment Variable Integration

The Container automatically handles environment variables: viper's `AutomaticEnv` is enabled and GTB installs an env-key replacer so the `.` separator maps to `_`:

```go
// Environment variables are automatically mapped
// For config key "database.host", environment variable "DATABASE_HOST" is checked
// GTB's env-key replacer maps the "." separator to "_" (viper has no default replacer)

container := config.NewFilesContainer(fs,
    config.WithLogger(l),
    config.WithConfigFiles("config.yaml"),
)

// This will check DATABASE_HOST environment variable
host := container.GetString("database.host")
```

### Environment Variable Prefix

By default, config keys map directly to environment variable names (e.g., `ai.provider` resolves from `AI_PROVIDER`). When `WithEnvPrefix` is set, all environment variable lookups are prefixed to prevent config pollution in shared environments:

```go
container := config.NewFilesContainer(fs,
    config.WithLogger(l),
    config.WithEnvPrefix("GTB"),
    config.WithConfigFiles("config.yaml"),
)

// With prefix "GTB", config key "ai.provider" resolves from GTB_AI_PROVIDER
// instead of AI_PROVIDER
provider := container.GetString("ai.provider")
```

The prefix is typically set via `Props.Tool.EnvPrefix` at the root command level, which propagates it to all configuration containers created during command execution. This is especially useful when multiple CLI tools share the same host environment and would otherwise collide on generic variable names like `LOG_LEVEL` or `AI_PROVIDER`.

### 4. Local Dotenv Support

For local development, the configuration system automatically looks for and loads environment variables from a `.env` file in the current working directory.

!!! tip "Local Overrides"
    The `.env` loader is initialized automatically by every `Container`. This is the recommended way to manage local API keys and tokens without modifying your `config.yaml`.

### 5. Configuration Merging

Combine multiple configuration files with automatic merging:

```go
// Multiple files are merged in order, with later files taking precedence
container := config.NewFilesContainer(fs,
    config.WithLogger(l),
    config.WithConfigFiles(
        "defaults.yaml",    // Base configuration
        "config.yaml",      // Environment-specific
        "local.yaml",       // Local overrides
    ),
)

// The Load function also supports merging from multiple discovered files
paths := []string{"config.yaml", "config.local.yaml", "/etc/myapp/config.yaml"}
container, err := config.Load(paths, fs, false,
    config.WithLogger(l),
)
```

## Usage Examples

### Basic Value Access

```go
// Simple value access
appName := container.GetString("app.name")
debugMode := container.GetBool("app.debug")
port := container.GetInt("app.port")

// Type conversion with automatic handling
timeout := container.GetDuration("database.timeout") // "30s" -> 30 * time.Second
startTime := container.GetTime("app.start_time")
maxSize := container.GetFloat("cache.max_size")

// Check if a key exists
if container.Has("feature.experimental") {
    experimental := container.GetBool("feature.experimental")
    // Handle experimental feature
}
```

### Hierarchical Configuration

```go
// Access nested configuration sections using Sub()
dbConfig := container.Sub("database")
if dbConfig != nil {
    host := dbConfig.GetString("host")
    port := dbConfig.GetInt("port")
    name := dbConfig.GetString("name")

    connectionString := fmt.Sprintf("%s:%d/%s", host, port, name)
}

// Sub returns a new Containable for the nested section
cacheConfig := container.Sub("cache")
redisConfig := cacheConfig.Sub("redis") // Nested: cache.redis.*
```

#### Environment-Aware `Sub()`

Viper's native `Sub()` returns a detached `*viper.Viper` holding a **snapshot** of the sub-tree's data. Later root-level `Set` calls, file reloads, and the full multi-source precedence chain don't propagate into that detached copy, and a write-back (`WriteConfigAs`) targets the snapshot rather than the live configuration. (Recent viper does propagate `AutomaticEnv` + `SetEnvPrefix` into the sub-viper, so prefixed env lookups still resolve — but the data is still a point-in-time copy.)

The GTB `Container.Sub()` avoids that trap. The returned view:

1. Keeps a **structural view** — Viper's own `Sub` sub-tree — used for `WriteConfigAs`, `Dump`, `ToJSON`, and `Validate` so those operations remain scoped to the sub-path.
2. Tracks the **root container** and an accumulated dot-prefix, and routes every `Get*`, `Set`, `Has`, and `IsSet` call through the root's Viper with a qualified key path. Root-level writes, hot reloads, and the full precedence chain — including `AutomaticEnv` + prefix binding — stay live no matter how many `Sub()` layers a caller walks.

```go
// With env prefix "MYTOOL" and GTB_GITHUB_AUTH_VALUE=ghp_xxx in env:
github := cfg.Sub("github")
github.GetString("auth.value")  // -> "ghp_xxx" (via AutomaticEnv)

// Nested Sub accumulates the full prefix:
bitbucket := cfg.Sub("bitbucket")
auth := bitbucket.Sub("auth")
auth.GetString("token")         // qualifies to "bitbucket.auth.token"
```

`Sub()` still returns `nil` when the key is absent from the entire config hierarchy (file, defaults, flags) — existing `if sub != nil` guards continue to work. The env-aware delegation only kicks in for sub-containers that were returned non-nil.

**When this matters:** any code that passes `cfg.Sub(...)` into a resolver — `pkg/vcs/auth.ResolveToken`, `pkg/vcs/github.NewGitHubClient`, `pkg/vcs/bitbucket.resolveCredentials`, `pkg/setup/update.requireReleaseToken` — benefits automatically. A prefixed env var set by the user (e.g. `MYTOOL_GITHUB_AUTH_VALUE`, `MYTOOL_GITLAB_AUTH_VALUE`) is honoured without any caller changes.

Configuration works seamlessly with Cobra flags:

```go
func NewDatabaseCommand(props *props.Props) *cobra.Command {
    var dbHost string

    cmd := &cobra.Command{
        Use:   "database",
        Short: "Database operations",
        RunE: func(cmd *cobra.Command, args []string) error {
            // Flag takes precedence over config
            if dbHost == "" {
                dbHost = props.Config.GetString("database.host")
            }

            props.Logger.Info("Connecting to database", "host", dbHost)
            return nil
        },
    }

    cmd.Flags().StringVar(&dbHost, "db-host", "", "Database host")

    return cmd
}
```

The manual `if dbHost == ""` dance above is only needed when a flag is **not** bound to a config key. The recommended approach is to bind the flag so `props.Config.Get*` resolves precedence for you — see [Binding CLI flags to config](#binding-cli-flags-to-config).

### Binding CLI flags to config

GTB documents a configuration precedence of **flags > env > file > embedded > defaults**. For a CLI flag to participate in that precedence, it must be *bound* to a configuration key. Binding wires the flag into viper via `BindPFlag` during config load, after the file and env layers are established, so viper's native order (`BindPFlag` above `AutomaticEnv`) yields the documented result.

Register bound flags on the root command using the `RootOption`s:

```go
portFlags := pflag.NewFlagSet("server", pflag.ContinueOnError)
portFlags.Int("server-port", 8080, "server port")

rootCmd := root.NewCmdRootWithOptions(props,
    // Explicit map: config key -> flag.
    root.WithBoundFlags(map[string]*pflag.Flag{
        "server.port": portFlags.Lookup("server-port"),
    }),
)
```

For zero-boilerplate binding, use the convention helper, which derives the config key from the flag name by replacing hyphens with dots (`--server-port` → `server.port`):

```go
rootCmd := root.NewCmdRootWithOptions(props,
    root.WithConventionBoundFlags(portFlags), // binds every flag in the set
)
```

Both options register the supplied flags on the root command's persistent flag set, so cobra parses them and GTB binds them during the pre-run.

**Per-command flags** are bound automatically: a subcommand's own *local* flags are mapped by the same hyphen-to-dot convention when that command runs, so `mytool serve --server-port 9090` overrides `server.port` for the `serve` command's `RunE`.

**Only changed flags are bound.** A flag the user did *not* set on the command line is filtered out (`flag.Changed == false`) and never overrides config, so an unset flag's default never masks file or env values.

The built-in `--debug` and `--ci` flags are folded through the same binding path, so `Config.GetBool("ci")` reflects `--ci`. `--debug` additionally retains its immediate effect on the log level.

To bind a flag directly onto a container (advanced; the options above are preferred), use `Containable.BindPFlag`:

```go
// key, flag — bind only when flag.Changed is true.
if flag.Changed {
    _ = props.Config.BindPFlag("server.port", flag)
}
```
