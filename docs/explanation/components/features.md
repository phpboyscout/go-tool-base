---
title: Features
description: >-
  The feature core: what a binary offers, declared at init into a registry; what a tool has chosen,
  resolved once into a set; and how a request asks, through an evaluator. Interfaces with one
  default each, no GTB import, shaped to leave for go/features.
---

# Features

The feature core is the extracted
[`gitlab.com/phpboyscout/go/features`](https://features.go.phpboyscout.uk) module
(API on [pkg.go.dev](https://pkg.go.dev/gitlab.com/phpboyscout/go/features)),
which `props` and `setup` build on
(spec [0199](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0199-features-as-a-value-and-a-root-that-owns-its-registries)).
This page is GTB's side: how the framework uses the module and what it adds.
The module's own documentation covers the core in full; the summary below is
the map, not the territory.

The core answers three questions, each through an interface with one default
implementation, and imports nothing of GTB.

| Question | Role | Default |
|---|---|---|
| What does this binary offer? | `Registry` (declare, contribute, snapshot) | `NewRegistry()`; `Default()` is the init-time instance |
| What was offered when the root was built? | `Snapshot` (immutable, ordered) | taken by `Registry.Snapshot()` |
| What has the tool chosen? | `Resolver` → `Set` | `DefaultResolver()`: defaults, then the tool's states |
| Is it on for this request? | `Evaluator` | a `Set` (static); `Dynamic` over a `Backend` in a later phase |

## Declaration is init-time; everything else is a value

A blank import runs `init`, and `init` can only reach package state, so a
package declares its feature and its contributions on `features.Default()`.
That is the one process-wide thing here. Everything from `main` on works with
values: a root takes a `Snapshot`, resolves a `Set`, and hands the set down.

Nothing seals. A snapshot is an immutable copy, so a registration made after it
is not in it and a fresh snapshot sees it; the panics the earlier seals raised
are gone with them (D1). A test that needs a feature, an initialiser or a
middleware of its own builds `NewRegistry()` and never writes to the default
(D5); `internal/repopolicy` enforces that.

## Descriptors, kinds and order

A `Descriptor` is an interface (`FeatureID`, `FeatureKind`, `DefaultOn`,
`IsDynamic`) so a consumer carries its own facts beside it: GTB's
`props.FeatureDescriptor` adds the generated-code identifiers the generator
emits. `Kind` classifies so "every forge" is a query: `builtin` (the framework's
commands), `forge` (a forge integration a blank import contributes) and `link`
(a feature whose only effect is a blank import, the OS keychain; its presence
is its enablement, it declares no runtime default, and the generator toggles it
by writing or removing the file that imports it). A snapshot's order is derived
from data, never from `init` sequencing: descriptors that report a `Rank`
first (GTB's built-ins in their declared order, then any plugin that sets an
`Order`, which the forges do so a chooser lists GitHub first), then the rest by
kind and ID.

## Contributions and slots

A contribution is a value registered under a feature ID and a `Slot` name. The
core knows a slot is a name and a value; `setup` defines GTB's slots
(`SlotInitialiser`, `SlotSubcommand`, `SlotInitFlag`, `SlotCheck`,
`SlotAssets`, `SlotMiddleware`) and asserts the types on the way out.
`features.Global` is the ID for a contribution that applies to every feature,
which is how global middleware is registered. A `Set` hands out only the
contributions of enabled features; `ContributionsOf[T]` does the typed read,
and that is how `init`, `doctor` and the root find their initialisers,
checks, asset bundles and middleware, and how `doctor` finds the credentials
of enabled features (`credentialposture.SlotCredential`, read through
`credentialposture.DeclaredFor(set)`). The generator's catalogue is another
reader: `templates.Catalogue()` is the snapshot narrowed to the scaffoldable
kinds, so there is no second table of features to keep in step.

## The set on Props, and the root that owns the rest

`props.New` snapshots the default registry and resolves `Props.Features`
from `Tool.Features`; `p.GetFeatures()` is the nil-safe read (a literal
`Props` resolves on first read). Every static decision asks it:
`p.GetFeatures().Enabled(props.AiCmd)`. `Props.Flags` is the request-time
`Evaluator`, defaulting to the set. The root builds its own middleware chain
(`setup.MiddlewareChain`) from the set and hands it down the tree, and the
init command binds each enabled feature's flags on itself and hands the run's
flags to the initialiser providers, so two roots in one process share nothing
(D3). `root.WithRegistry`, `WithResolver` and `WithChain`, and
`props.WithFeatures`, `WithSet` and `WithFlags`, are the doors through which a
test or a consumer replaces any of those parts (D12).

## Resolving a set

`Resolve(snapshot, states)` applies every descriptor's default, then the
tool's states in order. Enabling an ID nothing declared is an error
(`ErrUnknownFeature`); disabling one is ignored and listed by `Set.Ignored()`
so `doctor` can report it (OQ2: you can switch off what you did not link, you
cannot switch on what you did not link).

## Static and dynamic

Anything that shapes the command tree, `--help`, completion, `doctor` or a
generated project is **static** and reads `Set.Enabled`, fixed at construction.
A descriptor reporting `IsDynamic()` may be overridden at evaluation time by a
`Backend`; the built-ins never are.

The dynamic half is three layers (D10). `Evaluator` is what a service holds
(`Evaluate(ctx, id, EvalContext) (Decision, error)`; `Decision` carries
`Enabled`, a `Reason` and a `Variant`). `Backend` is what a vendor adapter
implements: `Resolve(ctx, id, fallback, ec)`, where the fallback is the caller's
static state and is what the backend returns for a flag it does not know or has
disabled (OpenFeature's `defaultValue`), plus `Init`, `Ready`, `Watch` and
`Close`. `features.Dynamic(set, backend, opts...)` is the one production
evaluator: static-only features never reach the backend; a dynamic one is
resolved with the `Set` state as fallback; `disabled` is a clean answer that
applies it; any error, a not-ready backend or an unknown flag answers the `Set`
state with `ReasonFallback` and the error alongside. `WithInitTimeout` is how a
short-lived process bounds a backend's start; `WithOnFallback` is where a
service logs. `features.SetBackend(set)` is the vendor-free backend.

GTB's own backend is `pkg/setup/flags.NewConfigBackend(store)`, which reads
`features.<id>.enabled` from the tool's config store and forwards every reload
on `Watch`; `flags.RegisterBackend(controller, name, backend)` gives any
backend the controller's lifecycle. A vendor is an adapter module that
implements `Backend` (D11). See the
[dynamic feature flags how-to](../../how-to/dynamic-feature-flags.md).
