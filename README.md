# Go Tool Base (GTB)

[![pipeline status](https://gitlab.com/phpboyscout/go-tool-base/badges/main/pipeline.svg)](https://gitlab.com/phpboyscout/go-tool-base/-/commits/main)
[![coverage report](https://gitlab.com/phpboyscout/go-tool-base/badges/main/coverage.svg)](https://gitlab.com/phpboyscout/go-tool-base/-/commits/main)
[![latest release](https://gitlab.com/phpboyscout/go-tool-base/-/badges/release.svg)](https://gitlab.com/phpboyscout/go-tool-base/-/releases)

**The Intelligent Application Lifecycle Framework for Go.**

Flag parsing is the easy part. What costs you is everything after it: configuration that layers properly, a self-update path you trust, errors a user can act on, and the same six commands written again in every tool. GTB is a batteries-included micro-framework (think Rails or Laravel) for Go command-line applications, and it ships those already built.

## ✅ What GTB IS / IS NOT

- **IS a full-lifecycle framework.** Configuration, versioning, auto-updates, embedded TUI docs, error handling and structured logging, out of the box.
- **IS a dependency injection container.** Services are passed explicitly through the `Props` container to every command constructor. Nothing reaches for a global.
- **IS an AI-ready foundation.** Agentic loop orchestration and MCP exposure are built in.
- **NOT a web framework (like Gin/Fiber)** or a microservice generator (like Sponge). GTB primarily bootstraps CLI utilities and background daemons, though you can easily build a `serve` command that boots an HTTP router via GTB's DI container!

> [!IMPORTANT]
> **Full Documentation**: For detailed guides, component deep-dives, framework comparisons, and API references, please visit our documentation site:
> **[https://gtb.phpboyscout.uk](https://gtb.phpboyscout.uk)**

## 📦 CLI Installation

To install the `gtb` automation CLI, use the recommended installation script for your platform:

**macOS/Linux (bash/zsh):**
```bash
curl -sSL "https://gitlab.com/phpboyscout/go-tool-base/-/raw/main/install.sh" | bash
```

**Windows (PowerShell):**
```powershell
irm "https://gitlab.com/phpboyscout/go-tool-base/-/raw/main/install.ps1" | iex
```

### Homebrew (macOS)

Every release publishes a **cask** to the GitLab-hosted tap at [`phpboyscout/homebrew`](https://gitlab.com/phpboyscout/homebrew), so Homebrew handles upgrades for you:

```bash
brew tap phpboyscout/homebrew https://gitlab.com/phpboyscout/homebrew.git
brew install --cask gtb
```

Upgrade later with `brew upgrade --cask gtb`.

> [!NOTE]
> **Pass the tap URL.** The shorthand `brew tap phpboyscout/homebrew` resolves to `github.com/phpboyscout/homebrew-homebrew`, which does not exist. The tap is on GitLab, so the URL is required.
>
> If you tapped the pre-migration GitHub tap before it was withdrawn, remove it with `brew untap phpboyscout/homebrew`, or `brew update` will fail against it.

> [!NOTE]
> For developers building from source, you can still use `go install gitlab.com/phpboyscout/go-tool-base/cmd/gtb@latest`. Note the `/cmd/gtb` suffix: the `main` package is not at the module root. A source build carries no pre-built documentation assets, so the `docs` command falls back to a limited source-build mode.

## 🚀 Key Advantages & Features

- **🤖 AI Agentic Workflows**: Integrated support for Claude, Gemini, and OpenAI to power autonomous ReAct-style loops and built-in Q&A against your embedded docs.
- **🔌 Model Context Protocol (MCP)**: Expose your CLI commands automatically as MCP tools for use by IDEs and external AI agents.
- **📦 Auto Updates & Lifecycle**: Zero-config version checking and self-update, straight from GitHub/GitLab releases, via the built-in `update` command.
- **📕 TUI Documentation**: A built-in, interactive terminal browser for your markdown documentation. Forget generic man pages.
- **🧱 Scaffold**: Generate production-ready, interface-driven CLI tool skeletons in seconds.
- **⚙️ Robust Configuration**: Configuration merged from files, environment variables and embedded assets, in a precedence you can inspect.
- **🏢 Enterprise VCS**: Deep integration with GitHub Enterprise and GitLab (including nested group paths) for auth, PR management, and assets.
- **🩹 Error Handling**: Structured, testable error management with logging, stack traces, and integrated help context routing to user-facing output.

## 🏗️ Core Architecture

The framework is built around a centralized **Props** container that provides type-safe access to all system dependencies.

Much of what GTB once implemented now lives in the standalone [phpboyscout Go toolkit](https://go.phpboyscout.uk), small framework-free modules under `gitlab.com/phpboyscout/go/`, so a tool can take one without taking all of GTB. Where that happened, the `pkg/` package that remains is a **thin config adapter** that wires the module from `Props`.

| Component | Implementation | Responsibility |
| :--- | :--- | :--- |
| **[pkg/props](docs/explanation/components/props.md)** | GTB | Central dependency injection container for logger, config, assets, filesystem, version and error handling. |
| **[config](docs/explanation/components/config/)** | [go/config](https://config.go.phpboyscout.uk) | Layered, snapshot-coherent configuration: provenance-aware reads, comment-preserving writes, explicit hot reload, and published mocks. |
| **[pkg/chat](docs/explanation/components/chat/)** | [go/chat](https://chat.go.phpboyscout.uk) + provider modules | Unified multi-provider AI client (Claude, OpenAI, Gemini, Claude Local). `pkg/chat` owns the GTB config-key schema and registers the providers. |
| **[controls](docs/explanation/components/controls/)** | [go/controls](https://controls.go.phpboyscout.uk) | Service lifecycle: startup ordering, health probes, graceful shutdown. |
| **[pkg/http](docs/explanation/components/http.md), [pkg/grpc](docs/explanation/components/grpc.md), [pkg/gateway](docs/explanation/components/gateway.md)** | [go/transport](https://transport.go.phpboyscout.uk) | Hardened HTTP/gRPC servers and the REST gateway; the `pkg/` packages are the config adapters. |
| **[pkg/setup](docs/explanation/components/setup/)** | GTB | Bootstrap logic: auth, key management, command middleware, and pluggable self-updating. |
| **[pkg/vcs](docs/explanation/components/version-control.md)** | [go/forge](https://forge.go.phpboyscout.uk) + [go/repo](https://repo.go.phpboyscout.uk) | GitHub/GitLab/Gitea/Bitbucket releases and auth; `pkg/vcs` wires them from resolved config. |
| **[errorhandling](docs/explanation/components/error-handling.md)** | [go/errorhandling](https://errorhandling.go.phpboyscout.uk) | Structured errors with user-facing hints, exit codes, stack traces and log integration. |
| **[pkg/docs](docs/explanation/components/docs.md)**, **[output](docs/explanation/components/output.md)** | GTB / [go/output](https://output.go.phpboyscout.uk) | Interactive TUI documentation browser; structured text/JSON/YAML/CSV output. |

## 🛠️ Built-in Commands

Every tool built on GTB inherits these essential capabilities:

- **`init`**: Bootstraps local environments, configures GitHub/GitLab auth, and manages SSH keys.
- **`version`**: Reports the current version and checks for available updates.
- **`update`**: Downloads and installs the latest release binary from GitHub or GitLab.
- **`mcp`**: Exposes CLI commands as Model Context Protocol (MCP) tools for use in IDEs.
- **`docs`**: Interactive terminal browser for documentation with built-in AI Q&A.
- **`doctor`**: Runs diagnostic checks to validate configuration, connectivity, and runtime environment.
- **`changelog`**: Shows the tool's version history.
- **`telemetry`** *(opt-in)*: Manages pseudonymous usage telemetry.
- **`config`** *(opt-in)*: Reads and writes the tool's configuration.
- **`man`** *(opt-in)*: Generates roff man pages for the command tree.

Commands can be selectively enabled or disabled at bootstrap time via feature flags. See [Feature Flags](#-feature-flags) below.

## 🤖 AI Providers

GTB supports multiple AI providers via a unified `pkg/chat` interface:

| Provider | Constant | Notes |
| :--- | :--- | :--- |
| **Anthropic Claude** | `ProviderClaude` | Requires `ANTHROPIC_API_KEY` |
| **Claude Local** | `ProviderClaudeLocal` | Uses a locally installed `claude` CLI binary |
| **OpenAI** | `ProviderOpenAI` | Requires `OPENAI_API_KEY` |
| **OpenAI-Compatible** | `ProviderOpenAICompatible` | Any OpenAI-compatible endpoint |
| **Google Gemini** | `ProviderGemini` | Requires `GEMINI_API_KEY` |

Set the active provider with the `AI_PROVIDER` environment variable or in your tool's configuration.

## 🏁 Quick Start

The fastest way to create a new GTB-based tool is with the scaffold command:

```bash
gtb generate project
```

This launches an interactive wizard to configure your project. For automation:

```bash
gtb generate project --name mytool --repo myorg/mytool --description "My CLI tool" --path ./mytool
```

For a GitLab-hosted project with nested groups:

```bash
gtb generate project --name mytool --repo myorg/mygroup/mytool --git-backend gitlab --host gitlab.mycompany.com --path ./mytool
```

### Generated Project Structure

The scaffold produces a fully wired project. The key entry points are:

**`cmd/mytool/main.go`.** The entry point, reading version from `internal/version`. `Execute` runs the tree with a signal-aware context: SIGINT/SIGTERM cancel `cmd.Context()` for graceful shutdown, a second signal force-exits, and a signal-terminated run exits `128+signum`:
```go
func main() {
    rootCmd, p := root.NewCmdRoot(version.Get())
    gtbRoot.Execute(rootCmd, p)
}
```

**`pkg/cmd/root/cmd.go`.** Wires the Props container and the root command:
```go
//go:embed assets/*
var assets embed.FS

func NewCmdRoot(v version.Info) (*setup.Command, *props.Props) {
    l := logger.NewCharm(os.Stderr, logger.WithTimestamp(true))

    p := &props.Props{
        Tool: props.Tool{
            Name:        "mytool",
            Description: "My CLI tool",
            EnvPrefix:   "MYTOOL",
            ReleaseSource: props.ReleaseSource{
                Type:  "gitlab",
                Host:  "gitlab.com",
                Owner: "myorg",
                Repo:  "mytool",
            },
        },
        Logger:  l,
        FS:      afero.NewOsFs(),
        Version: v,
        Assets:  props.NewAssets(props.AssetMap{"root": &assets}),
    }
    p.ErrorHandler = errorhandling.New(logger.ToSlog(l), p.Tool.Help)

    return gtbRoot.NewCmdRoot(p, NewCmdServe(p)), p
}
```

> [!NOTE]
> Command constructors return **`*setup.Command`**, not `*cobra.Command`. It embeds `*cobra.Command` and carries the feature the command belongs to, so it behaves as a cobra command everywhere; `.Command` exposes the raw pointer when a cobra API needs one. Register child commands with `parent.Register(child)` rather than `AddCommand`, so the feature middleware chain is applied.

**`internal/version/version.go`.** Populated from GoReleaser ldflags at release, or from `runtime/debug` VCS info in development:
```go
var (
    version = "dev"
    commit  = "none"
    date    = "unknown"
)
```

## 🏳️ Feature Flags

Commands can be selectively disabled or opt-in features enabled via the `Tool` configuration:

| Feature | Default | Description |
| :--- | :--- | :--- |
| `update` | **enabled** | Self-update capability |
| `init` | **enabled** | Environment bootstrap command |
| `mcp` | **enabled** | Model Context Protocol server |
| `docs` | **enabled** | Documentation browser |
| `doctor` | **enabled** | Diagnostic health checks |
| `changelog` | **enabled** | Version history command |
| `ai` | disabled | AI-powered features (opt-in) |
| `config` | disabled | Configuration read/write command |
| `telemetry` | disabled | Pseudonymous usage telemetry |
| `man` | disabled | roff man-page generation |

`version` is always present and is not gated. Only builtin features may be default-enabled; a plugin feature that declares itself default-on is rejected with `ErrPluginDefaultOn`.

```go
p := &props.Props{
    Tool: props.Tool{
        // ...
        Features: props.SetFeatures(
            props.Disable(props.UpdateCmd), // disable self-update
            props.Enable(props.AiCmd),      // opt-in to AI features
        ),
    },
}
```

## 📂 Project Layout

Standard layout for GTB projects:

```
.
├── cmd/
│   └── mytool/
│       └── main.go              # Entry point
├── pkg/
│   └── cmd/
│       └── root/
│           ├── cmd.go           # Root command and Props setup
│           └── assets/
│               └── init/
│                   └── config.yaml  # Default configuration
├── internal/
│   └── version/
│       └── version.go           # Version info (ldflags + runtime/debug)
├── go.mod
└── README.md
```
