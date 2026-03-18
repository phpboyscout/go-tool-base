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

### [Command Constructor Pattern](command-constructors.md)
Understand why we use `NewCmd*` constructors for explicit dependency injection and testability.

### [Functional Options Pattern](functional-options.md)
Learn how the framework uses functional options for flexible, backward-compatible constructors across controllers, clones, and TUI components.

### [Interface Design Principles](interface-design.md)
Comprehensive guide to all public interfaces in GTB, their purposes, and mock generation strategies.


### [Project Structure](project-structure.md)
Understand the filesystem layout of a GTB project and the reasoning behind it.

### [Framework CLI](framework-cli.md)
Discover why we use a specialized CLI for scaffolding, regeneration, and maintaining architectural consistency.

### [Regeneration & Sync](regeneration.md)
Learn about the bi-directional synchronization between your manifest and source code.

### [Dependency Injection (Props)](props.md)
Learn about the `Props` container, the central nervous system that provides context to every command.

### [Configuration Precedence](config.md)
Understand how defaults, files, environment variables, and flags merge to create a robust runtime configuration.

### [Universal Asset Management](asset-management.md)
Explore embedded assets, multi-filesystem merging, and how the framework manages structured data across the application.

### [Integrated Documentation](integrated-docs.md)
Learn about the CLI-first documentation browser and AI-powered Q&A system.

### [Tool Initialisers & Feature Setup](feature-setup.md)
Understand the architecture of modular features, self-registration, and initialisation logic.

### [Auto-Update Lifecycle](auto-update.md)
Learn how the framework manages throttled version checks and atomic binary replacement.

### [VCS & Repository Abstraction](vcs-repositories.md)
Explore the polymorphic repository strategy and unified GitHub automation API.

### [Service Orchestration & Control](service-orchestration.md)
Understand how the framework manages the lifecycle, health, and graceful shutdown of background services.

### [TUI & Configuration Patterns](tui-patterns.md)
Understand best practices for interactive setup, environment precedence disclosure, and sensitive data handling.

### [Centralized Error Handling](error-handling.md)
Learn about the `ErrorHandler` interface and how the framework manages logging and user support.

### [AI Agents & MCP](ai-agents.md)
How to expose your CLI tool as an autonomous agent for LLMs to control.

### [AI-Powered Features](ai-features.md)
How to consume AI services to build intelligent features within your tool.
