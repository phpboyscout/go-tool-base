---
title: What is GTB?
description: An overview of what Go Tool Base (GTB) is, its core philosophy, and its key advantages.
date: 2026-03-23
tags: [overview, pitch, strategy]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# What is Go Tool Base (GTB)?

Modern CLI tools, DevOps workflows, and developer utilities demand far more than basic flag parsing. **Go Tool Base (GTB)** is a comprehensive lifecycle framework that makes Go applications **Agentic**, **Self-sustaining**, and **Self-documenting** out of the box.

It provides the "batteries included" experience of a macro-framework (like Rails or Laravel), but meticulously tailored for **Go command-line applications and beyond**.

---

## ✅ What GTB IS

- **A full-lifecycle application framework** for Go — covering configuration, versioning, auto-updates, embedded documentation, error handling, and AI integration.
- **A CLI-first framework** — its default mode is building command-line tools with a rich set of built-in commands (`init`, `version`, `update`, `docs`, `mcp`).
- **A dependency injection container** — the `Props` struct orchestrates services (config, assets, logging, VCS, AI) and is explicitly passed to commands for ultimate testability.
- **A scaffold and generator** — `gtb generate skeleton` creates a ready-to-ship, robustly structured project in seconds.
- **A general-purpose application bootstrap** — while CLI is the primary interface, GTB's `Props` container and lifecycle management can power **any** Go application: web services, daemons, background workers, or hybrid tools.

## ❌ What GTB is NOT

- **NOT a web framework.** It does not provide HTTP routers, middleware pipelines, or HTML template engines. It is not a competitor to Gin, Echo, Fiber, or Buffalo. *(However, a GTB-bootstrapped CLI tool could easily embed a web server as one of its subcommands).*
- **NOT a microservice scaffold.** It does not generate gRPC boilerplate like Sponge or Go-Blueprint.
- **NOT just a command router.** Unlike Cobra (a library for routing commands), GTB is an opinionated framework providing the entire application lifecycle around the router.
- **NOT a TUI component library.** It is not a replacement for Bubble Tea. It *uses* those libraries under the hood, wrapping them in a managed lifecycle.

---

## 🚀 Key Advantages & "Wow" Factors

1. **Integrated AI Assistant (`docs ask`)**  
   Your tool ships with a built-in expert. Users ask questions in natural language, and the AI answers using only your embedded docs—zero hallucination by design.
2. **Autonomous Tool Calling (Agentic Workflows)**  
   The AI can call local Go functions, inspect state, and iterate. This allows for true ReAct-style agent loops, not just text generation.
3. **Built-in Lifecycle Management**  
   Version checking via GitHub/GitLab, auto-updates (`update`), and environment bootstrapping (`init`) are zero-config.
4. **Rich TUI Documentation**  
   Interactive, searchable, Markdown-rendered documentation directly in the terminal—no browser needed.
5. **Model Context Protocol (MCP)**  
   Expose your tool's capabilities to external AI agents out-of-the-box. Your CLI becomes an AI-native building block.
6. **Zero Lock-in (The "Eject Path")**  
   GTB generates idiomatic, standard-library-compliant Go code. If you outgrow the framework, the generated code stands on its own.
7. **Architectural Consistency at Scale**  
   In enterprises running dozens of internal tools, GTB enforces a standardised layout, eliminating per-team architectural debates.
8. **Convention over Configuration**  
   Sensible defaults, a standard project structure, and predictable DI. A massive productivity multiplier for teams migrating from Laravel, Rails, or Django.

---

## 🔄 CLI-First, Not CLI-Only

While GTB is designed around a CLI interface, its architecture is application-agnostic. 

*Start with a CLI. Grow into whatever you need:*

| Use Case | How GTB Helps |
| :--- | :--- |
| **CLI Utilities** | Full built-in command suite, auto-updates, TUI docs |
| **Long-Running Daemons** | Use `Props` for DI, add a `run` command for the daemon loop |
| **Web Services** | Scaffold with GTB, add a `serve` command that boots an HTTP router |
| **DevOps Automation** | K8s init containers, migration runners, and automated setups |
