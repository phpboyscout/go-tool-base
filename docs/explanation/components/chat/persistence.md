---
title: Conversation Persistence
description: Save and restore chat conversations across sessions.
date: 2026-03-18
tags: [components, chat, ai, llm]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Conversation Persistence

## Conversation Persistence

Conversation state can be saved and restored across CLI invocations using the `PersistentChatClient` interface. Discover it via type assertion (same pattern as `StreamingChatClient`):

```go
if pc, ok := client.(chat.PersistentChatClient); ok {
    snapshot, err := pc.Save()
    // ... store snapshot for later
}
```

### Supported Providers

| Provider | Persistence | Notes |
|----------|------------|-------|
| Claude | Yes | System prompt sent in the API's dedicated `system` field (not as a user turn) |
| OpenAI | Yes | System prompt preserved as first SystemMessage |
| OpenAI-Compatible | Yes | Same as OpenAI |
| Gemini | Yes | System prompt restored to both config and history |
| ClaudeLocal | No | External subprocess — no internal state to persist |

### Saving and Restoring

```go
// Save current conversation
snapshot, err := pc.Save()

// Store to filesystem (with optional encryption)
store, _ := chat.NewFileStore(afero.NewOsFs(), "~/.mytool/conversations",
    chat.WithEncryption(encryptionKey), // optional, 32-byte AES-256 key
)
store.Save(ctx, snapshot)

// Later — restore from storage
snapshot, _ := store.Load(ctx, snapshotID)
err := pc.Restore(snapshot)

// Re-register tools (handlers are not serialised)
pc.SetTools(myTools)

// Continue the conversation
response, _ := pc.Chat(ctx, "Where were we?")
```

### Snapshot Contents

| Field | Included | Notes |
|-------|----------|-------|
| Messages | Yes | Provider-specific format (opaque JSON) |
| System prompt | Yes | Restored to provider-specific location |
| Tool metadata | Yes | Name, description, parameters only |
| Tool handlers | No | Must re-register via `SetTools` after restore |
| API tokens | No | Security — never persisted |
| Model name | Yes | For reference; client uses its configured model |

### FileStore

The built-in `FileStore` persists snapshots as JSON files with optional AES-256-GCM encryption:

```go
// Unencrypted
store, err := chat.NewFileStore(fs, "/path/to/conversations")

// Encrypted (key must be exactly 32 bytes)
store, err := chat.NewFileStore(fs, "/path/to/conversations",
    chat.WithEncryption(key),
)
```

Files are written with 0600 permissions. The directory is created with 0700 if it doesn't exist.

**Operations:** `Save`, `Load`, `List` (returns summaries without loading full messages), `Delete`.

#### Provider endpoint security

Every call to `chat.New` validates `Config.BaseURL` before any credentials leave the process. A misconfigured endpoint fails fast with a typed error instead of sending an `Authorization` header to an attacker-controlled host.

Rejection rules, cheapest first:

1. **Length** — rejected if `len(BaseURL) > chat.MaxBaseURLLength` (2 KiB).
2. **Control characters** — any byte in `0x00`–`0x1F` or `0x7F` rejected.
3. **Parse failure** — `url.Parse` must succeed.
4. **Userinfo** — URLs of the form `https://user:pass@host/` rejected unconditionally. Put credentials in `Token`, not the URL.
5. **Scheme** — must be `https`. The test-only `Config.AllowInsecureBaseURL` bool permits `http` for `httptest.Server` targets; the field is tagged `json:"-"` so production config cannot enable it.
6. **Host** — the URL must include a host.
7. **Placeholders** — `example.com`, `example.net`, `example.org`, `localhost.localdomain`, and any subdomain of these, are rejected to catch scaffolding values.

`ProviderOpenAICompatible` additionally requires a non-empty `BaseURL`.

On every successful provider construction, the package logs the endpoint hostname at INFO:

```
chat provider initialised  provider=openai-compatible  endpoint_host=proxy.corp.internal
```

Hostname only — never the URL path or query, which may carry provider-specific identifiers.

Downstream tool authors accepting `BaseURL` in their own config surface should call `chat.ValidateBaseURL` at the boundary so misconfiguration surfaces early:

```go
if err := chat.ValidateBaseURL(userInput, false); err != nil {
    return fmt.Errorf("bad base URL: %w", err)
}
```

Rejections wrap `chat.ErrInvalidBaseURL` — discriminate via `errors.Is`.

#### Snapshot storage security

`FileStore` refuses to touch any path built from a snapshot identifier that is not a canonical `google/uuid` string. Two layers of defence are applied to every `Save`, `Load`, and `Delete` call:

1. **Shape validation.** The ID must match `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$` (lowercase hex, canonical 8-4-4-4-12 hyphenation). This forecloses path-traversal at the shape level — no `..`, no `/`, no `\`, no NUL bytes, no Unicode lookalikes.
2. **Path containment.** After `filepath.Clean` + `filepath.Abs`, the resolved file path is verified to lie inside the store directory via `filepath.Rel`. This is defence-in-depth against future relaxation of the regex and platform-specific path quirks.

A rejected identifier returns an error wrapping the exported sentinel `chat.ErrInvalidSnapshotID`:

```go
if err := store.Load(ctx, userSuppliedID); err != nil {
    if errors.Is(err, chat.ErrInvalidSnapshotID) {
        // user-supplied ID was not a canonical UUID
        return fmt.Errorf("bad snapshot id: %w", err)
    }
    // otherwise it's an I/O error — unknown snapshot, permission denied, etc.
    return err
}
```

If your application accepts snapshot identifiers from an external source (CLI flag, HTTP handler, queue payload), validate them at the boundary via `chat.ValidateSnapshotID` rather than deferring the check to `Save`/`Load`/`Delete`:

```go
if err := chat.ValidateSnapshotID(id); err != nil {
    // reject the request before any filesystem work happens
    return err
}
```

`List` is intentionally robust rather than strict: files in the store directory whose names do not match the canonical UUID shape are logged at DEBUG level (via the optional `chat.WithLogger` option) and skipped, so one corrupt or manually-placed file cannot break snapshot enumeration for the user.

Snapshots constructed via `chat.NewSnapshot` always receive a fresh `uuid.New()` ID, so the validator is transparent for GTB-produced snapshots.

---
