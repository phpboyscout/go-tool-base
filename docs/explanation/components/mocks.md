---
title: Mocks Package
description: Auto-generated mock implementations for unit testing, created using Mockery.
date: 2026-02-16
tags: [components, mocks, testing, unit-testing]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Mocks Package

The mocks package provides auto-generated mock implementations of GTB interfaces, created using [Mockery](https://vektra.github.io/mockery/). These mocks enable comprehensive unit testing by allowing developers to simulate and control the behavior of GTB components during testing.

## Overview

The mocks package contains mock implementations for all major GTB interfaces, making it simple to write isolated unit tests without complex setup or external dependencies. All mocks are automatically generated and maintained using Mockery, ensuring they stay synchronized with interface changes.

**Key Benefits:**

- **Simplified Testing**: Mock complex dependencies without setup overhead
- **Behavioral Control**: Precisely control how mocked components behave during tests
- **Isolation**: Test individual components without external dependencies
- **Verification**: Ensure methods are called with expected parameters
- **Auto-Generated**: Always up-to-date with interface changes

## Available Mocks

### Configuration Mocks

Configuration mocks are published by the standalone
[`gitlab.com/phpboyscout/go/config`](https://config.go.phpboyscout.uk) module in its
`mocks` package: `MockReader`, `MockObservable` and `MockBinder`
(the old catch-all container mock is gone along with its interface).

#### **Reader Mock**
`MockReader` fakes `config.Reader`, the read-only surface the GTB adapter
functions accept (a real `*config.View` satisfies it too):

```go
import configmocks "gitlab.com/phpboyscout/go/config/mocks"

func TestSettingsResolution(t *testing.T) {
    cfg := configmocks.NewMockReader(t)

    // Setup expected behavior
    cfg.On("GetString", "app.timeout").Return("30s")
    cfg.On("GetBool", "app.debug").Return(true)

    // Pass to any function taking config.Reader
    result := resolveSettings(cfg)

    assert.NotNil(t, result)
}
```

#### **A real store instead of a mock**
`props.Props.Config` is a concrete `*config.Store`, so it cannot hold a mock.
When code under test needs a whole `Props`, build a real in-memory store, or
use the `pkg/props/test` fixture helper, which wires one for you:

```go
store, err := config.NewStore(t.Context(),
    config.WithReaders(config.NamedSource{Name: "test", Content: []byte("app:\n  debug: true\n")}),
)
require.NoError(t, err)

p := propstest.New(propstest.WithConfig(store))
result := myFunction(p) // reads via p.Config.View().GetBool("app.debug")
```

#### **Binder and Observable Mocks**
`MockBinder` fakes `config.Binder`, the first parameter of the reload-aware
`Observe*` adapters (a real `*config.Store` satisfies it); `MockObservable`
fakes the reload-notification surface. Both follow the same
`configmocks.NewMockX(t)` constructor-plus-expecter pattern as `MockReader`.

### Setup & Props Mocks

GTB generates mocks for its own configuration-adjacent interfaces:

- **`mocks/pkg/setup`**: `MockEditor` (the `setup.Editor` read/write surface an
  initialiser receives during `init`) and `MockInitialiser`.
- **`mocks/pkg/props`**: `MockConfigProvider` (`GetConfig()` returns the
  concrete `*config.Store`), `MockConfigReader` (`GetConfigView()` returns a
  `*config.View`) and `MockConfigFSProvider`.

### Controls Mocks

The controls supervisor has been extracted to the standalone
[`gitlab.com/phpboyscout/go/controls`](https://controls.go.phpboyscout.uk)
module, which ships **no mocks**. To fake a controller in your own tests,
generate a mock of its `Controllable` interface (or a narrower one) yourself, or
drive a real controller with `controls.WithoutSignals()`. See the module's
[testing guide](https://controls.go.phpboyscout.uk/how-to/testing/).

### Version Control Mocks

GTB no longer ships forge (GitHub/GitLab/Gitea/Bitbucket) client mocks. The
forge release and auth/SSH clients were extracted to the standalone
[`gitlab.com/phpboyscout/go/forge`](https://forge.go.phpboyscout.uk) module plus
its per-provider `forge-github`, `forge-gitlab`, `forge-gitea`, and
`forge-bitbucket` modules; each ships its own `mocks` subpackage, fake those
through the owning module's published `mocks` package rather than a GTB mock.

Repository (git) operations were extracted to the standalone
[`gitlab.com/phpboyscout/go/repo`](https://repo.go.phpboyscout.uk) module; fake
those through that module's published `mocks` package rather than a GTB mock.

## Testing Patterns

### Basic Mock Setup

```go
package mypackage_test

import (
    "testing"

    "github.com/stretchr/testify/assert"

    configmocks "gitlab.com/phpboyscout/go/config/mocks"
)

func TestMyFunction(t *testing.T) {
    // Setup a mock for a function that takes config.Reader
    cfg := configmocks.NewMockReader(t)

    // Configure mock behavior
    cfg.On("GetString", "key").Return("value")

    // Run test
    result := MyFunction(cfg)

    // Assertions (NewMockReader registers AssertExpectations via t.Cleanup)
    assert.NotNil(t, result)
}
```

For code that takes a whole `*props.Props`, mock nothing, build a hermetic
fixture with `pkg/props/test` (`propstest.New(...)`), which wires a real
in-memory `*config.Store` alongside a noop logger, in-memory filesystem, and
inert error handler.

### Advanced Mock Configuration

```go
func TestComplexScenario(t *testing.T) {
    mockConfig := configmocks.NewMockReader(t)

    // Multiple return values for different calls
    mockConfig.On("GetString", "database.host").Return("localhost")
    mockConfig.On("GetString", "database.port").Return("5432")
    mockConfig.On("GetBool", "database.ssl").Return(true)

    // Conditional behavior
    mockConfig.On("GetString", "env").Return("test")
    mockConfig.On("GetString", mock.MatchedBy(func(key string) bool {
        return strings.HasPrefix(key, "secret.")
    })).Return("mocked-secret")

    // Error simulation
    mockConfig.On("GetString", "invalid.key").Return("").Maybe()

    // Test your component (NewDatabaseComponent takes a config.Reader)
    component := NewDatabaseComponent(mockConfig)
    err := component.Connect()

    assert.NoError(t, err)
}
```

### Testing Error Conditions

```go
func TestErrorHandling(t *testing.T) {
    mockReader := configmocks.NewMockReader(t)

    // Simulate a lookup that resolves to an unusable value
    mockReader.On("GetString", "database.host").Return("")

    // Test error handling
    component := NewDatabaseComponent(mockReader)
    err := component.Connect()
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "database host not configured")
}
```

## Mock Generation

The mocks are automatically generated using **Mockery v3** (pinned via the
`tool` directive in `go.mod`). The configuration lives in the project's
`.mockery.yml` file:

```yaml
# Mockery v3: `template` replaces v2's `with-expecter` (expecters are always on now).
template: testify
dir: "mocks/{{ .InterfaceDirRelative }}"   # e.g. pkg/setup -> mocks/pkg/setup
filename: "{{.InterfaceName}}.go"
structname: "{{.Mock}}{{.InterfaceName}}"
formatter: goimports
recursive: true
all: true
packages:
  gitlab.com/phpboyscout/go-tool-base/pkg:
    config:
      all: true
      recursive: true
```

`dir` uses `{{ .InterfaceDirRelative }}`, so an interface in `pkg/setup`
generates into `mocks/pkg/setup/` (mirroring the source tree under `mocks/`).
Configuration mocks are not generated here at all, they ship with the
`go/config` module.

### Regenerating Mocks

To regenerate mocks after interface changes:

```bash
# Mockery is pinned as a Go tool (the go.mod `tool` directive) — no separate install.
just mocks        # regenerate every mock (preferred)

# Or invoke the pinned tool directly (reads .mockery.yml):
go tool mockery
```

Mockery v3 selects interfaces from `.mockery.yml`, not CLI flags, the v2
`--dir`/`--name` selectors no longer exist.

## Best Practices

### 1. **Use Descriptive Test Names**
```go
func TestConfigManager_LoadsDefaultValues_WhenNoConfigFile(t *testing.T) {
    // Test implementation
}
```

### 2. **Setup and Teardown**
```go
func setupMockConfig(t *testing.T) *configmocks.MockReader {
    mockConfig := configmocks.NewMockReader(t)

    // Common setup; AssertExpectations is registered via t.Cleanup
    mockConfig.On("GetString", "app.name").Return("test-app")

    return mockConfig
}

func TestMyFeature(t *testing.T) {
    mockConfig := setupMockConfig(t)

    // Test implementation
}
```

### 3. **Test Both Success and Failure Paths**
```go
func TestSettings_Resolve_Success(t *testing.T) {
    mockConfig := configmocks.NewMockReader(t)
    mockConfig.On("GetString", "vcs.provider").Return("github")

    provider, err := ResolveProvider(mockConfig)
    assert.NoError(t, err)
    assert.Equal(t, "github", provider)
}

func TestSettings_Resolve_Failure(t *testing.T) {
    mockConfig := configmocks.NewMockReader(t)
    mockConfig.On("GetString", "vcs.provider").Return("unknown-forge")

    _, err := ResolveProvider(mockConfig)
    assert.Error(t, err)
}
```

### 4. **Use Table-Driven Tests with Mocks**
```go
func TestConfigValidation(t *testing.T) {
    tests := []struct {
        name           string
        configSetup    func(*configmocks.MockReader)
        expectedResult bool
    }{
        {
            name: "valid config",
            configSetup: func(m *configmocks.MockReader) {
                m.On("GetString", "required.field").Return("value")
                m.On("GetBool", "optional.feature").Return(true)
            },
            expectedResult: true,
        },
        {
            name: "missing required field",
            configSetup: func(m *configmocks.MockReader) {
                m.On("GetString", "required.field").Return("")
                m.On("GetBool", "optional.feature").Return(false)
            },
            expectedResult: false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            mockConfig := configmocks.NewMockReader(t)
            tt.configSetup(mockConfig)

            result := ValidateConfig(mockConfig)
            assert.Equal(t, tt.expectedResult, result)
        })
    }
}
```

## Integration with GTB Testing

The mocks integrate seamlessly with GTB components. For testing commands that
use props, `Props.Config` is a concrete `*config.Store`, so feed it real
configuration through an in-memory reader source (or lean on `pkg/props/test`):

```go
func TestMyCommand(t *testing.T) {
    // Real in-memory config store — Props.Config is a concrete *config.Store.
    store, err := config.NewStore(t.Context(),
        config.WithReaders(config.NamedSource{Name: "test", Content: []byte("timeout: 30s\n")}),
    )
    require.NoError(t, err)

    // Create test props (or: propstest.New(propstest.WithConfig(store)))
    testProps := &props.Props{
        Tool: props.Tool{
            Name: "test-tool",
        },
        Config: store,
        Logger: logger.NewNoop(), // Silent logger for tests
    }

    // Test your command — reads resolve via testProps.Config.View()
    cmd := NewMyCommand(testProps)
    err = cmd.Execute()

    assert.NoError(t, err)
}
```

## Summary

The mocks package provides a comprehensive set of auto-generated mock implementations that make testing GTB applications straightforward and reliable. By using these mocks, developers can:

- Write isolated unit tests without complex dependencies
- Control component behavior precisely during testing
- Verify interactions between components
- Test error conditions safely
- Maintain tests that automatically stay current with interface changes

The combination of Mockery's auto-generation and GTB's interface-driven design creates a robust testing foundation that scales with your application's complexity.
