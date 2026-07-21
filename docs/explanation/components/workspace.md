---
title: Workspace
description: Project-root detection has been extracted to the standalone go/workspace module.
date: 2026-07-21
tags: [components, workspace, project, utility, module-extraction]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Workspace

Project-root detection — walking up from a starting directory to find a marker file
(`.gtb/manifest.yaml`, `go.mod`, `.git`) over an injected `afero.Fs` — now lives in the
standalone module **[`gitlab.com/phpboyscout/go/workspace`](https://gitlab.com/phpboyscout/go/workspace)**.

It is a framework-free utility with no Props or command-lifecycle coupling. GTB's own
generator commands use it to resolve the project root when run from a subdirectory (all of
`regenerate`, `generate command/docs/flag`, and `remove` resolve the root when `--path` is
`"."`), and any tool built on GTB can use it the same way.

- **Docs:** [workspace.go.phpboyscout.uk](https://workspace.go.phpboyscout.uk)
- **API reference:** [pkg.go.dev](https://pkg.go.dev/gitlab.com/phpboyscout/go/workspace)
- **Migration:** [project-root detection → `go/workspace`](../../reference/migration/v0.x-workspace-extracted.md)

```go
import "gitlab.com/phpboyscout/go/workspace"

ws, err := workspace.DetectFromCWD(afero.NewOsFs(), workspace.DefaultMarkers)
if err != nil {
    return errors.Wrap(err, "not inside a project")
}
// ws.Root, ws.Marker
```
