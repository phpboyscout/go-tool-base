---
title: "Generator Ignore File"
description: "Add .gtb/ignore file support for excluding files from generator output while maintaining hash tracking."
date: 2026-03-31
status: IMPLEMENTED
tags:
  - specification
  - generator
  - feature
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
---

# Generator Ignore File

Authors
:   Matt Cockayne

Date
:   31 March 2026

Status
:   IMPLEMENTED

---

## Overview

When regenerating a project, the GTB generator walks all embedded skeleton assets and either writes or prompts to overwrite each file. Users who have replaced or heavily customised certain generated files (e.g. CI workflows, Dockerfiles, linting configs) are forced to decline overwrites every time they regenerate. There is no way to permanently mark files as "hands off."

The `.gtb/ignore` file lets users declare glob patterns for files the generator should skip during generation and regeneration. Ignored files are never written or prompted — but their current on-disk content is still hashed and recorded in the manifest so that hash tracking remains complete.

## Design Decisions

**Gitignore-like syntax**: The format is familiar to all Go developers. Comments (`#`), blank lines, negation (`!`), and glob patterns work as expected.

**Hash tracking preserved**: Ignored files still have their on-disk hash recorded in the manifest. This means the generator knows the file exists and what state it's in, even though it won't touch it. This enables future features like drift detection.

**Force flag does not override ignore**: The `--force` flag bypasses conflict prompts, but it does NOT bypass ignore rules. If a file is in `.gtb/ignore`, it stays ignored regardless of flags.

**No external dependencies**: Pattern matching uses `filepath.Match` and `strings.Cut` from the standard library. No gitignore parsing library is needed.

**Backwards compatible**: Missing `.gtb/ignore` is valid — the generator behaves exactly as before.

## Implementation

### Files

| File | Action |
|------|--------|
| `internal/generator/ignore.go` | Created — `IgnoreRules`, `LoadIgnoreRules`, `IsIgnored` |
| `internal/generator/ignore_test.go` | Created — 10 pattern matching tests |
| `internal/generator/skeleton.go` | Modified — pass `*IgnoreRules` through `walkSkeletonAssets` and `generateSkeletonTemplateFiles` |
| `internal/generator/regenerate.go` | Modified — load ignore rules in `regenerateSkeletonFiles` |

### Integration Points

- `generateSkeletonTemplateFiles` and `walkSkeletonAssets` accept `*IgnoreRules`
- Before processing each file, `rules.IsIgnored(relPath)` is checked
- If ignored: `hashIgnoredFile` reads the on-disk content and records the hash
- If not ignored: normal rendering/writing flow
- `LoadIgnoreRules` is called in both `generateSkeletonFiles` (initial generation) and `regenerateSkeletonFiles` (regeneration)
