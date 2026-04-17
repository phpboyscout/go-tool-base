package chat

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateEncryptionKey(t *testing.T) {
	t.Parallel()

	key1, err := GenerateEncryptionKey()
	require.NoError(t, err)
	assert.Len(t, key1, 32, "key must be 32 bytes for AES-256")

	key2, err := GenerateEncryptionKey()
	require.NoError(t, err)
	assert.Len(t, key2, 32)

	// Two calls must produce different keys — they come from crypto/rand.
	// A 256-bit collision is astronomically unlikely; a failing test here
	// indicates a broken random source, not bad luck.
	assert.NotEqual(t, key1, key2, "two generated keys must differ")
}

func testSnapshot(t *testing.T) *Snapshot {
	t.Helper()

	return &Snapshot{
		ID:           "test-id-123",
		Provider:     ProviderClaude,
		Model:        "claude-3-5-sonnet",
		SystemPrompt: "You are a helpful assistant.",
		Messages:     json.RawMessage(`[{"role":"user","content":"hello"}]`),
		Tools: []ToolSnapshot{
			{Name: "search", Description: "Search the web"},
		},
		Metadata:  map[string]string{"env": "test"},
		CreatedAt: time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC),
		Version:   SnapshotVersion,
	}
}

func testKey(t *testing.T) []byte {
	t.Helper()

	key := make([]byte, aesKeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	return key
}

func TestFileStore_SaveLoad(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	store, err := NewFileStore(fs, "/snapshots")
	require.NoError(t, err)

	snap := testSnapshot(t)
	ctx := context.Background()

	require.NoError(t, store.Save(ctx, snap))

	loaded, err := store.Load(ctx, snap.ID)
	require.NoError(t, err)

	assert.Equal(t, snap.ID, loaded.ID)
	assert.Equal(t, snap.Provider, loaded.Provider)
	assert.Equal(t, snap.Model, loaded.Model)
	assert.Equal(t, snap.SystemPrompt, loaded.SystemPrompt)
	assert.JSONEq(t, string(snap.Messages), string(loaded.Messages))
	assert.Len(t, loaded.Tools, len(snap.Tools))
	assert.Equal(t, snap.Metadata["env"], loaded.Metadata["env"])
	assert.Equal(t, snap.Version, loaded.Version)
}

func TestFileStore_List(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	store, err := NewFileStore(fs, "/snapshots")
	require.NoError(t, err)

	ctx := context.Background()

	for _, id := range []string{"snap-1", "snap-2", "snap-3"} {
		snap := testSnapshot(t)
		snap.ID = id
		require.NoError(t, store.Save(ctx, snap))
	}

	summaries, err := store.List(ctx)
	require.NoError(t, err)
	assert.Len(t, summaries, 3)

	for _, s := range summaries {
		assert.NotEmpty(t, s.ID)
		assert.Equal(t, ProviderClaude, s.Provider)
		assert.Equal(t, "claude-3-5-sonnet", s.Model)
		assert.Equal(t, 1, s.MessageCount)
	}
}

func TestFileStore_Delete(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	store, err := NewFileStore(fs, "/snapshots")
	require.NoError(t, err)

	ctx := context.Background()
	snap := testSnapshot(t)

	require.NoError(t, store.Save(ctx, snap))
	require.NoError(t, store.Delete(ctx, snap.ID))

	_, err = store.Load(ctx, snap.ID)
	require.Error(t, err)
}

func TestFileStore_WithEncryption(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	key := testKey(t)

	store, err := NewFileStore(fs, "/snapshots", WithEncryption(key))
	require.NoError(t, err)

	ctx := context.Background()
	snap := testSnapshot(t)

	require.NoError(t, store.Save(ctx, snap))

	// Raw file should not contain plaintext
	raw, err := afero.ReadFile(fs, "/snapshots/"+snap.ID+".json")
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "helpful assistant")

	// Load should decrypt successfully
	loaded, err := store.Load(ctx, snap.ID)
	require.NoError(t, err)
	assert.Equal(t, snap.SystemPrompt, loaded.SystemPrompt)
}

func TestFileStore_EncryptionKeyMismatch(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	key1 := testKey(t)
	key2 := testKey(t)

	store1, err := NewFileStore(fs, "/snapshots", WithEncryption(key1))
	require.NoError(t, err)

	ctx := context.Background()
	snap := testSnapshot(t)

	require.NoError(t, store1.Save(ctx, snap))

	store2, err := NewFileStore(fs, "/snapshots", WithEncryption(key2))
	require.NoError(t, err)

	_, err = store2.Load(ctx, snap.ID)
	require.Error(t, err, "loading with wrong key should fail")
}

func TestFileStore_DirectoryCreation(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	store, err := NewFileStore(fs, "/deep/nested/dir")
	require.NoError(t, err)

	snap := testSnapshot(t)
	require.NoError(t, store.Save(context.Background(), snap))

	exists, err := afero.DirExists(fs, "/deep/nested/dir")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestFileStore_InvalidKeySize(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	_, err := NewFileStore(fs, "/snapshots", WithEncryption([]byte("too-short")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "32 bytes")
}

func TestFileStore_LoadNotFound(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	store, err := NewFileStore(fs, "/snapshots")
	require.NoError(t, err)

	_, err = store.Load(context.Background(), "nonexistent")
	require.Error(t, err)
}
