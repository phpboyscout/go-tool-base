---
title: Development Guide
description: Guide for contributing to GTB, including setup, testing, and architecture.
date: 2026-02-16
tags: [development, contributing, guide]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Development Guide

!!! info "Library Orientation"
    This guide covers how to contribute to GTB itself, set up a development environment, run tests, and understand the internal architecture. If you are developing an application *using* GTB, some sections may differ.

## Development Setup

### Prerequisites

- Go 1.26 or later
- Git configured for private repositories
- golangci-lint for code quality checks
- mockery for generating mocks (if modifying interfaces)
- [just](https://just.systems/) (for running build scripts)

### Clone and Setup

```bash
git clone https://github.com/phpboyscout/go-tool-base.git
cd gtb

# Install dependencies
go mod download

# Verify setup
just build
# Or manually:
go build ./...
go test ./...
```

### Environment Configuration

Set up your environment for private module development:

```bash
# Configure Git authentication
git config --global --replace-all url."https://${GITHUB_USERNAME}:${GITHUB_TOKEN}@github.com".insteadOf "https://github.com"

# Set Go private modules
go env -w GOPRIVATE=github.com
```

## Project Structure

Understanding the codebase organization:

```
gtb/
├── .github/                # CI/CD workflows and GitHub templates
├── .gtb/          # Configuration and manifest for the project
├── cmd/                    # Built-in shareable command implementations (docs, init, root, update, version)
├── docs/                   # Project documentation
├── internal/               # Internal components used exclusively by the CLI and not intended for public consumption
│   └── agent/              # AI Agent components used by the CLI
│   └── cmd/                # Cobra command logic use by the CLI
│   └── generator/          # Template generation logic use by the CLI to create CLI projects
├── pkg/                    # Public library packages
│   ├── chat/               # AI provider integrations (OpenAI, Gemini, Claude)
│   ├── config/             # Configuration management (Viper wrapper)
│   ├── controls/           # Service lifecycle management
│   ├── docs/               # TUI documentation browser and AI Q&A
│   ├── errorhandling/      # Custom error types and reporting
│   ├── props/              # Application-wide properties and dependency container
│   ├── setup/              # Initialization and project setup
│   ├── utils/              # Common utility functions
│   └── vcs/                # Version control (Git) utilities
├── main.go                 # CLI entry point
└── justfile                # Build and automation tasks
```

## Development Workflow

### Significant Feature Lifecycle

Non-trivial features follow a spec-first, branch-per-feature workflow. The spec and the implementation share a single branch and PR, ensuring the design is reviewed and approved before code is written.

#### 1. Create a Feature Branch

```bash
git checkout -b feature/my-new-feature
```

#### 2. Draft a Specification

Using an AI assistant (Cursor Agent, Claude, etc.), draft a feature spec following the **[Feature Specifications](./specs/index.md)** guide. The spec captures requirements, public API design, internal implementation, testing strategy, and implementation phases.

Save it to `docs/development/specs/YYYY-MM-DD-<feature-name>.md` with status `DRAFT`.

#### 3. Push and Raise a PR

Push your branch and open a PR containing **only** the spec. This is the review gate -- no implementation code yet.

```bash
git push -u origin feature/my-new-feature
```

Mark the PR as a spec review (e.g. prefix the title with `[SPEC]`) so reviewers know what to expect.

#### 4. Request SME Review

Request review from a Subject Matter Expert (SME). The reviewer should:

- Read the spec and assess feasibility, design decisions, and scope.
- If **approved**: update the spec's `status` to `APPROVED` (in both frontmatter and the document header) and push the change.
- If **changes are needed**: use GitHub's PR commenting facility to communicate required changes. The spec author iterates until the reviewer is satisfied.
- If **rejected**: update the spec's `status` to `REJECTED`, add a `## Rejection Rationale` section (see [Handling Rejections](./specs/index.md#handling-rejections)), and merge the PR to preserve the historical record.

!!! warning
    Do not begin implementation until the spec status is `APPROVED`.

#### 5. Implement the Feature

Once approved, develop the feature in the same branch. Use the spec as the primary reference -- it defines the implementation phases, public API, and acceptance criteria. AI agents can read the spec directly from the repository for full context.

Update the spec status to `IN PROGRESS` when development begins.

```bash
# Implement in phases as defined by the spec
# Verify as you go
go test ./...
golangci-lint run
```

#### 6. Code Review, Approval, and Merge

Once implementation is complete:

1. Update the spec status to `IMPLEMENTED`.
2. Ensure all CI checks pass and the [Verification Checklists](./verification-checklists.md) are satisfied.
3. Request code review following standard project conventions.
4. Merge the PR once approved.

!!! tip
    Keep the spec and implementation in the same PR. This ensures the design rationale is always co-located with the code it describes in the git history.

### Quick Fixes and Minor Changes

Not every change needs a spec. For bug fixes, small enhancements, and refactors that don't alter the public API or architecture, follow the standard branch-and-PR workflow without a spec.

### Testing

The project uses comprehensive testing with mocks:

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run specific package tests
go test ./config -v

# Run tests with race detection
go test -race ./...
```

### 3. Mock Generation

If you modify interfaces, regenerate mocks:

```bash
# Install mockery if not already installed
go install github.com/vektra/mockery/v2@latest

# generate all mocks for the project
mockery
```

### 4. Code Quality

The project enforces code quality standards:

```bash
# Run linter
golangci-lint run

# Apply Go fixes
go fix ./...

# Auto-fix issues where possible
golangci-lint run --fix

# Format code
go fmt ./...

# Check for common issues
go vet ./...
```

## Testing Philosophy

### Unit Testing

- **High Coverage**: Aim for >90% test coverage
- **Mocked Dependencies**: Use generated mocks for external dependencies
- **Table-Driven Tests**: Use table-driven patterns for multiple scenarios
- **Parallel Execution**: Tests should run in parallel where possible

Example test structure:
```go
func TestLoadEmbed(t *testing.T) {
    t.Parallel()

    tests := []struct {
        name          string
        paths         []string
        setup         func(*configMocks.MockEmbeddedFileReader)
        expectError   bool
        expectedFiles int
    }{
        {
            name:  "successful single file",
            paths: []string{"config.yaml"},
            setup: func(m *configMocks.MockEmbeddedFileReader) {
                m.EXPECT().ReadFile("config.yaml").Return([]byte("test"), nil)
            },
            expectError:   false,
            expectedFiles: 1,
        },
        // ... more test cases
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            // Test implementation
        })
    }
}
```

### Integration Testing

Integration tests exercise real external services and multi-component interactions. They are gated by environment variables and live in dedicated `*_integration_test.go` files.

```bash
# Run all integration tests
just test-integration        # sets INT_TEST=1 automatically

# Run only a specific group
INT_TEST_VCS=1 go test ./pkg/vcs/... -v
INT_TEST_CONTROLS=1 go test ./pkg/controls/... -v
```

Every integration test must call the shared gating helper as its first action:

```go
func TestSomethingIntegration(t *testing.T) {
    testutil.SkipIfNotIntegration(t, "mytag")
    // ... test code
}
```

The helper lives in `internal/testutil/` and supports:

- **`INT_TEST=1`** — enables all integration tests globally.
- **`INT_TEST_<TAG>=1`** — enables only tests matching that tag (e.g. `INT_TEST_VCS=1`).

See the **[Integration Testing](./integration-testing.md)** guide for full details, including the test inventory and writing guidelines.

## Architecture Principles

For a visual overview of component relationships and core workflows, see the **[Architectural Overview](../concepts/architecture.md)**.

### 1. Dependency Injection

All components receive their dependencies via constructor functions:

```go
func NewCmdRoot(ver string, commit string, date string) *cobra.Command {
    // Dependencies are injected, and root initialization handles built-in commands
}
```

### 2. Interface Segregation

Small, focused interfaces for better testability:

```go
type EmbeddedFileReader interface {
    ReadFile(name string) ([]byte, error)
}
```

### 3. Error Wrapping

Consistent error handling with context:

```go
if err != nil {
    return nil, errors.WrapPrefix(err, "failed to load config", 0)
}
```

### 4. Configuration Abstraction

Flexible configuration system supporting multiple sources:

- File-based configuration
- Embedded configuration
- Environment variables
- Command-line flags

## Adding New Components

### 1. Creating a New Package

When adding a new package:

```bash
mkdir newpackage
cd newpackage
```

**interface.go:**
```go
package newpackage

// Define interfaces first
type NewService interface {
    DoSomething(ctx context.Context, input string) error
}
```

**implementation.go:**
```go
package newpackage

import "context"

type service struct {
    l logger.Logger
    config Configurable
}

func NewService(l logger.Logger, config Configurable) NewService {
    return &service{
        l: l,
        config: config,
    }
}

func (s *service) DoSomething(ctx context.Context, input string) error {
    s.l.Debug("doing something", "input", input)
    // Implementation
    return nil
}
```

### 2. Adding Tests

**implementation_test.go:**
```go
package newpackage_test

import (
    "context"
    "testing"

    "github.com/phpboyscout/go-tool-base/newpackage"
    "github.com/phpboyscout/go-tool-base/mocks/pkg/config"
    "github.com/phpboyscout/go-tool-base/pkg/logger"
    "github.com/stretchr/testify/assert"
)

func TestService_DoSomething(t *testing.T) {
    t.Parallel()

    l := logger.NewNoop()
    mockConfig := configMocks.NewMockConfigurable(t)
    service := newpackage.NewService(l, mockConfig)

    err := service.DoSomething(context.Background(), "test")
    assert.NoError(t, err)
}
```

### 3. Generate Mocks

Add mockery directive to interface:

```go
type NewService interface {
    DoSomething(ctx context.Context, input string) error
}
```

## Debugging Tips

### 1. Verbose Logging

Enable debug logging during development:

```go
l := logger.NewCharm(os.Stderr,
    logger.WithLevel(logger.DebugLevel),
    logger.WithCaller(),
)
```

### 2. Test Debugging

Run specific tests with verbose output:

```bash
go test -v ./config -run TestLoadEmbed
```

### 3. Race Condition Detection

Run tests with race detection:

```bash
go test -race ./...
```

## Contributing Guidelines

### Pull Requests

For significant features, follow the full [Significant Feature Lifecycle](#significant-feature-lifecycle) above. For all PRs:

1. Create a feature branch from `main`.
2. Add comprehensive tests.
3. Update documentation.
4. Ensure all CI checks pass.
5. Submit PR with a clear description of the impact, linking to the spec if applicable.

### Code Review Checklist

- [ ] Feature spec exists and is `APPROVED` (for non-trivial features)
- [ ] Spec status updated to reflect current state (`IN PROGRESS` / `IMPLEMENTED`)
- [ ] Tests added/updated
- [ ] Documentation updated
- [ ] Code follows project conventions
- [ ] No breaking changes (or properly documented)
- [ ] Error handling follows project patterns
- [ ] Interfaces are properly mocked

### Release Process

Releases are automated via CI/CD, but follow semantic versioning:

- **Major**: Breaking changes
- **Minor**: New features, backward compatible
- **Patch**: Bug fixes, backward compatible

## Common Development Tasks

### Adding a New Built-in Command

1. Create package in `cmd/`
2. Implement command following existing patterns
3. Add to root command registration in `pkg/cmd/root/root.go`:
   ```go
   rootCmd.AddCommand(newbuiltin.NewCmdBuiltin(p))
   ```
4. Add comprehensive tests
5. Update documentation

### Modifying Configuration System

1. Update interfaces in `config/`
2. Regenerate mocks
3. Update tests
4. Verify backward compatibility
5. Update examples

## Detailed Development Guides

For specialized information on library-level development and architectural standards, see:

- **[Feature Specifications](./specs/index.md)**: Spec-first workflow for designing new features.
- **[IDE & Tooling Setup](./ide-setup.md)**: Standard configuration for library contributors.
- **[AI Integration Layer](./ai-integration.md)**: Deep dive into the `chat` package and provider abstraction.
- **[Error Handling](./error-handling.md)**: Patterns for library errors and wrapping.
- **[Environment Variables & .env](./environment-variables.md)**: Local configuration and secret management.
- **[Integration Testing](./integration-testing.md)**: Running and writing tests that depend on external services.
- **[Dependency Management](./dependency-management.md)**: Versioning policy and internal modules.
- **[Verification Checklists](./verification-checklists.md)**: Library and generator verification protocols.
- **[Glossary](glossary.md)**: Common terms used in the project.
- **[Security](security.md)**: Policies on secret management and vulnerability reporting.
- **[Security Decisions](security-decisions.md)**: Accepted risks and rationale from security audits.
- **[AI Collaboration](ai-collaboration.md)**: How to leverage project skills and workflows.

This development guide should help you contribute effectively to GTB!
