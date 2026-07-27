---
title: "Transport servers: honour a supplied server TLS config so mTLS is reachable on the managed path"
description: "transporthttp's managed start unconditionally replaces srv.TLSConfig with tlsPair.ServerConfig(), and transportgrpc's wrapTLS always builds pair.ServerConfig(\"h2\") with no override hook — so WithServerTLSConfig is silently clobbered and ClientCAs/ClientAuth can never reach a managed listener, making the mTLS auth verifiers dead code on that path. Fix by loading the pair's certificate INTO the caller-supplied config instead of replacing it, and extending gtls.Pair with client-CA fields for config-driven mTLS."
date: 2026-07-23
status: IMPLEMENTED
tags:
  - specification
  - transport
  - tls
  - mtls
  - security
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# Transport servers: honour a supplied server TLS config so mTLS is reachable on the managed path

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   IMPLEMENTED (2026-07-27). Shipped in go/tls v0.1.2 + go/transport v0.1.3 (tls MR !9, transport MR !14), red-first TDD with the defect reproduced against the module's pre-fix main before the change; GTB picks it up via the config-family / module dependency bumps. Open questions were resolved by the maintainer during triage.

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (HIGH finding, transport section),
    [TLS module extraction](2026-07-16-tls-module-extraction.md) (where gtls.Pair landed),
    [transport server stack extraction](2026-07-17-transport-server-stack.md)

---

## 1. Problem

`transporthttp.WithServerTLSConfig` (`go/transport/http/server.go:154-158`) is
applied in `newServer` (lines 186-189, falling back to `gtls.DefaultConfig()`)
— and then silently discarded on every managed path:

- **TLS enabled:** `start` (`server.go:256-263`) unconditionally replaces
  `srv.TLSConfig` with a fresh `tlsPair.ServerConfig()` — a bare
  `gtls.DefaultConfig()` plus the pair's certificate (`go/tls/tls.go:50-64`).
  The caller's config is clobbered.
- **TLS disabled:** `start` uses `srv.Serve` (line 287), so the custom config is
  never consulted at all.

The gRPC server is the same shape with no option at all: `wrapTLS`
(`go/transport/grpc/server.go:274-281`) always builds `pair.ServerConfig("h2")`;
there is no server-TLS-config hook on the managed path.

Neither `gtls.Pair` (`Enabled`/`Cert`/`Key`, `go/tls/config.go`) nor any
`ServerOption` can express `ClientCAs`/`ClientAuth`. **Server mTLS is therefore
unreachable via the managed path.**

**Failure scenario.** A caller configures client-cert auth:

```go
transporthttp.Register(..., tlsPair,
    transporthttp.WithServerTLSConfig(&tls.Config{
        ClientCAs:  pool,
        ClientAuth: tls.RequireAndVerifyClientCert,
    }),
    transporthttp.WithAuthMiddleware(transporthttp.WithMTLSVerifier(verifier)),
)
```

The server starts and serves **without** client-cert verification.
`r.TLS.VerifiedChains` is always empty, so `AuthMiddleware`'s mTLS branch
(`go/transport/http/auth.go:203-204`) can never match: every request 401s — or,
worse, is admitted by another configured verifier while the operator believes
mTLS is active. `WithGRPCMTLSVerifier` (`go/transport/grpc/auth.go:48-50`) is
equally dead on the managed gRPC path. The failure is silent: nothing logs or
errors when the supplied config is discarded.

## 2. Proposed change

Two alternatives were considered; both are presented, with (b) recommended and
(a) folded in as its mechanical core.

**(a) Merge instead of replace.** In `start`/`wrapTLS`, when the caller supplied
a server TLS config, load the pair's certificate **into** that config
(`cfg.Certificates = append(...)` via a new `gtls.Pair` helper, e.g.
`ApplyTo(cfg *tls.Config, nextProtos ...string) error`) instead of building a
fresh one; only fall back to `pair.ServerConfig()` when no config was supplied.
The gRPC server gains the missing `WithServerTLSConfig`-equivalent
`ServerOption`. This makes the existing option truthful for programmatic
callers, but mTLS remains unreachable from **config** — GTB tools drive these
servers from `gtls.Pair` resolved out of the config tree.

**(b) Extend `gtls.Pair` with client-CA fields (recommended), plus (a).** Add to
`Pair`:

```go
ClientCAs  []string `mapstructure:"client_cas" ...` // PEM CA files; empty = no client-cert auth
ClientAuth string   `mapstructure:"client_auth" ...` // "", "request", "require-verify"
```

`ServerConfig` builds the pool via the existing `gtls.CertPool` and sets
`ClientCAs`/`ClientAuth` accordingly (unknown `ClientAuth` values are a hard
error — fail closed). Because both managed transports already funnel through
`Pair.ServerConfig`, this single change makes mTLS reachable on HTTP **and**
gRPC, config-driven end to end, with no per-transport plumbing. (a) is still
implemented so a hand-built config with custom verification
(`VerifyPeerCertificate`, session tickets, etc.) is honoured rather than
clobbered; when **both** a custom config and Pair client-CA fields are set,
error on construction rather than guessing precedence.

At minimum — if (b) is rejected — the `WithServerTLSConfig` doc comment must
state that the managed path overrides it, and `Register` must **error** on the
conflicting combination (`tlsPair.Enabled && sc.tlsConfig != nil`) instead of
silently discarding a security-relevant setting.

## 3. Scope & release plan

1. **`go/tls`** — extend `Pair` with `ClientCAs`/`ClientAuth` + `ApplyTo`
   helper; `PairOverrides`/`MergePair` gain the new fields. Additive
   `feat(tls)` minor.
2. **`go/transport`** — honour supplied configs in `transporthttp` `start` and
   `transportgrpc` `wrapTLS`; add the gRPC server-TLS-config option; bump
   `go/tls`. `fix(http,grpc)` release after go/tls.
3. **`go/transit`** — not involved (client-side middleware only).
4. **GTB** — `pkg/tls` config adapter exposes the new keys
   (`*.tls.client_cas`, `*.tls.client_auth`) through the existing
   section-adapter pattern; `pkg/http`/`pkg/grpc`/`pkg/gateway` pick the rest up
   via dependency bumps. Docs: `docs/components/` TLS/server pages document the
   mTLS wiring end to end (Pair fields → verifier middleware).

## 4. Acceptance criteria

1. **mTLS end-to-end handshake test (HTTP)**: managed `transporthttp.Register`
   server with `Pair{ClientCAs, ClientAuth: require-verify}`; a client
   presenting a valid cert from that CA completes the handshake, the request
   reaches the handler with non-empty `r.TLS.VerifiedChains`, and
   `WithMTLSVerifier` authenticates it; a client with no cert (and one with a
   cert from a different CA) fails the handshake. This test is impossible
   against the current code.
2. **mTLS end-to-end handshake test (gRPC)**: same shape through the managed
   gRPC path; `WithGRPCMTLSVerifier` sees verified chains.
3. **Clobber regression test**: `WithServerTLSConfig` carrying a distinctive
   marker (e.g. `VerifyPeerCertificate` hook or a custom `MinVersion`) is
   observably in effect on the managed TLS path — the hook fires / the
   handshake reflects the setting.
4. Conflict handling: custom server config **and** Pair client-CA fields set →
   construction error, not silent precedence; invalid `ClientAuth` string →
   error.
5. `Pair` zero-value behaviour unchanged (no client-cert auth, existing
   handshake tests green); ALPN `"h2"` still advertised on the gRPC listener
   path with a merged config.
6. GTB config adapter round-trips the new keys; doctor/E2E smoke unaffected;
   coverage ≥90% on touched `go/tls` code.

## 5. Open questions

1. `ClientAuth` vocabulary: is two values (`request`, `require-verify`) enough,
   or expose all four `tls.ClientAuthType` non-zero modes? Fewer, fail-closed
   options are easier to hold; `VerifyClientCertIfGiven` has real use for
   mixed-auth listeners.
2. Should `WithServerTLSConfig` + `tlsPair.Enabled` **merge** (recommended, via
   `ApplyTo`) or **error**, forcing callers to choose one mechanism? Merging is
   more useful but creates a precedence story to document.
3. Does the gateway need its own client-CA surface for its outbound dial to the
   gRPC server (it already has `WithDialOptions`), or is documenting the
   existing escape hatch sufficient?
