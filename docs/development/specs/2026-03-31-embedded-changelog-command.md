---
title: "Embedded Changelog Command"
description: "A changelog command that displays version history from an embedded CHANGELOG.md generated at build time."
date: 2026-03-31
status: IMPLEMENTED
tags:
  - specification
  - changelog
  - command
  - feature
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
---

# Embedded Changelog Command

Authors
:   Matt Cockayne

Date
:   31 March 2026

Status
:   IMPLEMENTED

---

## Overview

GTB already has a changelog parser (`pkg/changelog`) and a pure-Go changelog generator (`cmd/changelog`) that produces `CHANGELOG.md` from conventional commits. However, this changelog was only included in release archives — users had no way to view it from the running binary.

This spec adds a `changelog` command that displays version history from an embedded `CHANGELOG.md`. The changelog is baked into the binary at build time via `go:embed`, so it always reflects the version the user is running. This naturally complements the update flow — after running `update`, users can run `changelog` to see what changed.

---

## Design

### Embedding

The `CHANGELOG.md` is embedded into the binary's root command assets at build time. The `go:generate` directive runs `go tool changelog generate` to produce it — the binary just needs to include it.

```go
//go:embed CHANGELOG.md
var changelog string
```

For generated tools, the generator scaffolds the embed directive and the changelog command registration.

### Feature Flag

```go
const ChangelogCmd = FeatureCmd("changelog")
```

Default **enabled** — the changelog is a low-risk, high-utility command.

### Command

```
mytool changelog                    # show full changelog
mytool changelog --version v1.2.0   # show changes for a specific version
mytool changelog --since v1.1.0     # show changes since a version
mytool changelog --latest           # show only the most recent release
mytool changelog --output json      # structured output for scripting
```

### Implementation

The command reads the embedded changelog string and passes it through `pkg/changelog` for parsing. The parser already supports structured extraction of version sections, so filtering by version or range is a matter of selecting the right sections.

**Text output:** Renders the raw markdown for the selected version(s) to stdout.

**JSON output:** Uses the existing `pkg/changelog` structured types wrapped in `pkg/output` response envelope.

### Integration with Update Flow

After a successful self-update, the update command could suggest:

```
Updated to v1.3.0. Run `mytool changelog --latest` to see what changed.
```

---

## Project Structure

```
pkg/cmd/changelog/
├── changelog.go       ← NEW: command implementation
├── changelog_test.go  ← NEW: tests
pkg/props/
├── tool.go            ← MODIFIED: add ChangelogCmd feature flag
pkg/cmd/root/
├── root.go            ← MODIFIED: register changelog command
```

For generated tools:
```
internal/cmd/root/
├── root.go            ← MODIFIED: embed CHANGELOG.md, register command
```

---

## Resolved Questions

1. **Asset system** — read from `props.Assets` via `fs.ReadFile`. Consumers include CHANGELOG.md in their asset embed. Fault-tolerant if missing.
2. **`--since "last updated"`** — deferred. Simple `--since v1.1.0` is sufficient for now.
3. **Generator scaffolding** — handled. The skeleton generator emits the `go tool changelog generate` directive and includes the tool in the generated `go.mod`.
4. **Missing changelog** — command is always registered (for discoverability). Shows "no changelog available" with a hint if the file is missing from assets.
