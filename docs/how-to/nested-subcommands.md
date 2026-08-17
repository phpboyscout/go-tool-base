---
title: Adding Nested Subcommands
description: Walk-through for adding a multi-level command tree (e.g. `tool deploy canary`) using setup.Command.Register, either via the generator or by hand.
date: 2026-05-31
tags: [how-to, commands, subcommands, generator, setup, composition]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Adding Nested Subcommands

GTB's command composition model (since v0.5) makes nested command trees straightforward: every `NewCmd<Name>` returns `*setup.Command`, and parents attach children via `parent.Register(child...)`. This guide walks through adding a `tool deploy canary` command — first using the generator, then showing the same shape by hand.

The end result is identical either way; the generator simply emits what you would write yourself.

---

## Option A: Use the generator

```bash
# Scaffold a project (skip if you already have one)
gtb generate skeleton \
    --name my-tool \
    --repo example/my-tool \
    --description "My tool" \
    --features init,update,doctor,config \
    --path .

# Add a root-level command
gtb generate command --name deploy --short "Deploy stuff"

# Add a nested subcommand under deploy
gtb generate command --name canary --parent deploy --short "Canary deploy"
```

The generator updates three things in one pass per command:

1. **Creates `pkg/cmd/<parent>/<name>/cmd.go`** with the canonical `NewCmd<Name>` constructor returning `*setup.Command`.
2. **Edits the parent's `cmd.go`** to add the import and a `cmd.Register(<pkg>.NewCmd<Name>(props))` call.
3. **Updates `.gtb/manifest.yaml`** so `gtb regenerate` knows the command tree.

Running `go build ./cmd/<tool>` after that produces a binary with the full `tool deploy canary` tree visible in `--help`.

---

## Option B: Write the constructors by hand

### 1. The leaf command — `canary`

```go
// pkg/cmd/deploy/canary/cmd.go
package canary

import (
    "github.com/spf13/cobra"

    "gitlab.com/phpboyscout/go-tool-base/pkg/props"
    "gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func NewCmdCanary(p *props.Props) *setup.Command {
    return setup.Wrap("canary", &cobra.Command{
        Use:   "canary",
        Short: "Canary deploy",
        RunE: func(cmd *cobra.Command, args []string) error {
            p.Logger.Info("running canary deploy")
            return nil
        },
    })
}
```

### 2. The parent command — `deploy`

The parent attaches its children with `Register` *before* returning:

```go
// pkg/cmd/deploy/cmd.go
package deploy

import (
    "github.com/spf13/cobra"

    "github.com/example/my-tool/pkg/cmd/deploy/canary"
    "gitlab.com/phpboyscout/go-tool-base/pkg/props"
    "gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func NewCmdDeploy(p *props.Props) *setup.Command {
    cmd := setup.Wrap("deploy", &cobra.Command{
        Use:   "deploy",
        Short: "Deploy stuff",
        RunE:  setup.GroupRunE,
    })

    cmd.Register(canary.NewCmdCanary(p))

    return cmd
}
```

Three important details:

- `RunE: setup.GroupRunE` is what a command that only groups its children should
  wire. Bare, it prints usage and succeeds; given a verb it does not have, it says
  so and exits `2`. **Leaving `RunE` unset does not get you that** — a command with
  no `Run`/`RunE` returns `flag.ErrHelp` before cobra evaluates `Args`, so
  `cobra.NoArgs` is silently inert on a group, and cobra's own unknown-command
  report is produced for the root command only. A group that wires nothing answers
  `my-tool deploy canry` with help and exit `0`.

- `cmd := setup.Wrap(...)` assigns the **composed** type to `cmd` from the start. Every later mutation — `cmd.Flags()`, `cmd.MarkFlagsMutuallyExclusive(...)`, `cmd.Register(child)` — flows through the embedded `*cobra.Command`.
- The parent does **not** thread a feature key into `Register`. Each child owns its own feature via its own `setup.Wrap` call — siblings can have completely different middleware stacks without the parent knowing.

### 3. Attach `deploy` to the root

```go
// pkg/cmd/root/cmd.go
rootCmd := gtbRoot.NewCmdRoot(p,
    deploy.NewCmdDeploy(p),
)
```

`root.NewCmdRoot(p, children...)` takes a variadic of `*setup.Command` and calls `Register` internally, so the variadic form and the explicit `rootCmd.Register(child)` form are equivalent.

---

## Subcommand `PersistentPreRunE` and the framework bootstrap

A nested command may define its own `PersistentPreRunE` for environmental setup. This does **not** disable the framework bootstrap (config load, telemetry, update check) that runs in the root command's hook. `NewCmdRoot` enables `cobra.EnableTraverseRunHooks`, so cobra runs every `PersistentPreRunE` from root to leaf:

1. The framework bootstrap (root hook) runs **first**.
2. Your subcommand's `PersistentPreRunE` runs **after** it.

So a hook on `deploy` or `canary` can rely on `props.Config`, `props.Collector`, and the resolved log level already being populated — no need to re-run any framework setup yourself. See [Command Middleware System → Hooks vs. the framework bootstrap](../explanation/components/setup/middleware.md#hooks-vs-the-framework-bootstrap) for the full ordering guarantee.

---

## Verifying the tree compiles and runs

```bash
go build ./...
./my-tool --help
# ...
# Available Commands:
#   deploy      Deploy stuff
#   ...

./my-tool deploy --help
# Available Commands:
#   canary      Canary deploy

./my-tool deploy canary --help
# Usage:
#   my-tool deploy canary [flags]
```

If the build fails with `cmd.Register undefined (type *cobra.Command has no field or method Register)`, the parent's `cmd` variable was typed as `*cobra.Command` — likely from an older template. Re-run `gtb regenerate` or update the constructor to use `cmd := setup.Wrap(...)` instead of `cmd := &cobra.Command{...}`.

---

## What the generator does that you should not skip

The generator's emission is **idempotent**: re-running `gtb generate command --name canary --parent deploy` does not double-register the child, because the AST inserter detects both `cmd.Register(...)` and legacy `setup.AddCommandWithMiddleware(...)` shapes and short-circuits. If you wire commands by hand you don't get this for free — adding the same child twice will compile but produce a runtime "command already added" panic from cobra.

If you mix generated and hand-written commands in the same project:

- Keep hand-written commands in packages the generator does not own (anything not listed in `.gtb/manifest.yaml`).
- For generated parents, prefer `gtb generate command --parent <name>` to add children — your hand edits inside a generated file will survive only if they sit outside the regions the generator rewrites.

---

## See also

- [Adding Custom Commands](custom-commands.md) — full how-to for the basic single-command pattern.
- [Command Constructor Pattern](../explanation/concepts/functional-options.md) — rationale for `NewCmd*` returning `*setup.Command`.
- [Command Middleware System](../explanation/components/setup/middleware.md) — how `Register` wraps `RunE` with middleware.
- [`gtb generate command`](../explanation/components/internal/commands/generate.md) — flag reference for the generator.
- [Migration v0.4 to v0.5](../reference/migration/v0.4-to-v0.5.md) — diff against the previous `AddCommandWithMiddleware` pattern.
