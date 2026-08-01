---
title: Contributor's Glossary
description: Definitions for terminology specific to the GTB library.
tags: [glossary, terminology, library]
---

# Contributor's Glossary

Terms and concepts central to the GTB framework.

| Term | Definition |
| :--- | :--- |
| **Generator-First** | The philosophy that the CLI structure, flags, and documentation are defined in `manifest.yaml`, and code is generated from this source of truth. |
| **Manifest** | The `manifest.yaml` file (or programmatic equivalent) that describes a CLI's structure. |
| **Props** | The "Properties" object used for dependency injection throughout the framework. |
| **Container** | The component in `go/config` that manages the lifecycle and merging of configuration from different sources. |
| **Controls** | The service lifecycle management system in `go/controls`. |
| **Autonomous Repair Agent** | An AI component that can automatically fix generated code or documentation if it fails validation. |
| **Skeleton** | A pre-defined project template used by the `generate skeleton` command to scaffold new projects. |
| **TUI** | Text User Interface. Refers to the interactive terminal interfaces (like the docs browser) built with Charm libraries. |
| **CommandPipeline** | The ordered five-step post-generation pipeline (`pipeline.go`) that runs after every `cmd.go` is written: copy assets → register in parent → re-register children → persist manifest → generate docs. Controlled by `PipelineOptions`. |
| **PipelineOptions** | A struct passed to `newCommandPipeline` that gates individual pipeline steps via `SkipAssets`, `SkipRegistration`, and `SkipDocumentation` booleans. |
| **ManifestCommandUpdate** | A struct (`manifest_update.go`) used as the single parameter to `updateCommandRecursive`. Replaces the previous 14-parameter function signature and makes manifest mutation call sites self-documenting. |
| **CommandContext** | A value type (`context.go`) that captures the fully-resolved name, parent path, and import path for a command. Created by `buildCommandContext`; used by `reRegisterChildCommands` to construct child generators with the correct package identity. |
| **buildSkeletonRootData** | A pure mapping function (`regenerate.go`) that converts a `Manifest` into a `SkeletonRootData`, including all `ManifestHelp` fields. It is the single source of truth for root `cmd.go` rendering during both initial skeleton generation and project regeneration. |
| **reRegisterChildCommands** | Pipeline step 3 (`pipeline.go`). After a parent `cmd.go` is overwritten, this step reads the manifest to find existing child commands and re-injects their `AddCommand` calls, preserving child registrations across regeneration. |
