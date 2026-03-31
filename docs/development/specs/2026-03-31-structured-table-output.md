---
title: "Structured Table Output"
description: "Table renderer for list-style command output that respects --output json and terminal width."
date: 2026-03-31
status: DRAFT
tags:
  - specification
  - output
  - table
  - tui
  - feature
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
---

# Structured Table Output

Authors
:   Matt Cockayne

Date
:   31 March 2026

Status
:   DRAFT

---

## Overview

`pkg/output` provides structured JSON/text formatting for command responses, but list-style output (release assets, config values, doctor results, telemetry status) is rendered ad-hoc. A table renderer that respects `--output json` for scripting and formats as a readable table for terminal use would bring consistency to all list commands.

---

## Design

### API

```go
// Table renders tabular data with automatic column sizing.
type Table struct {
    headers []string
    rows    [][]string
}

// NewTable creates a table with the given column headers.
func NewTable(headers ...string) *Table

// AddRow appends a row to the table. Values are positional by column.
func (t *Table) AddRow(values ...string) *Table

// Render writes the table to the writer in the specified format.
// Supports FormatText (aligned columns) and FormatJSON (array of objects).
func (t *Table) Render(w io.Writer, format Format) error

// RenderToOutput integrates with the existing pkg/output response system.
// When format is JSON, outputs as an array of objects keyed by header names.
// When format is text, outputs as a terminal-width-aware aligned table.
func (t *Table) RenderToOutput(w io.Writer, format Format, cmdName string) error
```

### Format-Aware Rendering

**Text (terminal):**
```
NAME         VERSION    STATUS
my-tool      v1.2.3     up to date
other-tool   v0.9.1     update available
```

- Columns auto-sized to content with minimum padding
- Truncates long values to terminal width (if detectable)
- Header row in uppercase or bold (if terminal supports it)

**JSON (`--output json`):**
```json
{
  "status": "success",
  "command": "list",
  "data": [
    {"name": "my-tool", "version": "v1.2.3", "status": "up to date"},
    {"name": "other-tool", "version": "v0.9.1", "status": "update available"}
  ]
}
```

Uses the existing `pkg/output` response envelope when rendered via `RenderToOutput`.

---

## Integration

The table component lives in `pkg/output` alongside the existing `Response` type. Commands that currently build ad-hoc table output (doctor, config list, telemetry status) can adopt it for consistency.

---

## Open Questions

1. Should the table support sorting by column?
2. Should there be a `FormatCSV` or `FormatTSV` option for piping to spreadsheet tools?
3. Should column alignment (left/right/center) be configurable per column?
