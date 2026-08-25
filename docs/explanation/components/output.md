---
title: Output
description: Structured CLI output formatting has been extracted to the standalone go/output module.
date: 2026-07-21
tags: [components, output, formatting, cli, module-extraction]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Output

Structured command output: text/JSON/YAML/CSV/TSV/Markdown rendering, the
`{status, command, data, error}` JSON envelope, tables, spinners, progress bars,
status lines, and glamour markdown rendering. Now lives in the standalone module
**[`gitlab.com/phpboyscout/go/output`](https://gitlab.com/phpboyscout/go/output)**.

It is a framework-free package built around a single configured `Renderer`: the
destination writer, output format, theme, and interactivity are fixed once and
every method reads them. The core carries no CLI-framework dependency, the
cobra binding (reading the `--output` flag and the command's writer) is the opt-in
**`go/output/cobra`** subpackage, which GTB's commands import as `ocobra`.

- **Docs:** [output.go.phpboyscout.uk](https://output.go.phpboyscout.uk)
- **API reference:** [pkg.go.dev](https://pkg.go.dev/gitlab.com/phpboyscout/go/output)
- **Migration:** [`go/output` → `go/output`](../../reference/migration/v0.x-output-extracted.md)

```go
import (
    "gitlab.com/phpboyscout/go/output"
    ocobra "gitlab.com/phpboyscout/go/output/cobra"
)

// In a cobra RunE — reads --output and the command's writer:
r := ocobra.NewRenderer(cmd)
if r.IsJSON() {
    return r.Emit(output.Response{Status: output.StatusSuccess, Command: "update", Data: result})
}
// human-readable path…
```

GTB consumes the module directly across its built-in commands (`version`,
`doctor`, `config`, `changelog`, `update`, `init`, `docs`) and the root
update-check spinner. There is no GTB-side adapter. The cut-over was a clean
repoint to the redesigned façade.
