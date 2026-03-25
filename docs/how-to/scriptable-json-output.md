---
title: Add Scriptable JSON Output to a Command
description: How to use pkg/output to give any command --output json support with the standard Response envelope, and how to render markdown for terminal output.
date: 2026-03-25
tags: [how-to, output, json, markdown, scripting, automation, glamour]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Add Scriptable JSON Output to a Command

`pkg/output` provides two things commands typically need: structured JSON output for CI/CD pipelines and scripts, and styled markdown rendering for terminal display. Both are controlled by the `--output` flag already defined on the root command.

---

## The Standard JSON Envelope

All built-in GTB commands wrap their JSON output in a standard `Response` envelope:

```json
{
  "status": "success",
  "command": "mycommand",
  "data": { ... }
}
```

Using this envelope means your command's JSON output follows the same schema as `version`, `doctor`, `update`, and `init` — consumers know where to look for the payload and can check `status` without parsing `data`.

---

## Step 1: Define Your Data Struct

Tag every exported field for JSON serialisation:

```go
type DeployResult struct {
    Environment string `json:"environment"`
    Version     string `json:"version"`
    Replicas    int    `json:"replicas"`
}
```

---

## Step 2: Use Writer with the Response Envelope

The `--output` flag is already registered on the root command — read it and pass it to `output.NewWriter`:

```go
import (
    "fmt"
    "io"
    "os"

    "github.com/phpboyscout/go-tool-base/pkg/output"
    "github.com/phpboyscout/go-tool-base/pkg/props"
)

func NewCmdDeploy(p *props.Props) *cobra.Command {
    return &cobra.Command{
        Use:   "deploy",
        Short: "Deploy to an environment",
        RunE: func(cmd *cobra.Command, args []string) error {
            format, _ := cmd.Flags().GetString("output")
            w := output.NewWriter(os.Stdout, output.Format(format))

            result := runDeploy(args[0])

            return w.Write(output.Response{
                Status:  output.StatusSuccess,
                Command: "deploy",
                Data:    result,
            }, func(out io.Writer) {
                fmt.Fprintf(out, "Deployed %s to %s (%d replicas)\n",
                    result.Version, result.Environment, result.Replicas)
            })
        },
    }
}
```

**Text output** (`mytool deploy production`):

```
Deployed v1.2.3 to production (3 replicas)
```

**JSON output** (`mytool deploy production --output json`):

```json
{
  "status": "success",
  "command": "deploy",
  "data": {
    "environment": "production",
    "version": "v1.2.3",
    "replicas": 3
  }
}
```

---

## Step 3: Add JSON Output to Existing Commands (Emit Pattern)

If your command already has text output via the logger or `fmt.Print` and you want to add a JSON path without changing the text path, use `output.Emit`. It writes the envelope only when `--output json` is set, and is a no-op in text mode.

```go
func runMigrate(cmd *cobra.Command, p *props.Props, env string) error {
    p.Logger.Info("Running migrations", "environment", env)

    count, err := runMigrations(env)
    if err != nil {
        return err
    }

    p.Logger.Infof("Applied %d migrations", count)

    return output.Emit(cmd, output.Response{
        Status:  output.StatusSuccess,
        Command: "migrate",
        Data:    map[string]any{"environment": env, "applied": count},
    })
}
```

---

## Step 4: Handle Errors in JSON Mode

Use `output.EmitError` to produce an error envelope in JSON mode. In text mode it is a no-op, so you can return the error as normal for text users.

```go
result, err := deploy()
if err != nil {
    _ = output.EmitError(cmd, "deploy", err)
    return err
}
```

JSON error output:

```json
{
  "status": "error",
  "command": "deploy",
  "error": "connection refused: could not reach production cluster"
}
```

---

## Step 5: Suppress Text-Only Work in JSON Mode

Use `output.IsJSONOutput` to skip expensive or interactive text-only operations (spinners, colour tables, progress bars) when the caller wants JSON:

```go
if !output.IsJSONOutput(cmd) {
    spinner := startSpinner("Deploying…")
    defer spinner.Stop()
}
```

---

## Rendering Markdown in Terminal Output

Many commands receive markdown content — AI responses, release notes, changelogs — and need to display it styled in the terminal. Use `output.RenderMarkdown`:

```go
notes, _ := fetchReleaseNotes(version)
fmt.Print(output.RenderMarkdown(notes))
```

`RenderMarkdown` detects the terminal width automatically, applies glamour's auto-style (light/dark theme aware), and falls back to the plain string if glamour fails.

### Combining Markdown and JSON Output

Use `Writer.Render` when a command produces markdown for terminals and structured data for JSON consumers. `Writer.Render` is a no-op in JSON mode, so both calls are unconditionally safe:

```go
func runChangelog(cmd *cobra.Command, p *props.Props) error {
    format, _ := cmd.Flags().GetString("output")
    w := output.NewWriter(os.Stdout, output.Format(format))

    notes, meta := fetchChangelog()

    // Writes glamour-styled output in text mode; no-op in JSON mode
    if err := w.Render(notes); err != nil {
        return err
    }

    // Writes envelope in JSON mode; no-op in text mode
    return output.Emit(cmd, output.Response{
        Status:  output.StatusSuccess,
        Command: "changelog",
        Data:    meta,
    })
}
```

---

## Testing Both Formats

```go
func TestDeploy_JSONOutput(t *testing.T) {
    var buf bytes.Buffer

    cmd := &cobra.Command{Use: "deploy"}
    cmd.Flags().String("output", "text", "output format")
    _ = cmd.Flags().Set("output", "json")
    cmd.SetOut(&buf)
    cmd.SetContext(context.Background())

    err := runDeploy(cmd, testProps, "staging")
    require.NoError(t, err)

    var resp output.Response
    require.NoError(t, json.Unmarshal(buf.Bytes(), &resp))
    assert.Equal(t, output.StatusSuccess, resp.Status)
    assert.Equal(t, "deploy", resp.Command)

    // Access nested data
    data, _ := json.Marshal(resp.Data)
    var result DeployResult
    require.NoError(t, json.Unmarshal(data, &result))
    assert.Equal(t, "staging", result.Environment)
}

func TestDeploy_TextOutput(t *testing.T) {
    var buf bytes.Buffer

    cmd := &cobra.Command{Use: "deploy"}
    cmd.Flags().String("output", "text", "output format")
    cmd.SetOut(&buf)
    cmd.SetContext(context.Background())

    err := runDeploy(cmd, testProps, "staging")
    require.NoError(t, err)

    // Text mode: no JSON envelope in output
    assert.Contains(t, buf.String(), "staging")
    assert.NotContains(t, buf.String(), `"status"`)
}
```

Pipe the JSON output through `jq` to confirm it parses cleanly:

```bash
mytool deploy staging --output json | jq '.data.environment'
# "staging"
```

---

## Choosing the Right Pattern

| Situation | Pattern |
|-----------|---------|
| New command, has both text and data output | `Writer.Write(Response{...}, textFunc)` |
| Existing command with logger/fmt text output | `output.Emit(cmd, Response{...})` |
| Command displays markdown (AI output, release notes) | `output.RenderMarkdown(content)` or `w.Render(markdown)` |
| Need to branch on format in logic (suppress spinners) | `output.IsJSONOutput(cmd)` |
| Error branch in JSON-capable command | `output.EmitError(cmd, name, err)` |

---

## Related Documentation

- **[Output component](../components/output.md)** — full API reference for `Writer`, `Response`, `Emit`, `RenderMarkdown`
- **[Adding Custom Commands](custom-commands.md)** — command wiring patterns
- **[Switch to Structured JSON Logging for Containers](structured-json-logging.md)** — complement to JSON output for daemon/container deployments
