---
title: TLS
description: Shared TLS plumbing used by every transport (HTTP, gRPC and the gateway).
date: 2026-05-31
tags: [components, tls, networking, security]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# TLS

The `pkg/tls` package holds the shared TLS plumbing used across every transport in the framework: HTTP, gRPC and the gateway. It provides the hardened default config, the typed `Pair` config shape with shared and per-transport resolution, and the client-side certificate-pool helpers.

Keeping this in one place deliberately decouples `pkg/http` and `pkg/grpc` from each other for TLS: neither depends on the other, and the gateway has a single dependency for it. The package is imported as `gtbtls`.

## Default Configuration

`DefaultConfig() *tls.Config` returns the hardened TLS configuration shared across the HTTP and gRPC servers and the HTTP client.

### Hardening

- **TLS 1.2 minimum**: enforces `VersionTLS12` as the floor.
- **Curated AEAD ciphers**: only ECDHE key exchange with AES-GCM (256/128) and ChaCha20-Poly1305 suites.
- **Modern curve preferences**: X25519 first, then P256.

```go
cfg := gtbtls.DefaultConfig()
```

## The `Pair` Type

`Pair` is the typed enabled/cert/key triple used to configure TLS for any transport. It carries `mapstructure`, `yaml` and `json` struct tags so the same shape marshals consistently wherever it is used.


> [!NOTE]
> See [pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/tls](https://pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/tls) for the full API definition.


### Methods

- **`(p Pair) Valid() bool`**: reports whether TLS is enabled and both certificate paths are present.
- **`(p Pair) Certificate() (tls.Certificate, error)`**: loads the X509 key pair described by the pair.
- **`(p Pair) ServerConfig(nextProtos ...string) (*tls.Config, error)`**: returns the hardened `DefaultConfig` with this pair's certificate loaded.

The `nextProtos` variadic on `ServerConfig` sets the ALPN protocols advertised by the listener. This is how the raw gRPC TLS listener advertises `h2`. When no protocols are passed, the config's defaults are left as-is.

```go
// Raw gRPC TLS listener advertising HTTP/2 via ALPN.
cfg, err := pair.ServerConfig("h2")
```

## Resolving Configuration

`Resolve(cfg config.Containable, transportPrefix string) Pair` resolves the TLS settings for a transport. It starts from the shared `SharedPrefix` (`server.tls`) and overrides each field individually from the transport-specific prefix whenever that key is set. This lets a single certificate serve every transport, with per-transport overrides where needed.

The transport-specific prefixes are `server.grpc.tls`, `server.http.tls` and `server.gateway.tls`.

### Cascade

TLS configuration cascades — transport-specific keys override the shared defaults field by field:

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
```

## Client-Side Trust

When a client needs to trust certificates that are not in the system roots — self-signed certificates or a private CA, such as the gateway dialling the gRPC server — the package provides two helpers.

- **`CertPool(caFiles ...string) (*x509.CertPool, error)`**: builds an x509 certificate pool seeded with the given PEM CA/cert files. Pass the same cert files the servers present to share one trust anchor across gRPC, HTTP and the gateway.
- **`ClientConfig(caFiles ...string) (*tls.Config, error)`**: returns a hardened client TLS config (`DefaultConfig`) that trusts the given CA/cert files via a custom pool. With no files it returns the default config, which trusts the system roots.

```go
// Gateway trusting the gRPC server's self-signed certificate.
clientCfg, err := gtbtls.ClientConfig("/etc/certs/server.crt")
```

## Constants

- **`SharedPrefix`** (`= "server.tls"`): the config prefix for TLS settings shared across every transport. A transport-specific prefix overrides individual fields, so one certificate can serve all transports with per-transport overrides where needed.

## See also

TLS answers *"is the channel private?"* — the transport's confidentiality layer. The other shared, cross-cutting transport concern is request handling: logging, auth, rate limiting, and circuit breaking, configured via middleware/interceptor chains with the same shared-then-per-transport config cascade. See [Transport Middleware & Resilience](../concepts/transport-middleware.md).
