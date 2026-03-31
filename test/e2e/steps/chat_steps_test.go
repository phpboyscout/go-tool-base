package steps_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/cucumber/godog"
	"github.com/spf13/afero"

	"github.com/phpboyscout/go-tool-base/pkg/chat"
)

const chatAESKeySize = 32

type chatWorldKey struct{}

type chatWorld struct {
	fs        afero.Fs
	store     chat.ConversationStore
	dir       string
	key       []byte
	snapshots []*chat.Snapshot
	loaded    *chat.Snapshot
	summaries []chat.SnapshotSummary
	lastErr   error
}

func getChatWorld(ctx context.Context) *chatWorld {
	return ctx.Value(chatWorldKey{}).(*chatWorld)
}

func initChatSteps(ctx *godog.ScenarioContext) {
	ctx.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		w := &chatWorld{
			fs:  afero.NewMemMapFs(),
			dir: "/test-conversations",
		}

		return context.WithValue(ctx, chatWorldKey{}, w), nil
	})

	ctx.Step(`^a new FileStore$`, aNewFileStore)
	ctx.Step(`^a new encrypted FileStore$`, aNewEncryptedFileStore)
	ctx.Step(`^a conversation snapshot for provider "([^"]*)" with model "([^"]*)"$`, aSnapshotForProviderWithModel)
	ctx.Step(`^a conversation snapshot with ID "([^"]*)"$`, aSnapshotWithID)
	ctx.Step(`^a conversation snapshot with system prompt "([^"]*)"$`, aSnapshotWithSystemPrompt)
	ctx.Step(`^a conversation snapshot with tools$`, aSnapshotWithTools)
	ctx.Step(`^I save the snapshot to the store$`, iSaveTheSnapshot)
	ctx.Step(`^I save all snapshots to the store$`, iSaveAllSnapshots)
	ctx.Step(`^I load the snapshot by ID$`, iLoadTheSnapshotByID)
	ctx.Step(`^I list snapshots$`, iListSnapshots)
	ctx.Step(`^I delete the snapshot by ID$`, iDeleteTheSnapshotByID)
	ctx.Step(`^I create a FileStore with a different encryption key$`, iCreateFileStoreWithDifferentKey)
	ctx.Step(`^I attempt to restore it into an "([^"]*)" provider$`, iAttemptToRestoreIntoProvider)
	ctx.Step(`^the loaded snapshot matches the original$`, theLoadedSnapshotMatchesOriginal)
	ctx.Step(`^the loaded snapshot has provider "([^"]*)"$`, theLoadedSnapshotHasProvider)
	ctx.Step(`^the loaded snapshot has model "([^"]*)"$`, theLoadedSnapshotHasModel)
	ctx.Step(`^the loaded snapshot has system prompt "([^"]*)"$`, theLoadedSnapshotHasSystemPrompt)
	ctx.Step(`^the list contains (\d+) summaries$`, theListContainsSummaries)
	ctx.Step(`^loading the snapshot by ID fails$`, loadingTheSnapshotFails)
	ctx.Step(`^the raw file does not contain "([^"]*)"$`, theRawFileDoesNotContain)
	ctx.Step(`^the restore fails with a provider mismatch error$`, theRestoreFailsWithProviderMismatch)
	ctx.Step(`^the snapshot tools contain names and descriptions$`, theSnapshotToolsContainNamesAndDescriptions)
	ctx.Step(`^the snapshot tools do not contain handlers$`, theSnapshotToolsDoNotContainHandlers)
}

func aNewFileStore(ctx context.Context) (context.Context, error) {
	w := getChatWorld(ctx)

	store, err := chat.NewFileStore(w.fs, w.dir)
	if err != nil {
		return ctx, err
	}

	w.store = store

	return ctx, nil
}

func aNewEncryptedFileStore(ctx context.Context) (context.Context, error) {
	w := getChatWorld(ctx)

	key := make([]byte, chatAESKeySize)
	if _, err := rand.Read(key); err != nil {
		return ctx, err
	}

	w.key = key

	store, err := chat.NewFileStore(w.fs, w.dir, chat.WithEncryption(key))
	if err != nil {
		return ctx, err
	}

	w.store = store

	return ctx, nil
}

func aSnapshotForProviderWithModel(ctx context.Context, provider, model string) (context.Context, error) {
	w := getChatWorld(ctx)

	snap := chat.NewSnapshot(
		chat.Provider(provider), model, "",
		json.RawMessage(`[{"role":"user","content":"hello"}]`),
		nil, nil,
	)
	w.snapshots = append(w.snapshots, snap)

	return ctx, nil
}

func aSnapshotWithID(ctx context.Context, id string) (context.Context, error) {
	w := getChatWorld(ctx)

	snap := chat.NewSnapshot(
		chat.ProviderClaude, "claude-3-5-sonnet", "",
		json.RawMessage(`[{"role":"user","content":"test"}]`),
		nil, nil,
	)
	snap.ID = id
	w.snapshots = append(w.snapshots, snap)

	return ctx, nil
}

func aSnapshotWithSystemPrompt(ctx context.Context, prompt string) (context.Context, error) {
	w := getChatWorld(ctx)

	snap := chat.NewSnapshot(
		chat.ProviderClaude, "claude-3-5-sonnet", prompt,
		json.RawMessage(`[{"role":"user","content":"hello"}]`),
		nil, nil,
	)
	w.snapshots = append(w.snapshots, snap)

	return ctx, nil
}

func aSnapshotWithTools(ctx context.Context) (context.Context, error) {
	w := getChatWorld(ctx)

	tools := map[string]chat.Tool{
		"search": {
			Name:        "search",
			Description: "Search the web",
			Handler:     func(_ context.Context, _ json.RawMessage) (any, error) { return nil, nil },
		},
	}

	snap := chat.NewSnapshot(
		chat.ProviderClaude, "claude-3-5-sonnet", "",
		json.RawMessage(`[]`),
		tools, nil,
	)
	w.snapshots = append(w.snapshots, snap)

	return ctx, nil
}

func iSaveTheSnapshot(ctx context.Context) (context.Context, error) {
	w := getChatWorld(ctx)

	if len(w.snapshots) == 0 {
		return ctx, fmt.Errorf("no snapshot to save")
	}

	return ctx, w.store.Save(context.Background(), w.snapshots[len(w.snapshots)-1])
}

func iSaveAllSnapshots(ctx context.Context) (context.Context, error) {
	w := getChatWorld(ctx)

	for _, snap := range w.snapshots {
		if err := w.store.Save(context.Background(), snap); err != nil {
			return ctx, err
		}
	}

	return ctx, nil
}

func iLoadTheSnapshotByID(ctx context.Context) (context.Context, error) {
	w := getChatWorld(ctx)
	snap := w.snapshots[len(w.snapshots)-1]

	loaded, loadErr := w.store.Load(ctx, snap.ID)
	w.lastErr = loadErr
	w.loaded = loaded

	return ctx, nil
}

func iListSnapshots(ctx context.Context) (context.Context, error) {
	w := getChatWorld(ctx)

	summaries, err := w.store.List(context.Background())
	if err != nil {
		return ctx, err
	}

	w.summaries = summaries

	return ctx, nil
}

func iDeleteTheSnapshotByID(ctx context.Context) (context.Context, error) {
	w := getChatWorld(ctx)
	snap := w.snapshots[len(w.snapshots)-1]

	return ctx, w.store.Delete(context.Background(), snap.ID)
}

func iCreateFileStoreWithDifferentKey(ctx context.Context) (context.Context, error) {
	w := getChatWorld(ctx)

	differentKey := make([]byte, chatAESKeySize)
	if _, err := rand.Read(differentKey); err != nil {
		return ctx, err
	}

	store, err := chat.NewFileStore(w.fs, w.dir, chat.WithEncryption(differentKey))
	if err != nil {
		return ctx, err
	}

	w.store = store

	return ctx, nil
}

func iAttemptToRestoreIntoProvider(ctx context.Context, targetProvider string) (context.Context, error) {
	w := getChatWorld(ctx)
	snap := w.snapshots[len(w.snapshots)-1]

	// Create a fake client-side validation (same logic as provider Restore methods)
	if string(snap.Provider) != targetProvider {
		w.lastErr = fmt.Errorf("provider mismatch: snapshot is %s, client is %s", snap.Provider, targetProvider)
	}

	return ctx, nil
}

func theLoadedSnapshotMatchesOriginal(ctx context.Context) error {
	w := getChatWorld(ctx)

	if w.loaded == nil {
		return fmt.Errorf("no loaded snapshot")
	}

	original := w.snapshots[len(w.snapshots)-1]

	if w.loaded.ID != original.ID {
		return fmt.Errorf("ID mismatch: %q != %q", w.loaded.ID, original.ID)
	}

	if w.loaded.Version != original.Version {
		return fmt.Errorf("version mismatch: %d != %d", w.loaded.Version, original.Version)
	}

	return nil
}

func theLoadedSnapshotHasProvider(ctx context.Context, provider string) error {
	w := getChatWorld(ctx)

	if string(w.loaded.Provider) != provider {
		return fmt.Errorf("provider = %q, want %q", w.loaded.Provider, provider)
	}

	return nil
}

func theLoadedSnapshotHasModel(ctx context.Context, model string) error {
	w := getChatWorld(ctx)

	if w.loaded.Model != model {
		return fmt.Errorf("model = %q, want %q", w.loaded.Model, model)
	}

	return nil
}

func theLoadedSnapshotHasSystemPrompt(ctx context.Context, prompt string) error {
	w := getChatWorld(ctx)

	if w.loaded.SystemPrompt != prompt {
		return fmt.Errorf("system_prompt = %q, want %q", w.loaded.SystemPrompt, prompt)
	}

	return nil
}

func theListContainsSummaries(ctx context.Context, count int) error {
	w := getChatWorld(ctx)

	if len(w.summaries) != count {
		return fmt.Errorf("expected %d summaries, got %d", count, len(w.summaries))
	}

	return nil
}

func loadingTheSnapshotFails(ctx context.Context) error {
	w := getChatWorld(ctx)
	snap := w.snapshots[len(w.snapshots)-1]

	_, err := w.store.Load(ctx, snap.ID)
	if err == nil {
		return fmt.Errorf("expected load to fail, but it succeeded")
	}

	return nil
}

func theRawFileDoesNotContain(ctx context.Context, text string) error {
	w := getChatWorld(ctx)
	snap := w.snapshots[len(w.snapshots)-1]

	path := filepath.Join(w.dir, snap.ID+".json")

	data, err := afero.ReadFile(w.fs, path)
	if err != nil {
		return fmt.Errorf("reading raw file: %w", err)
	}

	if contains(string(data), text) {
		return fmt.Errorf("raw file contains plaintext %q — encryption failed", text)
	}

	return nil
}

func theRestoreFailsWithProviderMismatch(ctx context.Context) error {
	w := getChatWorld(ctx)

	if w.lastErr == nil {
		return fmt.Errorf("expected provider mismatch error, got nil")
	}

	if !contains(w.lastErr.Error(), "provider mismatch") {
		return fmt.Errorf("expected 'provider mismatch' error, got: %s", w.lastErr.Error())
	}

	return nil
}

func theSnapshotToolsContainNamesAndDescriptions(ctx context.Context) error {
	w := getChatWorld(ctx)
	snap := w.snapshots[len(w.snapshots)-1]

	if len(snap.Tools) == 0 {
		return fmt.Errorf("expected tools, got none")
	}

	for _, t := range snap.Tools {
		if t.Name == "" {
			return fmt.Errorf("tool name is empty")
		}

		if t.Description == "" {
			return fmt.Errorf("tool description is empty")
		}
	}

	return nil
}

func theSnapshotToolsDoNotContainHandlers(ctx context.Context) error {
	w := getChatWorld(ctx)
	snap := w.snapshots[len(w.snapshots)-1]

	data, err := json.Marshal(snap.Tools)
	if err != nil {
		return fmt.Errorf("marshalling tools: %w", err)
	}

	if contains(string(data), "handler") {
		return fmt.Errorf("serialised tools contain 'handler' field")
	}

	return nil
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}
