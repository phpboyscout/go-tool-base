---
title: Output
description: Structured output formatting, JSON response envelopes, and glamour markdown rendering for CLI commands.
date: 2026-03-25
tags: [components, output, json, formatting, markdown, glamour, cli]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Output

`pkg/output` is GTB's single source of truth for command output formatting. It provides three complementary capabilities:

1. **`Writer`** — writes structured data as indented JSON or human-readable text from a single call site.
2. **Response envelope** — a standard `{status, command, data, error}` JSON schema shared by all built-in commands, with `Emit`/`IsJSONOutput`/`EmitError` helpers to produce it.
3. **Markdown rendering** — `RenderMarkdown` and `Writer.Render` apply glamour ANSI styling in text mode with automatic terminal width detection.

All three integrate with the `--output` flag defined on the root command.

---

## Quick Start

### Structured output with Writer

```go
import (
    "fmt"
    "io"
    "os"
    "github.com/phpboyscout/go-tool-base/pkg/output"
)

type Result struct {
    Name    string `json:"name"`
    Version string `json:"version"`
}

func runMyCommand(cmd *cobra.Command, args []string) error {
    format, _ := cmd.Flags().GetString("output")
    w := output.NewWriter(os.Stdout, output.Format(format))

    result := &Result{Name: "myapp", Version: "v1.2.3"}

    return w.Write(output.Response{
        Status:  output.StatusSuccess,
        Command: "mycommand",
        Data:    result,
    }, func(out io.Writer) {
        fmt.Fprintf(out, "Name:    %s\n", result.Name)
        fmt.Fprintf(out, "Version: %s\n", result.Version)
    })
}
```

`mytool mycommand` → human-readable text. `mytool mycommand --output json` → Response envelope:

```json
{
  "status": "success",
  "command": "mycommand",
  "data": {
    "name": "myapp",
    "version": "v1.2.3"
  }
}
```

### Markdown rendering

```go
// Render AI output or release notes in the terminal
fmt.Print(output.RenderMarkdown(markdownContent))

// Or via Writer (no-op in JSON mode)
w.Render("## Changes\n\n- Added new flag\n- Fixed crash")
```

---

## API Reference

### Format

```go
type Format string

const (
    FormatText Format = "text"  // Human-readable terminal output (default)
    FormatJSON Format = "json"  // Machine-readable JSON
)
```

---

### Response

The standard envelope for all command JSON output.

```go
type Response struct {
    Status  string `json:"status"`
    Command string `json:"command"`
    Data    any    `json:"data,omitempty"`
    Error   string `json:"error,omitempty"`
}
```

| Field | Values | Purpose |
|-------|--------|---------|
| `Status` | `"success"`, `"error"`, `"warning"` | Quick outcome check |
| `Command` | e.g. `"version"`, `"update"` | Which command produced the output |
| `Data` | any JSON-serialisable value | Command-specific payload |
| `Error` | error message string | Populated when `Status` is `"error"` |

**Status constants:**

```go
const (
    StatusSuccess = "success"
    StatusError   = "error"
    StatusWarning = "warning"
)
```

---

### Emit

Writes a `Response` to `cmd.OutOrStdout()` when `--output json` is set. No-op for text mode.

```go
func Emit(cmd *cobra.Command, resp Response) error
```

```go
return output.Emit(cmd, output.Response{
    Status:  output.StatusSuccess,
    Command: "deploy",
    Data:    map[string]any{"environment": "production", "version": "v2.1.0"},
})
```

Returns an error only if JSON serialisation or writing fails — not for text mode.

---

### IsJSONOutput

Returns true when the `--output` flag is set to `"json"`.

```go
func IsJSONOutput(cmd *cobra.Command) bool
```

Use this to skip text-only work (spinner animations, table headers, progress bars) when JSON output is requested:

```go
if !output.IsJSONOutput(cmd) {
    spinner := startSpinner("Fetching…")
    defer spinner.Stop()
}
```

---

### EmitError

Builds an error `Response` and emits it. No-op in text mode.

```go
func EmitError(cmd *cobra.Command, commandName string, err error) error
```

```go
if err := doWork(); err != nil {
    if emitErr := output.EmitError(cmd, "mycommand", err); emitErr != nil {
        return emitErr
    }
    // Log or handle for text mode
    return err
}
```

---

### Writer

```go
// NewWriter creates an output writer for the given io.Writer and format.
func NewWriter(w io.Writer, format Format) *Writer

// Write outputs data in the configured format.
// JSON mode: marshals data to indented JSON and writes it.
// Text mode: calls textFunc with the underlying writer.
func (o *Writer) Write(data any, textFunc func(io.Writer)) error

// Render writes glamour-styled markdown in text mode.
// In JSON mode it is a no-op — use Write for JSON output.
func (o *Writer) Render(markdown string) error

// IsJSON returns true when the writer is in JSON mode.
func (o *Writer) IsJSON() bool
```

**Note:** Pass the `Response` struct as the `data` argument to `Write` when you want the JSON envelope. Pass a plain struct if you have a specific reason to bypass the envelope (e.g. low-level data APIs).

---

### RenderMarkdown

Renders markdown to styled ANSI terminal output via glamour. Detects terminal width automatically; falls back to 80 columns. If glamour fails for any reason, returns the original string unchanged — no error is surfaced.

```go
func RenderMarkdown(content string) string
```

```go
releaseNotes := "## v1.2.0\n\n- Added feature X\n- Fixed bug Y"
fmt.Print(output.RenderMarkdown(releaseNotes))
```

---

## Usage Patterns

### Pattern 1 — Writer with Response envelope (built-in command style)

Use this for commands that already use `output.NewWriter`. Pass `Response` as the data, so text mode renders your formatted output and JSON mode gets the envelope.

```go
func runVersion(cmd *cobra.Command, p *props.Props) error {
    format, _ := cmd.Flags().GetString("output")
    w := output.NewWriter(os.Stdout, output.Format(format))

    info := getVersionInfo(p)

    return w.Write(output.Response{
        Status:  output.StatusSuccess,
        Command: "version",
        Data:    info,
    }, func(out io.Writer) {
        fmt.Fprintf(out, "Version: %s\n", info.Version)
    })
}
```

### Pattern 2 — Emit for commands that don't use Writer

Use `Emit` when your command produces text output via the logger or `fmt.Print` and you just need to add a JSON path. The call is placed after all text work is done; it writes nothing in text mode.

```go
func runDeploy(cmd *cobra.Command, p *props.Props) error {
    p.Logger.Info("Deploying…")

    result, err := deploy()
    if err != nil {
        return err
    }

    p.Logger.Info("Deployed", "version", result.Version)

    // JSON output only — text users see the logger output above
    return output.Emit(cmd, output.Response{
        Status:  output.StatusSuccess,
        Command: "deploy",
        Data:    result,
    })
}
```

### Pattern 3 — Markdown in text mode, JSON data in JSON mode

Use `Writer.Render` and `Writer.Write` together when a command produces rich markdown text output but structured data for JSON consumers.

```go
func runChangelog(cmd *cobra.Command, p *props.Props) error {
    format, _ := cmd.Flags().GetString("output")
    w := output.NewWriter(os.Stdout, output.Format(format))

    notes, meta := fetchChangelog()

    // Render markdown when in text mode
    if err := w.Render(notes); err != nil {
        return err
    }

    // Emit structured JSON when in JSON mode
    return output.Emit(cmd, output.Response{
        Status:  output.StatusSuccess,
        Command: "changelog",
        Data:    meta,
    })
}
```

`Writer.Render` is a no-op in JSON mode, so both calls are safe to make unconditionally.

---

## Testing

Use `bytes.Buffer` as the writer and set it on the command to capture output:

```go
func TestMyCommand_JSONOutput(t *testing.T) {
    var buf bytes.Buffer

    cmd := &cobra.Command{Use: "mycommand"}
    cmd.Flags().String("output", "text", "output format")
    _ = cmd.Flags().Set("output", "json")
    cmd.SetOut(&buf)

    err := runMyCommand(cmd, testProps)
    require.NoError(t, err)

    var resp output.Response
    require.NoError(t, json.Unmarshal(buf.Bytes(), &resp))
    assert.Equal(t, output.StatusSuccess, resp.Status)
    assert.Equal(t, "mycommand", resp.Command)
}

func TestMyCommand_TextOutput(t *testing.T) {
    var buf bytes.Buffer

    cmd := &cobra.Command{Use: "mycommand"}
    cmd.Flags().String("output", "text", "output format")
    cmd.SetOut(&buf)

    err := runMyCommand(cmd, testProps)
    require.NoError(t, err)

    // Verify text output — no JSON envelope
    assert.Contains(t, buf.String(), "myapp")
    assert.NotContains(t, buf.String(), `"status"`)
}
```

For `RenderMarkdown`, simply assert the output is non-empty — ANSI escape codes vary by terminal:

```go
result := output.RenderMarkdown("# Heading\n\n**bold** text")
assert.NotEmpty(t, result)
```

---

## Related Documentation

- **[Add Scriptable JSON Output to a Command](../how-to/scriptable-json-output.md)** — step-by-step guide to adding `--output json` support
- **[Switch to Structured JSON Logging for Containers](../how-to/structured-json-logging.md)** — complement to JSON output for daemon deployments
- **[Props](props.md)** — dependency injection container
