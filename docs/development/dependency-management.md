---
title: Dependency Management
description: Managing internal and external dependencies for the GTB library.
tags: [dependencies, go.mod, library, versioning]
---

# Dependency Management

GTB is a foundational library. We take dependency management seriously to avoid "dependency hell" for downstream consumers.

## Selecting Dependencies

Before adding a new external dependency:
1.  **Check Capability**: Can the functionality be implemented easily in the standard library?
2.  **Maintenance**: Is the library well-maintained and widely used?
3.  **License**: Ensure the license is compatible with the project.

## Versioning Policy

We follow **Semantic Versioning (SemVer)** strictly.

- **Major Version (v2.x.x)**: Breaking changes to the public API in `pkg/`.
- **Minor Version (vx.1.x)**: New features or backward-compatible API additions.
- **Patch Version (vx.x.1)**: Bug fixes and performance improvements.

## Updating Dependencies

When updating dependencies, always run the full test suite to ensure no regressions are introduced:

```bash
go get -u ./...
go mod tidy
task test
```

## Private Modules

If you are working with other internal packages, ensure your `GOPRIVATE` environment variable is set correctly.

```bash
go env -w GOPRIVATE=git.example.com
```
