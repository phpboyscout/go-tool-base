---
title: Add Doctor Check
description: Step-by-step guide to registering custom diagnostic checks with the doctor command.
date: 2026-03-23
tags: [how-to, doctor, diagnostics, setup]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

Register custom diagnostic checks so `doctor` validates your feature's health alongside the built-in checks.

!!! important
    **Doctor checks are read-only diagnostics.**
    They must not modify state. Return a `setup.CheckResult` describing what was found.

## Step 1: Write your check functions

Each check receives a `context.Context` and `*props.Props` and returns a `setup.CheckResult`.

```go
package myfeature

import (
    "context"

    "gitlab.com/phpboyscout/go-tool-base/pkg/props"
    "gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func checkMyService(_ context.Context, props *props.Props) setup.CheckResult {
    endpoint := props.Config.GetString("myfeature.endpoint")
    if endpoint == "" {
        return setup.CheckResult{
            Name:    "My Service",
            Status:  "warn",
            Message: "endpoint not configured",
        }
    }

    // Perform a health check...
    return setup.CheckResult{
        Name:    "My Service",
        Status:  "pass",
        Message: "reachable at " + endpoint,
    }
}
```

## Step 2: Register checks via the feature registry

Use `setup.RegisterChecks` in your package's `init()` block. The `CheckProvider` function receives `*props.Props` and returns a slice of `setup.CheckFunc`, allowing you to conditionally include checks based on configuration.

```go
package myfeature

import (
    "gitlab.com/phpboyscout/go-tool-base/pkg/props"
    "gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func init() {
    setup.RegisterChecks(props.FeatureID("myfeature"),
        []setup.CheckProvider{
            func(p *props.Props) []setup.CheckFunc {
                return []setup.CheckFunc{
                    checkMyService,
                }
            },
        },
    )
}
```

## Step 3: Import your package

As with initialisers, ensure your package is imported somewhere in the dependency graph — typically via a blank import in your command package:

```go
package command

import (
    _ "github.com/myorg/mytool/pkg/setup/myfeature"
)
```

When the feature is enabled via `props.Tool.IsEnabled()`, the doctor command will automatically discover and run your checks.

## Combining with Initialisers

If your feature already has an initialiser, you can register checks in the same `init()` block:

```go
func init() {
    setup.Register(props.FeatureID("myfeature"),
        []setup.InitialiserProvider{...},
        []setup.SubcommandProvider{...},
        []setup.FeatureFlag{...},
    )

    setup.RegisterChecks(props.FeatureID("myfeature"),
        []setup.CheckProvider{
            func(p *props.Props) []setup.CheckFunc {
                return []setup.CheckFunc{checkMyService}
            },
        },
    )
}
```

## Status Constants

Use the following status strings in your `CheckResult`:

| Status   | Meaning                           |
|----------|-----------------------------------|
| `"pass"` | Check succeeded                   |
| `"warn"` | Non-fatal issue, feature may work |
| `"fail"` | Critical problem                  |
| `"skip"` | Check not applicable              |

**Your status decides whether the run fails.** `doctor` exits non-zero when a
check is as bad as the run's threshold, or worse:

| threshold | fails on | when it applies |
|---|---|---|
| `warn` | `warn`, `fail` | the default **under CI** |
| `fail` | `fail` | the default **interactively** |
| `none` | nothing | only when asked for |

```bash
gtb doctor                  # warn gates in CI; a failure gates anywhere
gtb doctor --fail-on=warn   # opt a local run in
gtb doctor --fail-on=none   # escape hatch for a pipeline not ready yet
```

`skip` never fails a run, whatever the threshold. A check that could not run has
not found a problem, and failing a pipeline because something was unavailable is
the fastest way to have the gate switched off. If your check cannot reach what
it needs, return `skip` and say why — never `pass`.

The report always prints in full before the exit code is decided, so a gated run
still shows what gated it.

---

!!! tip
    Look at the built-in checks in `pkg/cmd/doctor/checks.go` for reference implementations.
