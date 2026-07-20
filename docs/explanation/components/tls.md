---
title: TLS
description: go-tool-base consumes the standalone go/tls module for hardened TLS, and adds the GTB config-key Resolve adapter that maps the server.tls cascade onto its typed Pair.
date: 2026-05-31
tags: [components, tls, networking, security]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# TLS

The hardened TLS plumbing has been **extracted into the standalone
[`gitlab.com/phpboyscout/go/tls`](https://gitlab.com/phpboyscout/go/tls) module**
(framework-free — its only dependency is `cockroachdb/errors`). Its full
documentation — the `DefaultConfig` hardening, the typed `Pair`, the
`ServerConfig`/`ClientConfig` builders, the `CertPool` helper, and the **security
threat model** — now lives at:

> **[tls.go.phpboyscout.uk](https://tls.go.phpboyscout.uk)**

Unlike the pure-repoint extractions, `pkg/tls` **remains** in go-tool-base as a thin
**facade**: it re-exports the module's core (so `gtbtls.Pair`, `gtbtls.DefaultConfig`,
`gtbtls.ClientConfig`, `gtbtls.CertPool` are unchanged) and keeps the one piece that
belongs to the framework — the config-key adapter `Resolve`. See the
[migration note](../../reference/migration/v0.x-tls-extracted.md).

## What go-tool-base adds: config resolution

The module works from typed `Pair` values; it is deliberately config-agnostic. GTB's
facade bridges that to its layered configuration with `Resolve`, which maps the
`server.tls` key cascade onto a `Pair`.

`Resolve(cfg config.Reader, transportPrefix string) Pair` starts from the shared
`SharedPrefix` (`server.tls`) and overrides each field individually from the
transport-specific prefix whenever that key is set. This lets a single certificate
serve every transport, with per-transport overrides where needed. The transport
prefixes are `server.grpc.tls`, `server.http.tls` and `server.gateway.tls`.

### Cascade

TLS configuration cascades — transport-specific keys override the shared defaults
field by field:

| Key | Shared Default | gRPC Override | HTTP Override | Gateway Override |
|-----|---------------|---------------|---------------|------------------|
| Enabled | `server.tls.enabled` | `server.grpc.tls.enabled` | `server.http.tls.enabled` | `server.gateway.tls.enabled` |
| Certificate | `server.tls.cert` | `server.grpc.tls.cert` | `server.http.tls.cert` | `server.gateway.tls.cert` |
| Private key | `server.tls.key` | `server.grpc.tls.key` | `server.http.tls.key` | `server.gateway.tls.key` |

To use one certificate for every transport, configure the shared keys only:

```yaml
server:
  tls:
    enabled: true
    cert: /etc/certs/server.crt
    key: /etc/certs/server.key
```

```go
// Each transport resolves against its own prefix; with only the shared keys
// set, all three receive the same pair.
pair := gtbtls.Resolve(cfg, "server.grpc.tls")
cfg, err := pair.ServerConfig("h2") // core builder, from go/tls
```

`SharedPrefix` (`= "server.tls"`) and `Resolve` are the only symbols that stay in
`pkg/tls`; everything else is re-exported from `go/tls`.

## See also

TLS answers *"is the channel private?"* — the transport's confidentiality layer. The
other shared, cross-cutting transport concern is request handling: logging, auth, rate
limiting, and circuit breaking, configured via middleware/interceptor chains with the
same shared-then-per-transport config cascade. See
[Transport Middleware & Resilience](../concepts/transport-middleware.md).
