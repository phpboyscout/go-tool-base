---
title: "Flag-to-config binding — implement the documented flags precedence layer"
description: "The documented configuration precedence puts CLI flags above env vars and file config, but there is no BindPFlag anywhere and no binding facility — built-ins like --debug are special-cased and arbitrary flags never reach Config. Add a WithBoundFlags binding hook applied during config load, and reconcile the two docs that contradict each other on whether flags beat env vars."
status: APPROVED
date: 2026-06-12
tags:
  - specification
  - config
  - cmd
  - flags
  - dx
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude (claude-fable-5)
    role: AI drafting assistant
---

# Flag-to-config binding

Authors
:   Matt Cockayne, Claude (claude-fable-5) *(AI drafting assistant)*

Date
:   2026-06-12

Status
:   APPROVED (open questions resolved in review 2026-06-12)

## Summary

GTB documents a configuration precedence of *CLI flags > env vars > file config > embedded assets > defaults*. The flags tier is **not implemented**: there is no `viper.BindPFlag` anywhere, no binding helper, and built-ins like `--debug` are special-cased in `configureLogging` rather than flowing through `Config`. An arbitrary downstream flag therefore never overrides config. This spec adds a real binding facility so the documented precedence holds, and reconciles two docs that currently disagree on whether flags beat env vars.

Finding addressed (from `docs/development/reports/codebase-audit-2026-06-12.md` §3.6):

- `flag-to-config-binding-unimplemented` — **Medium, missing-feature**

## Motivation

Downstream authors reasonably expect `mytool --port 9090` to override `port: 8080` in config, because the docs promise flags win. Today it silently does not, unless they hand-wire `BindPFlag` themselves. The special-casing of `--debug`/`--ci` is evidence of the missing general mechanism. Beyond the bug, two docs contradict each other on the flag-vs-env ordering — implementation forces us to pick one and document it precisely.

## Design decisions

### D1 — `WithBoundFlags` option

Add a `props`/root option that registers a set of pflags (or a whole `*pflag.FlagSet`) to be `BindPFlag`-ed onto the config container during config load, so `Config.Get*` reflects flag overrides at the documented precedence. Shape (illustrative, to be finalised):

```go
root.NewCmdRoot(props, root.WithBoundFlags(map[string]*pflag.Flag{
    "server.port": portFlag,
}))
```

The explicit map is the precise default. Additionally offer an **optional convention helper** that derives keys from flag names (`--server-port` ⇒ `server.port`) for authors who want zero-boilerplate binding (resolved O1).

### D2 — Bind during config load, after file/env; only changed flags

Binding happens at the point config is assembled (after file + env layers are established) so viper's precedence (`BindPFlag` sits above env) yields the documented result. **Only flags the user explicitly set are bound** (`flag.Changed`) (resolved O3): a flag left at its default must not override config — binding defaults is viper's classic clobber footgun, which `flag.Changed` filtering neutralises. (O2: viper's documented order already matches the desired precedence, so no custom precedence implementation is needed.)

### D3 — Fold the special-cased built-ins through the same path

`--debug`, `--ci` are currently read directly. Where it simplifies without changing behaviour, route them through the binding so there is one mechanism, not two. (Keep `--debug`'s immediate log-level effect; only the config-visibility changes.)

### D4 — Reconcile the docs

Pick the canonical precedence (recommend: flags > env > file > embedded > defaults, matching viper's `Set`/`BindPFlag`/`AutomaticEnv` order) and make `docs/concepts/config.md` and any conflicting how-to agree. This is a required deliverable, not optional.

### D5 — Bind per-command flags too (resolved O4)

Bind **both** root/persistent flags and **per-command** flags. A command's own flags bind into a command-scoped config view (the `Config` visible to that command's `RunE`), so e.g. `mytool serve --port 9090` overrides `server.port` for the `serve` command. Implementation: bind a command's flag set when that command runs (in the bootstrap path, which — per the bootstrap-prerun spec — now runs for every command), filtering by `flag.Changed` as in D2. Persistent/root flags bind once at the root; per-command flags bind per dispatch.

## Open questions

*All resolved during review (2026-06-12).*

1. **O1 — Binding API.** *Resolved:* **explicit `WithBoundFlags(map[string]*pflag.Flag)`** as the precise default, **plus an optional convention helper** that derives keys from flag names (`--server-port` → `server.port`) for ergonomics. (D1)
2. **O2 — viper precedence.** *Resolved:* viper's documented order already places `BindPFlag` above `AutomaticEnv` (flag > env > config), so no custom precedence is needed — the real subtlety is the default-clobber gotcha, handled by O3.
3. **O3 — changed vs default flags.** *Resolved:* bind **only flags the user explicitly set** (`flag.Changed`). A flag at its default must not override config — this neutralises viper's default-clobber behaviour. (D2)
4. **O4 — Scope.** *Resolved:* bind **both root/persistent and per-command flags** (per-command into a command-scoped config view), not root-only. (D5)

## Verification plan

1. **Unit** — with a bound flag set explicitly, `Config.GetInt("server.port")` returns the flag value; unset flag does not override config (O3).
2. **Precedence** — flag beats env beats file, proven with all three set to different values.
3. **Built-ins** — `--debug` still sets the log level; `--ci`/config `ci` behaviour unchanged.
4. **Docs** — the two precedence docs now agree and match the implementation; add a how-to.

## Out of scope

- Per-command flag binding (follow-up, O4).
- Changing the env-var prefix mechanism (`EnvPrefix` stays as-is).

## Related

- [2026-06-12 codebase audit](../reports/codebase-audit-2026-06-12.md) §3.6
- [Plan 3 — improvements](../plans/2026-06-12-audit-plan-3-improvements.md) Phase 16
- `docs/concepts/config.md`, `docs/components/config.md`
- [config env-prefix spec](2026-04-02-config-env-prefix.md)
