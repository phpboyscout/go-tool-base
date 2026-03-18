---
title: Regeneration & Synchronization
description: Bi-directional synchronization between the manifest and Go source code.
date: 2026-02-16
tags: [concepts, regeneration, manifest, sync]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Regeneration & Synchronization

At the heart of GTB's [Manifest-Driven Development](framework-cli.md) are the **Regeneration Commands**. These commands enable a bi-directional synchronization between your high-level design (`manifest.yaml`) and your actual Go implementation.

## The Bi-Directional Loop

GTB does not lock you into a single way of working. It supports two primary synchronization directions:

### 1. Manifest -> Code (`regenerate project`)

This is the **Design-First** workflow.

- **Action**: You update the `.gtb/manifest.yaml` file (e.g., renaming a command, adding flags, or moving a subcommand).
- **Result**: Running `regenerate project` rebuilds the wiring files (`cmd.go`, `init.go` if present) to match the new manifest structure.
- **Safety**:
    - It **never overwrites** your custom logic in `main.go` (which is excluded from hashing and generation if it exists).
    - It protects manual changes in `init.go` and `cmd.go` by verifying their content hashes against the manifest before regeneration.

### 2. Code -> Manifest (`regenerate manifest`)

This is the **Code-First** workflow.

- **Action**: You make structural changes directly in your Go source code (e.g., using a traditional Cobra implementation style).
- **Result**: Running `regenerate manifest` uses AST (Abstract Syntax Tree) scanning to inspect your code and rebuild the `manifest.yaml` to reflect the current state of your binary.

!!! warning "Structural Expectations"
    The `regenerate manifest` command relies on the [Standard Project Structure](project-structure.md). If your codebase has been manually modified to depart significantly from this structure (e.g., non-standard package naming or manual Cobra registration bypasses), the scanner may fail to correctly identify commands or their properties.

## Why is Regeneration Valuable?

### Architectural Integrity
In large CLI tools, it's easy for the command hierarchy to become inconsistent. Regeneration ensures that the "intended" design (in the manifest) and the "actual" design (in the code) stay perfectly in sync.

### Rapid Refactoring
Renaming a root command or moving 10 subcommands to a different parent is traditionally a painful manual process of renaming packages and updating imports. With GTB, you simply edit the manifest and run one command to refactor your entire project structure.

!!! important "Manual Cleanup Required"
    While regeneration creates the new command structure for you, it currently **does not** automatically:

    - **Remove Old Files**: Stale packages or commands from the previous design must be deleted manually.
    - **Migrate Logic**: Any custom business logic in a command's `main.go` file must be moved to the new location by the developer.

    We hope to implement this functoinality in future versions of the tool

### Cyclical Validation (The "Ultimate Test")

Regeneration provides a robust mechanism for validating the framework itself. By running a "Cyclical Sync":

1. Generate **Code A** from **Manifest A**.
2. Generate **Manifest B** from **Code A**.
3. If **Manifest A** and **Manifest B** are identical, you have absolute proof that the generator and scanner are perfectly consistent.

!!! tip "The Verification Loop"
    This cyclical sync is used internally by GTB to ensure that the code we generate today will always be correctly understood and manageable by the framework tomorrow.

## Summary

Regeneration transforms the `manifest.yaml` from a static configuration file into a **Living Design Document**. It gives you the freedom to evolve your tool's interface without the overhead of manual boilerplate management, while providing a mathematically verifiable guarantee of architectural consistency.
