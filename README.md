# Go Tool Base (GTB)

[![Tests](https://github.com/phpboyscout/go-tool-base/actions/workflows/test.yaml/badge.svg)](https://github.com/phpboyscout/go-tool-base/actions/workflows/test.yaml)
[![Coverage](https://img.shields.io/badge/coverage-68%25-brightgreen)](https://github.com/phpboyscout/go-tool-base/actions/workflows/test.yaml)

**The Intelligent Application Lifecycle Framework for Go.**

Modern CLI tools, DevOps workflows, and developer utilities demand far more than basic flag parsing. GTB works as a "batteries-included" micro-framework (like Rails or Laravel), but meticulously tailored for Go command-line applications and beyond.

## ✅ What GTB IS / IS NOT

- **IS a full-lifecycle framework** — provides configuration, versioning, auto-updates, embedded TUI docs, error handling, and structured logging cleanly out-of-the-box.
- **IS a dependency injection container** — services are explicitly passed via the decoupled `Props` container to every command constructor.
- **IS an AI-ready foundation** — built-in agentic loop orchestration and MCP exposition.
- **NOT a web framework (like Gin/Fiber)** or a microservice generator (like Sponge). GTB primarily bootstraps CLI utilities and background daemons, though you can easily build a `serve` command that boots an HTTP router via GTB's DI container!

> [!IMPORTANT]
> **Full Documentation**: For detailed guides, component deep-dives, framework comparisons, and API references, please visit our documentation site:
> **[https://gtb.phpboyscout.uk](https://gtb.phpboyscout.uk)**

## 📦 CLI Installation

To install the `gtb` automation CLI, use the recommended installation script for your platform:

**macOS/Linux (bash/zsh):**
```bash
curl -sSL -H "Authorization: Bearer ${GITHUB_TOKEN}" -H "Accept: application/vnd.github.v3.raw" "https://github.com/phpboyscout/go-tool-base/raw/main/install.sh" | bash
```

**Windows (PowerShell):**
```powershell
$env:GITHUB_TOKEN = "your_token_here"
irm "https://github.com/phpboyscout/go-tool-base/raw/main/install.ps1" -Headers @{Authorization = "Bearer $env:GITHUB_TOKEN"} | iex
```

> [!NOTE]
> For developers building from source, you can still use `go install github.com/phpboyscout/go-tool-base@latest`. However, this method will not include pre-built documentation assets, and the `docs` command will operate in a limited "source-build" mode.

## 🚀 Key Advantages & Features

- **🤖 AI Agentic Workflows**: Integrated support for Claude, Gemini, and OpenAI to power autonomous ReAct-style loops and built-in Q&A against your embedded docs.
- **🔌 Model Context Protocol (MCP)**: Expose your CLI commands automatically as MCP tools for use by IDEs and external AI agents.
- **📦 Auto Updates & Lifecycle**: Seamless, zero-config version checking and self-update capabilities directly from GitHub/GitLab releases via the built-in `update` command.
- **📕 TUI Documentation**: A built-in, interactive terminal browser for your markdown documentation. Forget generic man pages.
- **🧱 Scaffold**: Generate production-ready, interface-driven CLI tool skeletons in seconds.
- **⚙️ Robust Configuration**: Overridable configurations seamlessly merging from files, environment variables, and embedded assets.
- **🏢 Enterprise VCS**: Deep integration with GitHub Enterprise and GitLab (including nested group paths) for auth, PR management, and assets.
- **🩹 Error Handling**: Structured, testable error management with logging, stack traces, and integrated help context routing to user-facing output.

## 🏗️ Core Architecture

The framework is built around a centralized **Props** container that provides type-safe access to all system dependencies:

| Component | Responsibility |
| :--- | :--- |
| **[pkg/props](docs/components/props.md)** | Central dependency injection container for logger, config, and assets. |
| **[pkg/config](docs/components/config.md)** | Viper-powered configuration with observer patterns and testing mocks. |
| **[pkg/chat](docs/components/chat.md)** | Unified multi-provider AI client (Claude, OpenAI, Gemini, Claude Local). |
| **[pkg/controls](docs/components/controls.md)** | Service lifecycle management and message-based coordination. |
| **[pkg/setup](docs/components/setup/)** | Bootstrap logic: auth, key management, and pluggable self-updating. |
| **[pkg/vcs](docs/components/version-control.md)** | Pluggable GitHub/GitLab API and Git operations abstraction. |
| **[pkg/errorhandling](docs/components/error-handling.md)** | Structured errors with stack traces and log integration. |

## 🛠️ Built-in Commands

Every tool built on GTB inherits these essential capabilities:

- **`init`**: Bootstraps local environments, configures GitHub/GitLab auth, and manages SSH keys.
- **`version`**: Reports the current version and checks for available updates.
- **`update`**: Downloads and installs the latest release binary from GitHub or GitLab.
- **`mcp`**: Exposes CLI commands as Model Context Protocol (MCP) tools for use in IDEs.
- **`docs`**: Interactive terminal browser for documentation with built-in AI Q&A.
- **`doctor`**: Runs diagnostic checks to validate configuration, connectivity, and runtime environment.

Commands can be selectively enabled or disabled at bootstrap time via feature flags — see [Feature Flags](#-feature-flags) below.

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

**`cmd/mytool/main.go`** — entry point, reads version from `internal/version`:
```go
func main() {
    rootCmd, p := root.NewCmdRoot(version.Get())
    gtbRoot.Execute(rootCmd, p)
}
```

**`pkg/cmd/root/cmd.go`** — wires the Props container and root command:
```go
//go:embed assets/*
var assets embed.FS

func NewCmdRoot(v pkgversion.Info) (*cobra.Command, *props.Props) {
    l := logger.NewCharm(os.Stderr, logger.WithTimestamp(true))

    p := &props.Props{
        Tool: props.Tool{
            Name:        "mytool",
            Description: "My CLI tool",
            ReleaseSource: props.ReleaseSource{
                Type:  "github",
                Host:  "github.com",
                Owner: "myorg",
                Repo:  "mytool",
            },
        },
        Logger:  l,
        FS:      afero.NewOsFs(),
        Version: v,
        Assets:  props.NewAssets(props.AssetMap{"root": &assets}),
    }
    p.ErrorHandler = errorhandling.New(l, p.Tool.Help)

    return gtbRoot.NewCmdRoot(p), p
}
```

**`internal/version/version.go`** — populated from GoReleaser ldflags at release, or from `runtime/debug` VCS info in development:
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
| `ai` | disabled | AI-powered features (opt-in) |

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
