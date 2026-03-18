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
| **Container** | The component in `pkg/config` that manages the lifecycle and merging of configuration from different sources. |
| **Controls** | The service lifecycle management system in `pkg/controls`. |
| **Autonomous Repair Agent** | An AI component that can automatically fix generated code or documentation if it fails validation. |
| **Skeleton** | A pre-defined project template used by the `generate skeleton` command to scaffold new projects. |
| **TUI** | Text User Interface. Refers to the interactive terminal interfaces (like the docs browser) built with Charm libraries. |
