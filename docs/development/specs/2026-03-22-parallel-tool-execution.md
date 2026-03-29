---
title: "Parallel Tool Execution in ReAct Loop"
description: "Add optional parallel execution of independent tool calls within the ReAct loop to reduce latency for I/O-bound tools."
date: 2026-03-22
status: IMPLEMENTED
tags:
  - specification
  - chat
  - performance
  - concurrency
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-opus-4-6)
    role: AI drafting assistant
---

# Parallel Tool Execution in ReAct Loop

Authors
:   Matt Cockayne, Claude (claude-opus-4-6) *(AI drafting assistant)*

Date
:   22 March 2026

Status
:   DRAFT

---

## Overview

When an AI provider returns multiple tool calls in a single response step, the current ReAct loop executes them sequentially. For I/O-bound tools (HTTP requests, file reads, subprocess invocations), this creates unnecessary latency — each tool waits for the previous one to finish even though they are independent.

This specification adds **opt-in parallel tool execution** to the shared `executeTool` infrastructure. When enabled, multiple tool calls within a single ReAct step run concurrently via goroutines, bounded by a configurable concurrency limit.

### Motivation

- **Latency reduction**: If an AI returns 3 tool calls each taking 500ms, sequential execution takes ~1.5s while parallel execution takes ~500ms.
- **Natural fit**: Tool calls within a single response step are inherently independent — the AI issued them simultaneously, so there are no ordering dependencies.
- **Provider support**: OpenAI and Claude already support parallel tool calling at the API level. Gemini processes function calls sequentially but could benefit from parallel handler execution.

---

## Design Decisions

**Opt-in via Config**: Parallel execution is disabled by default to preserve existing behaviour. Enable it via `Config.ParallelTools` (bool) and control concurrency via `Config.MaxParallelTools` (int, default: 5).

**Shared helper, not per-provider**: A single `executeToolsParallel` function in `pkg/chat/tools.go` handles concurrency. Providers call it instead of looping over `executeTool` themselves. This builds on the deduplication work from the Chat Provider Deduplication spec.

**Result ordering preserved**: Results are returned in the same order as the input tool calls, regardless of completion order. This ensures deterministic conversation history.

**Context cancellation**: If any tool call fails critically (e.g., context cancelled), remaining in-flight tools are cancelled via a derived context. Non-critical tool errors (tool not found, handler error) still return error strings as conversation content — consistent with the existing `executeTool` behaviour.

**No shared state between tools**: Tool handlers already receive independent `json.RawMessage` inputs and return independent results. Parallel execution does not change this contract.

---

## Public API Changes

### Modified: `Config` struct

```go
type Config struct {
    // ... existing fields ...

    // ParallelTools enables concurrent execution of multiple tool calls
    // within a single ReAct step. Disabled by default.
    ParallelTools bool
    // MaxParallelTools limits the number of tools executing concurrently.
    // Zero means use the default (5). Only effective when ParallelTools is true.
    MaxParallelTools int
}
```

---

## Internal Implementation

### New Function: `executeToolsParallel` in `pkg/chat/tools.go`

```go
// ToolCall represents a single tool invocation request.
type ToolCall struct {
    Name  string
    Input json.RawMessage
}

// ToolResult holds the result of a single tool execution.
type ToolResult struct {
    Name   string
    Result string
}

// executeToolsParallel executes multiple tool calls concurrently, bounded by
// maxConcurrency. Results are returned in the same order as the input calls.
// If ctx is cancelled, in-flight tools are cancelled via a derived context.
func executeToolsParallel(
    ctx context.Context,
    logger *log.Logger,
    tools map[string]Tool,
    calls []ToolCall,
    maxConcurrency int,
) []ToolResult {
    if maxConcurrency <= 0 {
        maxConcurrency = 5
    }

    results := make([]ToolResult, len(calls))
    sem := make(chan struct{}, maxConcurrency)
    var wg sync.WaitGroup

    toolCtx, cancel := context.WithCancel(ctx)
    defer cancel()

    for i, call := range calls {
        wg.Add(1)
        sem <- struct{}{} // acquire semaphore

        go func(idx int, c ToolCall) {
            defer wg.Done()
            defer func() { <-sem }() // release semaphore

            result := executeTool(toolCtx, logger, tools, c.Name, c.Input)
            results[idx] = ToolResult{Name: c.Name, Result: result}
        }(i, call)
    }

    wg.Wait()
    return results
}
```

### Provider Changes

Each provider's ReAct loop gains a branch: if `ParallelTools` is enabled and multiple tool calls are present, call `executeToolsParallel` instead of the sequential loop.

#### Claude Example

```go
// In Chat(), when processing tool calls:
if len(toolUses) > 1 && c.cfg.ParallelTools {
    calls := make([]ToolCall, len(toolUses))
    for i, tu := range toolUses {
        calls[i] = ToolCall{Name: tu.Name, Input: tu.Input}
    }
    results := executeToolsParallel(ctx, c.props.Logger, c.tools, calls, c.cfg.MaxParallelTools)
    for i, r := range results {
        toolResults = append(toolResults, anthropic.NewToolResultBlock(toolUses[i].ID, r.Result, false))
    }
} else {
    // existing sequential path
}
```

Similar patterns for OpenAI and Gemini providers.

---

## Project Structure

```
pkg/chat/
├── tools.go           ← MODIFIED: add ToolCall, ToolResult, executeToolsParallel
├── tools_test.go      ← MODIFIED: add parallel execution tests
├── client.go          ← MODIFIED: add ParallelTools, MaxParallelTools to Config
├── claude.go          ← MODIFIED: use parallel path when enabled
├── openai.go          ← MODIFIED: use parallel path when enabled
├── gemini.go          ← MODIFIED: use parallel path when enabled
```

---

## Testing Strategy

| Test | Scenario |
|------|----------|
| `TestExecuteToolsParallel_SingleTool` | Single tool call → same result as sequential |
| `TestExecuteToolsParallel_MultipleTool` | Multiple tools → all results returned in order |
| `TestExecuteToolsParallel_OrderPreserved` | Slow tool first, fast tool second → results still in input order |
| `TestExecuteToolsParallel_Concurrency` | Verify semaphore bounds concurrent goroutines |
| `TestExecuteToolsParallel_ContextCancel` | Cancelled context → remaining tools see cancelled context |
| `TestExecuteToolsParallel_ToolError` | One tool errors → error string in result, others unaffected |
| `TestChat_ParallelToolsDisabled` | Default config → sequential execution (existing behaviour) |
| `TestChat_ParallelToolsEnabled` | ParallelTools=true → parallel execution path taken |

### Integration Tests

- **Parallel execution timing**: Register multiple tools with known sleep durations, execute in parallel, and assert total wall-clock time is less than the sum of individual durations (proving true parallelism).
- **Semaphore enforcement**: Set `MaxParallelTools=2`, register 5 tools that record their start/end timestamps via a shared atomic counter, and verify no more than 2 were executing concurrently at any point.
- **Context cancellation propagation**: Cancel the parent context while parallel tools are in-flight, and verify all tools receive the cancellation and the function returns promptly.
- Gate with `testutil.SkipIfNotIntegration(t, "chat")` in a dedicated `parallel_integration_test.go` file.

### E2E BDD Tests (Godog) — **Not applicable**

Parallel tool execution is an internal optimisation within the ReAct loop with no CLI surface or user-observable workflow. Concurrency semantics and ordering guarantees are best verified through the unit tests and integration tests above. See the [suitability assessment](2026-03-28-godog-bdd-strategy.md#suitability-assessment) for guidance on when Godog adds value.

### Coverage
- Target: 100% for `executeToolsParallel` function.
- Target: 90%+ for `pkg/chat/` overall.

---

## Linting

- `golangci-lint run --fix` must pass.
- No new `nolint` directives.

---

## Documentation

- Godoc for `ToolCall`, `ToolResult`, and `executeToolsParallel`.
- Update `docs/components/chat.md` with parallel tools configuration section.
- Update `Config` documentation table with new fields.

---

## Backwards Compatibility

- **No breaking changes**: Parallel execution is opt-in. Default behaviour (sequential) is preserved.
- **Thread safety**: `executeTool` is already stateless (operates on passed-in map and logger). Running it concurrently is safe as long as tool handlers themselves are safe — which is the caller's responsibility and should be documented.

---

## Future Considerations

- **Per-tool timeout**: A `Timeout` field on `Tool` could enforce per-tool deadlines within the parallel executor.
- **Tool dependency graph**: If tools ever need ordering (output of tool A feeds into tool B), a DAG-based executor could replace the simple parallel model. This is premature now.
- **Streaming + parallel**: When streaming is added, parallel tool execution could feed results back as they complete rather than waiting for all.

---

## Implementation Phases

### Phase 1 — Shared Helper
1. Add `ToolCall`, `ToolResult` types to `tools.go`
2. Implement `executeToolsParallel` with semaphore-bounded goroutines
3. Add comprehensive tests

### Phase 2 — Provider Integration
1. Add `ParallelTools` and `MaxParallelTools` to `Config`
2. Update Claude ReAct loop to use parallel path when enabled
3. Update OpenAI ReAct loop
4. Update Gemini ReAct loop

### Phase 3 — Documentation
1. Update `docs/components/chat.md`
2. Add configuration examples

---

## Verification

```bash
go build ./...
go test -race ./pkg/chat/...
go test ./...
golangci-lint run --fix

# Verify parallel function exists
grep -n 'func executeToolsParallel' pkg/chat/tools.go
# Should return one result

# Verify new config fields
grep -n 'ParallelTools' pkg/chat/client.go
# Should return field definitions
```
