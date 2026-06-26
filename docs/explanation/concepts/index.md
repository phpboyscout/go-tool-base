---
title: Concepts
description: Index of core concepts and architectural patterns in GTB.
date: 2026-02-16
tags: [concepts, index, overview]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Concepts

To get the most out of GTB, it is helpful to understand the core concepts and architectural patterns that drive its design. This section provides a deep dive into the framework's "Why" and the underlying mechanics.

## Core Pillars

### [Architecture Fundamentals](architecture.md)
Explore the high-level system design, command registry, and execution flow.

### [Command Constructor Pattern](functional-options.md)
Understand why we use `NewCmd*` constructors for explicit dependency injection and testability.

### [Functional Options Pattern](functional-options.md)
Learn how the framework uses functional options for flexible, backward-compatible constructors across controllers, clones, and TUI components.

### [Interface Design Principles](interface-design.md)
Comprehensive guide to all public interfaces in GTB, their purposes, and mock generation strategies.

### [Logging Abstraction](../components/logger.md)
Understand the unified `logger.Logger` interface, backend selection (charmbracelet, slog, noop), and ecosystem integration patterns.


### [Project Structure](project-structure.md)
Understand the filesystem layout of a GTB project and the reasoning behind it.

### [Framework CLI](framework-cli.md)
Discover why we use a specialized CLI for scaffolding, regeneration, and maintaining architectural consistency.

### [Regeneration & Sync](regeneration.md)
Learn about the bi-directional synchronization between your manifest and source code.

### [Dependency Injection (Props)](../components/props.md)
Learn about the `Props` container, the central nervous system that provides context to every command.

### [Configuration Precedence](../components/config/index.md)
Understand how defaults, files, environment variables, and flags merge to create a robust runtime configuration.

### [Universal Asset Management](../components/assets.md)
Explore embedded assets, multi-filesystem merging, and how the framework manages structured data across the application.

### [Asset Management](../components/assets.md)
A focused look at embedded assets, virtual filesystems, and configuration merging.

### [Integrated Documentation](../components/docs.md)
Learn about the CLI-first documentation browser and AI-powered Q&A system.

### [Tool Initialisers & Feature Setup](feature-setup.md)
Understand the architecture of modular features, self-registration, and initialisation logic.

### [Tool Initialisers](../components/setup/initialisers.md)
A deeper look at the Initialiser pattern for modular, self-registering feature configuration.

### [Command Middleware System](../components/setup/middleware.md)
Understand the middleware chain pattern for cross-cutting CLI command concerns.

### [Transport Middleware & Resilience](transport-middleware.md)
Understand middleware/interceptor chains as the extension point for cross-cutting transport concerns — logging, auth, rate limiting, retry, circuit breaking — across HTTP and gRPC, server and client.

### [Auto-Update Lifecycle](../components/update.md)
Learn how the framework manages throttled version checks and atomic binary replacement.

### [Release-binary Signing](release-binary-signing.md)
How gtb-derived tools establish a cryptographic chain of trust between you, the release pipeline, and the people running your CLI — without anyone holding a private key on their laptop.

### [VCS & Repository Abstraction](../components/vcs/repo.md)
Explore the polymorphic repository strategy and unified GitHub automation API.

### [Service Orchestration & Control](service-orchestration.md)
Understand how the framework manages the lifecycle, health, and graceful shutdown of background services.

### [TUI & Configuration Patterns](tui-patterns.md)
Understand best practices for interactive setup, environment precedence disclosure, and sensitive data handling.

### [Centralized Error Handling](../components/error-handling.md)
Learn about the `ErrorHandler` interface and how the framework manages logging and user support.

### [AI Agents & MCP](../components/mcp-agents.md)
How to expose your CLI tool as an autonomous agent for LLMs to control.

### [Manifest Architecture](manifest.md)
Understand how the `.gtb/manifest.yaml` acts as the source of truth for your CLI scaffolding.

### [AI-Powered Features](../components/chat.md)
How to consume AI services to build intelligent features within your tool.

### [Credentials Architecture](../components/credentials.md)
The conceptual storage modes, trust model, and consumer architecture for user-supplied secrets.

### [Telemetry Architecture & Concepts](../components/telemetry.md)
Architectural concepts, privacy controls, data handling, and design limitations behind GTB's telemetry framework.
