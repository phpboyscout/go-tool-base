---
title: How-to Guides
description: Collection of practical guides for common development tasks.
date: 2026-02-16
tags: [how-to, index, guides]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# How-to Guides

Practical, step-by-step instructions for common tasks and workflows in GTB. These guides focus on the "How" – providing actionable steps to build and extend your CLI tools.

## Getting Oriented

### [Migrating from Other Ecosystems](../coming-from-other-ecosystems.md)
A conceptual translation guide if you're coming to Go from Laravel, Rails, or Django.

## Development Workflows

### [Scaffolding a New CLI](new-cli.md)
Get up and running in seconds using the `gtb` CLI generator.

### [Using Command Middleware](use-middleware.md)
Add cross-cutting concerns like logging and auth checks to your command tree.

### [Implementing Custom Middleware](custom-middleware.md)
A hands-on guide to creating and registering your own domain-specific middleware.

### [Configuring Built-in Features](builtin-features.md)
How to toggle and tune framework features like Self-Updates, MCP, and AI documentation.

### [Adding Custom Commands](custom-commands.md)
A hands-on guide to implementing domain-specific logic and registering it with the command tree.

## Advanced Guides

### [Testing & Mocking](testing.md)
Strategies for unit testing your commands using the framework's built-in mocking capabilities.

### [AI Provider Setup](ai-integration.md)
Choosing an AI provider (Claude, OpenAI, Gemini) and securely configuring your environment.

### [Adding an Initialiser](add-initialiser.md)
Learn how to create and register a custom Initialiser for your feature.

### [Adding a Doctor Check](add-doctor-check.md)
Register custom diagnostic checks so the `doctor` command validates your feature's health.

## Output & Observability

### [Add Scriptable JSON Output to a Command](scriptable-json-output.md)
Use `pkg/output` to give any command a `--output json` flag for CI/CD and scripting integration.

### [Switch to Structured JSON Logging for Containers](structured-json-logging.md)
Replace the charmbracelet terminal logger with a `slog` JSON backend for daemon and container deployments.

## Configuration

### [React to Configuration Changes at Runtime](config-hot-reload.md)
Use `config.Observable` and `AddObserver` to reconfigure long-running services without restarting.

### [Define and Validate Config for a Component](validate-component-config.md)
Define config defaults via embedded assets and validate them at runtime using per-package schema validation.

## Error Handling

### [Write User-Facing Errors with Hints](user-facing-errors.md)
Use `cockroachdb/errors` and `ErrorHandler` to produce actionable error messages with recovery suggestions.

## AI Integration

### [Build a Command with Structured AI Responses](structured-ai-responses.md)
Use `chat.Ask` with a typed struct to receive deterministic, schema-validated responses from an AI provider.

### [Add Tool Calling to an AI Command](ai-tool-calling.md)
Expose Go functions as tools the AI can call, with the built-in ReAct loop managing the back-and-forth.

## Version Control & Releases

### [Configure Self-Updating](configure-self-updating.md)
Wire up `UpdateCmd` with GitHub or GitLab as the release source for automatic binary updates.

### [Automate GitHub Workflows](automate-github-workflows.md)
Create pull requests, download release assets, and read file contents using `GHClient`.

## Assets

### [Embed and Register Custom Assets](embed-custom-assets.md)
Ship default configs, templates, and data files with your tool using Go's `embed` package and `props.Assets`.

## Services

### [Add a gRPC Management Service](add-grpc-service.md)
Register a gRPC server with the controller, wire the standard health protocol, and configure the port.
