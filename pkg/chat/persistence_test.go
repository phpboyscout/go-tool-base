package chat

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSnapshot(t *testing.T) {
	t.Parallel()

	tools := map[string]Tool{
		"search": {
			Name:        "search",
			Description: "Search the web",
			Handler:     func(_ context.Context, _ json.RawMessage) (any, error) { return nil, nil },
		},
	}

	snap := NewSnapshot(ProviderClaude, "claude-3-5-sonnet", "You are helpful.", json.RawMessage(`[]`), tools, map[string]string{"env": "test"})

	assert.NotEmpty(t, snap.ID)
	assert.Equal(t, ProviderClaude, snap.Provider)
	assert.Equal(t, "claude-3-5-sonnet", snap.Model)
	assert.Equal(t, "You are helpful.", snap.SystemPrompt)
	assert.Equal(t, SnapshotVersion, snap.Version)
	assert.NotZero(t, snap.CreatedAt)
	assert.Equal(t, "test", snap.Metadata["env"])
}

func TestSnapshotTools_ExcludeHandlers(t *testing.T) {
	t.Parallel()

	tools := map[string]Tool{
		"search": {
			Name:        "search",
			Description: "Search the web",
			Handler:     func(_ context.Context, _ json.RawMessage) (any, error) { return nil, nil },
		},
	}

	snapshots := snapshotTools(tools)

	require.Len(t, snapshots, 1)
	assert.Equal(t, "search", snapshots[0].Name)
	assert.Equal(t, "Search the web", snapshots[0].Description)

	// ToolSnapshot should serialise to JSON without any handler field
	data, err := json.Marshal(snapshots[0])
	require.NoError(t, err)
	assert.NotContains(t, string(data), "handler")
}

func TestSnapshotTools_Empty(t *testing.T) {
	t.Parallel()

	assert.Nil(t, snapshotTools(nil))
	assert.Nil(t, snapshotTools(map[string]Tool{}))
}

func TestSnapshot_Serialization(t *testing.T) {
	t.Parallel()

	original := NewSnapshot(ProviderOpenAI, "gpt-4o", "Be concise.", json.RawMessage(`[{"role":"user","content":"hi"}]`), nil, nil)

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored Snapshot
	require.NoError(t, json.Unmarshal(data, &restored))

	assert.Equal(t, original.ID, restored.ID)
	assert.Equal(t, original.Provider, restored.Provider)
	assert.Equal(t, original.Model, restored.Model)
	assert.Equal(t, original.SystemPrompt, restored.SystemPrompt)
	assert.JSONEq(t, string(original.Messages), string(restored.Messages))
	assert.Equal(t, original.Version, restored.Version)
}

func TestSnapshot_Version(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 1, SnapshotVersion)
}

func TestClaudeLocal_NotPersistent(t *testing.T) {
	t.Parallel()

	var client ChatClient = &ClaudeLocal{}

	_, ok := client.(PersistentChatClient)
	assert.False(t, ok, "ClaudeLocal should not implement PersistentChatClient")
}
