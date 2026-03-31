---
title: "Progress & Spinner Output Helpers"
description: "Reusable progress bars, spinners, and status indicators for long-running CLI operations."
date: 2026-03-31
status: DRAFT
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
// Spinner shows a spinner with a message while a function executes.
// Returns the function's result. Falls back to a plain log message
// in non-interactive environments (CI=true or no TTY).
func Spin(msg string, fn func() error) error

// SpinWithResult is like Spin but returns a value alongside the error.
func SpinWithResult[T any](msg string, fn func() (T, error)) (T, error)
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
err := output.Spin("Checking for updates", func() error {
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

## Open Questions

1. Should `Spin` accept a context for cancellation, or is the function's own context sufficient?
2. Should the progress bar support custom formatting (e.g. bytes downloaded instead of count)?
3. Should there be a `MultiProgress` for tracking parallel operations?
4. Should the spinner style be configurable, or should GTB enforce a consistent look?
