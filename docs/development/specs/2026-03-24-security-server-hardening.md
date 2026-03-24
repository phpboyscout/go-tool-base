---
title: "Security & Server Hardening Specification"
description: "Security improvements to GTB server infrastructure including gating gRPC reflection behind a config flag, adding HTTP request size limits, documenting the secrets/config deployment model, and documenting the ophis MCP library rationale."
date: 2026-03-24
status: IMPLEMENTED
tags:
  - specification
  - security
  - hardening
  - documentation
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-opus-4-6)
    role: AI drafting assistant
---

# Security & Server Hardening Specification

Authors
:   Matt Cockayne, Claude (claude-opus-4-6) *(AI drafting assistant)*

Date
:   24 March 2026

Status
:   IMPLEMENTED

---

## Overview

The GTB server infrastructure has two security gaps and two documentation gaps identified during review:

1. **gRPC reflection is unconditionally enabled** -- `reflection.Register(srv)` is called on every gRPC server regardless of environment. Reflection exposes the full service schema to any client, which is useful during development but a security concern in production. It should be gated behind a config flag.

2. **HTTP server has no request size limits** -- The HTTP server configures read/write/idle timeouts but does not set `MaxHeaderBytes`. A malicious client can send arbitrarily large headers to consume server memory. A sensible default limit should be applied.

3. **No documentation of the secrets/config deployment model** -- GTB uses Viper for configuration, which supports environment variable overrides. The intended model for secrets in development vs. production is implicit knowledge that should be documented.

4. **No documentation of the ophis library choice** -- The MCP integration uses `github.com/njayp/ophis` rather than the official `modelcontextprotocol/go-sdk`. The rationale (seamless cobra integration, small footprint) is not recorded anywhere, which will cause confusion for contributors.

---

## Design Decisions

**Reflection default: off in production, on in development**: The most secure default is off. Development environments explicitly opt in. The config key `server.grpc.reflection` defaults to `false`. The GTB generator can set it to `true` in development config files.

**1MB `MaxHeaderBytes` default**: Go's `net/http` default is 1MB (`1 << 20`) when `MaxHeaderBytes` is 0 (the zero value). However, explicitly setting it documents the intention and makes it configurable. The 1MB default matches Go's implicit behaviour and is sufficient for all reasonable header sizes including large JWT tokens and cookie sets.

**Config over environment for the flag**: The reflection flag uses the standard Viper config path (`server.grpc.reflection`), which means it can be set via config file, environment variable (`SERVER_GRPC_REFLECTION=true`), or CLI flag. This is consistent with how all other GTB config works.

**Documentation in docs/, not code comments**: The secrets model and ophis rationale are architectural decisions that affect deployment and dependency choices. They belong in the documentation site, not buried in code comments that only developers reading that specific file will find.

---

## Public API Changes

### New Config Keys

```yaml
server:
  grpc:
    reflection: false  # Enable gRPC server reflection (default: false)
  http:
    max_header_bytes: 1048576  # Maximum header size in bytes (default: 1MB)
```

### Modified: `pkg/grpc/server.go`

```go
// Before:
func NewServer(cfg config.Containable) *grpc.Server {
    srv := grpc.NewServer()
    reflection.Register(srv)
    return srv
}

// After:
func NewServer(cfg config.Containable) *grpc.Server {
    srv := grpc.NewServer()
    if cfg.GetBool("server.grpc.reflection") {
        reflection.Register(srv)
    }
    return srv
}
```

### Modified: `pkg/http/server.go`

```go
// Before:
srv := &http.Server{
    Addr:         fmt.Sprintf(":%d", cfg.GetInt("server.port")),
    ReadTimeout:  cfg.GetDuration("server.read_timeout"),
    WriteTimeout: cfg.GetDuration("server.write_timeout"),
    IdleTimeout:  cfg.GetDuration("server.idle_timeout"),
}

// After:
maxHeaderBytes := cfg.GetInt("server.http.max_header_bytes")
if maxHeaderBytes == 0 {
    maxHeaderBytes = 1 << 20 // 1MB default
}

srv := &http.Server{
    Addr:           fmt.Sprintf(":%d", cfg.GetInt("server.http.port")),
    ReadTimeout:    cfg.GetDuration("server.read_timeout"),
    WriteTimeout:   cfg.GetDuration("server.write_timeout"),
    IdleTimeout:    cfg.GetDuration("server.idle_timeout"),
    MaxHeaderBytes: maxHeaderBytes,
}
```

---

## Internal Implementation

### 1. Gate gRPC Reflection Behind Config Flag

**File:** `pkg/grpc/server.go:20`

The change is a single conditional wrapping the existing `reflection.Register(srv)` call. The config key `server.grpc.reflection` is read via `cfg.GetBool()`, which returns `false` for unset keys -- making the secure default automatic.

For development convenience, the GTB generator should include `server.grpc.reflection: true` in the default development config template so that new projects have reflection enabled during development out of the box.

**Generator template update** (`internal/generator/templates/`):

```yaml
# config/development.yaml (generated)
server:
  grpc:
    reflection: true
```

### 2. Add `MaxHeaderBytes` to HTTP Server

**File:** `pkg/http/server.go`

The `MaxHeaderBytes` field is added to the `http.Server` struct literal. The value is read from config with a fallback to 1MB. This protects against header-based memory exhaustion attacks without requiring any application code changes.

The 1MB limit accommodates:
- Standard HTTP headers: typically under 8KB
- Large JWT tokens: rarely exceed 16KB
- Cookie sets: can reach 50-100KB in complex applications
- Generous margin for unexpected header growth

### 3. Document Secrets/Config Deployment Model

**New file:** `docs/development/security.md`

The document covers:

- **Viper config resolution order**: CLI flags > environment variables > config files > defaults
- **Environment variable mapping**: Viper's automatic env binding converts dot-separated config paths to upper-case underscore-separated environment variables (e.g., `server.http.port` maps to `SERVER_HTTP_PORT`)
- **Development machines**: Config file secrets (API keys, database passwords) are acceptable. They are equivalent to environment variables in `.bashrc`/`.zshrc` -- the threat model is the same (local machine compromise). Config files should be in `.gitignore`.
- **Container/Kubernetes deployments**: Config files are mounted from secrets mechanisms:
    - Kubernetes Secrets mounted as volumes
    - HashiCorp Vault with CSI driver or sidecar
    - AWS Secrets Manager with external secrets operator
    - Environment variables injected from secret stores
- **Key principle**: Secrets are runtime dependencies, not build-time dependencies. They are never committed to version control, never baked into container images, and never passed as build arguments.
- **GTB's role**: GTB provides the config abstraction (Viper) and the convention (config paths). The deployment platform provides the secrets mechanism. GTB does not and should not implement its own secrets management.

### 4. Document Ophis Rationale

**New file:** `docs/components/commands/mcp.md` (or append to existing)

The document explains:

- **What ophis provides**: A library that bridges Cobra commands to MCP (Model Context Protocol) tool definitions. Each Cobra command automatically becomes an MCP tool with the command's flags mapped to tool parameters.
- **Why ophis was chosen over alternatives**:
    - Seamless Cobra integration: ophis reads Cobra command trees directly. No manual tool definition or schema duplication needed.
    - Small footprint: ophis is a thin translation layer, not a full MCP framework.
    - No equivalent found: At the time of evaluation, no other library provided drop-in Cobra-to-MCP bridging.
- **Relationship to `modelcontextprotocol/go-sdk`**: The official Go SDK (`modelcontextprotocol/go-sdk`) is a transitive dependency of ophis. GTB does not depend on it directly because ophis encapsulates the MCP protocol details. If ophis is ever abandoned, migrating to the official SDK directly is straightforward since the protocol layer is already present in the dependency tree.
- **When to reconsider**: If the official `modelcontextprotocol/go-sdk` adds native Cobra integration, or if ophis becomes unmaintained while the official SDK matures, the dependency should be re-evaluated.

---

## Project Structure

```
pkg/grpc/
├── server.go          <- MODIFIED: conditional reflection registration

pkg/http/
├── server.go          <- MODIFIED: add MaxHeaderBytes

docs/development/
├── security.md        <- NEW: secrets/config deployment model documentation

docs/components/commands/
├── mcp.md             <- NEW or MODIFIED: ophis rationale documentation
```

---

## Testing Strategy

### Unit Tests

| Test | Scenario |
|------|----------|
| `TestGRPCServer_ReflectionEnabled` | Set `server.grpc.reflection: true`, verify reflection service is registered |
| `TestGRPCServer_ReflectionDisabled` | Set `server.grpc.reflection: false` (or unset), verify reflection service is not registered |
| `TestGRPCServer_ReflectionDefaultOff` | Provide config with no reflection key, verify reflection is not registered |
| `TestHTTPServer_MaxHeaderBytes_Configured` | Set `server.http.max_header_bytes: 2097152`, verify server has 2MB limit |
| `TestHTTPServer_MaxHeaderBytes_Default` | Provide no config, verify server defaults to 1MB |
| `TestHTTPServer_MaxHeaderBytes_Zero` | Set value to 0, verify fallback to 1MB default |
| `TestHTTPServer_RejectsOversizedHeaders` | Send request with headers exceeding the limit, verify 431 response |

### Integration Tests

| Test | Scenario |
|------|----------|
| `TestGRPCServer_ReflectionFunctional` | With reflection enabled, use gRPC reflection client to list services |
| `TestGRPCServer_ReflectionNotExposed` | With reflection disabled, verify reflection requests fail |

### Documentation Tests

| Test | Scenario |
|------|----------|
| `TestSecurityDoc_Exists` | Verify `docs/development/security.md` exists and is non-empty |
| `TestMCPDoc_Exists` | Verify `docs/components/commands/mcp.md` exists and is non-empty |

### Coverage

- Target: 90%+ for modified server files.
- All config key paths (set, unset, zero value) must be covered.

---

## Backwards Compatibility

- **gRPC reflection**: Existing deployments that rely on reflection being always-on will break unless they add `server.grpc.reflection: true` to their config. This is intentionally a breaking change -- the secure default is more important than silent backwards compatibility for a security-sensitive feature. The migration path is a single config line.

- **`MaxHeaderBytes`**: Go's default behaviour when `MaxHeaderBytes` is 0 is to use 1MB. Since this spec sets 1MB as the explicit default, there is no behaviour change for existing deployments. Only deployments that explicitly set a different value via config will see different behaviour, and that is opt-in.

- **Documentation**: New documentation has no backwards compatibility impact.

---

## Future Considerations

- **mTLS support**: The gRPC server could support mutual TLS for service-to-service authentication. This would complement the reflection gating by providing transport-level access control.

- **Rate limiting**: The `MaxHeaderBytes` limit protects against header-based attacks but not against request volume attacks. A future spec could add rate limiting middleware to both HTTP and gRPC servers.

- **Security headers**: The HTTP server could add security headers (HSTS, CSP, X-Content-Type-Options) via middleware. This is orthogonal to request size limits but part of the same hardening theme.

- **Config encryption at rest**: For environments where config files contain secrets and filesystem encryption is not available, Viper supports encrypted config values. This could be documented as an option.

- **Audit logging**: Security-relevant events (reflection access, oversized request rejection) could be logged at a dedicated audit level for compliance requirements.

---

## Implementation Phases

### Phase 1 -- gRPC Reflection Gating
1. Add conditional check around `reflection.Register(srv)` in `pkg/grpc/server.go`
2. Update generator development config template to include `server.grpc.reflection: true`
3. Add tests for reflection enabled, disabled, and default states

### Phase 2 -- HTTP MaxHeaderBytes
1. Add `MaxHeaderBytes` to HTTP server construction in `pkg/http/server.go`
2. Read from `server.http.max_header_bytes` config with 1MB fallback
3. Add tests for configured, default, and zero-value scenarios

### Phase 3 -- Security Documentation
1. Create `docs/development/security.md` with secrets/config deployment model
2. Create or update `docs/components/commands/mcp.md` with ophis rationale

### Phase 4 -- Verification
1. Run full test suite
2. Run linter
3. Review documentation for accuracy and completeness

---

## Verification

```bash
# Build
go build ./...

# Full test suite with race detector
go test -race ./...

# Specific packages
go test -race -cover ./pkg/grpc/...
go test -race -cover ./pkg/http/...

# Lint
golangci-lint run --fix

# Verify reflection is conditional
grep -rn 'reflection.Register' --include='*.go' pkg/grpc/
# Should show the call inside a conditional block

# Verify MaxHeaderBytes is set
grep -rn 'MaxHeaderBytes' --include='*.go' pkg/http/
# Should show explicit assignment

# Verify documentation exists
test -f docs/development/security.md && echo "security doc exists"
test -f docs/components/commands/mcp.md && echo "mcp doc exists"
```
