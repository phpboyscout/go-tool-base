---
title: "Assets Resource Leak Fix Specification"
description: "Fix deferred Close inside a for loop in openMergedCSV that causes file handle accumulation until function return."
date: 2026-03-21
status: IMPLEMENTED
tags:
  - specification
  - props
  - assets
  - bug-fix
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-opus-4-6)
    role: AI drafting assistant
---

# Assets Resource Leak Fix Specification

Authors
:   Matt Cockayne, Claude (claude-opus-4-6) *(AI drafting assistant)*

Date
:   21 March 2026

Status
:   DRAFT

---

## Overview

`pkg/props/assets.go:243–295` contains a resource leak in the `openMergedCSV` method. A `defer f.Close()` statement inside a `for` loop means all file handles opened during iteration remain open until the function returns, rather than being closed per iteration. With many registered filesystems, this can exhaust file descriptors.

```go
for _, fsName := range a.order {
    f, err := ef.Open(name)
    // ...
    defer func() { _ = f.Close() }()  // BUG: all defers stack until return
    // ...
}
```

---

## Design Decisions

**Extract helper function**: Move the per-filesystem CSV reading into a helper method. The helper's `defer` fires on each call, ensuring files are closed promptly. This is cleaner than manual `f.Close()` calls with error-path bookkeeping.

---

## Public API Changes

None. This is an internal bug fix with no public API impact.

---

## Internal Implementation

### New Helper: `readCSVFromFS`

```go
// readCSVFromFS reads all CSV rows from a single filesystem.
// The file is closed before this function returns.
func (a *embeddedAssets) readCSVFromFS(ef fs.FS, name string) ([][]string, error) {
    f, err := ef.Open(name)
    if err != nil {
        return nil, err
    }
    defer func() { _ = f.Close() }()

    reader := csv.NewReader(f)
    return reader.ReadAll()
}
```

### Updated `openMergedCSV`

```go
func (a *embeddedAssets) openMergedCSV(name string) (fs.File, error) {
    var allRows [][]string
    found := false

    for _, fsName := range a.order {
        ef := a.embedded[fsName]
        if ef == nil {
            continue
        }

        rows, err := a.readCSVFromFS(ef, name)
        if err != nil || len(rows) == 0 {
            continue
        }

        if !found {
            allRows = rows
        } else {
            allRows = append(allRows, rows...)
        }
        found = true
    }

    if !found {
        return nil, fs.ErrNotExist
    }

    var buf bytes.Buffer
    writer := csv.NewWriter(&buf)
    _ = writer.WriteAll(allRows)
    writer.Flush()

    return &mergedFile{
        name:   name,
        Reader: bytes.NewReader(buf.Bytes()),
    }, nil
}
```

---

## Project Structure

```
pkg/props/
├── assets.go       ← MODIFIED: extract readCSVFromFS, fix openMergedCSV
├── assets_test.go  ← MODIFIED: add resource leak test
```

---

## Testing Strategy

| Test | Scenario |
|------|----------|
| `TestOpenMergedCSV_SingleFS` | One FS with CSV → returns rows correctly |
| `TestOpenMergedCSV_MultipleFS` | Three FSs with CSV → rows merged correctly |
| `TestOpenMergedCSV_NotFound` | No FS contains the CSV → returns `fs.ErrNotExist` |
| `TestOpenMergedCSV_EmptyCSV` | FS contains empty CSV → skipped |
| `TestOpenMergedCSV_FilesClosedPromptly` | Use a spy FS that tracks `Close()` calls → all files closed before function returns |

### Spy FS for Close Tracking

```go
type spyFile struct {
    fs.File
    closed *atomic.Bool
}

func (f *spyFile) Close() error {
    f.closed.Store(true)
    return f.File.Close()
}
```

### Coverage
- Target: 90%+ for `pkg/props/`.

---

## Linting

- `golangci-lint run --fix` must pass.
- The fix resolves any `gocritic`/`deferInLoop` warnings on this code path.
- No new `nolint` directives.

---

## Documentation

- Godoc for new `readCSVFromFS` helper method.
- No user-facing documentation changes.

---

## Backwards Compatibility

- No breaking changes. Behaviour is identical — files are just closed sooner.

---

## Future Considerations

- **Streaming CSV merge**: For very large CSVs, a streaming merge that doesn't load all rows into memory could be added. Out of scope for this fix.

---

## Implementation Phases

### Phase 1 — Fix
1. Add `readCSVFromFS` helper method
2. Refactor `openMergedCSV` to use the helper
3. Remove the `defer` from the loop

### Phase 2 — Tests
1. Add the spy FS test verifying prompt closure
2. Add edge case tests (empty, missing, multiple)
3. Run with race detector

---

## Verification

```bash
go build ./...
go test -race ./pkg/props/...
go test ./...
golangci-lint run --fix

# Verify no defer-in-loop remains
grep -A2 'for.*range' pkg/props/assets.go | grep 'defer'  # should return nothing
```
