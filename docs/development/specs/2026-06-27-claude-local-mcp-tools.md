---
title: "Tool-calling for ProviderClaudeLocal via a local MCP server"
description: "Specification for implementing ChatClient.SetTools on the claude-CLI subprocess provider (ProviderClaudeLocal) by exposing the caller's in-process Tool handlers through a short-lived localhost MCP server and passing --mcp-config / --allowedTools to the claude subprocess. Introduces a Close() method on the ChatClient interface to manage the server's lifecycle."
date: 2026-06-27
status: DRAFT
tags:
  - specification
  - chat
  - mcp
  - tools
  - claude-local
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Tool-calling for `ProviderClaudeLocal` via a local MCP server

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   27 June 2026

Status
:   DRAFT

---

## 1. Context & motivation

`ChatClient.SetTools` is implemented for every provider **except**
`ProviderClaudeLocal` (the `claude` CLI subprocess provider in
`pkg/chat/claude_local.go`), where it returns an informative error
(`claude_local.go:108-115`). The claude-API, OpenAI, Gemini, and fallback
providers all register tools and run the ReAct loop via the shared dispatch
engine in `pkg/chat/tools.go`.

The original `pkg/chat` open-source-readiness spec
(`2026-03-18-pkg-chat-open-source-readiness.md`, §`SetTools`) deferred local-mode
tool-calling to "a future spec … expose tools via a local MCP server and pass
`--mcp-config` to the subprocess." This is that spec.

The `claude` CLI runs a full agentic tool-use loop in print mode (`claude -p
… --output-format json`) and can call tools served by an MCP server registered
via `--mcp-config`. Because the caller's `Tool.Handler`s are **in-process Go
closures**, the MCP server must be reachable by the (separate) `claude` process
over a network transport — not stdio — so we run a localhost HTTP MCP server in
the host process and point the subprocess at it.

## 2. Goals / non-goals

**Goals**

- Implement `(*ClaudeLocal).SetTools` so local-mode chats can call caller-defined
  tools, with parity of behaviour (handler dispatch, panic recovery, result
  marshalling) to the other providers — by reusing `pkg/chat/tools.go`.
- Keep the tool handlers in-process (no separate plugin binary).
- Respect the project's security conventions (loopback binding, least
  privilege, redaction).

**Non-goals**

- Streaming tool output for local mode.
- Per-call token/cost accounting for tool calls (the `claude` CLI's JSON usage
  block is already best-effort; unchanged here).
- Changing tool-calling for the other providers.
- Exposing GTB cobra commands as MCP tools — that is the existing `mcp` command
  (ophis); this is a different surface (chat `Tool` closures).

## 3. Design overview (chosen approach)

```
caller ──SetTools(tools)──▶ ClaudeLocal
                              │  builds tool registry (map[string]Tool)
                              │  starts localhost MCP server (streamable HTTP,
                              │    127.0.0.1:0, per-session bearer token)
                              ▼
caller ──Chat/Ask──▶ ClaudeLocal.runClaude
                              │  writes 0600 mcp-config temp file
                              │  claude -p … --mcp-config <file>
                              │        --allowedTools "mcp__gtb__*"
                              ▼
                          claude subprocess ──HTTP(+Bearer)──▶ MCP server
                              │  agent loop calls tools                │
                              │◀──────── tool results ────────────────┘
                              ▼  (handlers run executeTool() from tools.go)
                          final JSON result ──▶ ClaudeLocal ──▶ caller
caller ──Close()──▶ ClaudeLocal shuts the MCP server down
```

### 3.1 `Close()` on the `ChatClient` interface (decided)

A new method is added to the interface (`pkg/chat/client.go`):

```go
type ChatClient interface {
    Add(ctx context.Context, prompt string) error
    Ask(ctx context.Context, question string, target any) error
    SetTools(tools []Tool) error
    Chat(ctx context.Context, prompt string) (string, error)
    Usage() Usage
    Close() error // NEW
}
```

Rationale: the MCP server is the first long-lived resource a chat client owns;
a uniform teardown hook is the idiomatic Go answer and enables a single
long-lived server reused across a `SetTools → many Chat/Ask` conversation
(rather than a per-call listener — see §7 Alternatives).

- Claude-API / OpenAI / Gemini: `Close() error { return nil }` (no-op).
- `fallbackClient.Close()`: fan out to every wrapped client, joining errors.
- `ClaudeLocal.Close()`: shut down the MCP server (idempotent; safe to call when
  no server was started).

This is a **breaking change** to a public interface (pre-1.0 → minor bump).
The mockery mock for `ChatClient` gains one hand-added method (avoid a full
`just mocks` regen — see project mockery-churn guidance). A migration note will
instruct downstreams to `defer client.Close()`.

### 3.2 The `[]Tool` → MCP server bridge (new)

A new internal helper (proposed `pkg/chat/mcptools.go`) turns the tool registry
into a running MCP server:

- Build an `mcp.Server` via the go-sdk (`github.com/modelcontextprotocol/go-sdk`,
  promoted from indirect to direct).
- For each `Tool`, register it with the lower-level `(*Server).AddTool(t, h)`,
  supplying the JSON schema directly from `Tool.Parameters` (invopop
  `*jsonschema.Schema`) rather than letting the SDK infer one. Each MCP handler
  delegates to the existing `executeTool(...)` path from `tools.go`, so dispatch,
  panic recovery, and result marshalling are identical to other providers.
- Serve over **streamable HTTP** (`mcp.NewStreamableHTTPHandler`) on a
  `127.0.0.1:0` listener (ephemeral port discovered via `listener.Addr()`, the
  `pkg/docs/serve.go` idiom). The go-sdk's built-in DNS-rebinding and
  cross-origin protections stay enabled.
- Wrap the handler in `pkg/http` `AuthMiddleware` with a per-session random
  bearer token (decided — see §4).

### 3.3 Subprocess wiring (new)

In `claude_local.go`:

- `SetTools`: store the registry, start the MCP server (fail-fast if the listener
  can't bind), record the bound URL + token. A second `SetTools` replaces tools
  and refreshes the server (parity with the "replace, not merge" semantics of the
  other providers).
- `runClaude` / `buildArgs`: when tools are registered, write a `0600` mcp-config
  temp file and append `--mcp-config <file>` and `--allowedTools "mcp__gtb__*"`
  to the argv. The temp file is removed after the call. Server name `gtb`
  (so tools are `mcp__gtb__<name>`).
- The mcp-config JSON (HTTP server form):

  ```json
  {"mcpServers":{"gtb":{"type":"http","url":"http://127.0.0.1:PORT/mcp",
    "headers":{"Authorization":"Bearer <token>"}}}}
  ```

Flags must be repassed every invocation (the `claude` CLI does not persist
`--mcp-config`/`--allowedTools` across `--resume`); this fits the existing
stateless `buildArgs` perfectly.

## 4. Security

- **Loopback only** (`127.0.0.1`), ephemeral port, go-sdk DNS-rebinding +
  cross-origin protection.
- **Per-session bearer token** (decided): a random token guards the endpoint so
  other local processes cannot invoke the caller's tool handlers during the call
  window. Token lives only in memory + the `0600` temp file.
- **`0600` mcp-config temp file, not inline `--mcp-config`** — inline JSON would
  expose the token in `ps`/process listings (documented `claude` CLI caveat).
- Redaction: `runClaude` already logs argv at DEBUG; ensure the `--mcp-config`
  value is the file path (no token) and that nothing logs the token.
- `--allowedTools "mcp__gtb__*"` scopes pre-authorisation to exactly our server.

## 5. Testing strategy

- **Unit (no claude binary):** start the bridge server, connect a go-sdk MCP
  *client* over HTTP, call a registered tool, assert the underlying
  `Tool.Handler` ran and the result/schema round-trips. Assert auth rejects a
  missing/wrong bearer token. Assert `Close()` stops the listener (port no longer
  accepts) and is idempotent.
- **Unit (subprocess seam):** use the existing `Config.ExecCommand` fake to assert
  `buildArgs` emits `--mcp-config <file>` + `--allowedTools` only when tools are
  set, and that the temp file is `0600` and removed after the call.
- **E2E (gated):** behind the existing integration gate + a real authenticated
  `claude` binary, a scenario where the model must call a tool to answer.
  Desktop/credentialed; not on the default unit run.
- Maintain ≥90% coverage for the new `pkg/chat` code.

## 6. Migration impact

- `ChatClient` gains `Close()` — downstream implementers of the interface must
  add it; callers should `defer client.Close()`. Migration note under
  `docs/reference/migration/`. Ships as a minor (pre-1.0 policy).
- `github.com/modelcontextprotocol/go-sdk` promoted to a direct dependency.

## 7. Alternatives considered

- **Per-call MCP server (rejected):** start/stop the listener inside each
  `Chat`/`Ask`. Avoids the `Close()` interface change but spins an HTTP server
  up/down on every turn and churns the port/token in `--mcp-config`; wasteful for
  multi-turn loops. `Close()` is the better long-term design (§3.1).
- **stdio transport (rejected):** the `claude` subprocess's stdio is its own; it
  cannot reach our in-process closures over stdio. A stdio MCP server would have
  to be a separate spawned binary, which cannot access the caller's Go handlers.
- **Spawn a separate MCP binary (rejected):** breaks the in-process-handler goal.

## 8. Open questions

1. **`--permission-mode` value.** `--allowedTools "mcp__gtb__*"` is the core
   pre-authorisation mechanism in headless mode; whether to *also* pass an
   explicit `--permission-mode` (and which value) needs verification against the
   pinned `claude` CLI version during implementation. Default plan: rely on
   `--allowedTools` alone; add `--permission-mode` only if the CLI still prompts.
2. **Config opt-out.** Should a `Config` field allow disabling local-mode MCP
   tools (e.g. `DisableLocalMCPTools`)? Proposed: **no new required config** —
   the feature activates when `SetTools` is called with a non-empty tool set;
   add an opt-out only if a concrete need arises.
3. **Server start timing.** Start in `SetTools` (fail-fast on bind error) vs lazy
   on first `Chat`/`Ask`. Proposed: **start in `SetTools`**.
4. **Should the bridge be reusable beyond `pkg/chat`?** It is generically useful
   ("expose `[]Tool` as an MCP server"). Proposed: keep it unexported in
   `pkg/chat` for now; extract later if a second caller appears.
5. **`--max-turns` cap.** Whether to bound the agent loop for local mode.
   Proposed: leave unset (parity with other providers' step handling) unless a
   runaway risk is identified.

## 9. Work items (once approved)

1. Add `Close() error` to `ChatClient`; implement no-op for API/OpenAI/Gemini,
   fan-out for fallback; hand-add the mock method.
2. Promote `modelcontextprotocol/go-sdk` to a direct dependency.
3. Build the `[]Tool` → MCP server bridge (`pkg/chat/mcptools.go`) with schema
   translation + `executeTool` delegation + auth + loopback/ephemeral serving.
4. Implement `(*ClaudeLocal).SetTools` + `Close()` + `--mcp-config`/`--allowedTools`
   wiring in `buildArgs`/`runClaude`; `0600` temp-file lifecycle.
5. Tests (unit + gated e2e); docs (`docs/components`/`concepts` + migration note).
