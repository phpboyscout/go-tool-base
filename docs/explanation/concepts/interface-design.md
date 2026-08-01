---
title: Interface Design
description: Comprehensive guide to GTB interfaces, their purposes, and implementation strategies.
date: 2026-03-25
tags: [concepts, patterns, interfaces, go-idioms, testing]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Interface Design

GTB follows Go's interface design principles: small, focused interfaces that enable flexible composition, dependency injection, and comprehensive testing. This guide provides a complete reference to all public interfaces in the framework.

## Design Philosophy

GTB interfaces follow these key principles:

**Interface Segregation**
:   Interfaces are kept small and focused on specific behaviours rather than encompassing all possible methods.

**Accept Interfaces, Return Concrete — Except Provider Factories**
:   Functions accept interface parameters for flexibility. The default return is a concrete type for clarity. The deliberate exception is a *factory constructor* that selects among several interchangeable implementations behind one contract (the provider pattern): these return the interface, because the concrete type is an implementation detail the caller must not depend on. `chat.New` returns `ChatClient`, `errorhandling.New` returns `ErrorHandler`, and the `logger.New*` constructors return `Logger` for exactly this reason. A constructor with a single concrete implementation still returns that concrete type.

**Consumer-Defined Interfaces**
:   Interfaces are defined where they're consumed, not where they're implemented, following Go idioms.

**Testing First**
:   All interfaces are designed with testability in mind, enabling mock implementations via Mockery.

---

## Interface Reference

### Logging

#### Logger

**Package:** `pkg/logger`
**Purpose:** Unified logging abstraction accepted by all GTB packages instead of a concrete logger type.

```go
// Mirrors the method set of *slog.Logger exactly (see pkg.go.dev for the full
// signatures); a *slog.Logger therefore satisfies logger.Logger directly.
type Logger interface {
    Debug(msg string, args ...any)
    Info(msg string, args ...any)
    Warn(msg string, args ...any)
    Error(msg string, args ...any)
    // …Context variants, Log, LogAttrs
    With(args ...any) *slog.Logger
    WithGroup(name string) *slog.Logger
    Enabled(ctx context.Context, level slog.Level) bool
    Handler() slog.Handler
}
```

**Implementations:** `NewCharm` (CLI), `NewSlog` (observability stacks), `NewNoop` (tests)

**Key Design Decisions:**

- Mirrors `*slog.Logger` exactly, so a plain `*slog.Logger` satisfies it directly and any type with the same method set works as a custom backend — no adapter needed
- Structured-only: format-string helpers (`Infof`) and `Fatal` were dropped; use `log.Info("msg", "key", val)` and return errors for exit paths
- `Handler()` provides slog ecosystem interoperability; runtime level/format are set via the `logger.SetLevel(log, slog.Level)` / `logger.SetFormatter(log, f)` helpers rather than interface methods

**Usage Example:**

```go
l := logger.NewCharm(os.Stderr, logger.WithLevel(logger.InfoLevel))
props := &props.Props{Logger: l}
```

See [Logger component documentation](../components/logger.md) for the full backend reference.

---

### Props Provider Interfaces

GTB packages that need only a subset of `Props` declare narrow provider
interfaces. This makes dependencies explicit and keeps test setup minimal.

```go
type LoggerProvider interface {
    GetLogger() logger.Logger
}

type ConfigProvider interface {
    GetConfig() *config.Store
}

type ErrorHandlerProvider interface {
    GetErrorHandler() errorhandling.ErrorHandler
}
```

**Key Design Decisions:**

- Packages declare only the provider interfaces they need
- `*props.Props` satisfies all provider interfaces
- Tests pass a minimal struct implementing only the required interface

---

### Configuration Interfaces

#### Reader

**Package:** `go/config`  
**Purpose:** The read surface a consumer depends on — deliberately small and
free of any dependency's types, so what sits behind it can be replaced (as the
Store replaced Viper) without touching consumers.

```go
type Reader interface {
    Get(path string) any
    GetString(path string) string
    GetBool(path string) bool
    GetInt(path string) int
    GetFloat(path string) float64
    GetDuration(path string) time.Duration
    GetTime(path string) time.Time
    // ...the full typed-getter family, plus:
    Has(path string) bool
    IsSet(path string) bool
    SectionExists(path string) bool
    Keys() []string
    Unmarshal(target any) error
    UnmarshalKey(path string, target any) error
    Origin(path string) (Source, bool)
    Shadowed(path string) []Source
    Explain(path string) string
}
```

**Primary Implementation:** `*View` — a read surface **pinned to one
snapshot**, obtained from the live store with `store.View()`.

**Key Design Decisions:**

- Reads are snapshot-coherent: two values read from one view always belong to
  the same resolved configuration, even under hot reload
- Provenance is part of the read surface — `Origin`/`Shadowed`/`Explain`
  report which layer supplied a value
- Writes are deliberately absent: they go through the Store's transactional
  `Apply`, which edits the target document in place

**Usage Example:**

```go
func loadDatabaseConfig(cfg config.Reader) (*DatabaseConfig, error) {
    section, err := config.UnmarshalSection[DatabaseConfig](cfg, "database")
    if err != nil {
        return nil, err
    }

    return &section.Value, nil
}
```

---

#### Observable

**Package:** `go/config`  
**Purpose:** Configuration change notification callback.

```go
type Observable interface {
    Run(cfg Observed) error
}
```

`Observed` embeds `Reader` and is pinned to the snapshot that triggered the
notification, so an observer never reads values from a later change partway
through its own callback.

**Usage Example:**

```go
type ConfigReloader struct {
    service *MyService
}

func (r *ConfigReloader) Run(cfg config.Observed) error {
    return r.service.Reconfigure(cfg)
}

store.AddObserver(&ConfigReloader{service: myService})
```

---

### Asset Management

#### Assets

**Package:** `pkg/props`  
**Purpose:** Unified access to embedded filesystems with automatic merging.

```go
type Assets interface {
    fs.FS
    fs.ReadDirFS
    fs.GlobFS
    fs.StatFS
    
    Slice() []fs.FS
    Names() []string
    Get(name string) fs.FS
    Register(name string, fs fs.FS)
    For(names ...string) Assets
    Merge(others ...Assets) Assets
    Exists(name string) (fs.FS, error)
    Mount(f fs.FS, prefix string)
}
```

**Primary Implementation:** `*embeddedAssets`

**Key Design Decisions:**

- Composes standard library `fs.*` interfaces for compatibility
- Named registration enables selective asset access
- Automatic YAML/JSON/CSV merging across registered filesystems

**Usage Example:**

```go
// Register assets from multiple packages
p.Assets.Register("core", &coreAssets)
p.Assets.Register("myfeature", &featureAssets)

// Access merged configuration
file, err := p.Assets.Open("config.yaml")  // Merges all config.yaml files

// Access specific asset set
docs := p.Assets.For("core")  // Only core assets
```

---

### Error Handling

#### ErrorHandler

**Package:** `gitlab.com/phpboyscout/go/errorhandling`  
**Purpose:** Centralised error processing with logging, help display, and exit handling.

```go
type ErrorHandler interface {
    Check(err error, prefix string, level string)
    Fatal(err error, prefixes ...string)
    Error(err error, prefixes ...string)
    Warn(err error, prefixes ...string)
    SetUsage(usage func() error)
}
```

**Primary Implementation:** `*StandardErrorHandler`

**Key Design Decisions:**

- Separates error logging from exit behaviour (testable)
- Supports multiple severity levels
- Integrates rich stack traces, hints, and details via `cockroachdb/errors`

**Usage Example:**

```go
func myCommand(p *props.Props) *cobra.Command {
    return &cobra.Command{
        Run: func(cmd *cobra.Command, args []string) {
            err := doWork(args)
            p.ErrorHandler.Fatal(err)  // Logs and exits if err != nil
        },
    }
}
```

---

### AI Integration

#### ChatClient

**Package:** `pkg/chat`  
**Purpose:** Unified interface for AI provider interactions.

```go
type ChatClient interface {
    Add(prompt string) error
    Ask(question string, target any) error
    SetTools(tools []Tool) error
    Chat(ctx context.Context, prompt string) (string, error)
}
```

**Implementations:** OpenAI, Claude, and Gemini clients (internal)

**Key Design Decisions:**

- Provider-agnostic interface hides API differences
- Tool calling abstraction enables agentic workflows
- `Ask` supports structured JSON responses

**Usage Example:**

```go
type Analysis struct {
    Issues []string `json:"issues"`
    Score  int      `json:"score"`
}

client, _ := chat.New(ctx, props, chat.Config{Provider: chat.ProviderClaude})
var result Analysis
client.Ask("Analyse this code for issues", &result)
```

---

### Service Lifecycle

#### Controllable (composed interface)

**Module:** [`gitlab.com/phpboyscout/go/controls`](https://controls.go.phpboyscout.uk)
**Purpose:** Manage multiple concurrent services with coordinated lifecycle.

The service supervisor is an extracted module, not a GTB package, so its
interface definitions live with it rather than being reproduced here — a copy in
this repository has nothing compiling against it and drifts silently.

`Controllable` composes five narrow role interfaces — `Runner`,
`HealthReporter`, `StateAccessor`, `Configurable` and `ChannelProvider` — so a
consumer depends only on the part it actually uses. This is the same principle as
GTB's own [Props provider interfaces](#props-provider-interfaces), applied to a
lifecycle supervisor.

- Full definitions and the narrowing guide:
  [controls: testing](https://controls.go.phpboyscout.uk/how-to/testing/)
- Generated API reference:
  [pkg.go.dev](https://pkg.go.dev/gitlab.com/phpboyscout/go/controls)

**What GTB adds on top:**

- `ControllerOpt` functions accept `Configurable`, not `Controllable`, to enforce
  the narrowest dependency
- `SetLogger` accepts `*slog.Logger`; wrap an application `logger.Logger` with
  `logger.ToSlog(...)` at the call site
- OS signal handling is **opt-in** via `controls.WithSignals()` and belongs to a
  standalone `main`, never to a GTB command — inside a command the root owns
  signals and the controller observes `cmd.Context()`. See
  [the controls component page](../components/controls/index.md).

---

### Version Control

#### RepoLike

**Package:** `pkg/vcs`  
**Purpose:** Abstract Git repository operations for local and in-memory repos.

```go
type RepoLike interface {
    SourceIs(int) bool
    SetSource(int)
    SetRepo(*git.Repository)
    GetRepo() *git.Repository
    SetKey(*ssh.PublicKeys)
    SetBasicAuth(string, string)
    GetAuth() transport.AuthMethod
    SetTree(*git.Worktree)
    GetTree() *git.Worktree
    Checkout(plumbing.ReferenceName) error
    CheckoutCommit(plumbing.Hash) error
    CreateBranch(string) error
    OpenInMemory(string, string, ...CloneOption) (*git.Repository, *git.Worktree, error)
    OpenLocal(string, string) (*git.Repository, *git.Worktree, error)
    Open(RepoType, string, string, ...CloneOption) (*git.Repository, *git.Worktree, error)
    WalkTree(func(*object.File) error) error
    AddToFS(fs afero.Fs, gitFile *object.File, fullPath string) error
}
```

**Primary Implementation:** `*Repo`

**Key Design Decisions:**

- Polymorphic switching between local filesystem and in-memory storage
- Functional options for clone configuration
- Integration with `afero.Fs` for test isolation

---

### Initialisation

#### Initialiser

**Package:** `pkg/setup`  
**Purpose:** Pluggable initialisation steps for CLI tools.

```go
type Initialiser interface {
    Name() string
    IsConfigured(cfg config.Reader) bool
    Configure(props *props.Props, cfg setup.Editor) error
}
```

**Implementations:** GitHub initialiser, AI initialiser, custom feature initialisers

**Key Design Decisions:**

- Self-registration via feature registry
- Idempotent—checks existing config before prompting
- Decoupled from core init command logic

---

## Interface Relationships

```mermaid
classDiagram
    class Props {
        +Config *config.Store
        +Assets Assets
        +ErrorHandler ErrorHandler
    }
    
    class Reader {
        <<interface>>
        +Get(key) any
        +Origin(path) Source
        +AddObserver(Observable)
    }
    
    class Observable {
        <<interface>>
        +Run(Observed) error
    }
    
    class Assets {
        <<interface>>
        +Register(name, fs.FS)
        +Merge(Assets) Assets
    }
    
    class ErrorHandler {
        <<interface>>
        +Fatal(err)
        +Error(err)
    }
    
    Props o-- Store
    Props o-- Assets
    Props o-- ErrorHandler
    Store --> Observable : notifies
```

---

## Testing with Interfaces

All interfaces have auto-generated mocks in `mocks/pkg/`:

```go
import (
    "testing"
    mocks_config "gitlab.com/phpboyscout/go/config/mocks"
)

func TestMyFunction(t *testing.T) {
    mockCfg := configmocks.NewMockReader(t)
    mockCfg.On("GetString", "api.url").Return("http://test.example.com")
    mockCfg.On("GetInt", "api.timeout").Return(30)
    
    result := MyFunction(mockCfg)
    
    mockCfg.AssertExpectations(t)
}
```

### Generating Mocks

Mocks are generated using Mockery. After adding a new interface:

```bash
task mocks  # or: mockery
```

---

## Creating New Interfaces

When designing new interfaces for your GTB application:

### 1. Keep Interfaces Small

```go
// ✓ Good: Focused interface
type Reader interface {
    Read(key string) ([]byte, error)
}

// ✗ Avoid: Kitchen sink interface
type DataStore interface {
    Read(key string) ([]byte, error)
    Write(key string, data []byte) error
    Delete(key string) error
    List(prefix string) ([]string, error)
    Watch(key string, callback func([]byte)) error
    Transaction(func(tx Tx) error) error
    // ... many more methods
}
```

### 2. Define Where Consumed

```go
// Define interface in the package that uses it
package mycommand

// FileReader is what mycommand needs to read files
type FileReader interface {
    ReadFile(path string) ([]byte, error)
}

func NewCommand(reader FileReader) *cobra.Command {
    // ...
}
```

### 3. Accept Interfaces, Return Concrete (Factories Excepted)

Accept interfaces and, by default, return the concrete type so callers keep
full access to the implementation:

```go
// ✓ Good: single implementation — accept interface, return concrete
func NewService(cfg config.Reader) *MyService {
    return &MyService{cfg: cfg}
}

// ✗ Avoid for a single implementation: returning the interface needlessly
//   hides the concrete type
func NewService(cfg config.Reader) ServiceInterface {
    return &MyService{cfg: cfg}
}
```

The exception is a **factory constructor** that picks one of several
interchangeable implementations behind a shared contract. Here returning the
interface is correct — the concrete type is intentionally hidden so the caller
cannot couple to a specific provider:

```go
// ✓ Good: factory over multiple providers — return the interface
func New(ctx context.Context, p *props.Props, cfg Config) (ChatClient, error) {
    switch cfg.Provider {
    case ProviderClaude:
        return newClaude(ctx, p, cfg)
    case ProviderGemini:
        return newGemini(ctx, p, cfg)
    // ...
    }
}
```

This is why `chat.New`, `errorhandling.New`, and the `logger.New*` constructors
return interfaces rather than concrete structs.

---

## Related Documentation

- **[Props Container](../components/props.md)**: How interfaces compose in the central dependency container
- **[Mocks Package](../components/mocks.md)**: Using generated mocks for testing
- **[Error Handling](../components/error-handling.md)**: ErrorHandler interface patterns
- **[Configuration](../components/config/index.md)**: Store, Reader and Observable usage
