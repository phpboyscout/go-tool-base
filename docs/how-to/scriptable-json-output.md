---
title: Add Scriptable JSON Output to a Command
description: How to use the go/output module to give any command --output json support with the standard Response envelope, and how to render markdown for terminal output.
date: 2026-07-21
tags: [how-to, output, json, markdown, scripting, automation, glamour]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Add Scriptable JSON Output to a Command

The **[`go/output`](https://output.go.phpboyscout.uk)** module provides two things
commands typically need: structured JSON output for CI/CD pipelines and scripts,
and styled markdown rendering for terminal display. Both are controlled by the
`--output` flag already defined on the root command.

Everything hangs off a single configured `Renderer` (`output.New(...)`). For cobra
commands, the opt-in `go/output/cobra` subpackage reads the `--output` flag and the
command's writer for you. Import it aliased to avoid the clash with `spf13/cobra`:

```go
import (
    "gitlab.com/phpboyscout/go/output"
    ocobra "gitlab.com/phpboyscout/go/output/cobra"
)
```

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

Using this envelope means your command's JSON output follows the same schema as
`version`, `doctor`, `update`, and `init`, consumers know where to look for the
payload and can check `status` without parsing `data`.

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

## Step 2: Build a Renderer with the Response Envelope

The `--output` flag is already registered on the root command. `ocobra.NewRenderer`
builds a `Renderer` from the command. Its writer is `cmd.OutOrStdout()` and its
format is the `--output` value:

```go
func NewCmdDeploy(p *props.Props) *setup.Command {
    return setup.Wrap("deploy", &cobra.Command{
        Use:   "deploy",
        Short: "Deploy to an environment",
        RunE: func(cmd *cobra.Command, args []string) error {
            r := ocobra.NewRenderer(cmd)

            result := runDeploy(args[0])

            return r.Write(output.Response{
                Status:  output.StatusSuccess,
                Command: "deploy",
                Data:    result,
            }, func(out io.Writer) {
                fmt.Fprintf(out, "Deployed %s to %s (%d replicas)\n",
                    result.Version, result.Environment, result.Replicas)
            })
        },
    })
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

If your command already has text output via the logger or `fmt.Print` and you want
to add a JSON path without changing the text path, use `ocobra.Emit`. It writes the
envelope only when `--output json` is set, and is a no-op in text mode.

```go
func runMigrate(cmd *cobra.Command, p *props.Props, env string) error {
    p.Logger.Info("Running migrations", "environment", env)

    count, err := runMigrations(env)
    if err != nil {
        return err
    }

    p.Logger.Info("migrations applied", "count", count)

    return ocobra.Emit(cmd, output.Response{
        Status:  output.StatusSuccess,
        Command: "migrate",
        Data:    map[string]any{"environment": env, "applied": count},
    })
}
```

---

## Step 4: Handle Errors in JSON Mode

Use `ocobra.EmitError` to produce an error envelope in JSON mode. In text mode it is
a no-op, so you can return the error as normal for text users.

```go
result, err := deploy()
if err != nil {
    _ = ocobra.EmitError(cmd, "deploy", err)
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

Use `ocobra.IsJSONOutput` to skip expensive or interactive text-only operations
(spinners, colour tables, progress bars) when the caller wants JSON:

```go
if !ocobra.IsJSONOutput(cmd) {
    err := output.New().Spin(cmd.Context(), "Deploying…", func(ctx context.Context) error {
        return deploy(ctx)
    })
    _ = err
}
```

---

## Rendering Markdown in Terminal Output

Many commands receive markdown content (AI responses, release notes, changelogs) 
and need to display it styled in the terminal. Use `output.RenderMarkdown`:

```go
notes, _ := fetchReleaseNotes(version)
fmt.Print(output.RenderMarkdown(notes))
```

`RenderMarkdown` detects the terminal width automatically, applies glamour's
auto-style (light/dark theme aware), and falls back to the plain string if glamour
fails.

### Combining Markdown and JSON Output

Use `Renderer.Render` when a command produces markdown for terminals and structured
data for JSON consumers. `Render` is a no-op in JSON mode, so both calls are
unconditionally safe:

```go
func runChangelog(cmd *cobra.Command, p *props.Props) error {
    r := ocobra.NewRenderer(cmd)

    notes, meta := fetchChangelog()

    // Writes glamour-styled output in text mode; no-op in JSON mode
    if err := r.Render(notes); err != nil {
        return err
    }

    // Writes envelope in JSON mode; no-op in text mode
    return r.Emit(output.Response{
        Status:  output.StatusSuccess,
        Command: "changelog",
        Data:    meta,
    })
}
```

---

## Testing Both Formats

Because the `Renderer` takes its writer and format as injected values, tests drive
it with a `bytes.Buffer`, no TTY, no globals, fully parallel-safe:

```go
func TestDeploy_JSONOutput(t *testing.T) {
    var buf bytes.Buffer

    cmd := &cobra.Command{Use: "deploy"}
    ocobra.RegisterOutputFlag(cmd)
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
    ocobra.RegisterOutputFlag(cmd)
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
| New command, has both text and data output | `r.Write(output.Response{...}, textFunc)` |
| Existing command with logger/fmt text output | `ocobra.Emit(cmd, output.Response{...})` |
| Command displays markdown (AI output, release notes) | `output.RenderMarkdown(content)` or `r.Render(markdown)` |
| Need to branch on format in logic (suppress spinners) | `ocobra.IsJSONOutput(cmd)` |
| Error branch in JSON-capable command | `ocobra.EmitError(cmd, name, err)` |

---

## Related Documentation

- **[Output component](../explanation/components/output.md)**: the extracted module and its GTB wiring
- **[Module docs](https://output.go.phpboyscout.uk)**: full guides, and the [API reference on pkg.go.dev](https://pkg.go.dev/gitlab.com/phpboyscout/go/output)
- **[Adding Custom Commands](custom-commands.md)**: command wiring patterns
- **[Switch to Structured JSON Logging for Containers](structured-json-logging.md)**: complement to JSON output for daemon/container deployments
