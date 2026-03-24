---
title: "Generate Dry-Run Specification"
description: "Add a --dry-run flag to generate commands that previews file tree and content diffs without writing to disk."
date: 2026-03-21
status: IMPLEMENTED
tags:
  - specification
  - generator
  - feature
  - cli
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-opus-4-6)
    role: AI drafting assistant
---

# Generate Dry-Run Specification

Authors
:   Matt Cockayne, Claude (claude-opus-4-6) *(AI drafting assistant)*

Date
:   21 March 2026

Status
:   IMPLEMENTED

---

## Overview

The `generate` commands write files directly to disk with no preview mechanism. Users cannot see what will be created, modified, or deleted before it happens. This is particularly risky for commands that generate multiple files across the project tree.

A `--dry-run` flag will preview the planned file operations — showing the file tree, new file contents, and diffs for modified files — without writing anything.

---

## Design Decisions

**Flag on generator, not per-subcommand**: The `--dry-run` flag is added to the generator's shared configuration, making it available to all generate subcommands automatically.

**Afero overlay filesystem**: Rather than adding conditional logic throughout the generator, use an `afero.CopyOnWriteFs` overlay. In dry-run mode, writes go to an in-memory layer while reads fall through to the real filesystem. After generation completes, diff the overlay against the real filesystem to produce the preview.

**Unified diff output**: For modified files, show unified diffs (like `git diff`). For new files, show the full content. For deleted files (if applicable), show the path only. This matches developer expectations from version control tools.

**No partial execution**: Dry-run is all-or-nothing. Either everything writes or nothing writes. There is no interactive "approve each file" mode in this spec.

---

## Public API Changes

### Modified: `generator.Config`

```go
type Config struct {
    // ... existing fields ...

    // DryRun previews file operations without writing to disk.
    // When true, the generator reports what would be created, modified, or deleted.
    DryRun bool
}
```

### New: `DryRunResult`

```go
// DryRunResult contains the preview of planned file operations.
type DryRunResult struct {
    Created  []FilePreview
    Modified []FilePreview
    Deleted  []string
}

// FilePreview represents a single file operation in a dry run.
type FilePreview struct {
    Path    string
    Content []byte    // full content for new files
    Diff    string    // unified diff for modified files
}
```

---

## Internal Implementation

### Overlay Filesystem

```go
func (g *Generator) createFS() afero.Fs {
    if g.config.DryRun {
        // Writes go to memory, reads fall through to real FS
        return afero.NewCopyOnWriteFs(g.fs, afero.NewMemMapFs())
    }
    return g.fs
}
```

### Dry-Run Diff Generation

```go
func (g *Generator) produceDryRunResult(overlay afero.Fs) (*DryRunResult, error) {
    result := &DryRunResult{}

    // Walk the memory layer to find all written files
    memLayer := overlay.(*afero.CopyOnWriteFs) // extract the overlay layer
    // Compare each file in the overlay against the base filesystem
    // ...

    return result, nil
}
```

### Output Formatting

```go
func (r *DryRunResult) Print(w io.Writer) {
    if len(r.Created) > 0 {
        fmt.Fprintln(w, "Files to create:")
        for _, f := range r.Created {
            fmt.Fprintf(w, "  + %s\n", f.Path)
        }
    }
    if len(r.Modified) > 0 {
        fmt.Fprintln(w, "\nFiles to modify:")
        for _, f := range r.Modified {
            fmt.Fprintf(w, "  ~ %s\n", f.Path)
            fmt.Fprintln(w, f.Diff)
        }
    }
    if len(r.Deleted) > 0 {
        fmt.Fprintln(w, "\nFiles to delete:")
        for _, f := range r.Deleted {
            fmt.Fprintf(w, "  - %s\n", f)
        }
    }
}
```

### Command Integration

In each generate subcommand's `RunE`:

```go
func runGenerate(cmd *cobra.Command, args []string) error {
    dryRun, _ := cmd.Flags().GetBool("dry-run")
    cfg := generator.Config{
        DryRun: dryRun,
        // ... other config ...
    }

    gen := generator.New(props, cfg)
    result, err := gen.Run(ctx)
    if err != nil {
        return err
    }

    if dryRun && result.DryRun != nil {
        result.DryRun.Print(os.Stdout)
        return nil
    }

    return nil
}
```

### Flag Registration

```go
func setupGenerateFlags(cmd *cobra.Command) {
    cmd.PersistentFlags().Bool("dry-run", false, "preview changes without writing files")
}
```

---

## Project Structure

```
internal/generator/
├── generator.go       ← MODIFIED: DryRun config, overlay FS, result type
├── dryrun.go          ← NEW: DryRunResult, diff generation, output formatting
├── dryrun_test.go     ← NEW: dry-run tests
├── docs.go            ← UNCHANGED (writes to g.fs which is now overlay)
├── commands.go        ← UNCHANGED (same)
internal/cmd/generate/
├── generate.go        ← MODIFIED: --dry-run flag registration
```

---

## Testing Strategy

| Test | Scenario |
|------|----------|
| `TestDryRun_NewFiles` | Generate into empty dir → all files listed as "created" |
| `TestDryRun_ModifiedFiles` | Generate with existing files → diffs shown |
| `TestDryRun_NoWrite` | Dry-run mode → real filesystem unchanged |
| `TestDryRun_OutputFormat` | Preview output matches expected format |
| `TestDryRun_EmptyResult` | No changes needed → empty result |
| `TestDryRunResult_Print` | Print to buffer → verify formatting |
| `TestGenerator_DryRunFlag` | `--dry-run` flag parsed correctly |

### No-Write Verification

```go
func TestDryRun_NoWrite(t *testing.T) {
    baseFs := afero.NewMemMapFs()
    // Set up initial state
    afero.WriteFile(baseFs, "existing.go", []byte("original"), 0644)

    gen := generator.New(props, generator.Config{DryRun: true})
    gen.SetFS(baseFs)

    _, err := gen.Run(context.Background())
    assert.NoError(t, err)

    // Verify original file unchanged
    content, _ := afero.ReadFile(baseFs, "existing.go")
    assert.Equal(t, "original", string(content))

    // Verify no new files on base FS
    exists, _ := afero.Exists(baseFs, "new-generated-file.go")
    assert.False(t, exists)
}
```

### Coverage
- Target: 90%+ for `internal/generator/`.

---

## Linting

- `golangci-lint run --fix` must pass.
- No new `nolint` directives.

---

## Documentation

- Godoc for `DryRunResult` and `FilePreview` types.
- Godoc for `Config.DryRun` field.
- Add `--dry-run` to the generate command help text.
- Update `docs/components/generator.md` with dry-run usage examples.

---

## Backwards Compatibility

- **No breaking changes**. The flag defaults to `false`, preserving existing behaviour.
- Existing generate commands work identically without the flag.

---

## Future Considerations

- **Interactive approval**: A `--interactive` mode that shows each file and asks for confirmation before writing.
- **JSON output**: `--dry-run --output json` for CI integration — parse the preview programmatically.
- **Protection awareness**: Dry-run should indicate which files are protected and would be skipped.

---

## Implementation Phases

### Phase 1 — Infrastructure
1. Add `DryRun` field to `generator.Config`
2. Implement overlay filesystem creation
3. Implement `DryRunResult` type and diff generation

### Phase 2 — Output Formatting
1. Implement `Print()` method with unified diff format
2. Handle new files, modified files, and deleted files

### Phase 3 — Command Integration
1. Add `--dry-run` flag to generate commands
2. Wire flag through to generator config
3. Display results when dry-run is active

### Phase 4 — Tests
1. Add no-write verification test
2. Add output format tests
3. Add end-to-end dry-run tests

---

## Verification

```bash
go build ./...
go test -race ./internal/generator/...
go test ./...
golangci-lint run --fix

# Manual verification
go run . generate docs --dry-run
# Should show preview without writing files
```
