---
title: What is GTB?
description: An overview of what Go Tool Base (GTB) is, its core philosophy, and its key advantages.
date: 2026-03-23
tags: [overview, pitch, strategy]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# What is Go Tool Base (GTB)?

Command-line tools, long-running services and binaries that serve their own UI all demand far more than basic flag parsing. **Go Tool Base (GTB)** is a comprehensive lifecycle framework that makes Go applications **Agentic**, **Self-sustaining**, and **Self-documenting** out of the box.

It provides the "batteries included" experience of a macro-framework (like Rails or Laravel), tailored for **Go applications**: the one-shot command-line tool, the bot or daemon that runs for weeks, and the binary that serves a local web studio or backs a desktop app.

---

## ✅ What GTB IS

- **A full-lifecycle application framework** for Go: covering configuration, versioning, auto-updates, embedded documentation, error handling, and AI integration.
- **A framework for three shapes of application**: command-line tools, long-running processes and binaries with their own UI, often all in one binary. Every shape gets the same built-in commands (`init`, `version`, `update`, `doctor`, `docs`, `mcp`).
- **A dependency injection container**: the `Props` struct orchestrates services (config, assets, logging, VCS, AI) and is explicitly passed to commands for ultimate testability.
- **A scaffold and generator**: `gtb generate skeleton` creates a ready-to-ship, robustly structured project in seconds.
- **A service supervisor**: long-running work registers with [go/controls](https://controls.go.phpboyscout.uk) for ordered start-up, health probes and graceful shutdown, and a run with no terminal attached (systemd, a container, cron) starts without prompting anyone.

## ❌ What GTB is NOT

- **NOT a web framework.** It does not provide HTTP routers, middleware pipelines, or HTML template engines. It is not a competitor to Gin, Echo, Fiber, or Buffalo. *(A GTB application can still serve HTTP: a `serve` or `studio` command boots the handler you choose on GTB's hardened server, inside the same lifecycle.)*
- **NOT a microservice scaffold.** It does not generate gRPC boilerplate like Sponge or Go-Blueprint.
- **NOT just a command router.** Unlike Cobra (a library for routing commands), GTB is an opinionated framework providing the entire application lifecycle around the router.
- **NOT a TUI component library.** It is not a replacement for Bubble Tea. It *uses* those libraries under the hood, wrapping them in a managed lifecycle.

---

## 🚀 Key Advantages & "Wow" Factors

1. **Integrated AI Assistant (`docs ask`)**  
   Your tool ships with a built-in expert. Users ask questions in natural language, and the AI answers using only your embedded docs, zero hallucination by design.
2. **Autonomous Tool Calling (Agentic Workflows)**  
   The AI can call local Go functions, inspect state, and iterate. This allows for true ReAct-style agent loops, not just text generation.
3. **Built-in Lifecycle Management**  
   Version checking via GitHub/GitLab, auto-updates (`update`), and environment bootstrapping (`init`) are zero-config.
4. **Rich TUI Documentation**  
   Interactive, searchable, Markdown-rendered documentation directly in the terminal, no browser needed.
5. **Model Context Protocol (MCP)**  
   Expose your tool's capabilities to external AI agents out-of-the-box. Your CLI becomes an AI-native building block.
6. **Zero Lock-in (The "Eject Path")**  
   GTB generates idiomatic, standard-library-compliant Go code. If you outgrow the framework, the generated code stands on its own.
7. **Architectural Consistency at Scale**  
   In enterprises running dozens of internal tools, GTB enforces a standardised layout, eliminating per-team architectural debates.
8. **Convention over Configuration**  
   Sensible defaults, a standard project structure, and predictable DI. A massive productivity multiplier for teams migrating from Laravel, Rails, or Django.

---

## 🔄 One Framework, Three Shapes

The tools built on GTB today are split roughly evenly between one-shot command-line tools and applications that run for a long time or carry a UI. The framework treats them the same way: one `Props` container, one configuration stack, one update path.

| Shape | Examples | How GTB Helps |
| :--- | :--- | :--- |
| **Command-line tools** | release orchestration, signing, scaffolding | Full built-in command suite, auto-updates, TUI docs, scriptable JSON output |
| **Long-running processes** | chat and forge bots, workers, daemons | `go/controls` supervision with health probes and graceful shutdown; unattended start under systemd or in a container |
| **Binaries with their own UI** | a local browser studio, a desktop app's backend | A `serve` or `studio` command on GTB's hardened HTTP server, sharing config, logging and credentials with the CLI |
| **DevOps automation** | K8s init containers, migration runners | Unattended runs, `doctor` checks, config from env and flags with no file needed |
