# Go Tool Base (GTB)

A comprehensive Go framework for building mission-critical CLI tools. GTB provides a standardized foundation with dependency injection, service lifecycle management, AI-powered automation, and deep GitHub Enterprise integration.

> [!IMPORTANT]
> **Full Documentation**: For detailed guides, component deep-dives, and API references, please visit our documentation site:
> **[https://pages.github.com/ptps/gtb/](https://pages.github.com/ptps/gtb/)**

## 📦 CLI Installation

To install the `gtb` automation CLI, use the recommended installation script for your platform:

**macOS/Linux (bash/zsh):**
```bash
curl -sSL -H "Authorization: Bearer ${GITHUB_TOKEN}" -H "Accept: application/vnd.github.v3.raw" "https://github.com/ptps/gtb/raw/main/install.sh" | bash
```

**Windows (PowerShell):**
```powershell
$env:GITHUB_TOKEN = "your_token_here"
irm "https://github.com/ptps/gtb/raw/main/install.ps1" -Headers @{Authorization = "Bearer $env:GITHUB_TOKEN"} | iex
```

> [!NOTE]
> For developers building from source, you can still use `go install github.com/phpboyscout/gtb@latest`. However, this method will not include pre-built documentation assets, and the `docs` command will operate in a limited "source-build" mode.

## 🚀 Key Features

- **Scaffold**: Generate production-ready CLI tools in seconds using the built-in generator.
- **AI-Powered**: Integrated support for Claude, Gemini, and OpenAI to power autonomous repair and documentation Q&A.
- **Robust Configuration**: Flexible loading from files, environment variables, and embedded assets with hot-reloading support.
- **🔄 Lifecycle Management**: Unified control for starting, stopping, and coordinating concurrent background services.
- **🏢 Enterprise VCS**: Deep integration with GitHub Enterprise and GitLab for repository operations, PR management, and release assets.
- **🩹 Error Handling**: Structured error management with stack traces, severity levels, and integrated help context.
- **🛡️ Multi-Factor Auth**: Built-in support for GitHub and GitLab authentication and SSH key management.
- **📦 Auto Updates**: Seamless version checking and self-update capabilities directly from GitHub and GitLab releases.

## 🏗️ Core Architecture

The framework is built around a centralized **Props** container that provides type-safe access to all system dependencies:

| Component | Responsibility |
| :--- | :--- |
| **[pkg/props](docs/components/props.md)** | Central dependency injection container for logger, config, and assets. |
| **[pkg/config](docs/components/config.md)** | Viper-powered configuration with observer patterns and testing mocks. |
| **[pkg/chat](docs/components/chat.md)** | Unified multi-provider AI client (OpenAI, Vertex AI, Anthropic). |
| **[pkg/controls](docs/components/controls.md)** | Service lifecycle management and message-based coordination. |
| **[pkg/setup](docs/components/setup.md)** | Bootstrap logic: auth, key management, and pluggable self-updating. |
| **[pkg/vcs](docs/components/version-control.md)** | Pluggable GitHub/GitLab API and Git operations abstraction. |
| **[pkg/errorhandling](docs/components/error-handling.md)** | Structured errors with stack traces and log integration. |

## 🛠️ Built-in Commands

Every tool built on GTB inherits these essential capabilities:

- **`init`**: Bootstraps local environments, configures GitHub auth, and manages SSH keys.
- **`version`**: Reports the current version and automatically checks for available updates.
- **`update`**: Downloads and installs the latest release binary from GitHub or GitLab.
- **`mcp`**: Exposes CLI commands as Model Context Protocol (MCP) tools for use in IDEs.
- **`docs`**: Interactive terminal browser for documentation with built-in AI Q&A.

## 🏁 Quick Start

```go
package main

import (
	"embed"
	"os"

	"github.com/phpboyscout/gtb/pkg/cmd/root"
	"github.com/phpboyscout/gtb/pkg/props"
	"github.com/charmbracelet/log"
	"github.com/spf13/afero"
)

//go:embed assets/*
var assets embed.FS

func main() {
	p := &props.Props{
		Tool: props.Tool{
			Name: "mytool",
			GitHub: props.GitHub{Org: "myorg", Repo: "mytool"},
		},
		Logger: log.New(os.Stderr),
		FS:     afero.NewOsFs(),
		Assets: assets,
	}

	rootCmd := root.NewCmdRoot(p, []embed.FS{assets})
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
```

## 📂 Project Layout

Standardized layout for GTB projects:

```bash
.
├── cmd/
│   └── tool/          # CLI package
│       ├── main.go    # Entry point
│       └── assets/    # Embedded configs/templates
├── pkg/
│   └── cmd/           # Internal command implementations
│       └── root/      # Root command setup
├── go.mod
└── README.md
```

---
*Built with ❤️ by the PTPS Team.*
