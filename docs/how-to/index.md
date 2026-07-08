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

### [Migrating from Other Ecosystems](../about/coming-from-other-ecosystems.md)
A conceptual translation guide if you're coming to Go from Laravel, Rails, or Django.

## Framework CLI Workflows

### [Scaffolding a New Project](framework-cli/scaffold-project.md)
Get up and running in seconds using the `gtb` CLI generator.

### [Generating Commands](framework-cli/generate-commands.md)
Add functionality and build your command tree with ease.

### [Adding Flags to Commands](framework-cli/add-flags.md)
Inject new flags into existing commands effortlessly.

### [Regenerating Components](framework-cli/regenerate-components.md)
Update your scaffolding configurations to match modified manifests.

### [Removing Commands](framework-cli/remove-commands.md)
Cleanly delete command files and deregister routing structures.

### [Applying Custom Templates](framework-cli/apply-templates.md)
Layer your own files over the skeleton with `gtb template`.

### [Converting Scripts to Go](framework-cli/convert-scripts-to-go.md)
Turn existing shell scripts into Go code with AI assistance.

### [Generating AI Documentation](framework-cli/generate-docs.md)
Generate and maintain documentation for your CLI commands and packages using AI.

### [Exposing an MCP Server](framework-cli/expose-mcp-server.md)
Expose your CLI as a Model Context Protocol server for IDE and agent integration.

## Development Workflows

### [Using Command Middleware](use-middleware.md)
Add cross-cutting concerns like logging and auth checks to your command tree.

### [Implementing Custom Middleware](custom-middleware.md)
A hands-on guide to creating and registering your own domain-specific middleware.

### [Configuring Built-in Features](builtin-features.md)
How to toggle and tune framework features like Self-Updates, MCP, and AI documentation.

### [Adding Custom Commands](custom-commands.md)
A hands-on guide to implementing domain-specific logic and registering it with the command tree.

### [Adding Nested Subcommands](nested-subcommands.md)
Build multi-level command trees (e.g. `tool deploy canary`) via the generator or by hand using `setup.Command.Register`.

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

### [Auto-initialise Configuration on First Run](auto-initialise-config.md)
Write the default config automatically when it is missing, or hand config bootstrap to a specific command, via `props.Tool.Bootstrap`.

### [Bind CLI Flags to Config](bind-flags-to-config.md)
Make `--server-port` override `server.port` by binding flags into the configuration precedence (flags > env > file).

### [React to Configuration Changes at Runtime](config-hot-reload.md)
Use `config.Observable` and `AddObserver` to reconfigure long-running services without restarting.

### [Observe Typed Config Sections](observe-typed-config.md)
Use `config.ObserveSection` to rehydrate typed settings and react only when a config struct changes.

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

### [Persist Chat Conversations](persist-chat-conversations.md)
Save and restore AI chat conversations across CLI invocations using snapshots and the FileStore.

## Version Control & Releases

### [Use In-Memory Repositories](use-in-memory-repos.md)
Learn how to use afero-backed in-memory git repositories for high-speed testing and isolated CI environments.

### [Configure Self-Updating](configure-self-updating.md)
Wire up `UpdateCmd` with GitHub, GitLab, Bitbucket, Gitea, Codeberg, or a direct HTTP server as the release source for automatic binary updates.

### [Add a Custom Release Source](custom-release-source.md)
Implement and register a custom `release.Provider` so your tool can self-update from any backend — S3, Artifactory, Nexus, or a proprietary store.

### [Secure Releases — Checksum Verification](secure-releases.md)
Publish `checksums.txt` alongside release binaries so `Update()` rejects tampered or truncated downloads. Covers the fail-open library default, the `setup.DefaultRequireChecksum` opt-in for fail-closed tools, and per-provider manifest retrieval (Bitbucket downloads, Direct's `checksum_url_template`).

## Telemetry

### [Create a Custom Telemetry Backend](custom-telemetry-backend.md)
Implement the `telemetry.Backend` interface to send usage analytics to any platform.

### [Create a Custom Deletion Requestor](custom-deletion-requestor.md)
Implement the `telemetry.DeletionRequestor` interface for GDPR-compliant data deletion from custom backends.

## Code Generation

### [Configure Generator Ignore Rules](configure-generator-ignore.md)
Use `.gtb/ignore` to prevent the generator from overwriting customised files during regeneration.

### [Automate GitHub Workflows](automate-github-workflows.md)
Create pull requests, download release assets, and read file contents using `GHClient`.

## Assets

### [Embed and Register Custom Assets](embed-custom-assets.md)
Ship default configs, templates, and data files with your tool using Go's `embed` package and `props.Assets`.

## Services & Lifecycle

### [Register Health Checks](register-health-checks.md)
Integrate database ping, HTTP, or custom logic checks into the controller's liveness and readiness probes.

### [Manage Background Services](manage-background-services.md)
Orchestrate long-running daemons, crons, and background workers with graceful shutdown contexts.

### [Add a gRPC Management Service](add-grpc-service.md)
Register a gRPC server with the controller, wire the standard health protocol, and configure the port.

### [Expose a gRPC Service as REST](expose-grpc-as-rest.md)
Put a JSON/REST surface over an existing gRPC service with the grpc-gateway — annotate the proto, mount `gateway.New`, and let `DialLocal` handle the connection.

### [Serve Interactive API Docs](serve-api-docs.md)
Generate an OpenAPI v3 spec and serve it with an embedded Stoplight Elements "try it" console using `pkg/openapi`.

### [Verify Requests (API Keys & JWT/OIDC)](verify-requests-with-authn.md)
Authenticate HTTP requests with `pkg/authn` verifiers and the fail-closed `AuthMiddleware` — API keys, JWT/OIDC, mTLS, and an authorization policy.

## Security & Signatures

### [Add HTTP Security Headers](security-headers.md)
Implement HSTS, CSP, and other security headers for your tool using the `pkg/http` middleware chain.

### [Add a Custom Signing Backend](add-signing-backend.md)
Implement a custom PKI or KMS integration to sign your release binaries.

### [Mint a New Signing Key](mint-signing-key.md)
Generate an Ed25519 signing keypair for securing your distributed release assets.

### [Publish Keys to WKD](publish-wkd.md)
Export and publish your public keys to a Web Key Directory for automatic client discovery.

### [Sign Releases](sign-releases.md)
Automate the cryptographic signing of your build artifacts and manifests prior to distribution.

### [Generate a Rotation Key](generate-rotation-key.md)
Securely rotate your cryptographic keys without breaking backwards compatibility for existing clients.

## Credentials

### [Configure Credentials](configure-credentials.md)
Choose a storage mode for AI API keys, VCS tokens, and Bitbucket app passwords — env-var reference (recommended default), OS keychain (opt-in), or literal config (legacy) — and migrate between them safely.

### [Implement a Custom Credential Backend](custom-credential-backend.md)
Plug Hashicorp Vault, AWS Secrets Manager, 1Password Connect, or any other secret store into your tool by implementing the `credentials.Backend` interface and registering it at startup.

### [Migrate literal credentials off config](migrate-literal-credentials.md)
Use `config migrate-credentials` to move plaintext credentials in your tool's YAML out into environment-variable references or the OS keychain. Supports interactive and silent (CI/CD) flows.
