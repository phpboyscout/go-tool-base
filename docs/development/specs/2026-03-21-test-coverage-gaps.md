---
title: "Test Coverage Gaps Specification"
description: "Prioritised test plan to achieve 90%+ coverage across pkg/ packages, focusing on pkg/version, pkg/vcs, pkg/chat providers, and pkg/controls servers."
date: 2026-03-21
status: IMPLEMENTED
tags:
  - specification
  - testing
  - coverage
  - quality
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-opus-4-6)
    role: AI drafting assistant
---

# Test Coverage Gaps Specification

Authors
:   Matt Cockayne, Claude (claude-opus-4-6) *(AI drafting assistant)*

Date
:   21 March 2026

Status
:   IMPLEMENTED

---

## Overview

Code review identified several packages with insufficient test coverage. While some packages have strong coverage, others — particularly those involving external service interactions — have minimal or no tests. This spec defines a prioritised plan to achieve 90%+ coverage across all `pkg/` packages.

### Current Coverage Gaps

| Package | Estimated Coverage | Gap |
|---------|-------------------|-----|
| `pkg/version` | Low | Pure functions — easy to test, no mocking needed |
| `pkg/vcs/gitlab` | Low | HTTP client interactions — needs mock server |
| `pkg/vcs/release` | Low | Provider abstraction layer — needs interface mocks |
| `pkg/chat` (providers) | Medium | `Ask()`/`Chat()` paths — needs mock HTTP servers |
| `pkg/controls/grpc` | Low | gRPC server lifecycle — needs test server setup |
| `pkg/controls/http` | Low | HTTP server lifecycle — needs `httptest.Server` |
| `pkg/docs` | Medium | MkDocs parsing edge cases |

---

## Design Decisions

**Mock HTTP servers over recorded responses**: Use `httptest.Server` and custom handlers rather than recorded response fixtures. This allows testing error paths, timeouts, and edge cases that recordings cannot cover.

**Table-driven tests**: All test suites use table-driven patterns for consistency and easy extension.

**No external service dependencies**: All tests must run offline. Mock all HTTP, gRPC, and filesystem interactions.

**Race detector mandatory**: All new tests must pass with `-race` since many of these packages involve concurrency.

---

## Public API Changes

None. This spec adds tests only.

---

## Internal Implementation

### Priority 1: `pkg/version`

Pure functions with no external dependencies — highest value per effort.

```go
func TestCompareVersions(t *testing.T) {
    tests := []struct {
        name     string
        a, b     string
        expected int
    }{
        {"equal", "1.0.0", "1.0.0", 0},
        {"a greater major", "2.0.0", "1.0.0", 1},
        {"b greater minor", "1.0.0", "1.1.0", -1},
        {"a greater patch", "1.0.2", "1.0.1", 1},
        {"prerelease vs release", "1.0.0-beta", "1.0.0", -1},
        {"v prefix", "v1.0.0", "1.0.0", 0},
        {"empty a", "", "1.0.0", -1},
        {"empty b", "1.0.0", "", 1},
        {"both empty", "", "", 0},
        {"invalid a", "not-a-version", "1.0.0", -1},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := CompareVersions(tt.a, tt.b)
            assert.Equal(t, tt.expected, result)
        })
    }
}

func TestIsDevelopment(t *testing.T) {
    tests := []struct {
        name    string
        version string
        isDev   bool
    }{
        {"dev version", "dev", true},
        {"development", "development", true},
        {"release", "1.2.3", false},
        {"empty", "", true},
    }
    // ...
}
```

### Priority 2: `pkg/vcs/gitlab`

Mock HTTP server for GitLab API interactions.

```go
func TestGitLabClient_CreateRelease(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        switch {
        case r.Method == "POST" && strings.Contains(r.URL.Path, "/releases"):
            w.WriteHeader(http.StatusCreated)
            json.NewEncoder(w).Encode(map[string]string{"tag_name": "v1.0.0"})
        default:
            w.WriteHeader(http.StatusNotFound)
        }
    }))
    defer server.Close()

    client := NewGitLabClient(server.URL, "test-token")
    err := client.CreateRelease(context.Background(), "v1.0.0", "Release notes")
    assert.NoError(t, err)
}

func TestGitLabClient_CreateRelease_Unauthorized(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusUnauthorized)
    }))
    defer server.Close()

    client := NewGitLabClient(server.URL, "bad-token")
    err := client.CreateRelease(context.Background(), "v1.0.0", "notes")
    assert.Error(t, err)
}
```

### Priority 3: `pkg/vcs/release`

Test the provider abstraction with mock implementations.

```go
type mockReleaseProvider struct {
    createFunc func(ctx context.Context, tag, notes string) error
    latestFunc func(ctx context.Context) (string, error)
}

func TestReleaseManager_Create(t *testing.T) {
    tests := []struct {
        name      string
        provider  mockReleaseProvider
        tag       string
        expectErr bool
    }{
        {
            name: "success",
            provider: mockReleaseProvider{
                createFunc: func(ctx context.Context, tag, notes string) error { return nil },
            },
            tag: "v1.0.0",
        },
        {
            name: "provider error",
            provider: mockReleaseProvider{
                createFunc: func(ctx context.Context, tag, notes string) error {
                    return errors.New("API error")
                },
            },
            tag:       "v1.0.0",
            expectErr: true,
        },
    }
    // ...
}
```

### Priority 4: `pkg/chat` Providers

Mock HTTP servers for each provider's API.

```go
func TestClaude_Ask_Success(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        resp := map[string]any{
            "content": []map[string]string{{"type": "text", "text": `{"answer": "42"}`}},
            "stop_reason": "end_turn",
        }
        json.NewEncoder(w).Encode(resp)
    }))
    defer server.Close()

    client := newTestClaudeClient(t, server.URL)
    var result struct{ Answer string }
    err := client.Ask(context.Background(), "What is the answer?", &result)
    assert.NoError(t, err)
    assert.Equal(t, "42", result.Answer)
}

func TestClaude_Ask_ContextCancelled(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        time.Sleep(5 * time.Second) // simulate slow response
    }))
    defer server.Close()

    ctx, cancel := context.WithCancel(context.Background())
    cancel() // cancel immediately

    client := newTestClaudeClient(t, server.URL)
    var result string
    err := client.Ask(ctx, "question", &result)
    assert.Error(t, err)
}
```

Similar patterns for OpenAI and Gemini with their respective API response formats.

### Priority 5: `pkg/controls/grpc` and `pkg/controls/http`

```go
func TestHTTPServer_StartStop(t *testing.T) {
    ctrl := controls.NewController(context.Background())
    srv := NewHTTPServer(ctrl, ":0") // random port

    ctrl.Start()
    // Verify server is accepting connections
    // ...
    ctrl.Stop()
    // Verify server has shut down
}

func TestGRPCServer_StartStop(t *testing.T) {
    ctrl := controls.NewController(context.Background())
    srv := NewGRPCServer(ctrl, ":0")

    ctrl.Start()
    // Verify gRPC server is serving
    // ...
    ctrl.Stop()
}
```

### Priority 6: `pkg/docs`

Edge cases in MkDocs nav parsing.

```go
func TestParseNav_EmptyNav(t *testing.T) {
    result := ParseNav([]byte("nav: []"))
    assert.Empty(t, result)
}

func TestParseNav_NestedSections(t *testing.T) {
    yaml := `nav:
  - Home: index.md
  - Guide:
    - Getting Started: guide/start.md
    - Advanced:
      - Plugins: guide/advanced/plugins.md`
    result := ParseNav([]byte(yaml))
    assert.Len(t, result, 2)
    // verify nested structure
}

func TestParseNav_InvalidYAML(t *testing.T) {
    result := ParseNav([]byte("not: valid: yaml: ["))
    assert.Empty(t, result)
}
```

---

## Project Structure

```
pkg/version/
├── version_test.go        ← MODIFIED: comprehensive table-driven tests
pkg/vcs/gitlab/
├── gitlab_test.go         ← NEW/MODIFIED: mock HTTP server tests
pkg/vcs/release/
├── release_test.go        ← NEW/MODIFIED: mock provider tests
pkg/chat/
├── claude_test.go         ← MODIFIED: mock API server tests
├── openai_test.go         ← MODIFIED: mock API server tests
├── gemini_test.go         ← MODIFIED: mock API server tests
├── testhelpers_test.go    ← NEW: shared test utilities (mock servers, factories)
pkg/controls/grpc/
├── grpc_test.go           ← NEW/MODIFIED: server lifecycle tests
pkg/controls/http/
├── http_test.go           ← NEW/MODIFIED: server lifecycle tests
pkg/docs/
├── docs_test.go           ← MODIFIED: edge case tests
```

---

## Testing Strategy

### Test Categories

| Category | Packages | Approach |
|----------|----------|----------|
| Pure functions | `pkg/version` | Table-driven, no mocks |
| HTTP clients | `pkg/vcs/gitlab`, `pkg/chat/*` | `httptest.Server` with custom handlers |
| Abstractions | `pkg/vcs/release` | Interface mocks |
| Servers | `pkg/controls/grpc`, `pkg/controls/http` | Start/stop lifecycle, port 0 |
| Parsers | `pkg/docs` | Edge cases, malformed input |

### Shared Test Helpers

```go
// testhelpers_test.go in pkg/chat/
func newMockAPIServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
    t.Helper()
    server := httptest.NewServer(handler)
    t.Cleanup(server.Close)
    return server
}

func newTestClaudeClient(t *testing.T, baseURL string) ChatClient {
    t.Helper()
    // construct client pointing at mock server
}
```

### Coverage Targets

| Package | Current (est.) | Target |
|---------|---------------|--------|
| `pkg/version` | ~30% | 95%+ |
| `pkg/vcs/gitlab` | ~20% | 90%+ |
| `pkg/vcs/release` | ~30% | 90%+ |
| `pkg/chat` | ~50% | 90%+ |
| `pkg/controls/grpc` | ~10% | 80%+ |
| `pkg/controls/http` | ~10% | 80%+ |
| `pkg/docs` | ~60% | 90%+ |

### Coverage
- Overall target: 90%+ for `pkg/`.

---

## Linting

- `golangci-lint run --fix` must pass.
- No new `nolint` directives in test files.
- Test files should follow the same linting rules as production code (except `fmt.Errorf` is acceptable in tests).

---

## Documentation

- Godoc for shared test helpers explaining their purpose.
- Comments in test tables explaining non-obvious test cases.
- No user-facing documentation changes.

---

## Backwards Compatibility

- No breaking changes. Tests only.

---

## Future Considerations

- **Integration tests**: A separate `integration_test.go` build tag for tests that hit real APIs (with API keys from CI secrets).
- **Coverage CI gate**: Add a CI step that fails if coverage drops below threshold.
- **Fuzz testing**: `pkg/version/CompareVersions` and `pkg/docs/ParseNav` are good candidates for Go's native fuzzing.
- **Benchmark tests**: Chat provider response parsing could benefit from benchmarks if performance becomes a concern.

---

## Implementation Phases

### Phase 1 — Pure Functions (pkg/version)
1. Add comprehensive table-driven tests for all exported functions
2. Achieve 95%+ coverage

### Phase 2 — VCS Packages
1. Add mock HTTP server tests for `pkg/vcs/gitlab`
2. Add mock provider tests for `pkg/vcs/release`
3. Achieve 90%+ coverage for both

### Phase 3 — Chat Providers
1. Create shared test helpers (`testhelpers_test.go`)
2. Add mock API server tests for Claude, OpenAI, Gemini
3. Test error paths, timeouts, context cancellation
4. Achieve 90%+ coverage

### Phase 4 — Controls Servers
1. Add lifecycle tests for `pkg/controls/http`
2. Add lifecycle tests for `pkg/controls/grpc`
3. Achieve 80%+ coverage

### Phase 5 — Docs Package
1. Add edge case tests for nav parsing
2. Add malformed input tests
3. Achieve 90%+ coverage

---

## Verification

```bash
# Full test suite with race detector
go test -race ./...

# Coverage report
go test -coverprofile=coverage.out ./pkg/...
go tool cover -func=coverage.out | tail -1  # total coverage

# Per-package coverage
go test -coverprofile=coverage.out ./pkg/version/...
go tool cover -func=coverage.out

go test -coverprofile=coverage.out ./pkg/vcs/...
go tool cover -func=coverage.out

go test -coverprofile=coverage.out ./pkg/chat/...
go tool cover -func=coverage.out

go test -coverprofile=coverage.out ./pkg/controls/...
go tool cover -func=coverage.out

# Lint
golangci-lint run --fix
```
