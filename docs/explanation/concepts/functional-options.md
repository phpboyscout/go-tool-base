---
title: Functional Options Pattern
description: Understanding and using the functional options pattern for flexible, extensible constructors.
date: 2026-02-17
tags: [concepts, patterns, go-idioms, functional-options]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Functional Options Pattern

GTB extensively uses the **Functional Options** pattern to provide flexible, backward-compatible constructors. This pattern allows you to configure objects with optional parameters while maintaining clean APIs and avoiding "options struct" bloat.

## Why Functional Options?

Traditional constructor approaches in Go have limitations:

**Positional Arguments**
:   Become unwieldy with many parameters and break backward compatibility when parameters change.

**Config Structs**
:   Require knowledge of all possible fields upfront and often require passing empty/default values.

**Functional Options**
:   Allow callers to specify only the options they care about, with sensible defaults for everything else.

```go
// ❌ Positional: Unclear what each parameter means
client := NewClient("localhost", 8080, true, 30, nil, "")

// ❌ Config struct: Must specify all fields
client := NewClient(Config{
    Host: "localhost",
    Port: 8080,
    Secure: true,
    Timeout: 30,
    Logger: nil,   // ← Must include even default values
    Name: "",
})

// ✓ Functional Options: Clean and self-documenting
client := NewClient("localhost",
    WithPort(8080),
    WithTLS(),
    WithTimeout(30*time.Second),
)
```

## Pattern Structure

The functional options pattern consists of three parts:

### 1. Option Type Definition

Define a function type that modifies the target struct:

```go
// Option is a function that configures a Controller
type ControllerOpt func(Controllable)
```

### 2. Option Factory Functions

Create factory functions that return configured options:

```go
// WithLogger returns an option that sets the controller's logger
func WithLogger(l *slog.Logger) ControllerOpt {
    return func(c Configurable) {
        c.SetLogger(l)
    }
}

// WithoutSignals returns an option that disables signal handling
func WithoutSignals() ControllerOpt {
    return func(c Controllable) {
        c.SetSignalsChannel(nil)
    }
}
```

### 3. Constructor with Variadic Options

Accept options as variadic parameters and apply them:

```go
func NewController(ctx context.Context, opts ...ControllerOpt) *Controller {
    // Create with defaults
    c := &Controller{
        ctx:      ctx,
        logger:   slog.New(slog.DiscardHandler),
        messages: make(chan Message, 100),
        health:   make(chan HealthMessage, 100),
        errs:     make(chan error, 100),
        signals:  make(chan os.Signal, 1),
        wg:       &sync.WaitGroup{},
        state:    Unknown,
    }
    
    // Apply options
    for _, opt := range opts {
        opt(c)
    }
    
    return c
}
```

---

## Usage in GTB

### Service Controller Options

The standalone [`go/controls`](https://controls.go.phpboyscout.uk) module (which
GTB consumes) uses functional options for controller configuration:

```go
import "gitlab.com/phpboyscout/go/controls"

// Create controller with defaults
controller := controls.NewController(ctx)

// Create controller with custom logger
controller := controls.NewController(ctx,
    controls.WithLogger(myLogger),
)

// Create controller for testing (no OS signals)
controller := controls.NewController(ctx,
    controls.WithoutSignals(),
    controls.WithLogger(testLogger),
)
```

**Available Options:**

| Option | Purpose |
| :--- | :--- |
| `WithLogger(l)` | Set a custom `*slog.Logger` for the controller |
| `WithoutSignals()` | Disable OS signal handling (useful for testing) |

---

### Git Clone Options

The extracted [`go/repo`](https://repo.go.phpboyscout.uk) module uses functional
options for configuring repository clones:

```go
import "gitlab.com/phpboyscout/go/repo"

// Full clone (default)
gitRepo, worktree, err := r.OpenInMemory(url, branch)

// Shallow clone for faster CI
gitRepo, worktree, err := r.OpenInMemory(url, branch,
    repo.WithShallowClone(1),
)

// Optimized clone for specific branch without tags
gitRepo, worktree, err := r.OpenInMemory(url, branch,
    repo.WithShallowClone(1),
    repo.WithSingleBranch("main"),
    repo.WithNoTags(),
)

// Clone with submodules
gitRepo, worktree, err := r.OpenInMemory(url, branch,
    repo.WithRecurseSubmodules(),
)
```

**Available Options:**

| Option | Purpose |
| :--- | :--- |
| `WithShallowClone(depth)` | Limit clone history to specified depth |
| `WithSingleBranch(branch)` | Clone only the specified branch |
| `WithNoTags()` | Skip fetching tags |
| `WithRecurseSubmodules()` | Recursively clone submodules |

---

### Documentation Browser Options

The `pkg/docs` package uses functional options for TUI configuration:

```go
import "gitlab.com/phpboyscout/go-tool-base/pkg/docs"

// Standard documentation browser
model := docs.New(assets,
    docs.WithTitle("My Tool Documentation"),
)

// Documentation with AI integration
model := docs.New(assets,
    docs.WithTitle("My Tool Documentation"),
    docs.WithAskFunc(myAIHandler),
)
```

---

### AI Form Options

The `pkg/setup/ai` package uses functional options for customizing the AI configuration form:

```go
import "gitlab.com/phpboyscout/go-tool-base/pkg/setup/ai"

// Default AI setup form
initialiser := ai.NewAIInitialiser()

// Custom form with additional fields
initialiser := ai.NewAIInitialiser(
    ai.WithAIForm(func(cfg *ai.AIConfig) []*huh.Form {
        // Return custom form configuration
    }),
)
```

---

### Logger Options

The `pkg/logger` package builds its Charm logger with a `CharmOption` family:

```go
import "gitlab.com/phpboyscout/go-tool-base/pkg/logger"

log := logger.NewCharm(os.Stderr,
    logger.WithLevel(logger.InfoLevel),
    logger.WithTimestamp(true),
    logger.WithCaller(false),
)
```

This is also the seam a config-driven host uses: `logger.Config` carries the
same knobs and produces the option slice, so you build from config rather than
setting fields after the fact — `logger.NewCharm(w, cfg.CharmOptions()...)`.
That matters because `Timestamp`/`Caller` are construction-time only (no runtime
setter), so they *must* be passed as options. See the
[logger component](../components/logger.md#config-driven-construction).

---

## Creating Custom Options

Follow these guidelines when implementing functional options in your own code:

### Step 1: Define the Option Type

```go
type ServerOption func(*Server)
```

### Step 2: Create Option Factories

Each option factory should be a simple function that returns a closure:

```go
// WithPort sets the server port
func WithPort(port int) ServerOption {
    return func(s *Server) {
        s.port = port
    }
}

// WithTLS enables TLS with the provided certificate
func WithTLS(certFile, keyFile string) ServerOption {
    return func(s *Server) {
        s.tlsEnabled = true
        s.certFile = certFile
        s.keyFile = keyFile
    }
}

// WithMiddleware adds middleware to the chain
func WithMiddleware(mw ...Middleware) ServerOption {
    return func(s *Server) {
        s.middleware = append(s.middleware, mw...)
    }
}
```

### Step 3: Apply in Constructor

```go
func NewServer(opts ...ServerOption) *Server {
    // Start with sensible defaults
    s := &Server{
        port:       8080,
        tlsEnabled: false,
        middleware: make([]Middleware, 0),
    }
    
    // Apply all provided options
    for _, opt := range opts {
        opt(s)
    }
    
    return s
}
```

---

## Best Practices

### Naming Conventions

- Option types: `*Opt` or `*Option` (e.g., `ControllerOpt`, `CloneOption`)
- Option factories: `With*` prefix (e.g., `WithLogger`, `WithPort`)
- Negation options: `Without*` prefix (e.g., `WithoutSignals`, `WithNoTags`)

### Default Values

Always provide sensible defaults so the constructor works with zero options:

```go
// This should work without any options
controller := NewController(ctx)
```

### Validation

Validate option values when they're applied, not just at use time:

```go
func WithPort(port int) ServerOption {
    return func(s *Server) {
        if port < 1 || port > 65535 {
            // Log warning or set to default
            s.port = 8080
            return
        }
        s.port = port
    }
}
```

### Documentation

Document each option with its purpose and default behavior:

```go
// WithTimeout sets the request timeout duration.
// Default: 30 seconds.
func WithTimeout(d time.Duration) ServerOption {
    return func(s *Server) {
        s.timeout = d
    }
}
```

---

## Testing with Functional Options

Functional options make testing easier by allowing precise configuration:

```go
func TestServerWithCustomConfig(t *testing.T) {
    // Create server with test-specific configuration
    server := NewServer(
        WithPort(0),           // Random available port
        WithoutTLS(),          // Skip TLS for unit tests
        WithLogger(testutil.NewTestLogger(t)),
    )
    
    // Test server behavior...
}
```

---

## Related Patterns

- **[Props Container](../components/props.md)**: Dependency injection using a central struct
- **[Service Orchestration](service-orchestration.md)**: Controller options for service lifecycle
- **[repo module](https://repo.go.phpboyscout.uk/how-to/in-memory/)**: Clone options for repository operations

