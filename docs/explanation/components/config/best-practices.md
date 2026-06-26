---
title: Best Practices & Integration
description: Recommended patterns, GTB integration, initialisers, and sensitive-value masking.
date: 2026-02-16
tags: [components, config, configuration, viper]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Best Practices & Integration

## Best Practices

### 1. Use Concrete Types in Production

- Use `*config.Container` for production configuration management
- Use `config.Containable` interface for testing and dependency injection
- Reserve the interface for mocking and testing scenarios

### 2. Configuration Loading Strategy

```go
// Recommended: Use multiple configuration files with precedence
func setupConfiguration(l logger.Logger, fs afero.Fs) (*config.Container, error) {
    // Load in order of precedence (later files override earlier ones)
    container := config.NewFilesContainer(fs,
        config.WithLogger(l),
        config.WithConfigFiles(
            "defaults.yaml",      // Base defaults
            "config.yaml",        // Environment configuration
            "local.yaml",         // Local overrides
        ),
    )

    return container, validateConfig(container)
}
```

### 3. Error Handling

- Always validate required configuration keys
- Provide meaningful error messages for missing or invalid configuration
- Use the `Has()` method to check for optional configuration

### 4. Observer Pattern Usage

```go
// Use observers for configuration-dependent services
func setupConfigWatching(container *config.Container, l logger.Logger) {
    container.AddObserverFunc(func(cfg config.Containable) error {
        // Reconfigure logging level
        if cfg.Has("log.level") {
            newLevel := cfg.GetString("log.level")
            // Update logger configuration
        }

        // Return any error; it is logged by the framework
        if someValidationFails {
            return fmt.Errorf("configuration validation failed")
        }

        return nil
    })
}
```

### 5. Testing Configuration

- Use `NewReaderContainer` for simple test configurations
- Create helper functions for common test configuration setups
- Mock the `Containable` interface for unit tests that need specific configuration behavior

### 6. Environment Variable Integration

```go
// Take advantage of automatic environment variable mapping
// For config key "database.connection.host"
// Environment variable "DATABASE_CONNECTION_HOST" will be checked automatically

func getDatabaseConfig(cfg config.Containable) DatabaseConfig {
    return DatabaseConfig{
        Host:     cfg.GetString("database.connection.host"),     // Checks DATABASE_CONNECTION_HOST
        Port:     cfg.GetInt("database.connection.port"),        // Checks DATABASE_CONNECTION_PORT
        Database: cfg.GetString("database.connection.database"), // Checks DATABASE_CONNECTION_DATABASE
        Username: cfg.GetString("database.connection.username"), // Checks DATABASE_CONNECTION_USERNAME
        Password: cfg.GetString("database.connection.password"), // Checks DATABASE_CONNECTION_PASSWORD
    }
}
```

### 7. Configuration Debugging

```go
// Add debugging support for configuration issues
func debugConfiguration(cfg *config.Container, l logger.Logger) {
    if cfg.GetBool("debug.config") {
        l.Info("Current configuration:", "config", cfg.ToJSON())

        // Log observer count
        observers := cfg.GetObservers()
        l.Info("Configuration observers registered", "count", len(observers))
    }
}
```

## Integration with GTB

The configuration component integrates seamlessly with other GTB components:

```go
// In your Props setup
func setupProps() (*props.Props, error) {
    l := logger.NewCharm(os.Stderr)
    fs := afero.NewOsFs()

    // Load configuration
    cfg := config.NewFilesContainer(fs,
        config.WithLogger(l),
        config.WithEnvPrefix("MYAPP"),
        config.WithConfigFiles("config.yaml"),
    )

    // Create Props with configuration
    p := &props.Props{
        Config: cfg,
        Logger: l,
        FS:     fs,
    }

    return p, nil
}
```

This configuration component provides the foundation for all other GTB components, offering consistent configuration access patterns while maintaining excellent testability through the abstraction layer over viper.


### 2. Feature Flags

```yaml
# config.yaml
features:
  auth: true
  telemetry: false
  experimental_ui: true
```

```go
func isFeatureEnabled(cfg config.Containable, feature string) bool {
    return cfg.GetBool("features." + feature)
}

func requireFeature(cfg config.Containable, feature string) error {
    if !isFeatureEnabled(cfg, feature) {
        return fmt.Errorf("feature '%s' is not enabled", feature)
    }
    return nil
}
```

### 3. Configuration Sections

```go
type DatabaseConfig struct {
    Host     string        `yaml:"host"`
    Port     int           `yaml:"port"`
    Name     string        `yaml:"name"`
    Timeout  time.Duration `yaml:"timeout"`
}

func loadDatabaseConfig(cfg config.Containable) (*DatabaseConfig, error) {
    dbSection := cfg.Sub("database")
    if dbSection == nil {
        return nil, fmt.Errorf("database configuration section not found")
    }

    return &DatabaseConfig{
        Host:    dbSection.GetString("host"),
        Port:    dbSection.GetInt("port"),
        Name:    dbSection.GetString("name"),
        Timeout: dbSection.GetDuration("timeout"),
    }, nil
}
```

## Configuration Key Conventions

### 1. Configuration Keys

Use consistent, hierarchical naming:

```yaml
# Good: Hierarchical and descriptive
app:
  name: "myapp"
  server:
    port: 8080
    timeout: "30s"
database:
  connection:
    host: "localhost"
    port: 5432

# Avoid: Flat, unclear naming
appname: "myapp"
serverport: 8080
dbhost: "localhost"
```

### 2. Default Values

Always provide sensible defaults:

```go
func getConfigWithDefaults(cfg config.Containable) Config {
    return Config{
        Port:           cfg.GetInt("app.port"),              // 0 if not set
        Timeout:        cfg.GetDuration("server.timeout"),   // 0 if not set
        MaxConnections: max(cfg.GetInt("server.max_connections"), 100), // Default to 100
    }
}
```

### 3. Type Safety

Use specific getter methods for type safety:

```go
// Good: Type-safe access
port := cfg.GetInt("app.port")
timeout := cfg.GetDuration("server.timeout")
enabled := cfg.GetBool("feature.enabled")

// Avoid: Generic access requiring type assertions
port := cfg.Get("app.port").(int) // Panic if wrong type
```

### 4. Error Handling

Handle missing configuration gracefully:

```go
func setupService(cfg config.Containable) (*Service, error) {
    host := cfg.GetString("service.host")
    if host == "" {
        return nil, fmt.Errorf("service.host is required")
    }

    port := cfg.GetInt("service.port")
    if port == 0 {
        port = 8080 // Default
    }

    return NewService(host, port), nil
}
```

The Configuration component provides a robust and flexible foundation for managing application settings in GTB applications.


## Initialiser Integration

The `config.Containable` interface is also the standard for [Tool Initialisers](../setup/initialisers.md). When creating a custom initialiser, you will use this interface to check for existing configuration (`IsConfigured`) and to write new values (`Set`), ensuring a consistent API across the entire lifecycle of the application.

## Sensitive Value Masking

The masking system uses two independent strategies:

1. **Key-name matching** — checks the leaf segment of the dotted key path against known
   patterns: `token`, `password`, `secret`, `key`, `apikey`, `auth`.
2. **Value-content matching** — checks the value against known token regexps (e.g. GitHub
   PATs: `ghp_...`, `github_pat_...`). This covers cases like `github.auth.value` where
   the key name `value` is not sensitive but the content may be a token.

### Custom patterns

Tool authors can extend the masker via functional options on `NewCmdConfig`:

```go
import (
    cmdconfig "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/config"
    "regexp"
)

cmdconfig.NewCmdConfig(props,
    cmdconfig.WithKeyPattern("credential"),
    cmdconfig.WithValuePattern(regexp.MustCompile(`^sk-[A-Za-z0-9]{32}$`)),
)
```

## Relationship with `init`

| Workflow | Command |
| :--- | :--- |
| First-run bootstrap | `init` |
| Re-configure a subsystem interactively | `init <subsystem>` (e.g. `init ai`, `init github`) |
| Read a single value in a script or CI | `config get <key>` |
| Write a single value in a script or CI | `config set <key> <value>` |
| Remove a single value | `config unset <key>` |
| Find where config actually lives | `config path` |
| Hand-edit the file safely (re-validated) | `config edit` |
| Inspect all resolved config | `config list` |
| Validate config against schema | `config validate` |

Both `InitCmd` and `ConfigCmd` should be disabled in containerized services where local
YAML config is not applicable.

## Implementation

- **`pkg/cmd/config/`** — Command implementations (`get`, `set`, `unset`, `list`, `path`,
  `edit`, `validate`)
- **`pkg/cmd/config/sensitive.go`** — `Masker` type with dual-strategy detection
- **`pkg/config` `Container.ConfigFiles()`** — ordered list of contributing files backing
  `config path` (added in v0.22; see the [migration note](../../../reference/migration/v0.21-config-files-accessor.md))
- Feature flag: `props.ConfigCmd` (default: disabled)
