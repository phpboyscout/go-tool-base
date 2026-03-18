---
title: Init Command
description: Initialize tool configuration, GitHub authentication, and SSH keys.
date: 2026-02-16
tags: [components, commands, init, setup]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Init Command

The `init` command initializes the tool's configuration and performs initial setup.

## Usage

```bash
mytool init [flags]
mytool init [subcommand]
```

## Description

Initializes the default configuration for the tool, sets up authentication with GitHub (if needed), and configures SSH keys. This command creates configuration files in the appropriate directories and prepares the tool for first use.

## Flags

| Flag | Description | Default |
| :--- | :--- | :--- |
| `-d, --dir` | Directory to initialize the config in | `~/.mytool/` |
| `-c, --clean` | Reset existing configuration and replace with defaults | `false` |
| `-l, --skip-login` | Skip the GitHub login process | `false` (or `true` in CI) |
| `-k, --skip-key` | Skip SSH key configuration | `false` (or `true` in CI) |

!!! info "CI Mode Detection"
    When the `CI` environment variable is set to `true`, the `--skip-login` and `--skip-key` flags default to `true` to avoid interactive prompts in automated environments.

## Example

```bash
# Initialize with default settings
mytool init

# Initialize and reset existing config
mytool init --clean

# Initialize to a custom directory
mytool init --dir /etc/mytool/
```

## Subcommands

### Init GitHub

Force reconfiguration of GitHub authentication and SSH keys, regardless of current configuration state.

**Usage:**
```bash
mytool init github [--dir <path>]
```

**Description:**
Runs the full GitHub authentication flow (token generation and SSH key configuration) even if already configured. Useful when tokens expire or you need to switch accounts.

### Init AI

When the AI feature is enabled, the `init` command gains an `ai` subcommand for configuring AI provider integration.

**Usage:**
```bash
mytool init ai
```

**Description:**
Configures the AI provider and API keys used by AI-powered features. Presents an interactive form to select a provider and enter API keys for **Claude**, **OpenAI**, and **Gemini**.

## Implementation

The init command is implemented in `cmd/initialise/init.go` and uses the `pkg/setup` package to perform the actual initialization work.
