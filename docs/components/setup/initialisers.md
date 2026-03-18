---
title: Initialisers
description: Technical reference for the Initialiser pattern and built-in implementations like GitHub and AI.
date: 2026-02-16
tags: [components, setup, initialisers, architecture]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Initialisers

This document provides a technical deep dive into the `Initialiser` interface, the lifecycle of an initialiser, and the specific implementation details of the built-in initialisers.

For a high-level conceptual overview of the Initialiser pattern, please see the [Initialisers Concept Documentation](../../concepts/initialisers.md).

## Interface Definition

The `setup.Initialiser` interface is the core contract for all initialization logic. It is defined in `pkg/setup/setup.go`:

```go
type Initialiser interface {
    // Name returns a human-readable name for logging.
    Name() string

    // IsConfigured returns true if the feature's config is already present.
    // It should check for the presence of required configuration keys.
    IsConfigured(cfg config.Containable) bool

    // Configure runs the interactive setup and writes values into the config.
    // It is only called if IsConfigured returns false.
    Configure(props *props.Props, cfg config.Containable) error
}
```

### Key Considerations for Implementers

1.  **Idempotency**: `IsConfigured` must be robust. It is called every time `init` is run. If it returns `false` incorrectly, the user will be prompted unnecessarily.
2.  **Configuration Isolation**: While `Configure` receives the full `config.Containable`, an initialiser should ideally only modify keys relevant to its feature domain (e.g., `github.*` or `ai.*`).
3.  **Error Handling**: Errors returned from `Configure` will halt the entire initialization process. Ensure critical failures are handled gracefully or returned with explanatory context.

## Registration Lifecycle

Initialisers are registered via the `setup.Register` function, typically in a package's `init()` function.

```go
func Register(
    featureName string,
    initialisers []InitialiserProvider,
    subcommands []SubcommandProvider,
    flags []FeatureFlag,
)
```

### The Registration Flow

1.  **Package Init**: When the application starts, packages invoke `setup.Register`. The setup package stores these providers in a global registry.
2.  **Command Construction**:
    *   The **Root Init Command** iterates over the registry.
    *   It checks `props.Tool.IsEnabled(feature)` to see if the feature is active.
    *   If active, it adds any registered `FeatureFlag`s to the root `init` command flags.
3.  **Command Execution**:
    *   When `init` runs, it calls `setup.Initialise`.
    *   `setup.Initialise` instantiates `Initialiser`s using the registered `InitialiserProvider`s.
    *   It iterates through them, calling `IsConfigured`.
    *   If not configured (and not skipped via flag), `Configure` is executed.

## Built-in Initialisers Implementation

### 1. GitHub Initialiser

**Package**: `pkg/setup/github`

The GitHub initialiser manages two distinct configuration areas: **Authentication** (OAuth token) and **SSH Keys**.

#### Configuration Keys
*   `github.auth.value`: The GitHub Personal Access Token (PAT).
*   `github.auth.env`: (Optional) Name of the environment variable holding the token.
*   `github.ssh.key.path`: Path to the private SSH key.
*   `github.ssh.key.type`: Type of key (e.g., `rsa`, `ed25519`) or `agent`.

#### Technical Workflow
1.  **Auth Check**: Checks for `GITHUB_TOKEN` env var. If present, it validates it against the GitHub API `user` endpoint. If valid, it skips prompting.
2.  **Token Prompt**: If no valid env var, it prompts the user to paste a token.
3.  **SSH Scan**: Scans `~/.ssh` for files matching standard patterns (`id_rsa`, `id_ed25519`, etc.).
4.  **Key Selection**: Uses `charmbracelet/huh` to present a list of found keys + a "Generate New" option.
5.  **Agent Support**: Can be configured to use `ssh-agent` instead of a direct key file.

### 2. AI Initialiser

**Package**: `pkg/setup/ai`

The AI initialiser abstracts over multiple LLM providers, normalizing their configuration into a common structure.

#### Configuration Keys
*   `ai.provider`: The selected provider identifier (`openai`, `claude`, `gemini`).
*   `ai.claude.key`: Anthropic API key.
*   `ai.openai.key`: OpenAI API key.
*   `ai.gemini.key`: Google Gemini API key.

#### Technical Workflow
1.  **Provider Selection**: User selects a provider from a list.
2.  **Key Input**: User inputs the API key.
    *   *Security Note*: The input field is masked (echo mode password).
3.  **Env Var Detection**: The initialiser checks for standard environment variables (e.g., `OPENAI_API_KEY`) corresponding to the selected provider.
    *   It displays a **warning note** in the UI if an env var is detected, informing the user that the env var will take precedence over the config file value they are about to set.
4.  **Persistence**: The provider choice and the specific key are written to the config file.

## Creating Custom Initialisers

For a step-by-step guide on implementing your own initialiser, referring to the [How-to Guide](../../how-to/add-initialiser.md).
