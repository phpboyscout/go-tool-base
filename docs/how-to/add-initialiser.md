---
title: Add Initialiser
description: Step-by-step guide to creating and registering custom Initialisers.
date: 2026-02-16
tags: [how-to, initialisers, setup, configuration]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

Practical, step-by-step instructions for creating and registering a custom Initialiser.

!!! important
    **Initialisers are for configuration, not logic.**
    The goal of an Initialiser is to ensure the necessary values (API keys, paths, preferences) are present in the `config.yaml`. The actual functional logic of your feature should reside in your `cobra.Command` and its associated business logic.

## Step 1: Implement the Initialiser Interface

Create a new package or add a file to your feature package that implements `setup.Initialiser`.

```go
package myfeature

import (
	"github.com/phpboyscout/gtb/pkg/config"
    "github.com/phpboyscout/gtb/pkg/props"
)

type MyInitialiser struct {
    skip bool
}

func (m *MyInitialiser) Name() string { return "My Feature" }

func (m *MyInitialiser) IsConfigured(cfg config.Containable) bool {
    return m.skip || cfg.GetString("myfeature.key") != ""
}

func (m *MyInitialiser) Configure(props *props.Props, cfg config.Containable) error {
    props.Logger.Info("Configuring My Feature...")
    // Perform interactive work here (e.g., using huh)
    cfg.Set("myfeature.key", "some-value")
    return nil
}
```

## Step 2: Set up Flag Registration (Optional)

If your initialiser should be skippable via a CLI flag (e.g., `--skip-myfeature`), define a package-level variable and a flag provider function.

```go
var skipMyFeature bool

func registerFlags(cmd *cobra.Command) {
    cmd.Flags().BoolVar(&skipMyFeature, "skip-myfeature", false, "Skip setting up my feature")
}
```

## Step 3: Register your Initialiser

Use the `setup.Register` function in your package's `init()` block. This ensures that when your package is imported, the initialiser becomes available to the framework.

```go
package myfeature

import (
    "github.com/phpboyscout/gtb/pkg/props"
    "github.com/phpboyscout/gtb/pkg/setup"
    "github.com/spf13/cobra"
)

func init() {
    setup.Register(props.FeatureCmd("myfeature"),
        []setup.InitialiserProvider{
            func(p *props.Props) setup.Initialiser {
                return &MyInitialiser{skip: skipMyFeature}
            },
		},
        []setup.SubcommandProvider{
            func(p *props.Props) []*cobra.Command {
                return []*cobra.Command{
                    {
                        Use:   "myfeature",
                        Short: "Force reconfigure my feature",
                        Run: func(cmd *cobra.Command, args []string) {
                            // Logic to run configuration directly
                        },
                    },
                }
            },
        },
        []setup.FeatureFlag{registerFlags},
    )
}
```

## Step 4: Import your setup package

Because Initialisers use package-level `init()` functions for registration, you must ensure your setup package is imported somewhere in your dependency graph.

The best practice is to add a **blank import** in the package where your feature's functional CLI logic is registered. This ensures that whenever your command is included in a tool, its configuration step is automatically registered too.

```go
package command

import (
    // Blank import triggers the setup package's init() registration
	_ "github.com/phpboyscout/gtb/pkg/setup/myfeature"
)

func NewCmdMyFeature(p *props.Props) *cobra.Command {
    // ...
}
```

## Best Practices

- **Check Env Vars**: In your `IsConfigured` or `Configure` methods, check for environment variable overrides. If a value is provided via ENV, skip the interactive prompt.
- **Modular Assets**: If your feature requires default configuration values, place them in `assets/init/config.yaml` within your package's embedded filesystem and mount them in your constructor.
- **Standalone Commands**: If users might want to re-configure only your feature later, register a **SubcommandProvider** to add a command like `init myfeature`.

---

!!! tip
    Look at the **AI Initialiser** (`pkg/setup/ai`) or **GitHub Initialiser** (`pkg/setup/github`) for comprehensive real-world examples.
