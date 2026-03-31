---
title: Persist Chat Conversations
description: How to save and restore AI chat conversations across CLI invocations using snapshots and the FileStore.
date: 2026-03-31
tags: [how-to, chat, persistence, ai, conversations]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Persist Chat Conversations

By default, chat conversation history is lost when a `ChatClient` is destroyed. The persistence feature lets you save conversation state as snapshots and restore them later — enabling multi-turn conversations across CLI invocations.

This guide walks through:

1. Checking if your provider supports persistence
2. Saving a conversation snapshot
3. Storing snapshots with the FileStore
4. Restoring a conversation
5. Adding encryption for sensitive conversations

---

## Step 1: Check Provider Support

Not all providers support persistence. Use a type assertion to check:

```go
client, _ := chat.New(ctx, props, cfg)

pc, ok := client.(chat.PersistentChatClient)
if !ok {
    log.Info("Provider does not support persistence")
    return
}
```

Claude, OpenAI, OpenAI-compatible, and Gemini all support persistence. ClaudeLocal does not (it delegates to an external subprocess with no accessible state).

---

## Step 2: Save a Conversation

After a multi-turn conversation, save the current state:

```go
// Have a conversation
client.Chat(ctx, "What is Go's concurrency model?")
client.Chat(ctx, "How do channels differ from mutexes?")

// Save the state
pc := client.(chat.PersistentChatClient)
snapshot, err := pc.Save()
if err != nil {
    return fmt.Errorf("saving conversation: %w", err)
}

// snapshot.ID is a UUID that uniquely identifies this snapshot
fmt.Println("Saved conversation:", snapshot.ID)
```

The snapshot captures:
- Full message history (in the provider's native format)
- System prompt
- Tool metadata (names, descriptions, parameter schemas)
- Timestamp and version

It does **not** capture API tokens (security) or tool handler functions (not serialisable).

---

## Step 3: Store with FileStore

Use `FileStore` to persist snapshots to the filesystem:

```go
store, err := chat.NewFileStore(props.FS, filepath.Join(configDir, "conversations"))
if err != nil {
    return err
}

// Save
if err := store.Save(ctx, snapshot); err != nil {
    return fmt.Errorf("storing snapshot: %w", err)
}

// List all saved conversations
summaries, _ := store.List(ctx)
for _, s := range summaries {
    fmt.Printf("  %s  %s  %s  (%d messages)\n",
        s.ID, s.Provider, s.Model, s.MessageCount)
}

// Load a specific conversation
loaded, _ := store.Load(ctx, summaries[0].ID)

// Delete when no longer needed
store.Delete(ctx, summaries[0].ID)
```

Files are stored as `<id>.json` with 0600 permissions. The directory is created with 0700 if it doesn't exist.

---

## Step 4: Restore a Conversation

Create a new client and restore the snapshot:

```go
// Create a fresh client (same provider as the snapshot)
client, _ := chat.New(ctx, props, cfg)
pc := client.(chat.PersistentChatClient)

// Restore the conversation state
if err := pc.Restore(loaded); err != nil {
    return fmt.Errorf("restoring conversation: %w", err)
}

// Re-register tools if the conversation used them
pc.SetTools(myTools)

// Continue where you left off
response, _ := pc.Chat(ctx, "Can you summarise what we discussed?")
```

!!! warning "Provider must match"
    The snapshot's provider must match the client's provider. Restoring a Claude snapshot into an OpenAI client returns an error.

!!! warning "Re-register tool handlers"
    Tool handler functions are not serialised. After restoring, call `SetTools` with the same tool definitions including their handlers. The AI will remember the tool calls from the conversation history, but the handlers need to be live for new tool calls.

---

## Step 5: Add Encryption

For conversations containing sensitive content, enable AES-256-GCM encryption:

```go
// Key must be exactly 32 bytes
key := []byte("your-32-byte-encryption-key-here!")

store, err := chat.NewFileStore(props.FS, dir, chat.WithEncryption(key))
if err != nil {
    return err // returns error if key is not 32 bytes
}

// Save and Load work identically — encryption is transparent
store.Save(ctx, snapshot)
loaded, _ := store.Load(ctx, snapshot.ID)
```

When encryption is enabled:
- Snapshot JSON is encrypted before writing to disk
- Raw file contents are indistinguishable from random data
- Loading with the wrong key returns an error
- The nonce is randomly generated per save and prepended to the ciphertext

!!! tip "Key management"
    The framework does not store or manage encryption keys. You are responsible for secure key storage — consider using the OS keychain, environment variables, or a secrets manager.

---

## Complete Example

```go
func resumableChat(ctx context.Context, p *props.Props, conversationID string) error {
    cfg := chat.Config{
        Provider:     chat.ProviderClaude,
        SystemPrompt: "You are a helpful coding assistant.",
    }

    client, err := chat.New(ctx, p, cfg)
    if err != nil {
        return err
    }

    pc, ok := client.(chat.PersistentChatClient)
    if !ok {
        return errors.New("provider does not support persistence")
    }

    store, err := chat.NewFileStore(p.FS, filepath.Join(setup.GetDefaultConfigDir(p.FS, p.Tool.Name), "conversations"))
    if err != nil {
        return err
    }

    // Restore previous conversation if ID provided
    if conversationID != "" {
        snapshot, err := store.Load(ctx, conversationID)
        if err != nil {
            return fmt.Errorf("loading conversation: %w", err)
        }
        if err := pc.Restore(snapshot); err != nil {
            return fmt.Errorf("restoring conversation: %w", err)
        }
        p.Logger.Info("Resumed conversation", "id", conversationID)
    }

    // Chat
    response, err := pc.Chat(ctx, "Tell me about error handling in Go")
    if err != nil {
        return err
    }
    fmt.Println(response)

    // Save for next time
    snapshot, err := pc.Save()
    if err != nil {
        return err
    }
    if err := store.Save(ctx, snapshot); err != nil {
        return err
    }

    p.Logger.Info("Conversation saved", "id", snapshot.ID)
    return nil
}
```

---

## Related Documentation

- [Chat Component](../components/chat.md) — full chat client documentation
- [AI Integration](ai-integration.md) — setting up AI providers
- [AI Tool Calling](ai-tool-calling.md) — configuring tools for AI
- [Chat Persistence Specification](../development/specs/2026-03-26-chat-conversation-persistence.md) — design spec
