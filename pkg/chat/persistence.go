package chat

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/invopop/jsonschema"
)

// SnapshotVersion is the current version of the snapshot format.
// Increment when the format changes in a way that requires migration.
const SnapshotVersion = 1

// PersistentChatClient extends ChatClient with the ability to save and restore
// conversation state. Discover via type assertion (same pattern as StreamingChatClient):
//
//	if pc, ok := client.(chat.PersistentChatClient); ok {
//	    snapshot, err := pc.Save()
//	}
//
// ClaudeLocal does not implement this interface — it delegates to an external
// subprocess and has no internal message state to persist.
type PersistentChatClient interface {
	ChatClient
	// Save captures the current conversation state as an immutable snapshot.
	// The snapshot includes provider-specific messages as opaque JSON, tool
	// metadata (without handlers), and configuration. Tokens are never saved.
	Save() (*Snapshot, error)
	// Restore replaces the current conversation state with a previously saved
	// snapshot. The snapshot's Provider must match the client's provider.
	// After restore, tools must be re-registered via SetTools with live handlers.
	Restore(snapshot *Snapshot) error
}

// Snapshot is an immutable point-in-time capture of a conversation.
type Snapshot struct {
	// ID uniquely identifies this snapshot.
	ID string `json:"id"`
	// Provider identifies which chat provider created this snapshot.
	Provider Provider `json:"provider"`
	// Model is the AI model used in the conversation.
	Model string `json:"model"`
	// SystemPrompt is the system instruction active at snapshot time.
	SystemPrompt string `json:"system_prompt,omitempty"`
	// Messages contains provider-specific message history as opaque JSON.
	// The format varies by provider — do not parse or modify directly.
	Messages json.RawMessage `json:"messages"`
	// Tools captures tool metadata (name, description, parameters) without
	// handlers. After restoring, call SetTools to re-register live handlers.
	Tools []ToolSnapshot `json:"tools,omitempty"`
	// Metadata holds arbitrary key-value pairs for consumer use.
	Metadata map[string]string `json:"metadata,omitempty"`
	// CreatedAt is when this snapshot was taken.
	CreatedAt time.Time `json:"created_at"`
	// Version is the snapshot format version for forward compatibility.
	Version int `json:"version"`
}

// ToolSnapshot captures tool metadata without the handler function.
type ToolSnapshot struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Parameters  *jsonschema.Schema `json:"parameters,omitempty"`
}

// SnapshotSummary is a lightweight view of a snapshot for listing without
// loading the full message history.
type SnapshotSummary struct {
	ID           string    `json:"id"`
	Provider     Provider  `json:"provider"`
	Model        string    `json:"model"`
	CreatedAt    time.Time `json:"created_at"`
	MessageCount int       `json:"message_count"`
}

// ConversationStore persists and retrieves conversation snapshots.
type ConversationStore interface {
	// Save writes a snapshot to the store.
	Save(ctx context.Context, snapshot *Snapshot) error
	// Load retrieves a snapshot by ID.
	Load(ctx context.Context, id string) (*Snapshot, error)
	// List returns summaries of all stored snapshots.
	List(ctx context.Context) ([]SnapshotSummary, error)
	// Delete removes a snapshot by ID.
	Delete(ctx context.Context, id string) error
}

// NewSnapshot creates a Snapshot with a new UUID and the current timestamp.
func NewSnapshot(provider Provider, model, systemPrompt string, messages json.RawMessage, tools map[string]Tool, metadata map[string]string) *Snapshot {
	return &Snapshot{
		ID:           uuid.New().String(),
		Provider:     provider,
		Model:        model,
		SystemPrompt: systemPrompt,
		Messages:     messages,
		Tools:        snapshotTools(tools),
		Metadata:     metadata,
		CreatedAt:    time.Now().UTC(),
		Version:      SnapshotVersion,
	}
}

// snapshotTools converts the tool registry to serialisable snapshots,
// stripping the non-serialisable Handler function.
func snapshotTools(tools map[string]Tool) []ToolSnapshot {
	if len(tools) == 0 {
		return nil
	}

	snapshots := make([]ToolSnapshot, 0, len(tools))

	for _, t := range tools {
		snapshots = append(snapshots, ToolSnapshot{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		})
	}

	return snapshots
}
