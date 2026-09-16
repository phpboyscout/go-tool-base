---
title: Dynamic feature flags
description: >-
  Declare a feature a backend may override at request time, wire the evaluator into a service with the
  tool's own config store as the first backend, flip it live, and know where a vendor adapter plugs in.
---

# Dynamic feature flags

GTB's features are **static** by default: which commands exist, what `doctor`
inventories and what a generated project scaffolds are fixed when the root is
built, from the tool's manifest and the binary's imports. That is what keeps
`--help` deterministic offline. A **dynamic** feature is one you have opted in
to overriding at request time, from a backend, for the code paths that read it
(spec [0199](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0199-features-as-a-value-and-a-root-that-owns-its-registries)
D7, D10, D11). Nothing dynamic ever shapes the command tree.

## 1. Declare the feature as dynamic

A feature opts in on its descriptor. Built-ins never do; yours can:

```go
func init() {
    props.RegisterFeature(props.FeatureDescriptor{
        ID:           "beta-ranking",
        ConstName:    "BetaRanking",
        ConstPackage: "example.com/mytool/pkg/features",
        Kind:         "mytool",
        Dynamic:      true, // a Backend may override the static state
    })
}
```

The manifest entry (`Enable("beta-ranking")` or its absence) is the feature's
**static state**, and it doubles as the **fallback**: what every evaluation
answers when the backend errors, is not ready, or does not know the flag.

## 2. Wire an evaluator into the service

`Props.Flags` is the request-time evaluator. It defaults to the static set, so a
service written against `p.GetFlags()` works before any backend exists. A
`features.Dynamic` over a backend replaces it:

```go
backend := flags.NewConfigBackend(p.Config) // the tool's own config store

p.Flags = features.Dynamic(p.GetFeatures(), backend,
    features.WithInitTimeout(2*time.Second),
    features.WithOnFallback(func(id features.ID, err error) {
        p.Logger.Warn("flag fell back to the manifest", "feature", id, "error", err)
    }),
)

// The backend's Init and Close ride the controller's lifecycle.
flags.RegisterBackend(controller, "flags", backend)
```

A request then asks:

```go
dec, err := p.GetFlags().Evaluate(ctx, "beta-ranking", features.EvalContext{
    TargetingKey: userID,
    Attributes:   map[string]any{"tier": tier},
})
if dec.Enabled { /* ... */ }
```

`dec.Reason` says how the answer was reached: `static` (a static-only feature,
or the `Set` answered), `default` or `targeting` (the backend's rule),
`disabled` (the backend is not overriding this; your manifest state applies, no
error), `fallback` (the backend errored or was not ready; the error is returned
alongside), `error` (no such feature). A CLI, which is short-lived, gives the
backend a short `WithInitTimeout` and lives with fallbacks; a service gives it
none and registers it with the controller.

## 3. Flip it live with the config-store backend

`flags.NewConfigBackend(store)` reads `features.<id>.enabled` from the tool's
config store, taking a fresh view on every evaluation and forwarding every
store reload on `Watch`. With the root's hot reload wired, editing the config
file (or `mytool config set features.beta-ranking.enabled true`) changes the
next evaluation:

```yaml
features:
  beta-ranking:
    enabled: true
```

An absent key is "no override" (`disabled`, your manifest applies); a
non-boolean value is `flags.ErrNotABool` and a fallback. For many tools this
is all the dynamic flag they need.

## 4. Where a vendor plugs in

No vendor SDK is in `go-tool-base`. A backend is a module of its own that
implements `features.Backend` (`Resolve` with the fallback as input, `Init`,
`Ready`, `Watch`, `Close`); the first is planned as an OpenFeature adapter,
which opens every provider that speaks OpenFeature (LaunchDarkly, Unleash and
therefore GitLab Feature Flags, flagd). Swapping it in is the one line that
constructs the backend; nothing that calls `Evaluate` changes.

## Testing

`features.SetBackend(set)` is a `Backend` that answers statically, so a
`Dynamic` can be exercised with no vendor. Hand in your own `Backend` fake to
prove the fallback path: a `Ready()` that returns false makes every dynamic
evaluation a `fallback` with `features.ErrBackendNotReady`.
