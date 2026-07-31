---
title: Attach External Commands
description: How to attach a whole Cobra command tree from an external Go module onto a generated project's root, so it survives regeneration.
date: 2026-07-29
tags: [how-to, generator, commands, external-module, regeneration]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Attach External Commands

A gtb-generated CLI sometimes needs to expose a **whole command tree that lives
in a separate module** — commands you did not create with `gtb generate command`
and whose logic lives elsewhere. The canonical example is the `sign`/`keys` tree
from `gitlab.com/phpboyscout/go/signing-cli`.

The wrong way is to hand-edit the generated root (or `cmd/<tool>/main.go`) and
mark it `.gtb/ignore` — the customisation is either re-rendered away, or the file
stops receiving generator improvements. `gtb attach` makes external-command
attachment a **first-class, manifest-declared entity**: it is re-rendered into
the root on every `regenerate`, so it survives `gtb regenerate` and
`gtb enable signing`.

There are two channels: the **declarative** channel (the common case, zero code)
and the **adapter** escape hatch (for shapes the declarative form cannot
express).

## Declarative attachment

Attach one constructor at a time. Pass the module path with an explicit version
pin, the exported constructor, and the dependencies it takes from the props
container:

```bash
gtb attach command gitlab.com/phpboyscout/go/signing-cli@v0.1.0 \
  --constructor NewCmdSign --arg logger --wrap
```

This:

1. records the attachment in `.gtb/manifest.yaml` (under
   `properties.external_commands`);
2. re-renders `pkg/cmd/root/cmd.go` to wire the call in; and
3. pins the module in `go.mod` at the given version.

The rendered call for the example above is:

```go
setup.Wrap("", signingcli.NewCmdSign(p.GetLogger()))
```

Re-run the command for each additional constructor from the same module — a
second attach for the same module **appends** to it:

```bash
gtb attach command gitlab.com/phpboyscout/go/signing-cli@v0.1.0 \
  --constructor NewCmdKeys --arg logger --wrap
```

### The injection vocabulary

`--arg` names a dependency the generator derives from the props value `p`. It is
a **closed set** — this keeps the generated root type-safe and reviewable:

| `--arg` token | Rendered expression |
|---------------|---------------------|
| `logger`      | `p.GetLogger()`     |
| `props`       | `p`                 |
| `config`      | `p.Config`          |
| `fs`          | `p.FS`              |
| `version`     | `p.Version`         |

Pass `--arg` once per argument, in order. A constructor that takes no arguments
takes no `--arg`.

### `--wrap`: return type, not gating

Use `--wrap` when the constructor returns a `*cobra.Command` — it is wrapped in
`setup.Wrap("", …)` so it joins the framework's middleware pipeline. **Omit**
`--wrap` when the constructor already returns a `*setup.Command`; it is attached
directly. `--wrap` describes the return type only; declarative attachments are
always on (there is no feature gate — see [Gating](#gating-an-attachment)).

### Other flags

| Flag | Purpose |
|------|---------|
| `--import-path` | Package to import when it differs from the module path. |
| `--alias` | Import alias for the package in the generated root. |
| `--name` | Expected top-level command name, for best-effort collision detection. |
| `--path`, `-p` | Path to the project root (default `.`). |

## The adapter escape hatch

When a constructor's shape cannot be expressed with the closed vocabulary (extra
dependencies, custom wrapping, conditional assembly, or you want to gate it on
your own config), scaffold the adapter:

```bash
gtb attach adapter
```

This creates `pkg/cmd/external/attach.go` — **once** — and wires
`external.Commands(p)` into the root. The file is author-owned: gtb never
overwrites it, so it is safe to edit and survives `regenerate`. Fill in
`Commands`:

```go
package external

func Commands(p *props.Props) []*setup.Command {
    return []*setup.Command{
        setup.Wrap("", someexternal.NewCmdThing(p.GetLogger(), p.Config)),
    }
}
```

Because you own this file, you also own its imports (`go mod tidy` resolves them)
and any gating logic you want inside `Commands`.

## Listing and detaching

```bash
gtb attach list                                              # show all attachments
gtb detach command gitlab.com/phpboyscout/go/signing-cli     # remove one
```

`detach` drops the manifest entry and re-renders the root without the wiring;
`go mod tidy` then prunes the now-unused require from `go.mod`.

## Why it survives regeneration

The attachment lives in the manifest, and the generator re-renders the root from
the manifest on **every** operation that touches it — `regenerate project`,
`enable`/`disable` of any feature, and `enable signing`. There is no file to hold
`.gtb/ignore`, and nothing to clobber: the wiring is re-emitted, not preserved.
This is the exact failure the feature fixes — `gtb enable signing` used to
re-render the root and silently drop a hand-wired external command.

## Gating an attachment

Declarative attachments are always on. If you need a command that can be toggled,
use the adapter channel and gate it yourself inside `Commands` — for example on a
config key or an environment variable. First-class `enable`/`disable` integration
for external attachments is deliberately out of scope (it would open gtb's curated
feature catalogue to arbitrary downstream names); revisit it via a spec if a real
need arises.

## Related

- [Generate a New CLI Command](nested-subcommands.md) — for commands whose logic
  lives *in* this project.
- [Configure Generator Ignore Rules](configure-generator-ignore.md) — for
  holding non-command files hands-off.
- Spec: [`0182-external-command-attachment`](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0182-external-command-attachment).
