---
title: "Command Composition: setup.Command wrapper with Register"
description: "Replace setup.AddCommandWithMiddleware(parent, cmd, feature) with a composed setup.Command type that carries its own feature and exposes a Register method, so subcommand registration wires middleware automatically and the generator never has to thread a feature key through each call. Fixes the nested-command compile regression by construction."
date: 2026-05-30
status: APPROVED
tags:
  - specification
  - generator
  - setup
  - middleware
  - cobra
  - breaking-change
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-opus-4-8)
    role: AI drafting assistant
---

# Command Composition: setup.Command wrapper with Register

Authors
:   Matt Cockayne, Claude (claude-opus-4-8) *(AI drafting assistant)*

Date
:   30 May 2026

Status
:   APPROVED

---

## Overview

Subcommand registration in a generated tool currently goes through a free
function, `setup.AddCommandWithMiddleware(parent, cmd, feature)`, whose third
argument is a `props.FeatureCmd` used only to look up which middleware should
wrap the command. The generator has to produce that feature key at every
registration site, and it produces the wrong thing for user commands, which
causes a generated project with nested commands not to compile.

This spec proposes composing `*cobra.Command` into a small `setup.Command` type
that carries its own `Feature`, and adding a `Register` method that wires
middleware from each child's own feature. The generated code becomes
self-describing (`setup.Wrap(props.FeatureCmd("child"), cmd)`) and registration
becomes a clean method call (`parent.Register(child.NewCmdChild(p))`), with no
feature key threaded through the call site. The
[nested-command regression](#the-regression) is fixed by construction.

This is a **breaking change** to the generated command surface (`NewCmd<Name>`
return types) and therefore targets **v0.5.0**. Because gtb regenerates all of
that code, the migration is "run `gtb regenerate`", not a hand-edit.

---

## Background

### How middleware is wired today

`setup.AddCommandWithMiddleware` (`pkg/setup/middleware.go`) wraps a command's
`RunE` with `Chain(feature, runE)`, where `Chain` composes global middleware
plus `featureMiddleware[feature]` (middleware registered for that feature via
`RegisterMiddleware`). The `feature` argument is therefore a **middleware lookup
key**, not an enable/disable gate.

```go
func AddCommandWithMiddleware(parent, cmd *cobra.Command, feature props.FeatureCmd) {
	if cmd.RunE != nil {
		cmd.RunE = Chain(feature, cmd.RunE)
	}
	for _, sub := range cmd.Commands() {
		ApplyMiddlewareRecursively(sub, feature)
	}
	parent.AddCommand(cmd)
}
```

The framework's own root registers built-ins with this function, passing either
a built-in feature constant or an empty feature:

```go
// pkg/cmd/root/root.go
setup.AddCommandWithMiddleware(rootCmd, version.NewCmdVersion(props), "")        // generic
setup.AddCommandWithMiddleware(rootCmd, update.NewCmdUpdate(props), p.UpdateCmd) // built-in feature
```

### The regression

Commit `8974154` ("feat(setup): implement command middleware system and
generator integration") changed the generator's nested-command registration
(`internal/generator/ast.go`, `createRegistrationStmts`) from:

```go
cmd.AddCommand(child.NewCmdChild(p))                                    // before — compiled
```

to:

```go
setup.AddCommandWithMiddleware(cmd, child.NewCmdChild(p), props.ChildCmd) // after — does not compile
```

`props.<Name>Cmd` constants (`UpdateCmd`, `DocsCmd`, …) are **hand-declared in
`pkg/props/tool.go` for built-in features only**. The generator never creates a
`props.ChildCmd` for a user command, so the generated parent `cmd.go` references
an undefined symbol:

```
pkg/cmd/hello/cmd.go: props.ChildCmd undefined (type *props.Props has no field or method ChildCmd)
```

The bug only triggers for **nested** commands (a `--parent` that is itself a
user command), where the registration uses the wrapping path. Top-level commands
(`--parent root`) register directly inside `NewCmdRoot` and are unaffected. The
generator's integration tests assert on generated file *content*, never compile
the output, so CI stays green.

The established idiom elsewhere in the *same* generator is
`props.FeatureCmd(name)` — e.g. the generated `init.go` already does
`setup.Register(props.FeatureCmd("child"), …)`. The nested-registration path is
the lone deviation that assumed a constant.

---

## Proposed design

### `setup.Command`

A new type in `pkg/setup` composes the cobra command with its feature.

```go
// pkg/setup/command.go
package setup

// Command composes *cobra.Command with the feature it belongs to, so that
// registering a subcommand also wires up its middleware.
type Command struct {
	*cobra.Command
	Feature props.FeatureCmd
}

// Wrap pairs a cobra command with its feature.
func Wrap(feature props.FeatureCmd, cmd *cobra.Command) *Command {
	return &Command{Command: cmd, Feature: feature}
}

// Register adds child commands to this command, wiring each child's middleware
// from the child's own feature. A child's descendants are wired when the child
// registers them, so Register only wraps the immediate child's RunE — there is
// no recursive re-wrapping.
func (c *Command) Register(children ...*Command) {
	for _, child := range children {
		if child.RunE != nil {
			child.RunE = Chain(child.Feature, child.RunE)
		}
		c.AddCommand(child.Command)
	}
}
```

`*Command` embeds `*cobra.Command`, so it is usable anywhere a cobra command's
methods are needed; code that needs the raw `*cobra.Command` (e.g. to pass to a
cobra API) uses `.Command`.

### Generated code, before and after

Command file (`pkg/cmd/child/cmd.go`):

```go
// before
func NewCmdChild(props *props.Props) *cobra.Command {
	cmd := &cobra.Command{Use: "child", RunE: ...}
	return cmd
}

// after — the command owns its feature identity
func NewCmdChild(props *props.Props) *setup.Command {
	cmd := &cobra.Command{Use: "child", RunE: ...}
	return setup.Wrap(props.FeatureCmd("child"), cmd)
}
```

Parent registration (`pkg/cmd/parent/cmd.go`):

```go
// before (the buggy wrapping path)
setup.AddCommandWithMiddleware(cmd, child.NewCmdChild(p), props.ChildCmd)

// after — clean, no feature key at the call site
parent.Register(child.NewCmdChild(p))
```

Root (`pkg/cmd/root/cmd.go`, generated) registers its top-level commands the same
way, e.g. `rootCmd.Register(hello.NewCmdHello(p), greet.NewCmdGreet(p))`.

---

## Affected components and implementation plan

1. **`pkg/setup/command.go` (new):** the `Command` type, `Wrap`, `Register`.
   Reuse the existing `Chain` from `pkg/setup/middleware.go`.
2. **`pkg/cmd/root` (`root.go`):** `NewCmdRoot` returns `*setup.Command`;
   `Execute` accepts `*setup.Command` (or keeps `*cobra.Command` and callers pass
   `.Command`). Built-in registrations migrate from
   `AddCommandWithMiddleware(rootCmd, …, feature)` to the `Register` method, with
   built-in features still supplied via `setup.Wrap(p.UpdateCmd, …)` at each
   built-in command's construction (or an explicit feature on `Wrap`).
3. **Generator templates:**
   - `internal/generator/templates/command.go` (`CommandRegistration`):
     `NewCmd<Name>` returns `*setup.Command`, wrapping with
     `setup.Wrap(props.FeatureCmd("<name>"), cmd)`.
   - `internal/generator/templates/skeleton_root.go`: `NewCmdRoot` returns
     `*setup.Command`; root registration uses `.Register(...)`.
   - `internal/generator/ast.go` (`createRegistrationStmts` /
     `insertIntoRoot`): emit `<parent>.Register(<pkg>.NewCmd<Name>(p))` instead
     of `setup.AddCommandWithMiddleware(...)` / direct `AddCommand`. This removes
     the `props.<Name>Cmd` selector entirely and makes the
     [unused-`setup`-import fix](2026-05-29-config-generic-validation.md) moot for
     this path (the parent now references `setup.Command`/`Register`, which is a
     real use).
   - Generated `main.go`: `gtbRoot.Execute(rootCmd, p)` continues to work; adjust
     for the new root type as needed (`.Command` if `Execute` stays
     `*cobra.Command`).
4. **`AddCommandWithMiddleware`:** keep it (now implemented in terms of the same
   `Chain`) for backward compatibility and for any non-generated callers, or
   deprecate it. See open questions.

---

## Middleware semantics

- Each command's `RunE` is wrapped exactly once, with **its own** feature, at the
  point its parent calls `Register`. Because each command registers its own
  children inside its `NewCmd<Name>`, the whole tree is wired by composition and
  no command is wrapped twice. This is cleaner than the current
  `ApplyMiddlewareRecursively`, which re-applies the *parent's* feature down the
  subtree.
- A user command's feature (`props.FeatureCmd("child")`) has no registered
  feature-specific middleware unless the author adds some via
  `RegisterMiddleware(props.FeatureCmd("child"), …)`, in which case it now has a
  natural, stable key to target. Global middleware always applies.
- Behaviour for built-in features is unchanged as long as their `Wrap` uses the
  same `p.<Feature>Cmd` constant the root passes today.

---

## Migration and compatibility

- **Breaking:** `NewCmd<Name>` and `NewCmdRoot` change return type from
  `*cobra.Command` to `*setup.Command`. This ripples to `Execute` and generated
  `main.go`.
- **Migration is regeneration.** All affected code is generated; `gtb regenerate`
  rewrites it. Hand-written code that called `NewCmd<Name>` expecting a
  `*cobra.Command` uses `.Command` (the embedded field).
- **Versioning:** target **v0.5.0** (pre-1.0, so a minor that requires
  regeneration is acceptable per the project's 0.x policy). Add an entry to
  `docs/migration/` describing the regenerate step.

---

## Out of scope

- Changing what middleware does, or the global/feature middleware registries.
- The enable/disable feature-flag system (`props.Tool` features) — `Feature`
  here is only a middleware key.
- Converting `AddCommandWithMiddleware` callers outside the generator (decide in
  open questions).

---

## Alternative considered: cobra Annotations (non-breaking)

Instead of changing return types, store the feature in cobra's built-in
`Annotations map[string]string` and provide a free
`setup.Register(parent *cobra.Command, children ...*cobra.Command)` that reads
`child.Annotations[FeatureKey]`. `NewCmd<Name>` keeps returning `*cobra.Command`
(non-breaking), the feature stays co-located, and the call site stays clean. The
cost is a stringly-typed metadata channel and slightly more implicit wiring.
Chosen against in favour of the typed `setup.Command` for clarity, but it is the
fallback if a breaking change is undesirable before v1.0.

---

## Resolutions

The originally-listed open questions were resolved during spec review
(2026-05-30):

1. **`Wrap` signature.** `func Wrap(feature props.FeatureCmd, cmd *cobra.Command) *Command` — feature first (the lookup-key-first convention) and the name *Wrap* reads cleanly at call sites.
2. **`AddCommandWithMiddleware`.** Kept as a **thin deprecated shim** that delegates to `Register`, with a `// Deprecated:` marker pointing at `Command.Register`. Removal targeted at v1.0; protects any downstream callers.
3. **`Execute` signature.** Changes to accept `*setup.Command`. Generated `main.go` passes the typed root directly. Keeps the migration consistent end to end.
4. **Regression coverage.** A gated generator integration test scaffolds a project with a **nested** command and runs `go build` of the generated module. This is the bug-class catcher the file-content-only tests missed; it lands alongside the generator changes.

The interim stopgap (a v0.4.2 one-line generator fix) is **not pursued** — the redesign supersedes it.

---

## Test plan

- Unit: `setup.Command.Register` wraps the immediate child's `RunE` with the
  child's feature; does not double-wrap descendants; `AddCommand` is called.
- Generator: generated `NewCmd<Name>` returns `*setup.Command` and wraps with
  `setup.Wrap(props.FeatureCmd("<name>"), …)`; parent/root use `.Register(...)`.
- Integration (the gap that let the regression through): scaffold a project, add
  a **nested** command, and **`go build`** it — it must compile. Extend
  `TestFullLifecycle` / add an e2e that builds the generated module.
