---
title: How to Test Code That Uses Configuration
description: Build in-memory test containers, use the generated config mocks, and test observers.
date: 2026-02-16
tags: [how-to, config, testing, mocks, observers]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# How to Test Code That Uses Configuration

The `pkg/config` package is built for testability: unlike a bare `*viper.Viper`,
the `Containable` interface can be mocked, and reader-backed containers let you
feed config from an in-memory string. This guide covers the common recipes. For
general test scaffolding (test `Props`, filesystem mocking, race avoidance), see
[How to Test Components](testing.md); for the design, see the
[Config component](../explanation/components/config/index.md).

## Build a test configuration in memory

Use `NewReaderContainer` with a YAML string — no files on disk:

```go
func TestMyFunction(t *testing.T) {
    fs := afero.NewMemMapFs()

    testConfigYAML := `
app:
  name: "test-app"
  debug: true
  port: 8080
database:
  host: "localhost"
  port: 5432
`

    container := config.NewReaderContainer(fs,
        config.WithConfigFormat("yaml"),
        config.WithConfigReaders(strings.NewReader(testConfigYAML)),
    )

    result := MyFunctionThatNeedsConfig(container)
    assert.Equal(t, "expected", result)
}
```

## Use the generated mocks

GTB ships auto-generated mocks (via [mockery](https://github.com/vektra/mockery))
in `mocks/pkg/config`. **Prefer these over hand-written fakes** — they are
generated from the real interfaces (`MockContainable`, `MockObservable`,
`MockEmbeddedFileReader`), so they stay in sync and verify expectations on
cleanup.

```go
import (
    "testing"

    "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/config"
    "github.com/stretchr/testify/assert"
)

func TestWithProvidedMocks(t *testing.T) {
    mockConfig := config.NewMockContainable(t)

    mockConfig.EXPECT().GetString("database.host").Return("test-host")
    mockConfig.EXPECT().GetInt("database.port").Return(5432)
    mockConfig.EXPECT().Has("database.ssl").Return(true)
    mockConfig.EXPECT().GetBool("database.ssl").Return(false)

    service := NewDatabaseService(mockConfig)
    assert.NoError(t, service.Connect())
    // Expectations are verified automatically on cleanup.
}
```

Mock a nested section by returning another mock from `Sub()`:

```go
func TestConfigSubSection(t *testing.T) {
    mockConfig := config.NewMockContainable(t)
    mockSub := config.NewMockContainable(t)

    mockConfig.EXPECT().Sub("database").Return(mockSub)
    mockSub.EXPECT().GetString("host").Return("localhost")

    assert.Equal(t, "localhost", mockConfig.Sub("database").GetString("host"))
}
```

## Test observer behaviour

Observers often carry critical logic (restarting services, changing log levels)
and signal validation errors via their returned `error`. You don't need file
watching — invoke `Run(cfg)` directly.

**Observer logic, with a mock config:**

```go
func TestLogLevelObserver(t *testing.T) {
    mockConfig := config.NewMockContainable(t)
    mockConfig.EXPECT().GetString("log.level").Return("debug")

    called := false
    observer := &LogLevelObserver{
        onLevelChange: func(level string) { called = true; assert.Equal(t, "debug", level) },
    }

    require.NoError(t, observer.Run(mockConfig))
    assert.True(t, called)
}
```

**Registration + integration, with a reader container:**

```go
func TestObserverRegistration(t *testing.T) {
    container := config.NewReaderContainer(afero.NewMemMapFs(),
        config.WithConfigFormat("yaml"),
        config.WithConfigReaders(strings.NewReader("log:\n  level: \"info\"\n")),
    )

    called := false
    container.AddObserverFunc(func(cfg config.Containable) error {
        called = true
        if cfg.GetString("log.level") == "" {
            return errors.New("log level not configured")
        }
        return nil
    })

    // Reader containers don't watch files — run the observers directly.
    for _, observer := range container.GetObservers() {
        require.NoError(t, observer.Run(container))
    }
    assert.True(t, called, "observer should have been called")
}
```

**Error handling** — assert the observer rejects bad config:

```go
func TestObserverErrorHandling(t *testing.T) {
    container := config.NewReaderContainer(afero.NewMemMapFs(),
        config.WithConfigFormat("yaml"),
        config.WithConfigReaders(strings.NewReader("log:\n  level: \"invalid_level\"\n")),
    )

    container.AddObserverFunc(func(cfg config.Containable) error {
        valid := []string{"debug", "info", "warn", "error"}
        if level := cfg.GetString("log.level"); !slices.Contains(valid, level) {
            return fmt.Errorf("invalid log level %q, must be one of: %v", level, valid)
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

## Inspect and debug configuration in a test

When values aren't resolving as expected, dump the live state:

```go
container.Dump(os.Stdout)            // all values as JSON to stdout
configJSON := container.ToJSON()     // JSON string for structured logging

// Drop to the underlying viper for advanced introspection:
allSettings := container.GetViper().AllSettings()
```

Resolution precedence when a value surprises you: **flags → env → config files
(later files override earlier) → defaults**. For schema-based validation see
[Schema Validation](../explanation/components/config/validation.md); for general
runtime issues see the [Troubleshooting Guide](../development/troubleshooting.md).

## Related

- [How to Test Components](testing.md) — test `Props`, filesystem mocking, race avoidance
- [Config component](../explanation/components/config/index.md) — the `Containable`/`Observable` design
- [How to React to Configuration Changes](config-hot-reload.md) — production hot-reload
