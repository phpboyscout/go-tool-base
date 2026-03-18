---
title: Troubleshooting
description: Common issues and solutions for GTB development and usage.
date: 2026-02-16
tags: [troubleshooting, support, faq]
authors: [Matt Cockayne <matt@phpboyscout.com>]
hide:
  - navigation
---

# Troubleshooting

This guide provides solutions for common issues you might encounter when using or developing with `gtb`.

## Installation & Environment

### Private Modules (403 Forbidden / Terminal Prompts)
If you encounter errors fetching private modules (e.g., `github.com`), ensure your Go environment is configured to treat them as private.

```bash
go env -w GOPRIVATE=github.com
```

Also, ensure Git is configured to use your credentials for the private host:

```bash
git config --global --replace-all url."https://${GITHUB_USERNAME}:${GITHUB_TOKEN}@github.com".insteadOf "https://github.com"
```

### "Command not found" after install
If you installed the tool via `go install` but cannot run it:

1.  Check that `$GOPATH/bin` is in your `$PATH`.
2.  Run `go env GOPATH` to find your Go workspace path.

## Generator Issues

### "Command is protected"
The generator will not overwrite commands marked as `protected: true` in the `.gtb/manifest.yaml`.

**Solution 1: Explicitly Unprotect**
```bash
gtb generate command unprotect my-command
```

**Solution 2: Use Force**
If you want to overwrite it just this once:
```bash
gtb generate command --name my-command --force --protected=false
```

### Corrupt Manifest
If `.gtb/manifest.yaml` becomes corrupted or invalid, the tool might panic or fail to resolve commands.

**Recovery:**

1.  Back up the current manifest.
2.  Run `gtb regenerate manifest` to rebuild it from your existing code.
3.  Verify the new manifest looks correct.

### Linting Failures (golangci-lint)
The generator automatically runs `golangci-lint run --fix` after generating code. If this step fails (e.g., due to strict linter settings):

1.  The generated code **is still saved** to disk.
2.  Navigate to the generated file manually.
3.  Run `golangci-lint run` to see specific errors and fix them manually.

## Runtime Issues

### Configuration Not Loading
If your tool isn't picking up the configuration you expect:

1.  Check the search paths. By default, it looks in `~/.<toolname>/` and `/etc/<toolname>/`.
2.  Enable debug logging to see exactly what files are being loaded:
    ```bash
    ./my-tool --debug command
    ```
    Look for logs indicating "Config loaded from...".

### "Asset not found"
If you are getting errors about missing assets:

1.  Ensure you have run `go mod tidy`.
2.  Check that `//go:embed assets/*` directives are present in your `main.go` or command files.
3.  If developing locally, ensure the `assets/` directory exists relative to where you are running the command.

## AI Conversion Issues

### Rate Limiting / 429 Errors
If the AI provider returns rate limit errors:

1.  Wait a few moments and try again.
2.  Check your API key quotas.
3.  Try a different model using the `--model` flag if supported.

### "Generation failed" / Broken Code
If the AI generates code that doesn't compile:

1.  The generator will wrap the logic in a comment `// AI generation failed...` and return `nil`.
2.  Open the file and review the commented-out code.
3.  Manually fix the syntax errors or missing imports.
4.  You can try re-running with a refined prompt using `--prompt`.

## Advanced Features

### Auto-Update Failures
The "bait and switch" update pattern can fail in specific environments:

- **Permission Denied**: Ensure the user has write permissions to the directory where the binary resides.
- **File Locked (Windows)**: If the update fails because the file is in use, try closing all instances of the tool and running the update command explicitly.
- **Network Timeouts**: Check your connection to GitHub. You can increase the timeout by setting the `HTTP_TIMEOUT` environment variable.

### VCS & Repository Issues

- **SSH Agent Conflicts**: If `NewRepo` fails to find your keys, verify the agent is running (`ssh-add -l`).
- **Token Invalidation**: If using `GITHUB_TOKEN`, ensure it hasn't expired and has the `repo` scope.
- **Memory Pressure**: Large repositories pulled into `SourceMemory` (RAM) may cause OOM errors in resource-constrained environments (e.g., small CI runners).

### Service Orchestration

- **Zombie Services**: If a service hangs during `Stop()`, the `Controller` will wait indefinitely. Use the `Status` command to identify the hanging service and check its internal logs.
- **Signal Collision**: If your application traps signals manually, it may conflict with the `Controller` signal handler. Use the `WithoutSignals()` option when creating the controller if you need custom handling.

## Support & Feedback

### Error Signposting

If your tool is configured with a `HelpConfig`, error messages will include a link to a Slack channel or support team. Refer to the link provided in the error output for project-specific assistance.
