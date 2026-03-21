---
title: "Controller State Race Fix Specification"
description: "Fix check-then-act race condition in handleStopMessage by holding stateMutex for the full state transition or using atomic compare-and-swap."
date: 2026-03-21
status: IMPLEMENTED
tags:
  - specification
  - controls
  - concurrency
  - bug-fix
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-opus-4-6)
    role: AI drafting assistant
---

# Controller State Race Fix Specification

Authors
:   Matt Cockayne, Claude (claude-opus-4-6) *(AI drafting assistant)*

Date
:   21 March 2026

Status
:   DRAFT

---

## Overview

`pkg/controls/controller.go` contains a check-then-act race condition in `handleStopMessage`. The method reads the current state, compares it, and then sets a new state — but these operations are not atomic. If two goroutines send stop messages concurrently, both can observe `StateRunning`, both proceed to call `Stop()`, and the shutdown sequence executes twice.

```go
// Current code (simplified):
func (c *Controller) handleStopMessage(msg Message) {
    if c.GetState() == StateRunning {  // READ
        // ... another goroutine can also read StateRunning here
        c.SetState(StateStopping)      // WRITE — both goroutines reach this
        c.Stop()
    }
}
```

The `stateMutex` exists but is only held for individual `GetState()` and `SetState()` calls, not for the full check-then-act sequence.

---

## Design Decisions

**Hold mutex for full transition**: Rather than atomic compare-and-swap (which would require changing `State` to an atomic type), extend the existing `stateMutex` to cover the full check-then-act sequence. This is simpler and consistent with the existing mutex-based approach.

**New `compareAndSetState` method**: Encapsulate the pattern in a helper that acquires the lock, checks the expected state, and sets the new state atomically. Returns `true` if the transition succeeded. This can be reused for any future state transitions.

**No public API change**: The `GetState()`/`SetState()` methods remain for general use. The new method is internal.

---

## Public API Changes

None. This is an internal bug fix.

---

## Internal Implementation

### New Method: `compareAndSetState`

```go
// compareAndSetState atomically checks if the current state matches expected,
// and if so, sets it to next. Returns true if the transition occurred.
func (c *Controller) compareAndSetState(expected, next State) bool {
    c.stateMutex.Lock()
    defer c.stateMutex.Unlock()

    if c.state != expected {
        return false
    }
    c.state = next
    return true
}
```

### Updated `handleStopMessage`

```go
func (c *Controller) handleStopMessage(msg Message) {
    if !c.compareAndSetState(StateRunning, StateStopping) {
        // Already stopping or stopped — ignore duplicate stop
        return
    }

    c.logger.Info("stop message received", "source", msg.Source)
    c.stop()
}
```

### Updated `Stop()`

The public `Stop()` method should also use the atomic transition:

```go
func (c *Controller) Stop() {
    if !c.compareAndSetState(StateRunning, StateStopping) {
        return
    }
    c.stop()
}

// stop performs the actual shutdown sequence. Caller must have already
// transitioned state to StateStopping.
func (c *Controller) stop() {
    c.logger.Info("shutting down services")
    // ... existing shutdown logic ...
    c.SetState(StateStopped)
}
```

### Updated `Start()`

Apply the same pattern to `Start()` for consistency:

```go
func (c *Controller) Start() {
    if !c.compareAndSetState(StateStopped, StateRunning) {
        return
    }
    // ... existing start logic ...
}
```

### State Query Methods

`IsRunning()`, `IsStopped()`, and `IsStopping()` remain as-is — they are point-in-time reads and don't need to participate in transitions.

---

## Project Structure

```
pkg/controls/
├── controller.go      ← MODIFIED: compareAndSetState, updated transitions
├── controls_test.go   ← MODIFIED: race condition tests
```

---

## Error Handling

- `compareAndSetState` returns `false` (not an error) when the transition fails. Duplicate stops are silently ignored — this is the correct behaviour since the stop is already in progress.
- Logging can be added at the call site for observability if needed.

---

## Testing Strategy

| Test | Scenario |
|------|----------|
| `TestCompareAndSetState_Success` | Expected state matches → transition occurs, returns true |
| `TestCompareAndSetState_Failure` | Expected state doesn't match → no transition, returns false |
| `TestStop_ConcurrentCalls` | 100 goroutines call Stop() → shutdown executes exactly once |
| `TestHandleStopMessage_ConcurrentMessages` | 100 concurrent stop messages → shutdown executes once |
| `TestStop_AlreadyStopping` | Stop() while StateStopping → no-op |
| `TestStop_AlreadyStopped` | Stop() while StateStopped → no-op |
| `TestStart_AlreadyRunning` | Start() while StateRunning → no-op |

### Race Condition Test

```go
func TestStop_ConcurrentCalls(t *testing.T) {
    ctrl := NewController(context.Background())
    ctrl.Start()

    var stopCount atomic.Int32
    originalStop := ctrl.stop
    ctrl.stop = func() {
        stopCount.Add(1)
        originalStop()
    }

    var wg sync.WaitGroup
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            ctrl.Stop()
        }()
    }
    wg.Wait()

    assert.Equal(t, int32(1), stopCount.Load(), "stop should execute exactly once")
    assert.True(t, ctrl.IsStopped())
}
```

Note: The exact test implementation depends on whether the shutdown logic can be intercepted. If `stop()` is not easily mockable, count via a channel or observe the final state.

### Race Detector

All tests must pass with `-race`:

```bash
go test -race ./pkg/controls/...
```

### Coverage
- Target: 90%+ for `pkg/controls/`.

---

## Linting

- `golangci-lint run --fix` must pass.
- No new `nolint` directives.
- The race detector serves as the primary validation — linters cannot catch all data races.

---

## Documentation

- Godoc for `compareAndSetState` explaining its atomic semantics.
- Update godoc on `Stop()` and `Start()` to note that duplicate calls are no-ops.
- No user-facing documentation changes.

---

## Backwards Compatibility

- **No breaking changes**. External behaviour is unchanged — `Stop()` still stops the controller.
- Duplicate `Stop()` calls were previously racy and could cause double-shutdown. Now they are safely ignored. This is a behaviour improvement, not a breaking change.

---

## Future Considerations

- **State machine formalization**: If more states are added (e.g., `StatePaused`, `StateRestarting`), a formal state machine with a transition table would prevent invalid transitions at compile time.
- **State change callbacks**: `compareAndSetState` could optionally notify listeners when transitions occur, useful for health monitoring.
- **Context-based cancellation**: `Stop()` could accept a context with a deadline for graceful shutdown timeout enforcement.

---

## Implementation Phases

### Phase 1 — Add `compareAndSetState`
1. Add `compareAndSetState` method to `Controller`
2. Add unit tests for the method in isolation

### Phase 2 — Update State Transitions
1. Update `handleStopMessage` to use `compareAndSetState`
2. Update `Stop()` to use `compareAndSetState`
3. Update `Start()` to use `compareAndSetState`
4. Extract `stop()` as internal shutdown method

### Phase 3 — Concurrency Tests
1. Add concurrent stop test with race detector
2. Add concurrent start/stop interleaving test
3. Run full suite with `-race`

---

## Verification

```bash
go build ./...
go test -race ./pkg/controls/...
go test ./...
golangci-lint run --fix

# Verify compareAndSetState exists
grep -n 'compareAndSetState' pkg/controls/controller.go

# Run race detector specifically on controls package
go test -race -count=10 ./pkg/controls/...  # multiple runs to increase race detection probability
```
