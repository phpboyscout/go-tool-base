---
title: "Output Table Formatter Specification"
description: "Add structured table output to pkg/output with configurable columns, sorting, and multiple output format selection (table, JSON, YAML, CSV) for kubectl-style CLI output."
date: 2026-03-26
status: IMPLEMENTED
tags:
  - specification
  - output
  - table
  - formatting
  - feature
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-opus-4-6)
    role: AI drafting assistant
---

# Output Table Formatter Specification

Authors
:   Matt Cockayne, Claude (claude-opus-4-6) *(AI drafting assistant)*

Date
:   26 March 2026

Status
:   IMPLEMENTED

---

## Overview

`pkg/output` provides markdown rendering via glamour and a JSON response envelope (`Response`/`Emit`), but lacks structured tabular output. CLI tools frequently need to display lists of resources (services, configs, versions, connections) in a `kubectl get`-style table with aligned columns, optional sorting, and selectable output formats.

Today, commands that need tabular output either build ad-hoc `fmt.Fprintf` tables (inconsistent alignment, no machine-readable fallback) or dump raw JSON. This specification adds a `TableWriter` that produces aligned, width-aware tables for human consumption and cleanly switches to JSON, YAML, or CSV for machine consumption, integrating with the existing `output.Writer` and `output.Format` patterns.

---

## Design Decisions

**Functional options for table configuration**: Following the `pkg/http/client.go` pattern, table creation uses `TableOption` functions. This keeps the API extensible (future: colour, borders, header styles) without breaking callers.

**Separate `TableWriter` rather than extending `Writer`**: Tables have fundamentally different semantics from the existing `Write` method (which takes `data` + `textFunc`). A dedicated `TableWriter` keeps responsibilities clear and avoids overloading the `Writer` API.

**Terminal width detection and truncation**: Tables detect terminal width via `charmbracelet/x/term` (already a dependency for `RenderMarkdown`). Columns are truncated with ellipsis when the table exceeds the terminal width, prioritising the first N columns. A `--wide` or `WithNoTruncation` option disables truncation for piped output.

**Format extension**: Two new `Format` constants (`FormatYAML`, `FormatCSV`) are added alongside the existing `FormatText` and `FormatJSON`. The `--output` flag already exists on the root command; table-aware commands simply respect the wider set of values.

**Struct tag-based column definition**: Column names and extraction are derived from struct tags (`table:"NAME,sortable"`) on the row data type. This eliminates manual column definition for the common case while allowing explicit `Column` definitions for complex scenarios.

---

## Public API Changes

### New Types in `pkg/output`

```go
// Column defines a single table column.
type Column struct {
    // Header is the display name shown in the table header row.
    Header    string
    // Field is the struct field name or map key to extract the value from.
    Field     string
    // Width is the fixed column width. Zero means auto-sized to content.
    Width     int
    // Sortable indicates this column can be used as a sort key.
    Sortable  bool
    // Formatter is an optional function to format the cell value.
    Formatter func(any) string
}

// TableWriter renders structured data as an aligned table or machine-readable format.
type TableWriter struct {
    // unexported fields
}

// TableOption configures the TableWriter.
type TableOption func(*tableConfig)
```

### Constructor and Options

```go
// NewTableWriter creates a TableWriter that writes to the given io.Writer.
func NewTableWriter(w io.Writer, format Format, opts ...TableOption) *TableWriter

// WithColumns explicitly defines the table columns. When not provided,
// columns are derived from struct tags on the row data type.
func WithColumns(cols ...Column) TableOption

// WithSortBy sets the column to sort rows by. The column must be marked Sortable.
func WithSortBy(field string) TableOption

// WithSortDescending reverses the sort order.
func WithSortDescending() TableOption

// WithNoHeader suppresses the header row in text table output.
func WithNoHeader() TableOption

// WithNoTruncation disables terminal-width truncation.
// Useful when output is piped to a file or another process.
func WithNoTruncation() TableOption

// WithMaxWidth overrides automatic terminal width detection.
func WithMaxWidth(width int) TableOption
```

### Writing Data

```go
// WriteRows renders the provided slice as a table.
// T must be a struct type (for tag-based columns) or []map[string]any.
// For JSON/YAML formats, the raw data is marshalled directly.
// For CSV format, columns are used as the header row.
// For text format, an aligned table with padding is produced.
func (t *TableWriter) WriteRows(rows any) error
```

### Format Constants

```go
const (
    FormatText Format = "text"   // existing
    FormatJSON Format = "json"   // existing
    FormatYAML Format = "yaml"   // NEW
    FormatCSV  Format = "csv"    // NEW
)
```

### Struct Tag Convention

```go
type ServiceStatus struct {
    Name   string `json:"name"   table:"NAME,sortable"`
    Status string `json:"status" table:"STATUS"`
    Port   int    `json:"port"   table:"PORT,sortable"`
    Uptime string `json:"uptime" table:"UPTIME"`
}
```

### Usage Example

```go
services := []ServiceStatus{
    {Name: "api", Status: "running", Port: 8080, Uptime: "3d2h"},
    {Name: "worker", Status: "stopped", Port: 0, Uptime: "0s"},
}

format := output.Format(cmd.Flag("output").Value.String())
tw := output.NewTableWriter(cmd.OutOrStdout(), format,
    output.WithSortBy("NAME"),
)

if err := tw.WriteRows(services); err != nil {
    return err
}

// Text output:
// NAME     STATUS    PORT   UPTIME
// api      running   8080   3d2h
// worker   stopped   0      0s

// JSON output: [{"name":"api","status":"running","port":8080,"uptime":"3d2h"}, ...]
// CSV output:  NAME,STATUS,PORT,UPTIME\napi,running,8080,3d2h\n...
```

---

## Internal Implementation

### Table Rendering (Text Format)

```go
func (t *TableWriter) renderText(columns []Column, rows [][]string) error {
    // Calculate column widths (max of header and all cell values)
    widths := calculateWidths(columns, rows)

    // Apply terminal width truncation
    if !t.cfg.noTruncation {
        termWidth := t.detectTerminalWidth()
        widths = truncateWidths(widths, termWidth)
    }

    // Render header
    if !t.cfg.noHeader {
        t.renderRow(columns, widths, headerStyle)
    }

    // Render data rows
    for _, row := range rows {
        t.renderRow(row, widths, dataStyle)
    }

    return nil
}
```

### Struct Tag Parsing

```go
func columnsFromStruct(v any) ([]Column, error) {
    t := reflect.TypeOf(v)
    if t.Kind() == reflect.Slice {
        t = t.Elem()
    }
    if t.Kind() == reflect.Ptr {
        t = t.Elem()
    }
    if t.Kind() != reflect.Struct {
        return nil, errors.New("WriteRows requires a slice of structs")
    }

    var cols []Column
    for i := range t.NumField() {
        tag := t.Field(i).Tag.Get("table")
        if tag == "" || tag == "-" {
            continue
        }
        parts := strings.Split(tag, ",")
        col := Column{
            Header:   parts[0],
            Field:    t.Field(i).Name,
            Sortable: slices.Contains(parts[1:], "sortable"),
        }
        cols = append(cols, col)
    }
    return cols, nil
}
```

### Sorting

```go
func sortRows(rows [][]string, colIdx int, descending bool) {
    sort.SliceStable(rows, func(i, j int) bool {
        // Attempt numeric comparison first, fall back to string
        if descending {
            return rows[i][colIdx] > rows[j][colIdx]
        }
        return rows[i][colIdx] < rows[j][colIdx]
    })
}
```

### JSON / YAML / CSV Rendering

- **JSON**: `json.NewEncoder` with indentation (matching existing `Writer.Write` behaviour).
- **YAML**: `gopkg.in/yaml.v3` marshalling. This is a new dependency but lightweight and widely used.
- **CSV**: `encoding/csv` from the standard library. Column headers from the `table` tag become the CSV header row.

---

## Project Structure

```
pkg/output/
├── output.go         ← MODIFIED: add FormatYAML, FormatCSV constants
├── output_test.go    ← UNCHANGED
├── table.go          ← NEW: TableWriter, TableOption, Column, WriteRows
├── table_test.go     ← NEW: table rendering tests
```

---

## Testing Strategy

| Test | Scenario |
|------|----------|
| `TestTableWriter_TextFormat` | Struct slice &#8594; aligned text table with headers |
| `TestTableWriter_JSONFormat` | Struct slice &#8594; indented JSON array |
| `TestTableWriter_YAMLFormat` | Struct slice &#8594; YAML list |
| `TestTableWriter_CSVFormat` | Struct slice &#8594; CSV with header row |
| `TestTableWriter_SortBy` | Rows sorted by specified column ascending |
| `TestTableWriter_SortDescending` | Rows sorted descending |
| `TestTableWriter_SortNonSortable` | Sort by non-sortable column &#8594; error |
| `TestTableWriter_NoHeader` | Text output without header row |
| `TestTableWriter_Truncation` | Long values truncated to terminal width with ellipsis |
| `TestTableWriter_NoTruncation` | WithNoTruncation &#8594; full values rendered |
| `TestTableWriter_MaxWidth` | WithMaxWidth overrides detected width |
| `TestTableWriter_EmptyRows` | Empty slice &#8594; header only (text) or empty array (JSON) |
| `TestTableWriter_StructTags` | Columns derived from `table` struct tags |
| `TestTableWriter_ExplicitColumns` | WithColumns overrides struct tags |
| `TestTableWriter_CustomFormatter` | Column.Formatter applied to cell values |
| `TestTableWriter_MapSlice` | `[]map[string]any` input with explicit columns |
| `TestColumnsFromStruct_NoTags` | Struct without table tags &#8594; error |
| `TestColumnsFromStruct_SkipDash` | Fields tagged `table:"-"` are excluded |

### Coverage

- Target: 90%+ for `table.go`.

---

## Linting

- `golangci-lint run --fix` must pass.
- No new `nolint` directives.

---

## Documentation

- Godoc for `TableWriter`, `Column`, all `TableOption` functions, and `WriteRows`.
- Update `docs/components/output.md` with table formatting usage, struct tag reference, and format selection examples.
- Add examples showing each output format (text, JSON, YAML, CSV) for the same data.

---

## Backwards Compatibility

- **No breaking changes**. `FormatText` and `FormatJSON` retain their existing values. `FormatYAML` and `FormatCSV` are new constants.
- The existing `Writer` type and `Emit`/`EmitError` functions are unchanged.
- The `--output` flag on the root command already accepts a string; commands that support table output document the additional accepted values.

---

## Future Considerations

- **Colour and styling**: Header row in bold, status columns with colour coding (green for running, red for stopped). Would use `charmbracelet/lipgloss` which is already an indirect dependency.
- **Interactive table**: Bubble Tea-based scrollable table for large datasets, leveraging the existing `pkg/forms` TUI infrastructure.
- **Custom delimiters**: TSV or pipe-separated output for specific pipeline integrations.
- **Column selection**: A `--columns` flag to select which columns to display, similar to `kubectl get -o custom-columns`.

---

## Implementation Phases

### Phase 1 — Core Table Writer
1. Define `Column`, `TableWriter`, `TableOption` types
2. Implement text table rendering with auto-sized columns
3. Implement struct tag parsing for column derivation
4. Add text format tests

### Phase 2 — Machine-Readable Formats
1. Add `FormatYAML` and `FormatCSV` constants
2. Implement JSON, YAML, and CSV rendering paths
3. Add format-specific tests

### Phase 3 — Sorting and Truncation
1. Implement sort-by-column with ascending/descending
2. Implement terminal width detection and truncation
3. Add sorting and truncation tests

### Phase 4 — Integration
1. Wire into an existing command (e.g., `doctor`, `version`) as a reference implementation
2. Update documentation with usage examples

---

## Verification

```bash
go build ./...
go test -race ./pkg/output/...
go test ./...
golangci-lint run --fix

# Verify new types exist
grep -n 'type TableWriter struct' pkg/output/table.go
grep -n 'func NewTableWriter' pkg/output/table.go
grep -n 'FormatYAML\|FormatCSV' pkg/output/output.go
```
