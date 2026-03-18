---
title: Architectural Overview
description: High-level architectural overview of the GTB framework and component relationships.
date: 2026-02-16
tags: [concepts, architecture, design]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Architectural Overview

This guide provides a high-level view of how the various components of GTB interact to create a cohesive CLI framework.

## Component Relationships

At the heart of every GTB application is the `Props` container, which orchestrates the primary services. The following diagram illustrates the relationships between these core components:

```mermaid
classDiagram
    class Props {
        +Tool Tool
        +Logger Logger
        +Config Containable
        +Assets Assets
        +FS afero.Fs
        +Version Version
        +ErrorHandler ErrorHandler
    }
    class Containable {
        <<interface>>
        +Get(key) any
        +GetString(key) string
        +Sub(key) Containable
    }
    class Assets {
        <<interface>>
        +Add(fs)
        +Merge(assets)
    }
    class ErrorHandler {
        +Fatal(err)
        +Error(err)
    }

    Props o-- Containable : uses
    Props o-- Assets : uses
    Props o-- ErrorHandler : uses

    Containable ..> Viper : wraps
    Props ..> Afero : uses
    Props ..> CharmLog : uses
```

## Core Workflows

### 1. Application Initialization

When a user runs the `init` command, the framework performs a multi-stage bootstrapping process:

```mermaid
sequenceDiagram
    participant User
    participant CLI as Root Command
    participant Init as Init Command
    participant Setup as Setup Package
    participant Assets as Embedded Assets
    participant Config as Config File

    User->>CLI: run "init"
    CLI->>Init: delegate to Init
    Init->>Setup: Initialise(props)
    Setup->>Assets: Load default config template
    Setup->>User: Prompt for GitHub Login (optional)
    Setup->>User: Prompt for SSH Key (optional)
    Setup->>Config: Write merged configuration
    Setup-->>Init: Return config location
    Init-->>User: Success Message
```

### 2. Dependency Injection Flow

Dependencies are injected from the entry point (`main.go`) through the `Props` struct:

1.  **Creation**: `Props` is instantiated with the basic environment (Logger, Version).
2.  **Configuration**: The `config` package loads settings into `Props.Config`.
3.  **Command Wiring**: Subcommands are created with a reference to `Props`, giving them immediate access to all services.
4.  **Execution**: Commands use `Props.ErrorHandler` to ensure consistent terminal output and exit codes.

## Design Principles

*   **Explicit over Implicit**: We prefer passing `Props` over using `context.Context` for dependencies (see [Props documentation](../components/props.md) for the rationale).
*   **Interface Segregation**: Core services (Config, Assets, VCS) are defined by interfaces to enable clean mocking in unit tests.
*   **Consistent Error Handling**: All user-facing errors funnel through the `ErrorHandler` to maintain a unified look and feel.
