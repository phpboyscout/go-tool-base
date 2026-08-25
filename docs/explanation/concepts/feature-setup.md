---
title: Feature Setup & Registry
description: Rationale and implementation for modular initialization and self-registering features.
date: 2026-02-17
tags: [concepts, setup, initialization, registry, modularity]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Feature Setup & Registry

GTB uses a modular setup and registration pattern to decouple core framework logic from domain-specific features. This allows applications built with GTB to scale by adding new capabilities without modifying the central root command or initialization flow.

## The Feature Registry

The `FeatureRegistry` (found in `pkg/setup/registry.go`) acts as a central clearinghouse for features to announce their presence. It manages three types of contributions:

1.  **Initialisers**: Code that runs during the `init` command to configure a feature.
2.  **Subcommands**: Cobra commands that should be added to the CLI hierarchy.
3.  **Flags**: Global or command-specific flags that a feature requires.

### Self-Registration

Features register themselves using the `Register` function, typically called from a feature's package `init()` function or a high-level command constructor.

> [!WARNING]
> **Thread Safety**: The `FeatureRegistry` (and the `Register` functions) are NOT safe for concurrent use. All `Register*` calls MUST happen during `init()`: before `main()` starts and before any goroutines are spawned. Reading from the registry later is safe because the Go memory model guarantees that `init()` happens-before `main()`.

```go
func init() {
    setup.Register(
        props.FeaturePipeline, // The unique feature identifier
        []setup.InitialiserProvider{NewPipelineInitialiser},
        []setup.SubcommandProvider{NewPipelineCommands},
        nil, // No specific command flags
    )
}
```

## The Initialiser Interface

For features that require interactive setup (like configuring API keys or local paths), the `Initialiser` interface provides a standardized contract:

```go
type Initialiser interface {
    Name() string
    IsConfigured(cfg config.Reader) bool
    Configure(p *props.Props, cfg setup.Editor) error
}
```

- **`IsConfigured`**: Checks the existing configuration (through a pinned read-only view) to see if setup can be skipped.
- **`Configure`**: Executes the interactive setup (often using `huh` or prompt libraries) and writes values through the `setup.Editor`, whose `Set` edits the user's config file in place via the store's transactional `Apply`.

## The Init Workflow

When a user runs the `init` command, the `setup.Initialise` function (context first: `Initialise(ctx, p, opts)`) performs the following steps:

1.  **Bootstrap**: Creates the config directory and materialises the base `config.yaml` from the init template (`assets/init/config.yaml`, merged across every registered asset bundle): seeding a missing file, or merging new template keys under an existing one.
2.  **Open an editor**: Opens a `setup.Editor` over the target file, whose reads resolve the file layered over the tool's embedded defaults.
3.  **Discovery**: The `init` command retrieves all registered `InitialiserProvider` functions from the `globalRegistry` and passes the resulting initialisers in.
4.  **Execution**: Iterates through each initialiser, checking if it's already configured and running the `Configure` step if necessary.
5.  **Persistence**: Each `Set` is applied to the file in place as it happens (via the store's `Apply`), preserving template comments. There is no separate final write-back.

## Why use this pattern?

- **Decoupling**: The core `root` and `init` commands don't need to know about every possible feature. They simply iterate through what has been registered.
- **Scalability**: Adding a new feature is as simple as creating a new package that calls `setup.Register`.
- **Consistency**: All features follow the same setup and registration lifecycle, providing a predictable experience for both developers and users.

## Best Practices

- **Feature Enums**: Define unique identifiers for your features in `pkg/props` or a shared constants package to avoid collisions.
- **Idempotent Setup**: Ensure that `IsConfigured` accurately reflects the state of the configuration to avoid re-prompting users for information they've already provided.
- **Asset Integration**: If your feature requires default configuration values, ship them in an `assets/config.yaml` (the always-applied embedded-defaults layer) and put the human-facing, commented stanzas for the user's file in `assets/init/config.yaml`; register the bundle with `setup.RegisterAssets` so it is applied when the feature is enabled.
