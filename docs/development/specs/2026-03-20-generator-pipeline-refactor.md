---
title: "Generator Pipeline Refactor Specification"
description: "Refactor the internal generator package to replace four diverged command-specific codepaths with a shared CommandPipeline, a value-typed CommandContext, and a decomposed file layout that eliminates the mutable-config foot-gun and reduces the package from ~4 300 lines to focused, independently testable units."
date: 2026-03-20
status: IMPLEMENTED
tags:
  - specification
  - generator
  - refactor
  - architecture
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-sonnet-4-6)
    role: AI drafting assistant
---

# Generator Pipeline Refactor Specification

Authors
:   Matt Cockayne, Claude (claude-sonnet-4-6) *(AI drafting assistant)*

Date
:   20 March 2026

Status
:   IMPLEMENTED

---

## Overview

The `internal/generator` package currently contains ~4 300 lines across three God files (`commands.go` 1 449 lines, `manifest.go` 1 006 lines, `ast.go` 1 856 lines). Four distinct command flows — `generate project`, `generate command`, `regenerate project`, `regenerate manifest` — are woven together into these files without clean boundaries, making it difficult to reason about what is shared, what is command-specific, and where failures can propagate.

The core problem is that `generate command` and `regenerate project` share the same file-generation pipeline (`performGeneration` → `postGenerate`) but reach it through different orchestration code. When a bug is fixed in one path it has historically not been applied to the other (the `reRegisterChildCommands` omission being the most recent example). A secondary problem is that `regenerateCommandRecursive` currently mutates the shared `g.config` struct and relies on a `defer` to restore it — a pattern that is fragile under error conditions and opaque to readers.

This spec proposes three targeted improvements:

1. **`CommandPipeline`** — a dedicated struct that owns the `performGeneration` → `postGenerate` flow. Both `generate command` and `regenerate project` construct and run a pipeline; the orchestration differences live outside it.
2. **`CommandContext`** — a value type that replaces `g.config` mutation during recursive regeneration. Config is resolved once per command and passed explicitly.
3. **File decomposition** — split the three God files into focused units with single responsibilities, reducing per-file line counts and making the package navigable.

### Terminology

**`CommandPipeline`**
:   A struct that owns the ordered steps for generating a single command's files and updating downstream state (manifest, parent registration, documentation).

**`CommandContext`**
:   A value type carrying the fully resolved configuration for a single command invocation: name, parent path, flags, feature options, and source paths. Replaces the pattern of mutating `Generator.config` with a deferred restore.

**Pipeline step**
:   An individual unit of work within `CommandPipeline` (e.g., `generateFiles`, `registerInParent`, `updateManifest`). Steps are executed in a fixed order but can be individually enabled/disabled via `PipelineOptions`.

---

## Design Decisions

**Pipeline over middleware chain**: The pipeline is a plain struct with an ordered `Run()` method rather than a chain of `func(ctx) error` middleware. This keeps the execution order explicit in code rather than implicit in registration order, which matters for auditability and debugging.

**`CommandContext` as a value, not a pointer**: Passing `CommandContext` by value ensures that `regenerateCommandRecursive` cannot accidentally share state between sibling invocations. Each recursive call gets its own copy.

**No new public API**: All proposed types are `internal/generator`-scoped. The four public generator methods (`Generate`, `RegenerateProject`, `RegenerateManifest`, `GenerateSkeleton`) retain their existing signatures so downstream callers (CLI command handlers) are unaffected.

**Incremental decomposition**: Rather than a single big-bang rewrite, the file split is an independent step from the `CommandPipeline` introduction. The two can be reviewed and merged separately.

**`ManifestCommandUpdate` struct**: The 14-parameter `updateCommandRecursive` function is a maintenance hazard. Replacing the parameter list with a struct allows new fields to be added without cascading signature changes.

---

## Current Pain Points

### 1. Mutable config with defer restore

`regenerateCommandRecursive` (commands.go:1270) saves and restores `g.config` via defer:

```go
origConfig := *g.config
defer func() { *g.config = origConfig }()
g.setupCommandConfig(cmd, parentPath)
```

If a panic occurs mid-function, the defer runs but the stack is already unwound. Any goroutine spawned inside the function (or a called function) can observe the mutated state. The pattern is also a reader hazard — it is non-obvious that `g.config` changes meaning partway through a call.

### 2. `reRegisterChildCommands` creates bare child generators

```go
childGen := New(g.props, &Config{
    Path:   g.config.Path,
    Name:   child.Name,
    Parent: childParent,
})
childGen.registerSubcommand()
```

This bypasses all the configuration that `New()` normally resolves. Any future field added to `Config` that affects `registerSubcommand` will silently be missing from child re-registrations.

### 3. `updateCommandRecursive` has 14 parameters

```go
func updateCommandRecursive(
    commands *[]ManifestCommand,
    parentPath []string,
    name, description, longDescription string,
    aliases []string, args string,
    hashes map[string]string,
    withAssets, withInitializer, persistentPreRun, preRun bool,
    protected *bool, hidden bool,
    flags []ManifestFlag,
) bool
```

Adding any new manifest field requires updating every call site. There are currently four call sites spread across `manifest.go` and `recursive_test.go`.

### 4. `postGenerate` has six unrelated responsibilities

```go
func (g *Generator) postGenerate(ctx, data, cmdDir) error {
    generateAssetFiles()          // asset embedding
    registerSubcommand()          // AST manipulation
    updateParentCmdHash()         // manifest I/O
    reRegisterChildCommands()     // AST + manifest I/O
    updateManifest()              // manifest I/O
    handleDocumentationGeneration() // docs generation
}
```

All six steps are always executed in the same order with no way to skip steps selectively (e.g., regeneration may not want to re-run AI documentation). Errors in any step do not propagate — they are warned and swallowed, which makes `postGenerate` appear successful even when several sub-steps fail silently.

### 5. God files obscure the shared core

A developer reading `commands.go` must mentally filter 1 449 lines to find the 200 or so lines that constitute the shared pipeline. This increases the time to understand where a fix should land and increases the risk of a fix landing in the wrong place.

---

## Proposed Public API

All changes are internal. No changes to exported functions or types.

---

## Internal Implementation

### `CommandContext`

```go
// CommandContext holds the fully resolved configuration for a single command
// generation or regeneration pass.  It is constructed once and passed by value
// so recursive invocations cannot share or accidentally mutate each other's state.
type CommandContext struct {
    // Identity
    Name       string
    ParentPath []string // empty means direct child of root

    // Display
    Short string
    Long  string

    // Flags
    Flags []CommandFlag

    // Feature options
    WithAssets      bool
    WithInitializer bool
    PersistentPreRun bool
    PreRun          bool

    // Routing
    Path   string // filesystem root of the project
    Force  bool
    Hidden bool
    Args   string
    Aliases []string
    Protected *bool
}

// CmdDir returns the resolved filesystem path for this command's package.
func (c CommandContext) CmdDir() string { ... }

// ParentStr returns the slash-joined parent path string expected by Config.Parent.
func (c CommandContext) ParentStr() string { ... }
```

`CommandContext` replaces the `setupCommandConfig` + defer-restore pattern. `regenerateCommandRecursive` constructs one per command and passes it down rather than mutating shared state.

### `PipelineOptions`

```go
// PipelineOptions controls which steps the CommandPipeline executes.
// Zero value enables all steps.
type PipelineOptions struct {
    SkipAssets        bool // do not generate asset files
    SkipDocumentation bool // do not run documentation generation
    SkipRegistration  bool // do not modify the parent cmd.go
    Force             bool // overwrite even if hash conflicts exist
}
```

### `CommandPipeline`

```go
// CommandPipeline executes the ordered steps to generate or regenerate the
// files for a single command.  Both Generate() and regenerateCommandRecursive()
// construct and Run a pipeline; their differences live in the preparation code
// that produces a CommandContext, not in the pipeline itself.
type CommandPipeline struct {
    generator *Generator
    opts      PipelineOptions
}

func newCommandPipeline(g *Generator, opts PipelineOptions) *CommandPipeline

// Run executes all enabled pipeline steps in order for the given context.
// Each step is logged.  Steps that encounter non-fatal errors log a warning
// and continue; steps that encounter fatal errors return immediately.
func (p *CommandPipeline) Run(ctx context.Context, cmdCtx CommandContext) error

// Steps (unexported, called by Run in order):
//   1. generateFiles(ctx, cmdCtx) error
//   2. registerInParent(cmdCtx) error      — skippable via SkipRegistration
//   3. reRegisterChildren(cmdCtx) error    — always runs (repairs overwrites)
//   4. persistManifest(cmdCtx) error
//   5. generateDocumentation(ctx, cmdCtx)  — skippable via SkipDocumentation
```

`reRegisterChildren` replaces the current `reRegisterChildCommands`. Instead of creating bare child generators it calls `registerSubcommandForContext(childCtx CommandContext)` — a helper that constructs the correct `subcommandContext` from an explicit `CommandContext` rather than from `g.config`.

### `ManifestCommandUpdate`

```go
// ManifestCommandUpdate carries all the fields that updateCommandRecursive
// writes to a ManifestCommand entry.  New manifest fields are added here
// rather than to the function signature.
type ManifestCommandUpdate struct {
    Name             string
    Description      string
    LongDescription  string
    Aliases          []string
    Args             string
    Hashes           map[string]string
    Flags            []ManifestFlag
    WithAssets       bool
    WithInitializer  bool
    PersistentPreRun bool
    PreRun           bool
    Protected        *bool
    Hidden           bool
}

func updateCommandRecursive(
    commands *[]ManifestCommand,
    parentPath []string,
    update ManifestCommandUpdate,
) bool
```

---

## Project Structure

### Before

```
internal/generator/
├── ast.go           (1 856 lines — AST + utilities)
├── commands.go      (1 449 lines — all command orchestration)
├── manifest.go      (1 006 lines — manifest types + I/O + scanning)
├── hash.go
├── hash_test.go
├── recursive_test.go
├── export_test.go
├── skeleton.go
└── templates/
```

### After

```
internal/generator/
├── ast.go               (AST manipulation: register/deregister subcommands)
├── ast_extract.go       (AST reading: extractCommandMetadata, extractProjectProperties)
├── context.go           (CommandContext, PipelineOptions)
├── pipeline.go          (CommandPipeline — the shared core)
├── generate.go          (Generate(), AI generation, resolveGenerationFlags())
├── regenerate.go        (RegenerateProject(), regenerateCommandRecursive(),
│                          regenerateRootCommand(), buildSkeletonSubcommands())
├── removal.go           (Remove(), performRemoval())
├── files.go             (GenerateCommandFile(), generateRegistrationFile(),
│                          handleExecutionFile(), handleInitializerFile(),
│                          generateInitializerFile(), generateTestFile())
├── stubs.go             (ensureHookStubs(), ensureImport())
├── assets.go            (generateAssetFiles())
├── hash.go              (calculateHash(), verifyHash(), updateParentCmdHash())
├── manifest.go          (Manifest types, load/save, MarshalYAML impls)
├── manifest_update.go   (updateManifest(), updateRootCommand(),
│                          updateCommandRecursive(), ManifestCommandUpdate)
├── manifest_query.go    (findManifestCommand(), findCommandAt(),
│                          FindCommandParentPath(), findCommandRecursive())
├── manifest_scan.go     (RegenerateManifest(), scanCommands(), scanRecursive(),
│                          extractCommandMetadata(), buildCmdTree(), linkParentChild())
├── hash_test.go
├── pipeline_test.go     (NEW — tests for CommandPipeline steps)
├── recursive_test.go
├── export_test.go
└── templates/
```

Net effect: largest file shrinks from 1 856 to ~600 lines. The shared pipeline is isolated in one 200-line file.

---

## Error Handling

`postGenerate` currently swallows most errors with `g.props.Logger.Warnf(...)` and returns `nil`. This means callers cannot distinguish partial failure from success.

The `CommandPipeline` adopts the following policy:

- **Fatal steps** (`generateFiles`, `persistManifest`): return the error immediately; pipeline halts.
- **Advisory steps** (`registerInParent`, `reRegisterChildren`, `generateDocumentation`): log a warning and continue; a `PipelineResult` accumulates step errors so the caller can inspect them.

```go
type PipelineResult struct {
    Warnings []StepWarning
}

type StepWarning struct {
    Step string
    Err  error
}

func (p *CommandPipeline) Run(ctx context.Context, cmdCtx CommandContext) (PipelineResult, error)
```

Callers that only care about fatal errors (`if err != nil`) continue to work unchanged. Callers that want to surface warnings (e.g., `regenerate project` summary) can inspect `result.Warnings`.

---

## Testing Strategy

### Unit tests for `CommandPipeline`

`pipeline_test.go` (new file) uses an in-memory `afero.MemMapFs` and a stubbed `props.Props` to test each pipeline step independently:

- `TestPipeline_generateFiles_createsExpectedFiles`
- `TestPipeline_generateFiles_verifiesHashOnOverwrite`
- `TestPipeline_reRegisterChildren_preservesExistingRegistrations`
- `TestPipeline_reRegisterChildren_noopWhenNoChildren`
- `TestPipeline_persistManifest_updatesHashAfterRegistration`
- `TestPipeline_skipRegistration_optionHonoured`

### Updated `recursive_test.go`

The two `updateCommandRecursive` calls are updated to use `ManifestCommandUpdate`. No logic changes.

### Updated `hash_test.go`

`TestHashUpdateOnRegeneration` continues to work; no changes required. It exercises the pipeline end-to-end through `RegenerateProject()`.

### Integration

`TestGenerateAndRegenerate` (new) runs `Generate()` for a parent and child command against a `MemMapFs` project skeleton, then runs `RegenerateProject()`, and asserts that:

1. The child `AddCommand` call survives in the parent `cmd.go`.
2. The manifest hashes are consistent with file content.
3. `PipelineResult.Warnings` is empty.

---

## Migration & Compatibility

All changes are internal to `internal/generator`. The public interface of `Generator` — its constructor `New()` and its four exported methods — is unchanged. CLI command handlers in `internal/cmd/` require no modification.

`export_test.go` may need updating if it exports functions that are being moved or renamed, but no exported test helpers are removed.

The `Config` struct is not removed — it remains the constructor input. `CommandContext` is derived from `Config` inside the generator methods; it does not leak out.

---

## Future Considerations

- **Concurrent regeneration**: Once `g.config` mutation is eliminated via `CommandContext`, `regenerateCommandRecursive` could process sibling commands concurrently. This is out of scope for this spec but becomes structurally possible.
- **Pipeline hooks**: `PipelineOptions` could eventually accept `PreStep`/`PostStep` callbacks, enabling observability instrumentation. Not needed now.
- **`manifest_scan.go` interface**: `RegenerateManifest` could accept a `Scanner` interface rather than calling methods directly, improving testability of the scan-and-rebuild flow. Out of scope.

---

## Implementation Phases

### Phase 1 — `ManifestCommandUpdate` struct

Replace the 14-parameter `updateCommandRecursive` with `ManifestCommandUpdate`. Update all four call sites. Update `recursive_test.go`.

**Acceptance criteria:**
- `go test ./internal/generator/...` passes.
- `golangci-lint run` reports no new issues.
- The function signature change is the only diff in `manifest.go`.

### Phase 2 — `CommandContext` and elimination of mutable config

Introduce `CommandContext` and `CommandContext.CmdDir()`. Replace `setupCommandConfig` + defer-restore in `regenerateCommandRecursive` with a `buildCommandContext(cmd ManifestCommand, parentPath []string) CommandContext` constructor. Pass `CommandContext` explicitly through `prepareRegenerationData` and `performGeneration`.

**Acceptance criteria:**
- `regenerateCommandRecursive` contains no `origConfig`/`defer` pattern.
- `setupCommandConfig` is removed.
- Existing tests pass unchanged.

### Phase 3 — `CommandPipeline` extraction

Extract `performGeneration` + `postGenerate` into `CommandPipeline.Run()`. Replace the inline calls in `Generate()` and `regenerateCommandRecursive()`:

```go
// Before (Generate):
if err := g.performGeneration(ctx, cmdDir, &data); err != nil { return err }
if err := g.postGenerate(ctx, data, cmdDir); err != nil { return err }

// After:
pipeline := newCommandPipeline(g, PipelineOptions{Force: g.config.Force})
result, err := pipeline.Run(ctx, cmdCtx)
```

Fix `reRegisterChildCommands` to use `registerSubcommandForContext` instead of creating bare child generators.

Introduce `PipelineResult` and propagate advisory warnings to callers.

**Acceptance criteria:**
- All existing tests pass.
- New `pipeline_test.go` tests pass for all pipeline steps.
- `postGenerate` is deleted from `commands.go`.

### Phase 4 — File decomposition

Move functions into the new file layout described in [Project Structure](#project-structure). No logic changes — this is a pure file reorganisation.

**Acceptance criteria:**
- `go build ./...` and `go test ./internal/generator/...` pass.
- No function is in a different package after the move.
- Each new file is ≤ 400 lines.

### Phase 5 — `pipeline_test.go` and integration test

Add `pipeline_test.go` with the unit tests listed under [Testing Strategy](#testing-strategy). Add the `TestGenerateAndRegenerate` integration test.

**Acceptance criteria:**
- All new tests pass.
- Coverage for `pipeline.go` is ≥ 85%.

### Phase 6 — Fix `regenerateRootCommand` help config loss

**Background.** `generate project` and `regenerate project` both render `pkg/cmd/root/cmd.go` via `templates.SkeletonRoot(SkeletonRootData{...})`. During initial generation the full `SkeletonRootData` is populated from `SkeletonConfig`, including the five help fields (`HelpType`, `SlackChannel`, `SlackTeam`, `TeamsChannel`, `TeamsTeam`). These are stored in `Manifest.Properties.Help` immediately after generation. However `regenerateRootCommand` builds `SkeletonRootData` from the manifest without reading `Properties.Help`, so every `regenerate project` run silently overwrites `root/cmd.go` with an empty help configuration.

**Fix.** Extract a `buildSkeletonRootData` function in `regenerate.go` that maps all manifest fields — including `Properties.Help` — to `SkeletonRootData`. Replace the inline struct literal in `regenerateRootCommand` with a call to this helper.

```go
// buildSkeletonRootData constructs a complete SkeletonRootData from a Manifest
// so that regenerateRootCommand and any future callers produce root/cmd.go
// with all project settings intact — including help channel configuration.
func buildSkeletonRootData(m Manifest, subcommands []templates.SkeletonSubcommand) templates.SkeletonRootData {
    releaseProvider, org, repoName := m.GetReleaseSource()
    return templates.SkeletonRootData{
        Name:             m.Properties.Name,
        Description:      string(m.Properties.Description),
        ReleaseProvider:  releaseProvider,
        Host:             m.ReleaseSource.Host,
        Org:              org,
        RepoName:         repoName,
        Private:          m.ReleaseSource.Private,
        DisabledFeatures: calculateDisabledFeatures(m.Properties.Features),
        EnabledFeatures:  calculateEnabledFeatures(m.Properties.Features),
        HelpType:         m.Properties.Help.Type,
        SlackChannel:     m.Properties.Help.SlackChannel,
        SlackTeam:        m.Properties.Help.SlackTeam,
        TeamsChannel:     m.Properties.Help.TeamsChannel,
        TeamsTeam:        m.Properties.Help.TeamsTeam,
        Subcommands:      subcommands,
    }
}
```

`regenerateRootCommand` becomes:

```go
func (g *Generator) regenerateRootCommand(m Manifest) error {
    subcommands, err := g.buildSkeletonSubcommands(m.Commands)
    if err != nil {
        return err
    }
    data := buildSkeletonRootData(m, subcommands)
    // ... write root/cmd.go as before
}
```

**Acceptance criteria:**
- `regenerateRootCommand` reads all five `ManifestHelp` fields and passes them to `SkeletonRootData`.
- `buildSkeletonRootData` is the single point where manifest → root data mapping is defined.
- A new test `TestRegenerateProject_preservesHelpConfig` verifies that a manifest containing `properties.help` survives a `RegenerateProject` call with the help fields intact in the rendered `root/cmd.go`.
- `go test ./internal/generator/...` passes.
- `golangci-lint run` reports no new issues.
