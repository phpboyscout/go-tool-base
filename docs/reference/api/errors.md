---
title: "Error Catalogue"
description: "Sentinel errors defined across GTB packages, with descriptions and handling guidance."
date: 2026-03-25
tags: [components, errors, error-handling, sentinel]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Error Catalogue

This document lists all sentinel errors defined across GTB packages. All errors
use `github.com/cockroachdb/errors` for wrapping and stack traces.

Use `errors.Is(err, target)` to check for sentinel errors — this traverses
wrapped error chains correctly.

```go
import "github.com/cockroachdb/errors"

if errors.Is(err, config.ErrNoFilesFound) {
    // prompt user to run init
}
```

---

## `pkg/config`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrNoFilesFound` | no configuration files found please run init, or provide a config file using the --config flag | Prompt the user to run `init` or pass `--config`. Returned by `LoadFilesContainer` when no config files exist at any of the provided paths. |

---

## `pkg/controls`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrShutdown` | controller shutdown | Signals that the controller has stopped. Returned by `Wait()` in some shutdown paths. Generally expected — log at debug level and exit cleanly. |

---

## `pkg/errorhandling`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrNotImplemented` | command not yet implemented | Returned by commands that are scaffolded but not yet implemented. The error handler surfaces an issue-tracker link if one was provided via `NewErrNotImplemented(issueURL)`. |
| `ErrRunSubCommand` | subcommand required | Returned when a parent command is invoked without a subcommand. The error handler prints available subcommands automatically. |

### Constructor Functions

`NewErrNotImplemented(issueURL string) error` — creates an `ErrNotImplemented`
error with an optional issue URL. The error handler detects this and appends
the link to the user-facing output.

---

## `pkg/logger`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrInvalidLevel` | invalid level | Returned by `ParseLevel(s string)` when the string does not map to a known log level. Validate user-supplied log level strings at config load time. |

---

## `pkg/vcs/github`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrNoPullRequestFound` | no pull request found | Returned by `GetPullRequest` when no open PR exists for the given branch. Check before attempting PR operations. |

---

## `pkg/cmd/root`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrUpdateComplete` | update complete — restart required | Returned by the `update` command after a successful self-update. The root command's `Execute` detects this and exits with code 0, prompting the user to restart the tool. |

---

## Notes

### Internal package errors

The `internal/` packages define additional sentinel errors for generator and
code-generation use. These are not part of the public API and may change
without notice:

| Package | Errors |
|---------|--------|
| `internal/generator` | `ErrNotGoToolBaseProject`, `ErrCommandProtected`, `ErrInvalidPackageName`, `ErrParentCommandFileNotFound` |
| `internal/cmd/generate` | `ErrRepositoryInvalidFormat`, `ErrEmptyCommandPath`, `ErrCommandNotFound`, `ErrUpdateManifestFailed` |
| `internal/cmd/regenerate` | `ErrInvalidOverwriteValue` |
| `internal/generator/verifier` | `ErrVerificationFailed` |
| `internal/agent` | `ErrInvalidPackageName` |

### Adding new errors

When adding a sentinel error to a `pkg/` package:

1. Define it as a package-level `var` using `errors.New`:
   ```go
   var ErrMyCondition = errors.New("description of the condition")
   ```
2. Add an entry to this catalogue with a description and handling guidance.
3. Use `errors.Wrap(err, "context")` to add call-site context when returning
   the error through multiple layers.
