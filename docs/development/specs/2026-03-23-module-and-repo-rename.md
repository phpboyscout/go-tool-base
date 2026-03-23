---
title: Module and Repository Rename (gtb to go-tool-base)
description: Specification for the global rename of the GitHub repository and Go module from gtb to go-tool-base to align with the new pitch strategy.
date: 2026-03-23
status: DRAFT
tags: [architecture, module, branding]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Module and Repository Rename (gtb to go-tool-base)

## Problem Statement
The current Go module and GitHub repository are named `gtb` (`github.com/phpboyscout/gtb`). Market research has shown that the acronym "GTB" is heavily overloaded across multiple industries (cybersecurity, bioinformatics, spatial analysis). This creates significant brand confusion and SEO issues. To effectively position the framework as the premier "Intelligent Application Lifecycle Framework for Go", the public presence, module path, and repository name must correctly reflect the full unabbreviated name: **Go Tool Base**.

## Goals
- Rename the GitHub repository from `gtb` to `go-tool-base`.
- Rename the Go module path from `github.com/phpboyscout/gtb` to `github.com/phpboyscout/go-tool-base`.
- Ensure all internal import paths within the repository are updated to the new module path.
- Update the CLI generator (`gtb generate skeleton`/`command`) to output the new module path in generated projects.
- Preserve `gtb` as the executable binary name for brevity in the terminal.

## Non-Goals
- We are *not* renaming the executing binary. It will remain `gtb`.
- We are not changing the core architecture, interfaces, or logic in this workstream.

## Public API
This is a massive breaking change (v2 semantic scale, or handled carefully pre-v1 release) for any consumers already importing `github.com/phpboyscout/gtb`.

Every occurrence of:
`import "github.com/phpboyscout/gtb/pkg/..."`
Must become:
`import "github.com/phpboyscout/go-tool-base/pkg/..."`

## Data Models
No changes to structured types or databases.

## Error Cases
- **Stale imports in user projects**: Existing projects importing `github.com/phpboyscout/gtb` will eventually fail if GitHub removes the automated redirect, or if they attempt to upgrade to a version tagged only on the new module path.

## Testing Strategy
1. A global find-and-replace will be executed.
2. `go build ./...` and `go test ./...` must pass.
3. The generator must be invoked to create a skeleton project in a temporary directory, and the generated project's `go mod tidy` and `go build` must succeed to ensure the generator templates were correctly updated.

## Implementation Phases
1. **Repository Level**: Rename the repository in GitHub Settings from `gtb` to `go-tool-base`.
2. **Module Level**: Update `go.mod` to `module github.com/phpboyscout/go-tool-base`.
3. **Internal Imports**: Execute a global sed/find-replace for `"github.com/phpboyscout/gtb/` -> `"github.com/phpboyscout/go-tool-base/` across all `.go` files.
4. **Generator Templates**: Ensure `internal/generator/templates/` files output the correct import paths.
5. **CI/CD**: Update GitHub Actions workflows, `install.sh`, `install.ps1`, and GoReleaser configurations to reflect the new repository name.

## Open Questions
- Providing a transition period: Should we cut a final `v1.2.x` tag on the old module path with a deprecation warning before committing the `go.mod` change to main?
