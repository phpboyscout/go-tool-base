---
title: "Embedded Changelog Command"
description: "A changelog command that displays version history from an embedded CHANGELOG.md generated at build time."
date: 2026-03-31
status: DRAFT
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
:   DRAFT

---

## Overview

GTB already has a changelog parser (`pkg/changelog`) and git-cliff generates `CHANGELOG.md` at release time. However, this changelog is only included in release archives — users have no way to view it from the running binary.

This spec adds a `changelog` command that displays version history from an embedded `CHANGELOG.md`. The changelog is baked into the binary at build time via `go:embed`, so it always reflects the version the user is running. This naturally complements the update flow — after running `update`, users can run `changelog` to see what changed.

---

## Design

### Embedding

The `CHANGELOG.md` is embedded into the binary's root command assets at build time. goreleaser already generates it via git-cliff before building — the binary just needs to include it.

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

## Open Questions

1. Should the changelog be embedded as a Go string or read from the asset system (`props.Assets`)?
2. Should the `--since` flag accept "last updated" as a value (using the stored version from before the update)?
3. Should the generator scaffold the git-cliff config (`cliff.toml`) for new projects?
4. How should tools without a CHANGELOG.md handle the command? (Skip registration? Show "no changelog available"?)
