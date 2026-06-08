---
title: "Functional Options for HTTP and gRPC Server Construction"
description: "Introduce a functional-options (variadic) pattern to pkg/http and pkg/grpc server constructors so consumers can run multiple independent servers in one process — overriding the listen port directly or pointing each server at its own config prefix, instead of being locked to the single hardcoded default."
date: 2026-06-08
status: IMPLEMENTED
tags:
  - specification
  - http
  - grpc
  - server
  - functional-options
  - configuration
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Functional Options for HTTP and gRPC Server Construction

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   8 June 2026

Status
:   IMPLEMENTED

> **Decisions ratified by Matt on 2026-06-08** (see [Resolved Decisions](#resolved-decisions)):
> HTTP + gRPC both in scope now; **separate `ServerOption` / `RegisterOption`
> types** (not a single alias); ship prefix + port + the adjacent option set;
> keep the permissive `:0` ephemeral-port default **but** log the real bound
> port (from the listener) rather than `:0`.

---

## Problem Statement

`pkg/http` and `pkg/grpc` each expose a public `NewServer` constructor (and a
companion `Start`) designed to make standing up a server fast. The convenience
came at the cost of flexibility: the construction path **hardcodes a single
config prefix**, so the listen port and TLS settings can only ever be read from
one fixed location.

For HTTP (`pkg/http/server.go`):

```go
const DefaultConfigPrefix = "server.http"

func NewServer(ctx context.Context, cfg config.Containable, handler http.Handler) (*http.Server, error) {
    return newServer(ctx, cfg, handler, DefaultConfigPrefix) // prefix is fixed
}
```

The unexported `newServer(..., prefix string)` already accepts a prefix, but the
only public way to reach it with a non-default prefix is via `Register(...)`'s
`WithConfigPrefix` option. A consumer who builds servers directly through
`NewServer` + `Start` (i.e. without going through the `controls`-integrated
`Register` path) is locked to `server.http`.

The same shape exists for gRPC (`pkg/grpc/server.go`), except the port/TLS
prefix is hardcoded in `Start` and `DialLocal` rather than in `NewServer`:

```go
const (
    ConfigKeyPort       = "server.grpc.port"
    ConfigKeyReflection = "server.grpc.reflection"
    ConfigTLSPrefix     = "server.grpc.tls"
)
```

**A customer needs to run two independent HTTP servers in the same
application** (e.g. a public API server and a separate internal/admin server on
a different port). Today there is no clean, supported way to do this through the
public constructors: both servers resolve the same `server.http.port`, so the
second server cannot be given its own port or config block without dropping down
to hand-rolling `*http.Server` and re-implementing the TLS/listener wiring that
`pkg/http` exists to provide.

## Goals & Non-Goals

### Goals

1. Add a **functional-options variadic** to the public HTTP server constructor
   (`NewServer`) and its `Start` companion so a consumer can:
   - point the server at a **custom config prefix** (e.g. `server.admin`); and
   - set the **listen port directly** in code, bypassing config lookup.
2. Allow **two or more independent HTTP servers** to coexist in one process,
   each with its own port/prefix, through the public API alone.
3. Apply the same treatment to `pkg/grpc` **where it warrants it** — primarily
   `Start`, `DialLocal`, and `Register`, which currently hardcode
   `server.grpc.*` (see [gRPC Analysis](#grpc-analysis)).
3a. When the permissive `:0` ephemeral-port default is used, `Start` must log
   the **actually bound** address (resolved from the listener), never the
   literal `:0`, for both HTTP and gRPC.
4. Preserve **100% backward compatibility**: every existing call site continues
   to compile and behave identically. Adding a trailing variadic parameter is
   source-compatible in Go.
5. Reconcile the new options with the **existing `RegisterOption`** pattern so
   there is one coherent option vocabulary, not two competing ones.

### Non-Goals

- No change to the `controls` lifecycle, health endpoints, or middleware/chain
  semantics.
- No new transport features (HTTP/3, unix sockets, etc.) — only the
  port/prefix/construction knobs the customer needs, plus a small, clearly
  useful adjacent set (timeouts, max header bytes, TLS config).
- No change to config precedence rules (`<prefix>.port` → `server.port`
  fallback) beyond letting the consumer choose the prefix.
- Not introducing a stateful "server profile" object or a wrapper type around
  `*http.Server`/`*grpc.Server` — the constructors keep returning the stdlib /
  grpc-go types.

## Background: current public surface

### pkg/http

| Symbol | Signature (today) | Prefix source |
|--------|-------------------|---------------|
| `NewServer` | `(ctx, cfg, handler) (*http.Server, error)` | hardcoded `DefaultConfigPrefix` |
| `Start` | `(cfg, logger, srv) controls.StartFunc` | hardcoded `DefaultConfigPrefix` (TLS only) |
| `Register` | `(ctx, id, controller, cfg, logger, handler, opts ...RegisterOption)` | `WithConfigPrefix` (default `server.http`) |
| `RegisterOption` | `func(*registerConfig)` | — |
| `WithConfigPrefix` / `WithMiddleware` / `WithMaxRequestBodyBytes` | `RegisterOption` | — |

Note the port is baked into `srv.Addr` at `newServer` time; `Start` re-derives
only the **TLS** prefix. So a fully independent second server needs a matching
prefix supplied to **both** `NewServer` and `Start` (or to go through
`Register`, which threads one prefix to both internally).

### pkg/grpc

| Symbol | Signature (today) | Prefix source |
|--------|-------------------|---------------|
| `NewServer` | `(cfg, opt ...grpc.ServerOption) (*grpc.Server, error)` | reads `ConfigKeyReflection` only |
| `Start` | `(cfg, logger, srv) controls.StartFunc` | hardcoded `ConfigKeyPort` + `ConfigTLSPrefix` |
| `DialLocal` | `(cfg, opts ...grpc.DialOption) (*grpc.ClientConn, error)` | hardcoded `ConfigKeyPort` + `ConfigTLSPrefix` |
| `Register` | `(ctx, id, controller, cfg, logger, opts ...any)` | type-switches `RegisterOption` / `grpc.ServerOption` |
| `RegisterOption` | `func(*registerConfig)` | — |
| `WithInterceptors` | `RegisterOption` | — |

## Design

### HTTP: separate `ServerOption` and `RegisterOption`

Two distinct functional-option types (ratified decision). `ServerOption`
configures server **construction** (`NewServer`, `Start`); `RegisterOption`
configures **registration-only** behaviour (middleware chain, request-body
limit). `registerConfig` **embeds** `serverConfig`, so `Register` accepts both
families and threads the server knobs to `newServer`/`start`:

```go
// serverConfig carries construction settings consumed by NewServer / Start.
// Zero values mean "fall back to config / built-in default".
type serverConfig struct {
    prefix         string        // config prefix; default DefaultConfigPrefix
    port           *int          // explicit port override (nil = read from config)
    maxHeaderBytes int           // 0 = config / defaultMaxHeaderBytes
    readTimeout    time.Duration // 0 = built-in default
    writeTimeout   time.Duration
    idleTimeout    time.Duration
    tlsConfig      *tls.Config   // nil = gtbtls.DefaultConfig()
}

// registerConfig is the superset consumed by Register.
type registerConfig struct {
    serverConfig
    chain               *Chain
    maxRequestBodyBytes int64
}

// ServerOption configures NewServer / Start. It is also accepted by Register.
type ServerOption func(*serverConfig)

// RegisterOption configures Register-only behaviour.
type RegisterOption func(*registerConfig)
```

`ServerOption` constructors (the single home for the shared knobs — avoids the
duplicate-`WithConfigPrefix` problem two parallel types would otherwise create):

```go
// WithConfigPrefix sets the config prefix the server reads its port, TLS and
// max_header_bytes from (default "server.http"). Pass the SAME prefix to both
// NewServer and Start when constructing a server outside Register.
func WithConfigPrefix(prefix string) ServerOption

// WithPort sets the listen port explicitly, bypassing config lookup entirely.
// Highest precedence: overrides <prefix>.port and the server.port fallback.
func WithPort(port int) ServerOption

func WithMaxHeaderBytes(n int) ServerOption
func WithReadTimeout(d time.Duration) ServerOption
func WithWriteTimeout(d time.Duration) ServerOption
func WithIdleTimeout(d time.Duration) ServerOption
func WithTLSConfig(c *tls.Config) ServerOption
```

`RegisterOption` constructors, unchanged in behaviour:
`WithMiddleware`, `WithMaxRequestBodyBytes`.

New public signatures (all backward-compatible). `Register` widens its variadic
to `...any` and type-switches on `ServerOption` / `RegisterOption`, **exactly
mirroring the existing `pkg/grpc` `Register`** — so the two packages share one
registration shape:

```go
func NewServer(ctx context.Context, cfg config.Containable, handler http.Handler, opts ...ServerOption) (*http.Server, error)
func Start(cfg config.Containable, logger logger.Logger, srv *http.Server, opts ...ServerOption) controls.StartFunc
func Register(ctx context.Context, id string, controller controls.Controllable, cfg config.Containable, logger logger.Logger, handler http.Handler, opts ...any) (*http.Server, error)
```

> **Source-compatibility note.** Today the three `With*` helpers are typed
> `RegisterOption` and `Register` takes `...RegisterOption`. Reclassifying
> `WithConfigPrefix` to `ServerOption` and widening `Register` to `...any` keeps
> every *value-style* call site compiling (`Register(..., WithConfigPrefix("x"))`,
> `NewServer(..., WithConfigPrefix("x"))`). The only constructs that would break
> are the rare explicit `var o RegisterOption = WithConfigPrefix(...)` or
> spreading a pre-built `[]RegisterOption` slice into `Register`. Given the
> `pkg/http` v0.x tier and that this mirrors the already-shipped grpc shape, this
> is an accepted, documented nuance rather than a tracked breaking change.

### Ephemeral-port logging in `Start`

With the permissive `:0` default retained, `Start` must log the **actually bound
address**, not the configured `:0`. The listener returned by
`net.ListenConfig.Listen` resolves the OS-assigned port, so `Start` logs
`ln.Addr().String()` (e.g. `[::]:54317`) instead of `srv.Addr` / the configured
port string. This applies symmetrically to `pkg/grpc.Start`.

Port resolution precedence inside `newServer` becomes:

1. `WithPort(n)` if supplied; else
2. `cfg.GetInt(prefix + ".port")` if non-zero; else
3. `cfg.GetInt("server.port")` shared fallback.

### Two-HTTP-server usage (target outcome)

```go
// Public API server on server.http.*
pub, _ := http.NewServer(ctx, cfg, pubHandler)
ctrl.Register("public",
    controls.WithStart(http.Start(cfg, log, pub)),
    controls.WithStop(http.Stop(log, pub)))

// Internal admin server on its own config block (server.admin.*)
adm, _ := http.NewServer(ctx, cfg, admHandler, http.WithConfigPrefix("server.admin"))
ctrl.Register("admin",
    controls.WithStart(http.Start(cfg, log, adm, http.WithConfigPrefix("server.admin"))),
    controls.WithStop(http.Stop(log, adm)))

// ...or a fixed port with no config at all:
dbg, _ := http.NewServer(ctx, cfg, dbgHandler, http.WithPort(9090))
```

The integrated `Register` path already threads one prefix to both `NewServer`
and `Start` internally, so via `Register` a single `WithConfigPrefix` suffices.

### gRPC Analysis

The same customer need (N independent servers) applies, but the hardcoding lives
in `Start`/`DialLocal`/`Register`, **not** `NewServer` (which already takes a
`grpc.ServerOption` variadic and only reads the `reflection` flag). gRPC
therefore *does* warrant the pattern, with a different surface:

- Introduce `DefaultConfigPrefix = "server.grpc"` and derive the three keys from
  it (`<prefix>.port`, `<prefix>.reflection`, `<prefix>.tls`) instead of the
  fixed `ConfigKey*` constants. Keep the `ConfigKey*` constants as deprecated
  aliases of the default-prefix-derived values for backward compatibility.
- Add a grpc `ServerOption` type (functional option over a `serverConfig`
  struct: `prefix`, `port *int`) consumed by **`Start`** and **`DialLocal`**
  (which must agree on port/prefix for the gateway to dial the right backend),
  and threaded through `Register`.
- `NewServer` gains the same `ServerOption` so the `reflection` flag honours a
  custom prefix; caller `grpc.ServerOption`s continue to flow through as today.

New/changed gRPC signatures (all backward-compatible):

```go
func NewServer(cfg config.Containable, opts ...any) (*grpc.Server, error)       // accepts ServerOption + grpc.ServerOption (type-switch, mirroring Register)
func Start(cfg config.Containable, logger logger.Logger, srv *grpc.Server, opts ...ServerOption) controls.StartFunc
func DialLocal(cfg config.Containable, opts ...any) (*grpc.ClientConn, error)   // accepts ServerOption + grpc.DialOption
func Register(ctx, id, controller, cfg, logger, opts ...any) (*grpc.Server, error) // already ...any; add ServerOption to the type-switch

func WithConfigPrefix(prefix string) ServerOption // default "server.grpc"
func WithPort(port int) ServerOption
```

`grpc.NewServer` and `DialLocal` already take a variadic (`grpc.ServerOption` /
`grpc.DialOption`). Widening them to `...any` with a type-switch — exactly as
`grpc.Register` already does — keeps one call shape across the package. Existing
value-style call sites (`NewServer(cfg, grpc.Creds(...))`,
`DialLocal(cfg, grpc.WithBlock())`) continue to compile; only the rare
slice-spread of `[]grpc.ServerOption`/`[]grpc.DialOption` would need `[]any`.

> **Naming:** our type is the unqualified `ServerOption` inside `package grpc`;
> the grpc-go option type is `grpc.ServerOption` (the aliased
> `google.golang.org/grpc` import) — no in-package collision. Downstream callers
> already alias one of the two `grpc` imports.

## Data Models

No persisted data models. The only new types are the unexported `options` /
`grpcOptions` structs and the exported `Option` function types described above.

Config keys read (HTTP, per resolved `<prefix>`, default `server.http`):

| Key | Meaning |
|-----|---------|
| `<prefix>.port` | listen port (overridden by `WithPort`) |
| `server.port` | shared fallback port |
| `<prefix>.max_header_bytes` | max header bytes (overridden by `WithMaxHeaderBytes`) |
| `<prefix>.tls.*` | TLS cert/key resolution (in `Start`) |

Config keys read (gRPC, per resolved `<prefix>`, default `server.grpc`):

| Key | Meaning |
|-----|---------|
| `<prefix>.port` | listen port (overridden by `WithPort`) |
| `server.port` | shared fallback port |
| `<prefix>.reflection` | toggle server reflection |
| `<prefix>.tls.*` | TLS resolution |

## Error Cases

| Condition | Behaviour |
|-----------|-----------|
| `WithPort(n)` with `n < 0` or `n > 65535` | return a wrapped `errors.New` from `NewServer` (HTTP) / `Start` (gRPC) — invalid port. |
| `WithConfigPrefix("")` | return a wrapped error — empty prefix is a programming error (silently falling back to default would mask bugs). |
| No port resolvable (config 0, no `WithPort`) | unchanged from today: `Addr` becomes `:0`, OS assigns an ephemeral port. *(Open Question 2: should this stay permissive or error?)* |
| Unknown option type passed to a gRPC `...any` function | ignored, matching the existing `grpc.Register` behaviour (which silently skips non-matching types). *(Open Question 3.)* |

## Testing Strategy

Standard table-driven unit tests with `t.Parallel()`, mocks from
`mocks/pkg/config`. New `pkg/` code must hit **≥90% coverage**.

HTTP:

- `WithConfigPrefix` makes `NewServer` read `<prefix>.port`, `<prefix>.max_header_bytes`.
- `WithPort` overrides config and the `server.port` fallback; precedence order verified.
- `WithMaxHeaderBytes`, `WithReadTimeout`, etc. land on the returned `*http.Server`.
- Two `NewServer` calls with different prefixes/ports produce servers with
  distinct `Addr` — the core "two servers" scenario.
- Backward compatibility: existing `NewServer(ctx, cfg, handler)` / `Register`
  call sites still resolve `server.http`.
- Invalid port / empty prefix error cases.
- `Start` with a custom prefix resolves `<prefix>.tls.*`.

gRPC:

- `Start` / `DialLocal` honour `WithConfigPrefix` and `WithPort`; agree on the endpoint.
- Deprecated `ConfigKey*` constants still resolve the default-prefix values.
- `NewServer` reflection flag honours custom prefix; `grpc.ServerOption`s still applied.
- `Register` type-switch accepts the new `Option` alongside `grpc.ServerOption`.

### E2E BDD (Godog) assessment

Per the [Godog BDD strategy](2026-03-28-godog-bdd-strategy.md): this change is
**library-level construction wiring**, not a new CLI command or a user-visible
multi-step workflow. It is adequately covered by table-driven unit tests; **no
new Gherkin scenarios are warranted**. (If a downstream tool later exposes a
multi-server lifecycle via the CLI, BDD coverage would be added there, not in
GTB.)

## Implementation Phases

1. **HTTP options (core).** Introduce `Option` + `options`, the `RegisterOption`
   alias, `WithConfigPrefix`/`WithPort`/`WithMaxHeaderBytes`/timeout/TLS
   options; thread through `newServer`/`start`/`Register`; tests; docs.
2. **gRPC options.** Introduce `DefaultConfigPrefix` + prefix derivation,
   deprecated `ConfigKey*` aliases, `Option` + `WithConfigPrefix`/`WithPort`;
   thread through `NewServer`/`Start`/`DialLocal`/`Register`; tests; docs.
3. **Generator + docs.** Update `internal/generator/` templates if scaffolded
   server wiring is affected; update `docs/components/http.md`,
   `docs/components/grpc.md` (or equivalents) and any `docs/concepts/` service
   pages; add a two-server example.

Phases 1 and 2 are independently shippable `feat` commits.

## Resolved Decisions

All open questions were resolved by Matt on 2026-06-08:

1. **HTTP option types — SEPARATE.** Two distinct types: `ServerOption`
   (`NewServer`/`Start`) and `RegisterOption` (`Register`-only). `registerConfig`
   embeds `serverConfig`; `Register` widens to `...any` and type-switches. The
   shared knobs live as `ServerOption` constructors (single `WithConfigPrefix`).
2. **Zero-port — PERMISSIVE `:0`, but log the real port.** Keep the ephemeral
   default (useful in tests); `Start` logs the listener's resolved address, not
   `:0`. (Goal 3a.)
3. **gRPC `...any` widening — YES.** `NewServer` and `DialLocal` widen to
   `...any` with a type-switch, consistent with the existing `grpc.Register`.
4. **HTTP option set — FULL adjacent set now.** `WithConfigPrefix` + `WithPort`
   **plus** `WithMaxHeaderBytes`, `WithReadTimeout`/`WriteTimeout`/`IdleTimeout`,
   `WithTLSConfig`.
5. **gRPC scope — IN NOW.** Phase 2 ships in this change set, not a follow-up.
