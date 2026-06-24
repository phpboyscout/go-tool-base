---
title: Adding Custom Commands
description: Implementing and registering custom commands manually using setup.Command, setup.Wrap, and Register.
date: 2026-05-31
tags: [how-to, commands, custom, implementation]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Adding Custom Commands

While the CLI generator handles most of the boilerplate, it's important to understand how to implement and register commands manually. GTB v0.5+ uses a composed `*setup.Command` type that carries its own middleware feature key — the steps below show the idiomatic shape for hand-written commands.

## 1. Implement the command

Create a new package for your command (e.g. `pkg/cmd/greet`). The constructor takes `*props.Props` and returns `*setup.Command`:

```go
package greet

import (
    "github.com/spf13/cobra"

    "gitlab.com/phpboyscout/go-tool-base/pkg/props"
    "gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func NewCmdGreet(p *props.Props) *setup.Command {
    cmd := setup.Wrap("greet", &cobra.Command{
        Use:   "greet [name]",
        Short: "Greets the user",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            p.Logger.Info("Hello, " + args[0])
            return nil
        },
    })

    return cmd
}
```

`setup.Wrap(feature, cobraCmd)` returns a `*setup.Command` that embeds `*cobra.Command`, so every cobra method (`cmd.Flags()`, `cmd.MarkFlagsMutuallyExclusive(...)`, `cmd.SetContext(...)`) keeps working through the embedded pointer. The `"greet"` literal is implicitly converted to `props.FeatureCmd` (a named string type) by Go.

If you don't need feature-specific middleware, pass the empty string: `setup.Wrap("", &cobra.Command{...})` — global middleware (timing, recovery, telemetry) still applies.

## 2. Register the command

In your `main.go`, pass the command to `root.NewCmdRoot` as a variadic argument:

```go
func main() {
    rootCmd, p := localroot.NewCmdRoot(version.Get())

    rootCmd.Register(greet.NewCmdGreet(p))

    root.Execute(rootCmd, p)
}
```

Both forms work and have the same effect — pick whichever reads better in your tree:

| Style | When to use |
|---|---|
| Pass as variadic to `NewCmdRoot(p, child1, child2, ...)` | The set of top-level commands is known at construction time. The generated skeleton uses this form. |
| Call `rootCmd.Register(child)` after construction | Adding plugins conditionally, or attaching commands from another package after `NewCmdRoot` runs. |

## 3. Add subcommands

Subcommands work the same way. Inside your parent's constructor, call `Register` on the parent before returning:

```go
func NewCmdDeploy(p *props.Props) *setup.Command {
    cmd := setup.Wrap("deploy", &cobra.Command{Use: "deploy"})

    cmd.Register(
        canary.NewCmdCanary(p),
        rollback.NewCmdRollback(p),
    )

    return cmd
}
```

Each child is wrapped exactly once with **its own** feature key. The parent's feature is never propagated downward — siblings can carry completely different middleware stacks.

## 4. Best practices

- **Use the logger**: Always use `p.Logger` instead of `fmt.Println`. This ensures your output respects global flags like `--debug` or `--output json`.
- **Use `RunE`, not `Run`**: Return errors from `RunE` so middleware (and `props.ErrorHandler`) can format them consistently.
- **Handle errors via `ErrorHandler`**: For fatal exits use `p.ErrorHandler.Fatal(err)` — it adds hints, support-channel suggestions, and stack traces when `--debug` is set.
- **Leverage config**: Use `p.Config` for any user-adjustable settings.
- **Wrap once**: `cmd := setup.Wrap(...)` at the top of the constructor — every later mutation operates on the composed type, so flag setup and subcommand registration all chain naturally.

## See also

- [Command Constructor Pattern](../explanation/concepts/command-constructors.md) — rationale for `NewCmd*` and `*setup.Command`.
- [Command Middleware System](../explanation/concepts/command-middleware.md) — how `Register` wires global and feature middleware.
- [`gtb generate command`](../explanation/components/internal/commands/generate.md) — the generator emits exactly this shape, so adding commands via the generator is the same as writing them by hand.
- [Migration from v0.4 to v0.5](../reference/migration/v0.4-to-v0.5.md) — diff vs. the old `*cobra.Command` + `AddCommandWithMiddleware` pattern.
