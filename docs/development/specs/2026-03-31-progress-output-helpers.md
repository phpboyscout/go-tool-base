---
title: "Progress & Spinner Output Helpers"
description: "Reusable progress bars, spinners, and status indicators for long-running CLI operations."
date: 2026-03-31
status: IMPLEMENTED
tags:
  - specification
  - output
  - tui
  - progress
  - spinner
  - feature
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
---

# Progress & Spinner Output Helpers

Authors
:   Matt Cockayne

Date
:   31 March 2026

Status
:   DRAFT

---

## Overview

GTB provides `pkg/forms` for interactive input and `pkg/output` for structured responses, but has no standard components for communicating progress during long-running operations. Tool authors repeatedly implement spinners, progress bars, and status updates using charmbracelet components directly — each with slightly different patterns.

This spec adds thin, opinionated wrappers in `pkg/output` that provide consistent progress output across all GTB tools, with automatic fallback to plain text in non-interactive (CI) environments.

---

## Design

### Spinner

A blocking spinner for indeterminate operations (API calls, git operations, AI processing).

```go
// Spin shows a spinner with a message while a function executes.
// Returns the function's result. Falls back to a plain log message
// in non-interactive environments (CI=true or no TTY).
// The context is passed to the function and used for cancellation.
func Spin(ctx context.Context, msg string, fn func(ctx context.Context) error) error

// SpinWithResult is like Spin but returns a value alongside the error.
func SpinWithResult[T any](ctx context.Context, msg string, fn func(ctx context.Context) (T, error)) (T, error)
```

**Behaviour:**
- Interactive terminal: animated spinner with message (using charmbracelet/spinner)
- Non-interactive (CI, no TTY, piped output): prints `msg...` then `msg... done` or `msg... failed`
- Respects `--output json` — suppresses spinner, logs to stderr

### Progress Bar

A determinate progress indicator for operations with known total work.

```go
// Progress tracks progress of a known-total operation.
type Progress struct {
    total   int
    current int
}

// NewProgress creates a progress bar with the given total and description.
func NewProgress(total int, description string) *Progress

// Increment advances the progress bar by one unit.
func (p *Progress) Increment()

// IncrementBy advances the progress bar by n units.
func (p *Progress) IncrementBy(n int)

// Done marks the progress as complete and cleans up the display.
func (p *Progress) Done()
```

**Behaviour:**
- Interactive terminal: animated progress bar with percentage, count, and ETA
- Non-interactive: periodic log lines (`Processing: 50/100 (50%)`) at 10% intervals
- Respects `--output json` — suppresses bar, logs to stderr

### Status Line

A live-updating status message for multi-step operations.

```go
// Status displays a live-updating status message.
type Status struct{}

// NewStatus creates a status display.
func NewStatus() *Status

// Update replaces the current status message.
func (s *Status) Update(msg string)

// Success marks the current step as successful and moves to the next line.
func (s *Status) Success(msg string)

// Warn marks the current step as a warning.
func (s *Status) Warn(msg string)

// Fail marks the current step as failed.
func (s *Status) Fail(msg string)

// Done cleans up the status display.
func (s *Status) Done()
```

**Example output:**
```
✓ Loading configuration
✓ Connecting to API
⠋ Fetching release assets...
```

**Behaviour:**
- Interactive: live-updating with icons (✓, ⚠, ✗, spinner)
- Non-interactive: sequential log lines with status prefix

### Non-Interactive Detection

```go
// IsInteractive returns true if stdout is a TTY and CI mode is not active.
func IsInteractive() bool
```

Checks:
1. `os.Stdout` is a terminal (via `term.IsTerminal`)
2. `CI` environment variable is not `"true"`
3. `--output json` is not set (if accessible via context)

---

## Package Location

All helpers live in `pkg/output/` alongside the existing `Response` type. No new package needed.

---

## Dependencies

- `github.com/charmbracelet/bubbles/spinner` — already an indirect dependency via `charmbracelet/huh`
- `github.com/charmbracelet/bubbles/progress` — may need adding
- `golang.org/x/term` — already a dependency

---

## Usage Examples

### Spinner

```go
err := output.Spin(ctx, "Checking for updates", func(ctx context.Context) error {
    return updater.Check(ctx)
})
```

### Progress Bar

```go
bar := output.NewProgress(len(files), "Processing files")
defer bar.Done()

for _, f := range files {
    processFile(f)
    bar.Increment()
}
```

### Status Line

```go
status := output.NewStatus()
defer status.Done()

status.Update("Loading configuration")
cfg, err := loadConfig()
if err != nil {
    status.Fail("Configuration failed: " + err.Error())
    return err
}
status.Success("Configuration loaded")

status.Update("Connecting to API")
// ...
status.Success("Connected")
```

---

## Resolved Questions

1. **Context on Spin**: Yes — `Spin` accepts `context.Context` and passes it to the wrapped function. Cancellation stops the spinner.
2. **Custom progress formatting (bytes)**: Deferred — count-based covers the common case. Bytes formatting can be added later via a formatter option.
3. **MultiProgress for parallel operations**: Deferred — single progress covers 90% of use cases.
4. **Spinner style**: GTB enforces a consistent default style. A `WithStyle` option allows tool authors to override if needed.
