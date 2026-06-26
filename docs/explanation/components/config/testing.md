---
title: Testing & Mocking
description: Build test containers, use generated mocks, and test observer behaviour.
date: 2026-02-16
tags: [components, config, configuration, viper]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Testing & Mocking

## Testing and Mocking

One of the primary benefits of the config package is enhanced testability. Unlike viper, which is difficult to mock, the `Containable` interface enables comprehensive testing strategies.

### Creating Test Configurations

```go
func TestMyFunction(t *testing.T) {
    // Create in-memory configuration for testing
    fs := afero.NewMemMapFs()

    // Using a YAML string for test config
    testConfigYAML := `
app:
  name: "test-app"
  debug: true
  port: 8080
database:
  host: "localhost"
  port: 5432
  name: "testdb"
`

    container := config.NewReaderContainer(fs,
        config.WithConfigFormat("yaml"),
        config.WithConfigReaders(strings.NewReader(testConfigYAML)),
    )

    // Test your function with the test configuration
    result := MyFunctionThatNeedsConfig(container)
    assert.Equal(t, "expected", result)
}
```

### Mock Configuration Interface

The GTB library includes auto-generated mocks using [mockery](https://github.com/vektra/mockery). **Use these provided mocks instead of creating manual implementations:**

```go
import (
    "testing"

    "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/config"
    "github.com/stretchr/testify/assert"
)

func TestWithProvidedMocks(t *testing.T) {
    // Use the auto-generated mock
    mockConfig := config.NewMockContainable(t)

    // Set up expectations
    mockConfig.EXPECT().GetString("database.host").Return("test-host")
    mockConfig.EXPECT().GetInt("database.port").Return(5432)
    mockConfig.EXPECT().GetString("database.name").Return("testdb")
    mockConfig.EXPECT().Has("database.ssl").Return(true)
    mockConfig.EXPECT().GetBool("database.ssl").Return(false)

    // Test your function
    service := NewDatabaseService(mockConfig)
    err := service.Connect()
    assert.NoError(t, err)

    // Expectations are automatically verified on cleanup
}

func TestConfigSubSection(t *testing.T) {
    mockConfig := config.NewMockContainable(t)
    mockSubConfig := config.NewMockContainable(t)

    // Mock Sub() method to return another mock
    mockConfig.EXPECT().Sub("database").Return(mockSubConfig)
    mockSubConfig.EXPECT().GetString("host").Return("localhost")
    mockSubConfig.EXPECT().GetInt("port").Return(5432)

    // Use the mocked configuration
    dbConfig := mockConfig.Sub("database")
    host := dbConfig.GetString("host")
    port := dbConfig.GetInt("port")

    assert.Equal(t, "localhost", host)
    assert.Equal(t, 5432, port)
}
```

### Available Generated Mocks

The library provides the following auto-generated mocks in the `mocks/config` package:

- **`MockContainable`** - Mock implementation of the `Containable` interface
- **`MockObservable`** - Mock implementation of the `Observable` interface
- **`MockEmbeddedFileReader`** - Mock implementation of the `EmbeddedFileReader` interface

**Benefits of Using Provided Mocks:**

- **Type Safety**: Automatically generated from the actual interfaces
- **Comprehensive**: All interface methods are properly mocked
- **Test Integration**: Built-in support for testify assertions and cleanup
- **Maintenance**: Updated automatically when interfaces change

### Testing Observer Behavior

Testing observers is important because they often contain critical business logic that responds to configuration changes. Since observers in production are triggered by filesystem changes, testing requires special approaches.

#### Why Test Observers?

- **Critical Logic**: Observers often restart services, update logging levels, or reconfigure security settings
- **Error Handling**: Observers signal configuration validation errors via the returned `error`
- **Direct invocation**: Observers can be exercised by calling `Run(cfg)` directly — no file watching required

#### Testing Strategies

**1. Testing Observer Logic with Mock Configurations:**

```go
import (
    "testing"

    "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/config"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestLogLevelObserver(t *testing.T) {
    mockConfig := config.NewMockContainable(t)
    mockConfig.EXPECT().GetString("log.level").Return("debug")

    observerCalled := false

    observer := &LogLevelObserver{
        onLevelChange: func(level string) {
            observerCalled = true
            assert.Equal(t, "debug", level)
        },
    }

    require.NoError(t, observer.Run(mockConfig))
    assert.True(t, observerCalled)
}
```

**2. Testing Observer Registration and Integration:**

```go
func TestObserverRegistration(t *testing.T) {
    fs := afero.NewMemMapFs()

    container := config.NewReaderContainer(fs,
        config.WithConfigFormat("yaml"),
        config.WithConfigReaders(strings.NewReader(`
log:
  level: "info"
database:
  host: "localhost"
`)),
    )

    observerCalled := false

    container.AddObserverFunc(func(cfg config.Containable) error {
        observerCalled = true

        logLevel := cfg.GetString("log.level")
        if logLevel == "" {
            return errors.New("log level not configured")
        }

        return nil
    })

    // Execute the registered observers directly (file watching is not
    // available for reader containers).
    for _, observer := range container.GetObservers() {
        require.NoError(t, observer.Run(container))
    }

    assert.True(t, observerCalled, "Observer should have been called")
}
```

**3. Testing Observer Error Handling:**

```go
func TestObserverErrorHandling(t *testing.T) {
    fs := afero.NewMemMapFs()

    container := config.NewReaderContainer(fs,
        config.WithConfigFormat("yaml"),
        config.WithConfigReaders(strings.NewReader(`
log:
  level: "invalid_level"
`)),
    )

    container.AddObserverFunc(func(cfg config.Containable) error {
        logLevel := cfg.GetString("log.level")
        validLevels := []string{"debug", "info", "warn", "error"}

        if !slices.Contains(validLevels, logLevel) {
            return fmt.Errorf("invalid log level '%s', must be one of: %v", logLevel, validLevels)
        }

        return nil
    })

    var gotErr error
    for _, observer := range container.GetObservers() {
        if err := observer.Run(container); err != nil {
            gotErr = err
        }
    }

    require.Error(t, gotErr)
    assert.Contains(t, gotErr.Error(), "invalid log level")
}
```

**4. Testing Custom Observer Implementation:**

```go
// Example custom observer for testing
type TestServiceRestarter struct {
    restartCalled bool
    serviceName   string
}

func (t *TestServiceRestarter) Run(cfg config.Containable) error {
    if cfg.Has("service.restart_required") && cfg.GetBool("service.restart_required") {
        t.restartCalled = true
        if t.serviceName == "" {
            return errors.New("service name not configured")
        }
    }

    return nil
}

func TestCustomObserver(t *testing.T) {
    mockConfig := config.NewMockContainable(t)
    mockConfig.EXPECT().Has("service.restart_required").Return(true)
    mockConfig.EXPECT().GetBool("service.restart_required").Return(true)

    observer := &TestServiceRestarter{serviceName: "test-service"}

    require.NoError(t, observer.Run(mockConfig))
    assert.True(t, observer.restartCalled)
}
```

#### Best Practices for Testing Observers

1. **Test Observer Logic Separately**: Test the business logic within observers using mock configurations
2. **Test Error Handling**: Ensure observers properly report validation and runtime errors
3. **Test Concurrency**: Observers run concurrently, so test with multiple observers
4. **Mock Dependencies**: Use mock configurations to control test scenarios
5. **Verify Side Effects**: Test that observers actually perform their intended actions (logging, service restarts, etc.)

## Debugging and Introspection

### Configuration Debugging

The Container provides methods for inspecting configuration state, which is crucial when values aren't loading as expected.

#### Inspecting Loaded Values

```go
// Print all configuration values as JSON to stdout (great for quick debugging)
container.Dump(os.Stdout)

// Get configuration as JSON string for structured logging
configJSON := container.ToJSON()
l.Info("Current configuration", "config", configJSON)
```

#### Verifying Sources

If you aren't sure where a value is coming from (File vs Env vs Flag):

1.  **Flags** have the highest precedence.
2.  **Environment Variables** come next.
3.  **Configuration Files** are updated in the order they were loaded (later files override earlier ones).

To debug, you can inspect the underlying Viper instance:

```go
// Access underlying viper for advanced operations
viper := container.GetViper()
allSettings := viper.AllSettings()
```

For general runtime issues, see the [Troubleshooting Guide](../../../development/troubleshooting.md).

### Configuration Validation

For schema-based validation, see the [Schema Validation](#schema-validation) section above. For simple ad-hoc checks:

```go
func validateConfig(cfg config.Containable) error {
    if !cfg.Has("app.name") {
        return fmt.Errorf("required configuration key 'app.name' is missing")
    }

    port := cfg.GetInt("database.port")
    if port < 1 || port > 65535 {
        return fmt.Errorf("database.port must be between 1 and 65535, got %d", port)
    }

    return nil
}
```

## Containable Interface (For Testing and Mocking)

The `Containable` interface is primarily used for testing and when working with provided mocks. In production code, use the concrete `Container` type:


> [!NOTE]
> See [pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/config](https://pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/config) for the full API definition.
