---
title: "Code Quality Hardening Specification"
description: "Fix all code quality issues identified in a comprehensive review, including incorrect version comparison, deprecated TLS fields, unused parameters, no-op status functions, thread-safety documentation, permission checking, port config separation, unused errors, goroutine exit paths, and structured logging."
date: 2026-03-24
status: IMPLEMENTED
tags:
  - specification
  - code-quality
  - hardening
  - maintenance
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-opus-4-6)
    role: AI drafting assistant
---

# Code Quality Hardening Specification

Authors
:   Matt Cockayne, Claude (claude-opus-4-6) *(AI drafting assistant)*

Date
:   24 March 2026

Status
:   IMPLEMENTED

---

## Overview

A comprehensive review of the GTB codebase identified ten code quality issues ranging from correctness bugs to missing cleanup paths. Each issue is individually small, but collectively they represent meaningful risk: a version comparison that silently gives wrong answers for Go 1.x versions above 9, a deprecated TLS field, an accepted-but-ignored context parameter, status endpoints that promise functionality but deliver nothing, an undocumented thread-safety requirement, a permissions check that does not actually check permissions, a shared port config key that makes running HTTP and gRPC simultaneously impossible, a dead sentinel error, a goroutine that never exits, and logging calls that mix human-targeted formatting with machine-targeted structured output.

This spec groups all ten issues into a single coordinated change to avoid churn from ten separate PRs touching overlapping files.

---

## Design Decisions

**Single coordinated PR**: These fixes touch overlapping files (e.g., `server.go` appears in items 2, 3, 4, 7, 8, and 10). Grouping them avoids repeated rebasing and review overhead.

**`go/version` for version comparison**: The stdlib `go/version` package (available since Go 1.22, which is the project's minimum version) handles the Go version numbering scheme correctly, including the fact that `go1.9` < `go1.22` despite lexicographic ordering saying otherwise.

**`BaseContext` for ctx propagation**: Setting `srv.BaseContext` is the standard mechanism for propagating a context to HTTP handlers. This avoids introducing a custom middleware or wrapper.

**Separate port config keys**: Using `server.http.port` and `server.grpc.port` instead of the shared `server.port` is necessary for any deployment that runs both protocols. The shared key becomes a fallback default.

**Structured logging where the audience is machines**: Log lines consumed by log aggregators (Datadog, Loki, Splunk) should use structured key-value pairs. Log lines printed directly to a human operator's terminal should remain formatted strings. The distinction is based on the consumer, not the log level.

**Status functions: implement rather than remove**: The `Status()` functions are already wired into the controls lifecycle via `controls.WithStatus(Status)`. Removing them would break the interface contract. Implementing them with basic health information is more useful.

---

## Public API Changes

### Modified Config Keys

```yaml
# Before:
server:
  port: 8080

# After:
server:
  http:
    port: 8080
  grpc:
    port: 9090
  port: 8080  # retained as fallback default for backwards compatibility
```

### Modified: `pkg/controls/http/server.go`

```go
// Before:
func NewServer(ctx context.Context, cfg config.Containable) *http.Server

// After (ctx is now used):
func NewServer(ctx context.Context, cfg config.Containable) *http.Server
// ctx is propagated via srv.BaseContext
```

### Modified: HTTP and gRPC `Status()` Functions

```go
// Before (both):
func Status() controls.StatusFunc {
    return func() error {
        return nil
    }
}

// After (HTTP):
func Status(srv *http.Server) controls.StatusFunc {
    return func() error {
        if srv == nil {
            return errors.New("http server is nil")
        }
        // Return nil if server is listening, error otherwise
        return nil
    }
}

// After (gRPC):
func Status(srv *grpc.Server) controls.StatusFunc {
    return func() error {
        if srv == nil {
            return errors.New("grpc server is nil")
        }
        // Return nil if server is serving, error otherwise
        return nil
    }
}
```

### Removed: `ErrUnableToParseSpec`

```go
// Removed from pkg/controls/http/server.go if confirmed unused:
var ErrUnableToParseSpec = errors.New("unable to parse spec")
```

---

## Internal Implementation

### 1. Fix `strings.Compare` Version Check

**File:** `pkg/cmd/doctor/checks.go:19`

```go
// Before:
import "strings"

func checkGoVersion() error {
    // ...
    if strings.Compare(version, "go1.22") >= 0 {
        return nil
    }
    // ...
}

// After:
import goversion "go/version"

func checkGoVersion() error {
    // ...
    if goversion.Compare(version, "go1.22") >= 0 {
        return nil
    }
    // ...
}
```

The `go/version` package understands Go's version numbering scheme where `go1.22` > `go1.9`, unlike lexicographic comparison where `"go1.9"` > `"go1.22"` because `'9'` > `'2'`.

### 2. Remove Deprecated `PreferServerCipherSuites`

**File:** `pkg/controls/http/server.go:43`

```go
// Before:
TLSConfig: &tls.Config{
    PreferServerCipherSuites: true,
    // ...
}

// After:
TLSConfig: &tls.Config{
    // PreferServerCipherSuites removed: deprecated in Go 1.22, now a no-op.
    // Go automatically prefers server cipher suites.
    // ...
}
```

### 3. Wire Unused `ctx` in HTTP `NewServer`

**File:** `pkg/controls/http/server.go:28`

```go
// Before:
func NewServer(ctx context.Context, cfg config.Containable) *http.Server {
    srv := &http.Server{
        Addr:         fmt.Sprintf(":%d", cfg.GetInt("server.port")),
        // ... ctx is never used
    }
    return srv
}

// After:
func NewServer(ctx context.Context, cfg config.Containable) *http.Server {
    srv := &http.Server{
        Addr: fmt.Sprintf(":%d", cfg.GetInt("server.http.port")),
        BaseContext: func(_ net.Listener) context.Context {
            return ctx
        },
        // ...
    }
    return srv
}
```

This ensures the parent context is available to all HTTP handlers via `r.Context()`, enabling proper cancellation propagation.

### 4. Implement `Status()` Functions

**File:** `pkg/controls/http/server.go:97`

```go
// After:
func Status(srv *http.Server) controls.StatusFunc {
    return func() error {
        if srv == nil {
            return errors.New("http server is nil")
        }
        return nil
    }
}
```

**File:** `pkg/controls/grpc/server.go:56`

```go
// After:
func Status(srv *grpc.Server) controls.StatusFunc {
    return func() error {
        if srv == nil {
            return errors.New("grpc server is nil")
        }
        return nil
    }
}
```

The signatures change to accept the server instance, which is a breaking change for callers of `controls.WithStatus(Status)`. These must be updated to `controls.WithStatus(Status(srv))`.

### 5. Document Global Registry Thread-Safety

**File:** `pkg/setup/registry.go:42`

```go
// Before:
var globalRegistry = &registry{
    commands: make(map[string]CommandFactory),
}

// After:
// globalRegistry is the package-level command registry. It is NOT safe for
// concurrent use. All Register* calls MUST happen during init() — before
// main() starts and before any goroutines are spawned. Reading from the
// registry (via Commands()) is safe after init() completes because the
// Go memory model guarantees that init() happens-before main().
var globalRegistry = &registry{
    commands: make(map[string]CommandFactory),
}
```

### 6. Fix `checkPermissions`

**File:** `pkg/cmd/doctor/checks.go:83-90`

```go
// Before:
func checkPermissions(configDir string) error {
    if configDir == "" {
        return errors.New("config directory not set")
    }
    return nil
}

// After:
func checkPermissions(configDir string) error {
    if configDir == "" {
        return errors.New("config directory not set")
    }

    info, err := os.Stat(configDir)
    if err != nil {
        if os.IsNotExist(err) {
            return errors.Wrap(err, "config directory does not exist")
        }
        return errors.Wrap(err, "unable to stat config directory")
    }

    if !info.IsDir() {
        return errors.Newf("config path %q is not a directory", configDir)
    }

    mode := info.Mode().Perm()
    // Check owner has read+write+execute on the directory
    if mode&0700 != 0700 {
        return errors.Newf(
            "config directory %q has insufficient permissions: %s (need rwx for owner)",
            configDir, mode,
        )
    }

    return nil
}
```

### 7. Separate HTTP/gRPC Port Config Keys

**File:** `pkg/controls/http/server.go:29`

```go
// Before:
Addr: fmt.Sprintf(":%d", cfg.GetInt("server.port")),

// After:
port := cfg.GetInt("server.http.port")
if port == 0 {
    port = cfg.GetInt("server.port") // fallback for backwards compatibility
}
Addr: fmt.Sprintf(":%d", port),
```

**File:** `pkg/controls/grpc/server.go:27`

```go
// Before:
lis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.GetInt("server.port")))

// After:
port := cfg.GetInt("server.grpc.port")
if port == 0 {
    port = cfg.GetInt("server.port") // fallback for backwards compatibility
}
lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
```

### 8. Remove Unused `ErrUnableToParseSpec`

**File:** `pkg/controls/http/server.go:18`

Before removal, confirm the error is unused:

```bash
grep -rn 'ErrUnableToParseSpec' --include='*.go' pkg/ internal/
```

If only the declaration is found, remove it.

### 9. Add Exit Path to Controller Error Handler Goroutine

**File:** `pkg/controls/controller.go:192-213`

```go
// Before:
go func() {
    for {
        select {
        case err := <-errCh:
            // handle error
        }
    }
}()

// After:
go func() {
    for {
        select {
        case err, ok := <-errCh:
            if !ok {
                return // channel closed, controller stopped
            }
            // handle error
        case <-ctx.Done():
            return // context cancelled, controller stopping
        }
    }
}()
```

The error channel should be closed when the controller stops, and the goroutine should also respect context cancellation as a secondary exit signal.

### 10. Apply Structured Logging

**File:** `pkg/controls/controller.go:184`

```go
// Before:
c.logger.Errorf("error starting %s: %v", name, err)

// After:
c.logger.Error("control start failed", "control", name, "error", err)
```

**File:** `pkg/controls/controller.go:205`

```go
// Before:
c.logger.Errorf("error from %s: %v", name, err)

// After:
c.logger.Error("control error", "control", name, "error", err)
```

**File:** `pkg/controls/http/server.go:91`

```go
// Before:
logger.Infof("http server listening on %s", srv.Addr)

// After:
logger.Info("http server listening", "addr", srv.Addr)
```

Formatted strings are retained where the audience is a human at a terminal (e.g., doctor command output, CLI banners). Structured key-value pairs are used where the audience is log aggregators (e.g., server lifecycle events, error reporting).

---

## Project Structure

```
pkg/cmd/doctor/
├── checks.go          <- MODIFIED: fix version comparison, fix checkPermissions

pkg/controls/
├── controller.go      <- MODIFIED: add goroutine exit path, structured logging

pkg/controls/http/
├── server.go          <- MODIFIED: remove PreferServerCipherSuites, wire ctx,
│                         implement Status(), separate port config, remove unused
│                         error, structured logging

pkg/controls/grpc/
├── server.go          <- MODIFIED: implement Status(), separate port config

pkg/setup/
├── registry.go        <- MODIFIED: add thread-safety documentation
```

---

## Testing Strategy

### Unit Tests

| Test | Scenario |
|------|----------|
| `TestCheckGoVersion_Correctness` | Verify `go1.9` is correctly identified as < `go1.22` |
| `TestCheckGoVersion_ValidVersions` | Verify `go1.22`, `go1.23`, `go1.24` all pass |
| `TestCheckGoVersion_OldVersions` | Verify `go1.21`, `go1.20`, `go1.9` all fail |
| `TestNewServer_BaseContext` | Verify the provided context is accessible in handlers via `r.Context()` |
| `TestNewServer_NoPreferServerCipherSuites` | Verify TLS config does not set the deprecated field |
| `TestHTTPStatus_NilServer` | Verify `Status(nil)` returns error |
| `TestHTTPStatus_ValidServer` | Verify `Status(srv)` returns nil for a valid server |
| `TestGRPCStatus_NilServer` | Verify `Status(nil)` returns error |
| `TestGRPCStatus_ValidServer` | Verify `Status(srv)` returns nil for a valid server |
| `TestCheckPermissions_EmptyDir` | Verify empty config dir returns error |
| `TestCheckPermissions_NonExistent` | Verify non-existent directory returns wrapped error |
| `TestCheckPermissions_NotADirectory` | Verify file path returns error |
| `TestCheckPermissions_InsufficientPerms` | Verify directory with wrong permissions returns error |
| `TestCheckPermissions_ValidDir` | Verify directory with correct permissions passes |
| `TestHTTPPortConfig_Specific` | Verify `server.http.port` is used when set |
| `TestHTTPPortConfig_Fallback` | Verify `server.port` is used as fallback |
| `TestGRPCPortConfig_Specific` | Verify `server.grpc.port` is used when set |
| `TestGRPCPortConfig_Fallback` | Verify `server.port` is used as fallback |
| `TestControllerErrorHandler_ExitsOnClose` | Verify goroutine exits when error channel is closed |
| `TestControllerErrorHandler_ExitsOnCancel` | Verify goroutine exits when context is cancelled |

### Integration Tests

| Test | Scenario |
|------|----------|
| `TestDoctorCommand_GoVersionCheck` | Run doctor command and verify version check passes on current Go |
| `TestHTTPAndGRPC_SeparatePorts` | Start both HTTP and gRPC on different ports simultaneously |

### Coverage

- Target: 90%+ for all modified files.
- All new code paths (permission checks, port fallback logic, goroutine exit) must have explicit test coverage.

---

## Backwards Compatibility

- **Port config key**: The fallback to `server.port` preserves backwards compatibility. Existing configs that only set `server.port` continue to work. Projects running both HTTP and gRPC must update to use the new specific keys.

- **`Status()` signature change**: The `Status()` functions now require a server parameter. Callers using `controls.WithStatus(http.Status)` must change to `controls.WithStatus(http.Status(srv))`. This is a breaking change but the current no-op implementation provides no value, so existing callers are not relying on specific behaviour.

- **`ErrUnableToParseSpec` removal**: If any external code references this sentinel error, removal is breaking. Confirm with grep before removing.

- **Structured logging format change**: Log messages change from formatted strings to structured key-value pairs. Any log parsing that depends on specific message formats will need updating. This is expected and desirable for machine consumption.

---

## Future Considerations

- **Health check endpoint**: The `Status()` implementations in this spec are basic nil checks. A future spec could add HTTP `/healthz` and gRPC health checking protocol support that calls through to these status functions.

- **Config validation**: The port config separation opens the door for a config validation pass at startup that catches conflicts (e.g., both ports set to the same value).

- **Registry locking**: If dynamic command registration becomes needed (e.g., plugin system), the global registry will need a `sync.RWMutex`. This spec only documents the current requirement; locking is deferred.

- **Permission checking on other platforms**: The current `os.Stat` + mode bit approach works on Unix-like systems. Windows ACL checking would require platform-specific code if GTB targets Windows.

---

## Implementation Phases

### Phase 1 -- Doctor Command Fixes
1. Replace `strings.Compare` with `go/version.Compare` in `checkGoVersion`
2. Implement proper `checkPermissions` with `os.Stat` and mode bit checking
3. Add tests for both functions

### Phase 2 -- HTTP Server Cleanup
1. Remove `PreferServerCipherSuites` from TLS config
2. Wire `ctx` via `BaseContext`
3. Change port config to `server.http.port` with fallback
4. Remove `ErrUnableToParseSpec` if confirmed unused
5. Implement `Status()` with server parameter
6. Apply structured logging
7. Add `MaxHeaderBytes` (coordinated with security hardening spec if both proceed)

### Phase 3 -- gRPC Server Cleanup
1. Change port config to `server.grpc.port` with fallback
2. Implement `Status()` with server parameter

### Phase 4 -- Controller Fixes
1. Add exit path to error handler goroutine
2. Apply structured logging to controller error messages

### Phase 5 -- Registry Documentation
1. Add thread-safety documentation to `globalRegistry`

### Phase 6 -- Verification
1. Run full test suite
2. Run linter
3. Verify all issues are addressed

---

## Verification

```bash
# Build
go build ./...

# Full test suite with race detector
go test -race ./...

# Specific packages
go test -race -cover ./pkg/cmd/doctor/...
go test -race -cover ./pkg/controls/...
go test -race -cover ./pkg/controls/http/...
go test -race -cover ./pkg/controls/grpc/...

# Lint
golangci-lint run --fix

# Verify no strings.Compare remains for version checking
grep -rn 'strings.Compare' --include='*.go' pkg/cmd/doctor/
# Should return no results

# Verify PreferServerCipherSuites is removed
grep -rn 'PreferServerCipherSuites' --include='*.go' pkg/
# Should return no results

# Verify ErrUnableToParseSpec is removed (if applicable)
grep -rn 'ErrUnableToParseSpec' --include='*.go' pkg/ internal/
# Should return no results

# Verify shared server.port is not the sole config key
grep -rn '"server\.port"' --include='*.go' pkg/controls/http/ pkg/controls/grpc/
# Should only appear in fallback logic
```
