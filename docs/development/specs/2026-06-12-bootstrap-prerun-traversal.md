---
title: "Framework bootstrap vs child PersistentPreRunE — stop downstream hooks silently disabling setup"
description: "Cobra runs only the closest PersistentPreRunE, so a downstream subcommand that defines one silently shadows the root bootstrap (config load, telemetry, update check). Make the framework bootstrap run regardless — via EnableTraverseRunHooks or by moving bootstrap into Execute/outermost middleware — and warn when a downstream hook would shadow it."
status: IMPLEMENTED
date: 2026-06-12
tags:
  - specification
  - cmd
  - middleware
  - cobra
  - dx
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude (claude-fable-5)
    role: AI drafting assistant
---

# Framework bootstrap vs child `PersistentPreRunE`

Authors
:   Matt Cockayne, Claude (claude-fable-5) *(AI drafting assistant)*

Date
:   2026-06-12

Status
:   IMPLEMENTED (open questions resolved in review 2026-06-12; implemented 2026-06-13)

## Summary

The framework's bootstrap (config loading, log-level setup, feature-flag resolution, telemetry collector construction, update check) runs in the root command's `PersistentPreRunE`. Cobra executes only the **closest** `PersistentPreRunE` in the command chain — so the moment a downstream tool defines a `PersistentPreRunE` on a subcommand, the root bootstrap is **silently skipped** for that subtree. The docs steer authors straight into this. This spec makes the framework bootstrap run regardless and warns authors when their hook would otherwise shadow it.

Finding addressed (from `docs/development/reports/codebase-audit-2026-06-12.md` §3.6):

- `child-persistentprerune-silently-disables-framework-setup` — **Medium, missing-feature**

## Motivation

This is a silent, surprising footgun in the framework's core contract. A downstream author adds a subcommand-level `PersistentPreRunE` (a perfectly normal cobra pattern) and, with no error or warning, loses config loading, telemetry, and the update check for that subtree. The failure mode is invisible until something that depends on bootstrap (e.g. `props.Config`) is nil or stale at runtime.

## Design decisions

### D1 — Enable `cobra.EnableTraverseRunHooks` (resolved O1)

Set `cobra.EnableTraverseRunHooks = true` in `NewCmdRoot`. Cobra (v1.10.2, confirmed present) then runs **all** `PersistentPreRunE` hooks from root to leaf rather than only the closest, so the root bootstrap always runs and a downstream child hook runs *after* it (resolved O2: bootstrap always first). Verified during review that the flag is **not currently set anywhere** in the codebase — the bug is live and unmitigated.

This is chosen over moving bootstrap out of `PersistentPreRunE` (see Alternative considered) because it is a one-liner cobra fully supports, and crucially it **keeps bootstrap in `PersistentPreRunE` where cobra has already resolved `--debug`/`--config`/`--ci`** — avoiding the early-flag-resolution complexity the alternative would incur. The "process-global var" caveat is negligible for GTB: only the root command defines a persistent pre-run hook, so root→leaf traversal changes nothing for the framework's own tree.

### D2 — Bootstrap always runs; no opt-out (resolved O4)

Bootstrap runs for every command — there is no bootstrap-exempt annotation. That is the whole point of the fix (bootstrap must never be silently skipped); an "offline" subcommand simply doesn't use the parts of `Props` it doesn't need. Adding an opt-out would reintroduce the footgun.

### D3 — Warn on shadowing; document the ordering

When constructing the command tree, detect a downstream `PersistentPreRunE` and emit a one-time debug log noting that it runs *after* the framework bootstrap (root→leaf), so authors understand the order rather than being surprised. Update the docs/how-to that currently steer authors into the trap, documenting the guaranteed bootstrap-then-child ordering.

## Alternative considered — bootstrap in `Execute` / outermost middleware

Run bootstrap from the `Execute` wrapper (or as the outermost registered middleware) *before* cobra dispatches to any `PersistentPreRunE`. More durable (no dependency on cobra hook resolution) and would unify with the sealed middleware registry — but a real refactor: bootstrap currently reads cobra-resolved flags during `PersistentPreRunE`, so this requires resolving the persistent flag set early and special-casing `--help`/completion so they don't trigger bootstrap. Rejected as disproportionate for a pre-1.0 framework; revisit only if the middleware unification becomes desirable on its own merits (see the deferred O3 note).

## Open questions

*O1, O2, O4 resolved during review (2026-06-12); O3 deferred.*

1. **O1 — Approach.** *Resolved:* **Option A** — `cobra.EnableTraverseRunHooks = true`. Confirmed not currently set; cobra v1.10.2 supports it. (D1)
2. **O2 — Ordering guarantee.** *Resolved:* **bootstrap always first**, child hooks after (root→leaf). (D1)
3. **O3 — Middleware unification (deferred).** Whether bootstrap should ultimately *be* the outermost middleware (unifying with `command-composition-registration`, 2026-05-30) rather than a `PersistentPreRunE` is a separate architectural question, not needed for this fix. Tracked as a future consideration, not a blocker.
4. **O4 — Opt-out.** *Resolved:* **no opt-out** — bootstrap always runs. (D2)

## Verification plan

1. **Unit** — a command tree with a child `PersistentPreRunE` still has `props.Config` / `props.Collector` populated when the child runs.
2. **Regression** — the scenario the audit describes: define a subcommand hook, assert bootstrap still ran.
3. **Ordering** — assert the documented hook order.
4. **E2E** — a scaffolded tool with a custom subcommand hook resolves config correctly.
5. **Docs** — fix the how-to that steers authors into the trap; document the guaranteed ordering.

## Out of scope

- Rewriting the middleware registry (referenced, not replaced).
- Per-command bootstrap opt-out beyond what O4 resolves.

## Related

- [2026-06-12 codebase audit](../reports/codebase-audit-2026-06-12.md) §3.6
- [Plan 3 — improvements](../plans/2026-06-12-audit-plan-3-improvements.md) Phase 16
- [command composition registration spec](2026-05-30-command-composition-registration.md)
- `docs/concepts/command-middleware.md`, `docs/how-to/nested-subcommands.md`
