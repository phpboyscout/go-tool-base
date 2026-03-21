---
title: "Root Command Hardening Specification"
description: "Remove os.Exit from library code and eliminate package-level mutable state in the root command package to improve testability and reentrancy."
date: 2026-03-21
status: IMPLEMENTED
tags:
  - specification
  - cmd
  - root
  - testability
  - code-quality
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-opus-4-6)
    role: AI drafting assistant
---

# Root Command Hardening Specification

Authors
:   Matt Cockayne, Claude (claude-opus-4-6) *(AI drafting assistant)*

Date
:   21 March 2026

Status
:   DRAFT

---

## Overview

Two related issues in `pkg/cmd/root/root.go` compromise testability and reentrancy:

1. **`os.Exit(0)` in library code** (line 365): After a successful self-update, `PersistentPreRunE` calls `os.Exit(0)` directly. This bypasses deferred functions, makes the code path untestable without process-level hacks, and violates the Go convention that only `main()` should terminate the process.

2. **Package-level mutable state** (lines 29–33): Three package-level variables (`cfgPaths`, `redirectingToUpdate`, `defaultFormCreator`) are shared across all `NewCmdRoot` invocations. If two root commands are created in the same process (common in tests), they corrupt each other's state.

---

## Design Decisions

**Sentinel error for update completion**: Introduce `ErrUpdateComplete` as a sentinel error. `newRootPreRunE` returns it instead of calling `os.Exit`. The existing `Execute()` wrapper in `pkg/cmd/root/execute.go` catches this sentinel and exits cleanly, preserving the behaviour while keeping the library code testable.

**`rootState` struct**: All mutable state moves into a `rootState` struct instantiated per `NewCmdRootWithConfig` call. Closures created by `newRootPreRunE` capture the struct rather than package variables.

**No public API changes**: `NewCmdRoot` and `NewCmdRootWithConfig` signatures are unchanged. The `rootState` is an internal implementation detail.

---

## Public API Changes

### New Sentinel Error

```go
// ErrUpdateComplete is returned by PersistentPreRunE when a self-update
// has completed successfully. The Execute wrapper handles this by exiting
// cleanly without logging an error.
var ErrUpdateComplete = errors.New("update complete — restart required")
```

### Modified: `Execute()` in `pkg/cmd/root/execute.go`

```go
func Execute(rootCmd *cobra.Command, props *p.Props) {
    // ... existing setup ...
    if err := rootCmd.Execute(); err != nil {
        if errors.Is(err, ErrUpdateComplete) {
            props.Logger.Warnf("update complete — please run the command again")
            props.ErrorHandler.Exit(0)
            return
        }
        props.ErrorHandler.Check(err, "", errorhandling.LevelFatal)
    }
}
```

---

## Internal Implementation

### `rootState` Struct

```go
type rootState struct {
    cfgPaths            []string
    redirectingToUpdate bool
    formCreator         func(*bool) *huh.Form
}

func newRootState() *rootState {
    return &rootState{
        formCreator: createUpdatePromptForm,
    }
}
```

### Updated `NewCmdRootWithConfig`

```go
func NewCmdRootWithConfig(props *p.Props, configPaths []string, subcommands ...*cobra.Command) *cobra.Command {
    if props.ErrorHandler == nil {
        props.ErrorHandler = errorhandling.New(props.Logger, props.Tool.Help)
    }

    state := newRootState()
    mcpLogLevel := &slog.LevelVar{}

    var rootCmd = &cobra.Command{
        Use:               props.Tool.Name,
        Short:             props.Tool.Summary,
        Long:              props.Tool.Description,
        PersistentPreRunE: newRootPreRunE(props, configPaths, mcpLogLevel, state),
    }

    setupRootFlags(rootCmd, props, state)
    // ...
}
```

### Updated `setupRootFlags`

```go
func setupRootFlags(rootCmd *cobra.Command, props *p.Props, state *rootState) {
    // ...
    rootCmd.PersistentFlags().StringArrayVar(&state.cfgPaths, "config", defaultConfigPaths, "config files to use")
    // ...
}
```

### Updated `newRootPreRunE`

The closure captures `state` instead of package variables:

```go
func newRootPreRunE(props *p.Props, configPaths []string, mcpLogLevel *slog.LevelVar, state *rootState) func(*cobra.Command, []string) error {
    return func(cmd *cobra.Command, args []string) error {
        // ... same logic, but uses state.cfgPaths, state.redirectingToUpdate ...

        if updateResult.ShouldExit {
            return ErrUpdateComplete  // ← Instead of os.Exit(0)
        }

        return nil
    }
}
```

### Updated `handleOutdatedVersion`

```go
func handleOutdatedVersion(ctx context.Context, props *p.Props, message string, result *UpdateCheckResult, state *rootState, opts ...OutdatedVersionOption) {
    // ... uses state.redirectingToUpdate and state.formCreator ...

    if runUpdate {
        state.redirectingToUpdate = true
        // ...
        result.HasUpdated = true
        result.ShouldExit = true
    }
}
```

### Updated `shouldSkipUpdateCheck`

```go
func shouldSkipUpdateCheck(props *p.Props, cmd *cobra.Command, flags *FlagValues, state *rootState) bool {
    if props.Tool.IsDisabled(p.UpdateCmd) ||
        (props.Version != nil && props.Version.IsDevelopment()) ||
        state.redirectingToUpdate ||  // ← from state, not package var
        flags.CI ||
        props.Config.GetBool("ci") {
        return true
    }
    return setup.SkipUpdateCheck(props.FS, props.Tool.Name, cmd)
}
```

---

## Project Structure

```
pkg/cmd/root/
├── execute.go      ← MODIFIED: handle ErrUpdateComplete
├── root.go         ← MODIFIED: rootState, remove os.Exit, remove package vars
├── root_test.go    ← MODIFIED: test isolation, ErrUpdateComplete tests
```

---

## Error Handling

- `ErrUpdateComplete` is a sentinel error, not a failure. The `Execute` wrapper treats it as a clean exit.
- All existing error paths through `PersistentPreRunE` are unchanged — they still return wrapped errors.

---

## Testing Strategy

| Test | Scenario |
|------|----------|
| `TestExecute_ErrUpdateComplete` | `PersistentPreRunE` returns `ErrUpdateComplete` → `Execute` exits cleanly, no error logged |
| `TestRootState_Isolation` | Two `NewCmdRoot` calls → each has independent `cfgPaths` and `redirectingToUpdate` |
| `TestRootState_DefaultFormCreator` | `newRootState()` → `formCreator` is `createUpdatePromptForm` |
| `TestShouldSkipUpdateCheck_UsesState` | `state.redirectingToUpdate = true` → returns true |
| `TestHandleOutdatedVersion_SetsStateFlag` | After update → `state.redirectingToUpdate` is true |
| `TestNewRootPreRunE_ReturnsErrUpdateComplete` | Successful update → returns `ErrUpdateComplete` (not `os.Exit`) |

### Coverage
- Target: 90%+ for `pkg/cmd/root/`.

---

## Linting

- `golangci-lint run --fix` must pass.
- The removal of `os.Exit` from non-main code resolves potential `exitAfterDefer` warnings.
- No new `nolint` directives.

---

## Documentation

- Godoc for `ErrUpdateComplete` explaining its purpose and handling.
- Internal godoc for `rootState`.
- No user-facing documentation changes — behaviour is identical from the user's perspective.

---

## Backwards Compatibility

- **No breaking changes**. Public function signatures are unchanged.
- `Execute()` wrapper now handles one additional sentinel error — existing callers are unaffected.
- Tests that previously couldn't test the update-complete path can now do so.

---

## Future Considerations

- **Structured exit codes**: `ErrUpdateComplete` currently maps to exit code 0. If structured exit codes are needed in the future, the sentinel error pattern extends naturally.
- **Additional state**: If more per-command state emerges, `rootState` is the natural home.

---

## Implementation Phases

### Phase 1 — Introduce `rootState`
1. Define `rootState` struct and `newRootState()` constructor
2. Thread `state` through `NewCmdRootWithConfig`, `setupRootFlags`, `newRootPreRunE`, `checkForUpdates`, `shouldSkipUpdateCheck`, `handleOutdatedVersion`
3. Remove package-level `cfgPaths`, `redirectingToUpdate`, `defaultFormCreator` variables

### Phase 2 — Replace `os.Exit` with Sentinel
1. Define `ErrUpdateComplete` sentinel
2. Replace `os.Exit(0)` in `newRootPreRunE` with `return ErrUpdateComplete`
3. Update `Execute()` in `execute.go` to handle `ErrUpdateComplete`

### Phase 3 — Tests
1. Add `TestExecute_ErrUpdateComplete`
2. Add `TestRootState_Isolation`
3. Update existing tests to verify no shared state

---

## Verification

```bash
go build ./...
go test -race ./pkg/cmd/root/...
go test ./...
golangci-lint run --fix

# Verify no os.Exit in root.go (except in test helpers)
grep -n 'os\.Exit' pkg/cmd/root/root.go  # should return no results

# Verify no package-level vars remain
grep -n '^var ' pkg/cmd/root/root.go  # should only show ErrUpdateComplete
```
