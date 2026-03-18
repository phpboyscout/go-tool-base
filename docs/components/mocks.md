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

Located in `mocks/config/`:

#### **Containable Mock**
Mock implementation of `config.Containable` interface for testing configuration-dependent code:

```go
// Example usage in tests
func TestMyCommand(t *testing.T) {
    mockConfig := &mocks_config.Containable{}

    // Setup expected behavior
    mockConfig.On("GetString", "app.timeout").Return("30s")
    mockConfig.On("GetBool", "app.debug").Return(true)

    // Use in your test
    props := &props.Props{Config: mockConfig}
    result := myFunction(props)

    // Verify expectations
    mockConfig.AssertExpectations(t)
}
```

#### **EmbeddedFileReader Mock**
Mock for embedded file system operations:

```go
func TestAssetReading(t *testing.T) {
    mockReader := &mocks_config.EmbeddedFileReader{}

    mockReader.On("ReadFile", "config.yaml").Return([]byte("test: config"), nil)

    // Test code that reads embedded assets
    content, err := mockReader.ReadFile("config.yaml")
    assert.NoError(t, err)
    assert.Equal(t, "test: config", string(content))

    mockReader.AssertExpectations(t)
}
```

#### **Observable Mock**
Mock for configuration change observer pattern:

```go
func TestConfigObserver(t *testing.T) {
    mockObserver := &mocks_config.Observable{}

    mockObserver.On("Subscribe", mock.AnythingOfType("func()")).Return()
    mockObserver.On("Notify").Return()

    // Test observer registration and notification
    myObserver := func() { /* handle config change */ }
    mockObserver.Subscribe(myObserver)
    mockObserver.Notify()

    mockObserver.AssertExpectations(t)
}
```

### Controls Mocks

Located in `mocks/controls/`:

#### **Controllable Mock**
Mock implementation of `controls.Controllable` interface for testing service lifecycle:

```go
func TestServiceLifecycle(t *testing.T) {
    mockController := &mocks_controls.Controllable{}

    // Setup channel expectations
    mockController.On("Messages").Return(make(chan controls.Message))
    mockController.On("Errors").Return(make(chan error))
    mockController.On("Start").Return()
    mockController.On("Stop").Return()

    // Test service management
    service := NewMyService(mockController)
    service.Start()

    mockController.AssertExpectations(t)
}
```

### Version Control Mocks

Located in `mocks/vcs/`:

#### **GitHubClient Mock**
Mock for GitHub Enterprise API operations:

```go
func TestGitHubOperations(t *testing.T) {
    mockGHClient := &mocks_vcs.GitHubClient{}

    mockRepo := &github.Repository{
        Name: github.String("test-repo"),
        FullName: github.String("org/test-repo"),
    }

    mockGHClient.On("GetRepository", "org", "test-repo").Return(mockRepo, nil)

    // Test GitHub operations
    repo, err := mockGHClient.GetRepository("org", "test-repo")
    assert.NoError(t, err)
    assert.Equal(t, "test-repo", repo.GetName())

    mockGHClient.AssertExpectations(t)
}
```

#### **RepoLike Mock**
Mock for Git repository operations:

```go
func TestGitOperations(t *testing.T) {
    mockRepo := &mocks_vcs.RepoLike{}

    mockRepo.On("Clone", "https://github.com/org/repo.git", "/tmp/repo").Return(nil)
    mockRepo.On("Pull").Return(nil)
    mockRepo.On("GetCurrentBranch").Return("main", nil)

    // Test Git operations
    err := mockRepo.Clone("https://github.com/org/repo.git", "/tmp/repo")
    assert.NoError(t, err)

    branch, err := mockRepo.GetCurrentBranch()
    assert.NoError(t, err)
    assert.Equal(t, "main", branch)

    mockRepo.AssertExpectations(t)
}
```

## Testing Patterns

### Basic Mock Setup

```go
package mypackage_test

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/mock"

    mocks_config "github.com/phpboyscout/gtb/mocks/pkg/config"
    mocks_controls "github.com/phpboyscout/gtb/mocks/pkg/controls"
    "github.com/phpboyscout/gtb/pkg/props"
)

func TestMyFunction(t *testing.T) {
    // Setup mocks
    mockConfig := &mocks_config.Containable{}
    mockController := &mocks_controls.Controllable{}

    // Configure mock behavior
    mockConfig.On("GetString", "key").Return("value")
    mockController.On("Start").Return()

    // Create props with mocks
    testProps := &props.Props{
        Config: mockConfig,
        // ... other props
    }

    // Run test
    result := MyFunction(testProps, mockController)

    // Assertions
    assert.NotNil(t, result)
    mockConfig.AssertExpectations(t)
    mockController.AssertExpectations(t)
}
```

### Advanced Mock Configuration

```go
func TestComplexScenario(t *testing.T) {
    mockConfig := &mocks_config.Containable{}

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

    // Test your component
    component := NewDatabaseComponent(mockConfig)
    err := component.Connect()

    assert.NoError(t, err)
    mockConfig.AssertExpectations(t)
}
```

### Testing Error Conditions

```go
func TestErrorHandling(t *testing.T) {
    mockGHClient := &mocks_vcs.GitHubClient{}

    // Simulate GitHub API error
    mockGHClient.On("GetRepository", "org", "nonexistent").
        Return(nil, errors.New("repository not found"))

    // Test error handling
    _, err := mockGHClient.GetRepository("org", "nonexistent")
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "repository not found")

    mockGHClient.AssertExpectations(t)
}
```

## Mock Generation

The mocks are automatically generated using Mockery. The configuration is maintained in the project's `.mockery.yaml` file:

```yaml
with-expecter: true
dir: "mocks/{{.PackageName}}"
filename: "{{.InterfaceName}}.go"
packages:
  github.com/phpboyscout/gtb/pkg/config:
    interfaces:
      Containable:
      Observable:
      EmbeddedFileReader:
  github.com/phpboyscout/gtb/pkg/controls:
    interfaces:
      Controllable:
  github.com/phpboyscout/gtb/pkg/vcs:
    interfaces:
      GitHubClient:
      RepoLike:
```

### Regenerating Mocks

To regenerate mocks after interface changes:

```bash
# Install mockery if not already installed
go install github.com/vektra/mockery/v2@latest

# Generate all mocks
mockery

# Or generate mocks for specific package
mockery --dir=./pkg/config --name=Containable
```

## Best Practices

### 1. **Use Descriptive Test Names**
```go
func TestConfigManager_LoadsDefaultValues_WhenNoConfigFile(t *testing.T) {
    // Test implementation
}
```

### 2. **Setup and Teardown**
```go
func setupMocks(t *testing.T) (*mocks_config.Containable, *mocks_controls.Controllable) {
    mockConfig := &mocks_config.Containable{}
    mockController := &mocks_controls.Controllable{}

    // Common setup
    mockConfig.On("GetString", "app.name").Return("test-app")

    return mockConfig, mockController
}

func TestMyFeature(t *testing.T) {
    mockConfig, mockController := setupMocks(t)
    defer mockConfig.AssertExpectations(t)
    defer mockController.AssertExpectations(t)

    // Test implementation
}
```

### 3. **Test Both Success and Failure Paths**
```go
func TestRepository_Clone_Success(t *testing.T) {
    mockRepo := &mocks_vcs.RepoLike{}
    mockRepo.On("Clone", mock.AnythingOfType("string"), mock.AnythingOfType("string")).Return(nil)

    err := mockRepo.Clone("url", "path")
    assert.NoError(t, err)
}

func TestRepository_Clone_Failure(t *testing.T) {
    mockRepo := &mocks_vcs.RepoLike{}
    mockRepo.On("Clone", mock.AnythingOfType("string"), mock.AnythingOfType("string")).
        Return(errors.New("clone failed"))

    err := mockRepo.Clone("url", "path")
    assert.Error(t, err)
}
```

### 4. **Use Table-Driven Tests with Mocks**
```go
func TestConfigValidation(t *testing.T) {
    tests := []struct {
        name           string
        configSetup    func(*mocks_config.Containable)
        expectedResult bool
    }{
        {
            name: "valid config",
            configSetup: func(m *mocks_config.Containable) {
                m.On("GetString", "required.field").Return("value")
                m.On("GetBool", "optional.feature").Return(true)
            },
            expectedResult: true,
        },
        {
            name: "missing required field",
            configSetup: func(m *mocks_config.Containable) {
                m.On("GetString", "required.field").Return("")
                m.On("GetBool", "optional.feature").Return(false)
            },
            expectedResult: false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            mockConfig := &mocks_config.Containable{}
            tt.configSetup(mockConfig)

            result := ValidateConfig(mockConfig)
            assert.Equal(t, tt.expectedResult, result)

            mockConfig.AssertExpectations(t)
        })
    }
}
```

## Integration with GTB Testing

The mocks integrate seamlessly with GTB components. For testing commands that use props:

```go
func TestMyCommand(t *testing.T) {
    // Setup mocks
    mockConfig := &mocks_config.Containable{}
    mockConfig.On("GetString", "timeout").Return("30s")

    // Create test props
    testProps := &props.Props{
        Tool: props.Tool{
            Name: "test-tool",
        },
        Config: mockConfig,
        Logger: log.New(io.Discard), // Silent logger for tests
    }

    // Test your command
    cmd := NewMyCommand(testProps)
    err := cmd.Execute()

    assert.NoError(t, err)
    mockConfig.AssertExpectations(t)
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
