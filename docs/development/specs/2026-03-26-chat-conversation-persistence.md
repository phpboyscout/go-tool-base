---
title: "Chat Conversation Persistence Specification"
description: "Add serialization and deserialization of ChatClient conversation state to enable persistence and resumption across sessions."
date: 2026-03-26
status: IMPLEMENTED
tags:
  - specification
  - chat
  - persistence
  - feature
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-opus-4-6)
    role: AI drafting assistant
---

# Chat Conversation Persistence Specification

Authors
:   Matt Cockayne, Claude (claude-opus-4-6) *(AI drafting assistant)*

Date
:   26 March 2026

Status
:   DRAFT

---

## Overview

`ChatClient` instances in `pkg/chat/` maintain conversation history in memory. When a process crashes, is restarted, or a CLI session ends, the entire conversation context is lost. For tools that use multi-turn AI conversations (debugging assistants, interactive code reviewers, operational runbooks), this means users must re-establish context from scratch after any interruption.

This specification adds a `PersistentChatClient` interface that enables serialization and deserialization of conversation state. A provider-agnostic snapshot format captures messages, tool configurations, and metadata. Storage is abstracted behind a `ConversationStore` interface with a filesystem implementation provided out of the box. Sensitive content (API keys, tokens in conversation) can be encrypted at rest.

---

## Design Decisions

**Separate `PersistentChatClient` interface**: Not all consumers need persistence, and not all providers may support state export equally well. Providers that support persistence implement `PersistentChatClient` in addition to `ChatClient`. The type assertion pattern (matching the streaming spec) lets consumers discover capability at runtime.

**Provider-agnostic snapshot format**: Each provider stores messages in different native formats (Anthropic messages, OpenAI chat completions, Gemini content parts). Rather than forcing a lowest-common-denominator format, the snapshot stores provider-specific message data as opaque JSON alongside common metadata. This preserves full fidelity and avoids lossy conversion.

**ConversationStore interface**: Storage is abstracted so consumers can use the filesystem, a database, or a remote service. The default implementation uses `afero.Fs` for filesystem storage, keeping it testable with in-memory filesystems.

**Optional encryption**: Conversations may contain sensitive information (API keys, proprietary code, PII). An optional `EncryptionProvider` interface allows consumers to encrypt snapshots at rest. The default filesystem store supports this as an opt-in feature. No encryption is applied by default -- the consumer must explicitly provide a key.

**Snapshot, not journaling**: The persistence model is snapshot-based (save/load the full conversation state) rather than append-only journaling. Snapshots are simpler to implement, reason about, and recover from corruption. The trade-off is that partial recovery (replaying to a specific point) is not supported in this iteration.

**ClaudeLocal excluded**: `ClaudeLocal` uses a subprocess and does not expose its internal message history. It cannot implement `PersistentChatClient`. This is consistent with the streaming spec's treatment of `ClaudeLocal`.

---

## Public API Changes

### New Interface: `PersistentChatClient`

```go
// PersistentChatClient extends ChatClient with conversation persistence.
// Implementations that support persistence implement this interface
// in addition to ChatClient.
type PersistentChatClient interface {
    ChatClient

    // Save serializes the current conversation state into a Snapshot.
    Save() (*Snapshot, error)

    // Restore loads conversation state from a Snapshot, replacing the
    // current conversation history. The snapshot must have been created
    // by the same provider type.
    Restore(snapshot *Snapshot) error
}
```

### Snapshot Type

```go
// Snapshot represents a serializable conversation state.
type Snapshot struct {
    // ID is a unique identifier for this snapshot.
    ID string `json:"id"`
    // Provider identifies which provider created this snapshot.
    Provider Provider `json:"provider"`
    // Model is the model that was in use.
    Model string `json:"model"`
    // SystemPrompt is the system prompt that was configured.
    SystemPrompt string `json:"system_prompt"`
    // Messages contains the provider-specific message history as JSON.
    Messages json.RawMessage `json:"messages"`
    // Tools contains the tool definitions that were configured.
    Tools []ToolSnapshot `json:"tools,omitempty"`
    // Metadata contains arbitrary key-value pairs for consumer use.
    Metadata map[string]string `json:"metadata,omitempty"`
    // CreatedAt is when the snapshot was created.
    CreatedAt time.Time `json:"created_at"`
    // Version is the snapshot format version for forward compatibility.
    Version int `json:"version"`
}

// ToolSnapshot is a serializable representation of a Tool (excluding the Handler).
type ToolSnapshot struct {
    Name        string             `json:"name"`
    Description string             `json:"description"`
    Parameters  *jsonschema.Schema `json:"parameters"`
}
```

### ConversationStore Interface

```go
// ConversationStore provides persistence for conversation snapshots.
type ConversationStore interface {
    // Save persists a snapshot. If a snapshot with the same ID exists,
    // it is overwritten.
    Save(ctx context.Context, snapshot *Snapshot) error
    // Load retrieves a snapshot by ID.
    Load(ctx context.Context, id string) (*Snapshot, error)
    // List returns the IDs and metadata of all stored snapshots.
    List(ctx context.Context) ([]SnapshotSummary, error)
    // Delete removes a snapshot by ID.
    Delete(ctx context.Context, id string) error
}

// SnapshotSummary contains metadata about a stored snapshot without
// the full message history.
type SnapshotSummary struct {
    ID           string    `json:"id"`
    Provider     Provider  `json:"provider"`
    Model        string    `json:"model"`
    CreatedAt    time.Time `json:"created_at"`
    MessageCount int       `json:"message_count"`
}
```

### Filesystem Store

```go
// NewFileStore creates a ConversationStore backed by the filesystem.
// Snapshots are stored as JSON files in the given directory.
func NewFileStore(fs afero.Fs, dir string, opts ...FileStoreOption) ConversationStore

// FileStoreOption configures the filesystem store.
type FileStoreOption func(*fileStoreConfig)

// WithEncryption enables AES-256-GCM encryption for stored snapshots.
// The key must be exactly 32 bytes.
func WithEncryption(key []byte) FileStoreOption
```

### Usage

```go
// Save a conversation
client, _ := chat.New(ctx, props, cfg)
// ... interact with client ...

if pc, ok := client.(chat.PersistentChatClient); ok {
    snapshot, err := pc.Save()
    if err != nil { /* handle */ }

    store := chat.NewFileStore(props.FS, "~/.config/mytool/conversations")
    err = store.Save(ctx, snapshot)
}

// Resume a conversation
store := chat.NewFileStore(props.FS, "~/.config/mytool/conversations")
snapshot, err := store.Load(ctx, "conversation-id")

client, _ := chat.New(ctx, props, chat.Config{Provider: snapshot.Provider, Model: snapshot.Model})
if pc, ok := client.(chat.PersistentChatClient); ok {
    err = pc.Restore(snapshot)
    // Continue the conversation
    response, err := client.Chat(ctx, "Where were we?")
}
```

---

## Internal Implementation

### Claude Save/Restore

```go
func (c *Claude) Save() (*Snapshot, error) {
    messagesJSON, err := json.Marshal(c.messages)
    if err != nil {
        return nil, errors.Wrap(err, "failed to serialize claude messages")
    }

    return &Snapshot{
        ID:           uuid.New().String(),
        Provider:     ProviderClaude,
        Model:        string(c.model),
        SystemPrompt: c.system,
        Messages:     messagesJSON,
        Tools:        snapshotTools(c.tools),
        CreatedAt:    time.Now(),
        Version:      1,
    }, nil
}

func (c *Claude) Restore(snapshot *Snapshot) error {
    if snapshot.Provider != ProviderClaude {
        return errors.Newf("snapshot provider mismatch: expected %s, got %s", ProviderClaude, snapshot.Provider)
    }

    var messages []anthropic.MessageParam
    if err := json.Unmarshal(snapshot.Messages, &messages); err != nil {
        return errors.Wrap(err, "failed to deserialize claude messages")
    }

    c.messages = messages
    c.system = snapshot.SystemPrompt

    return nil
}
```

OpenAI and Gemini implementations follow the same pattern, marshalling/unmarshalling their native message types.

### FileStore Implementation

```go
type fileStore struct {
    fs  afero.Fs
    dir string
    key []byte // nil means no encryption
}

func (s *fileStore) Save(ctx context.Context, snapshot *Snapshot) error {
    data, err := json.MarshalIndent(snapshot, "", "  ")
    if err != nil {
        return errors.Wrap(err, "failed to serialize snapshot")
    }

    if s.key != nil {
        data, err = encrypt(s.key, data)
        if err != nil {
            return errors.Wrap(err, "failed to encrypt snapshot")
        }
    }

    path := filepath.Join(s.dir, snapshot.ID+".json")
    return afero.WriteFile(s.fs, path, data, 0o600)
}

func (s *fileStore) Load(ctx context.Context, id string) (*Snapshot, error) {
    path := filepath.Join(s.dir, id+".json")
    data, err := afero.ReadFile(s.fs, path)
    if err != nil {
        return nil, errors.Wrap(err, "failed to read snapshot file")
    }

    if s.key != nil {
        data, err = decrypt(s.key, data)
        if err != nil {
            return nil, errors.Wrap(err, "failed to decrypt snapshot")
        }
    }

    var snapshot Snapshot
    if err := json.Unmarshal(data, &snapshot); err != nil {
        return nil, errors.Wrap(err, "failed to deserialize snapshot")
    }

    return &snapshot, nil
}
```

### Encryption

AES-256-GCM with a random nonce prepended to the ciphertext. The nonce is extracted on decryption.

```go
func encrypt(key, plaintext []byte) ([]byte, error) {
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, err
    }

    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }

    nonce := make([]byte, gcm.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return nil, err
    }

    return gcm.Seal(nonce, nonce, plaintext, nil), nil
}
```

---

## Project Structure

```
pkg/chat/
├── client.go          <- MODIFIED: PersistentChatClient interface, Snapshot, ToolSnapshot types
├── persistence.go     <- NEW: ConversationStore, SnapshotSummary, snapshotTools helper
├── filestore.go       <- NEW: NewFileStore, FileStoreOption, WithEncryption, encrypt/decrypt
├── claude.go          <- MODIFIED: Save/Restore implementation
├── openai.go          <- MODIFIED: Save/Restore implementation
├── gemini.go          <- MODIFIED: Save/Restore implementation
├── claude_local.go    <- UNCHANGED: does not implement PersistentChatClient
├── persistence_test.go  <- NEW: persistence interface tests
├── filestore_test.go    <- NEW: store and encryption tests
```

---

## Testing Strategy

| Test | Scenario |
|------|----------|
| `TestClaude_SaveRestore` | Save state, create new client, restore, verify conversation continues |
| `TestOpenAI_SaveRestore` | Same for OpenAI |
| `TestGemini_SaveRestore` | Same for Gemini |
| `TestRestore_ProviderMismatch` | Restoring a Claude snapshot into an OpenAI client returns error |
| `TestSnapshot_Serialization` | Snapshot round-trips through JSON marshal/unmarshal |
| `TestSnapshot_ToolsExcludeHandlers` | ToolSnapshot does not contain Handler function references |
| `TestFileStore_SaveLoad` | Save then load, verify equality |
| `TestFileStore_List` | Multiple saves, list returns all summaries |
| `TestFileStore_Delete` | Save, delete, load returns not-found error |
| `TestFileStore_Overwrite` | Save with same ID overwrites previous snapshot |
| `TestFileStore_WithEncryption` | Save encrypted, load decrypted, verify content |
| `TestFileStore_EncryptionKeyMismatch` | Load with wrong key returns error |
| `TestFileStore_DirectoryCreation` | Store creates directory if it does not exist |
| `TestFileStore_Permissions` | Snapshot files are created with 0600 permissions |
| `TestClaudeLocal_NotPersistent` | Type assertion for PersistentChatClient fails |
| `TestSnapshot_Version` | Version field is set to 1 |

### Test Helpers

All filesystem tests use `afero.NewMemMapFs()` for isolation.

### Integration Tests

- **File store round-trip on real filesystem**: Use a temp directory (not afero MemMapFs), save a snapshot via `FileStore`, load it back, and verify byte-level equality. Confirms OS-level file permissions, encoding, and path handling.
- **Encrypted round-trip**: Save an encrypted snapshot to a real file, read the raw bytes and verify they are not plaintext, then load via `FileStore` with the correct key and verify content matches.
- **Multi-provider save/restore**: Create snapshots from Claude, OpenAI, and Gemini clients, save all to a single `FileStore`, list and verify summaries, then restore each and confirm provider metadata matches.
- **Corrupted file handling**: Write garbage bytes to a snapshot file path, attempt to load, and verify a descriptive error is returned without panic.
- Gate with `testutil.SkipIfNotIntegration(t, "chat")` in a dedicated `persistence_integration_test.go` file.

### E2E BDD Tests (Godog) — **Not applicable**

Conversation persistence is a library-level feature with no CLI surface. Serialisation, encryption, and store operations are best verified through the unit tests and integration tests above. See the [suitability assessment](2026-03-28-godog-bdd-strategy.md#suitability-assessment) for guidance on when Godog adds value.

### Coverage

- Target: 90%+ for `pkg/chat/` including persistence paths.

---

## Linting

- `golangci-lint run --fix` must pass.
- No new `nolint` directives.

---

## Documentation

- Godoc for `PersistentChatClient`, `Snapshot`, `ConversationStore`, `NewFileStore`, and all options.
- Document the type assertion pattern for discovering persistence support.
- Update `docs/components/chat.md` with persistence usage examples.
- Document encryption considerations and key management guidance.

---

## Backwards Compatibility

- **No breaking changes**. `ChatClient` interface is unchanged. Persistence is opt-in via type assertion.
- Providers that implement `PersistentChatClient` still satisfy `ChatClient`.
- `ClaudeLocal` is explicitly excluded -- documented, not a limitation.
- The `Snapshot.Version` field enables future format evolution without breaking existing stored snapshots.

---

## Future Considerations

- **Snapshot migration**: When the snapshot format version changes, provide automatic migration from older versions.
- **Conversation branching**: Save at a point, restore, and diverge -- creating conversation branches for exploring different approaches.
- **Cloud storage backends**: Implement `ConversationStore` for S3, GCS, or other cloud storage for team-shared conversation history.
- **Automatic checkpointing**: Periodically auto-save conversation state at configurable intervals as a crash recovery mechanism.
- **Message-level encryption**: Encrypt individual messages rather than the entire snapshot, allowing metadata queries on encrypted stores.
- **Token counting in summaries**: Include approximate token count in `SnapshotSummary` to help consumers estimate context window usage before restoring.

---

## Implementation Phases

### Phase 1 -- Interface and Types
1. Define `PersistentChatClient` interface
2. Define `Snapshot` and `ToolSnapshot` types
3. Define `ConversationStore` and `SnapshotSummary`
4. Add `snapshotTools` helper to convert `[]Tool` to `[]ToolSnapshot`
5. Add compile-time checks for provider implementations

### Phase 2 -- Provider Implementations
1. Implement `Save`/`Restore` for Claude
2. Implement `Save`/`Restore` for OpenAI
3. Implement `Save`/`Restore` for Gemini
4. Add provider mismatch validation in `Restore`

### Phase 3 -- FileStore
1. Implement `fileStore` with `Save`, `Load`, `List`, `Delete`
2. Implement AES-256-GCM encryption/decryption
3. Implement `WithEncryption` option
4. Ensure directory creation and 0600 file permissions

### Phase 4 -- Tests
1. Unit tests for each provider's Save/Restore
2. Unit tests for FileStore operations
3. Encryption round-trip tests
4. Error case tests (mismatch, corruption, missing files)
5. Run with race detector

---

## Verification

```bash
go build ./...
go test -race ./pkg/chat/...
go test ./...
golangci-lint run --fix

# Verify interface exists
grep -n 'PersistentChatClient' pkg/chat/client.go

# Verify provider implementations
grep -n 'func.*Save\|func.*Restore' pkg/chat/claude.go pkg/chat/openai.go pkg/chat/gemini.go

# Verify store
grep -n 'ConversationStore' pkg/chat/persistence.go
```
