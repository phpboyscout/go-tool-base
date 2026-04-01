package chat_test

import (
	"context"
	"encoding/json"

	"github.com/spf13/afero"

	"github.com/phpboyscout/go-tool-base/pkg/chat"
)

func ExampleNewFileStore() {
	// Create a FileStore for persisting chat conversation snapshots.
	store, err := chat.NewFileStore(afero.NewMemMapFs(), "/conversations")
	if err != nil {
		return
	}

	// Save, Load, List, Delete snapshots
	_ = store
}

func ExampleNewFileStore_withEncryption() {
	// Encrypt stored snapshots with AES-256-GCM (key must be 32 bytes).
	key := make([]byte, 32) // In real usage, use a secure key source

	store, err := chat.NewFileStore(afero.NewMemMapFs(), "/conversations",
		chat.WithEncryption(key),
	)
	if err != nil {
		return
	}

	_ = store
}

func ExampleNewSnapshot() {
	snap := chat.NewSnapshot(
		chat.ProviderClaude,
		"claude-3-5-sonnet",
		"You are a helpful assistant.",
		json.RawMessage(`[{"role":"user","content":"hello"}]`),
		nil,
		map[string]string{"session": "demo"},
	)

	_ = snap.ID        // UUID
	_ = snap.CreatedAt // timestamp
}

func ExamplePersistentChatClient() {
	// Discover persistence support via type assertion:
	//
	//   client, _ := chat.New(ctx, props, cfg)
	//   if pc, ok := client.(chat.PersistentChatClient); ok {
	//       snapshot, _ := pc.Save()
	//       // ... store snapshot ...
	//       pc.Restore(snapshot)
	//   }
	//
	// ClaudeLocal does not implement PersistentChatClient.

	_ = context.Background()
}
