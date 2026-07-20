package testutil

import (
	"context"
	"sync"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	configafero "gitlab.com/phpboyscout/go/config-afero"
)

// StoreFromYAML builds a config store over the given YAML document, with any
// further options (e.g. config.WithEnv) layered above it in precedence order.
func StoreFromYAML(t *testing.T, yaml string, opts ...config.StoreOption) *config.Store {
	t.Helper()

	store, err := config.NewStore(t.Context(), append([]config.StoreOption{
		config.WithReaders(config.NamedSource{Name: "test.yaml", Content: []byte(yaml)}),
	}, opts...)...)
	require.NoError(t, err)

	return store
}

// ViewFromYAML pins a view over the given YAML document.
func ViewFromYAML(t *testing.T, yaml string, opts ...config.StoreOption) *config.View {
	t.Helper()

	return StoreFromYAML(t, yaml, opts...).View()
}

// FileStoreFromYAML builds a store whose YAML document loads as a FILE layer
// (a writable config file on a MemMapFs) — for tests that depend on
// user-authored provenance or a writable Apply target, which a reader layer
// deliberately is not.
func FileStoreFromYAML(t *testing.T, yaml string, opts ...config.StoreOption) *config.Store {
	t.Helper()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "config.yaml", []byte(yaml), 0o600))

	store, err := config.NewStore(t.Context(), append([]config.StoreOption{
		config.WithFiles(configafero.Wrap(fs), "config.yaml"),
	}, opts...)...)
	require.NoError(t, err)

	return store
}

// FileViewFromYAML pins a view over a file-layer-backed YAML document.
func FileViewFromYAML(t *testing.T, yaml string, opts ...config.StoreOption) *config.View {
	t.Helper()

	return FileStoreFromYAML(t, yaml, opts...).View()
}

// MutableSource is a config backend whose content a test can replace before
// calling Store.Reload — the reload idiom the config module's own tests use.
type MutableSource struct {
	mu      sync.Mutex
	content []byte
}

// ID identifies the backend; it is constant so a reload replaces the layer.
func (m *MutableSource) ID() string { return "test.yaml" }

// Capabilities reports the source as read-only.
func (m *MutableSource) Capabilities() config.Capabilities { return config.Capabilities{} }

// Set replaces the backend's content; call Store.Reload to publish it.
func (m *MutableSource) Set(yaml string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.content = []byte(yaml)
}

// Load satisfies config.Backend over the current content.
func (m *MutableSource) Load(ctx context.Context, _ []config.Layer) ([]config.Layer, error) {
	m.mu.Lock()
	content := m.content
	m.mu.Unlock()

	return config.NewReaderBackend("test.yaml", content).Load(ctx, nil)
}

// MutableStoreFromYAML builds a store over content the test can change with
// MutableSource.Set followed by Store.Reload.
func MutableStoreFromYAML(t *testing.T, yaml string) (*config.Store, *MutableSource) {
	t.Helper()

	src := &MutableSource{content: []byte(yaml)}

	store, err := config.NewStore(t.Context(), config.WithBackend(src))
	require.NoError(t, err)

	return store, src
}
