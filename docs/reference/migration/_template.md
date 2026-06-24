---
title: "TEMPLATE — Migration Guide: vX.Y to vX.Z"
description: "Template for authoring GTB migration guides. Not a real migration guide."
search:
  exclude: true
---

!!! warning "This is a template, not a migration guide"
    Copy this file to `docs/migration/vX.Y-to-vX.Z.md`, replace all
    placeholder text, and remove this admonition before publishing.

# Migrating from vX.Y to vX.Z

This guide covers the breaking changes and deprecations introduced in vX.Z and
provides step-by-step instructions for upgrading your GTB-based tool.

---

## Breaking Changes

### Change Title

**Package:** `pkg/example`

**Before:**

```go
// old API
```

**After:**

```go
// new API
```

**Migration:** Step-by-step instructions for updating call sites.

---

## Deprecations

### Deprecated API

**Package:** `pkg/example`

**Deprecated:** Description of what is deprecated and why.

**Replacement:** What to use instead.

**Removal planned:** vX.Z+1

---

## New Features

Brief description of new features that are relevant to the migration (e.g.
opt-in replacements for deprecated APIs).
