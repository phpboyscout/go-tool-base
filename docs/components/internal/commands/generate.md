---
title: Generate Command
description: Internal command for scaffolding projects, commands, and flags.
date: 2026-02-16
tags: [components, internal, commands, generate]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Generate Command

The `generate` command group handles the creation of new assets (projects, commands, flags).

## Help Output (`generate --help`)

```text
Scaffold new projects (skeletons) or add new commands to existing gtb projects.

Usage:
  gtb generate [flags]
  gtb generate [command]

Available Commands:
  add-flag    Add a new flag to an existing command
  command     Generate a new command or subcommand
  docs        Generate documentation for a command using AI
  skeleton    Generate a new cli project skeleton

Flags:
  -h, --help              help for generate
      --model string      AI model to use (per-provider defaults apply when omitted)
      --provider string   AI provider to use (openai/claude/gemini/claude-local/openai-compatible)

Global Flags:
      --ci                   flag to indicate the tools is running in a CI environment
      --config stringArray   config files to use (default [/home/mcockayne/.gtb/config.yaml,/etc/gtb/config.yaml])
      --debug                forces debug log output
```

## Subcommands

### Skeleton

Generates a new project structure from scratch.

**Help (`generate skeleton --help`):**

```text
Generate a new cli project skeleton

Aliases:
  skeleton, project, cli

Flags:
  -d, --description string   Project description (default "A tool built with gtb")
  -f, --features strings     Features to enable (init, update, mcp, docs, doctor, changelog, keychain, ai, config, telemetry) (default [init,update,mcp,docs,doctor,changelog,keychain])
      --git-backend string   Git backend (github or gitlab) (default "github")
      --go-version string    Go version for go.mod (defaults to the running toolchain version)
  -h, --help                 help for skeleton
      --host string          Git Host (e.g. github.com) (default "github.com")
  -n, --name string          Project name (e.g. mytool)
  -p, --path string          Destination path (default ".")
  -r, --repo string          Git repository (e.g. myorg/mytool)
```

#### Feature flags

The `--features` flag controls which subsystems and side-effect activations appear in the scaffolded project. Defaults are selected in the interactive wizard and reflected in the manifest so `gtb regenerate` preserves the choice.

| Feature | Default | What it scaffolds |
|---------|---------|-------------------|
| `init` | on | `init` subcommand |
| `update` | on | self-update subsystem |
| `mcp` | on | MCP server subsystem |
| `docs` | on | built-in docs browser |
| `doctor` | on | `doctor` subsystem |
| `changelog` | on | changelog subsystem |
| `keychain` | on | `cmd/<name>/keychain.go` blank-importing `pkg/credentials/keychain` — activates the OS keychain backend by default. Delete the scaffolded file for a regulated build (no go-keyring / godbus / wincred linked). See [How to configure credentials](../../../how-to/configure-credentials.md) and [How to implement a custom credential backend](../../../how-to/custom-credential-backend.md). |
| `ai` | off (opt-in) | AI chat subsystem |
| `config` | off (opt-in) | `config` subsystem with get/set/list |
| `telemetry` | off (opt-in) | telemetry subsystem |

Example — generate a regulated build with no keychain code linked:

```bash
gtb generate project -n mytool -r myorg/mytool \
  --features init,update,mcp,docs,doctor,changelog
```

### Command

Generates a new Cobra command, optionally using AI or from a script.

**Help (`generate command --help`):**

```text
Generate a new command or subcommand with boilerplate code.

Examples:
  # Generate a command named 'login' in the current project
  gtb generate command --name login --short "Login to the system"

  # Generate a subcommand 'list' under 'login'
  gtb generate command --name list --parent login --short "List sessions"

  # Generate a command with flags and assets
  gtb generate command -n serve -f "port:int:Port to listen on" --assets

  # Generate a command from a script (e.g., bash)
  # The AI will attempt to convert the script to Go code
  # The autonomous agent is used by default for verification
  gtb generate command -n backup --script ./backup.sh

  # Use the original feedback loop instead of the autonomous agent
  gtb generate command -n backup --script ./backup.sh --agentless

  # Create a protected command (cannot be overwritten by generator)
  gtb generate command -n sensible --protected

  # Temporarily unprotect a command to allow overwrite
  gtb generate command -n sensible --protected=false --force

Usage:
  gtb generate command [flags]
  gtb generate command [command]

Available Commands:
  protect     Protect a command from being overwritten
  unprotect   Unprotect a command to allow overwriting

Flags:
      --agentless            Use original retry loop instead of autonomous agent
      --alias strings        Aliases for the command
      --assets               Include assets directory support
      --flag strings         Flags definition (name:type:description:persistent)
      --force                Overwrite existing files
  -h, --help                 help for command
      --long string          Long description
  -n, --name string          Command name (kebab-case)
      --parent string        Parent command name (default: root) (default "root")
  -p, --path string          Path to project root (default ".")
      --persistent-pre-run   Generate a PersistentPreRun hook
      --pre-run              Generate a PreRun hook
      --prompt string        Natural language description or path to a file containing the description
      --protected            Mark the command as protected (tri-state: --protected for true, --protected=false for false, omitted for nil)
      --script string        Path to a script to convert to Go (bash/python/js)
  -s, --short string         Short description
```

### Add Flag

Injects a new flag into an existing command file.

**Help (`generate add-flag --help`):**

```text
Add a new flag to an existing command

Usage:
  gtb generate add-flag [flags]

Flags:
  -c, --command string       Command name to add the flag to
  -d, --description string   Flag description
  -h, --help                 help for add-flag
  -n, --name string          Flag name
  -p, --path string          Path to project root (default ".")
      --persistent           Make the flag persistent
  -t, --type string          Flag type (string, bool, int, float64, stringSlice, intSlice) (default "string")
```

### Docs

Generates documentation for a command using AI analysis of the source code.

**Help (`generate docs --help`):**

```text
Generate comprehensive Markdown documentation for a Go command using AI.
This command analyzes the source code of the specified command and uses the AI integration to generate docs following Zensical/MkDocs conventions.

Examples:
  # Generate docs for a command
  gtb generate docs --path ./internal/cmd/mycmd

Usage:
  gtb generate docs [flags]

Flags:
      --command string   Name/Path of command to document
  -h, --help             help for docs
  -n, --name string      Command name (optional, inferred from path)
      --package string   Path to package to document (relative to project root)
      --parent string    Parent command name (optional, if not in manifest)
      --path string      Path to project root (default ".")
      --source string    Path to the command source code (deprecated, use --command)
```
