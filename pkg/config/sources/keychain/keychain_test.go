package keychain

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/errors"
)

// memKeychain is a credentials.Backend held in memory.
type memKeychain struct {
	available bool
	entries   map[string]string
}

func (m *memKeychain) Store(_ context.Context, service, account, secret string) error {
	m.entries[service+"/"+account] = secret

	return nil
}

func (m *memKeychain) Retrieve(_ context.Context, service, account string) (string, error) {
	return m.entries[service+"/"+account], nil
}

func (m *memKeychain) Delete(_ context.Context, service, account string) error {
	delete(m.entries, service+"/"+account)

	return nil
}

func (m *memKeychain) Available() bool { return m.available }

func settings(t *testing.T, yaml string) config.Reader {
	t.Helper()

	store, err := config.NewStore(t.Context(), config.WithReaders(config.NamedSource{Name: "settings", Content: []byte(yaml)}))
	require.NoError(t, err)

	return store.View()
}

// Spec 0204 D12: a keychain source reads the accounts its keys map names,
// under its service, and is writable and sensitive.
func TestFactory_ReadsTheDeclaredKeys(t *testing.T) {
	t.Parallel()

	kc := &memKeychain{available: true, entries: map[string]string{"mytool/instagram-token": "tok"}}

	backend, err := factoryFor(kc)(t.Context(), settings(t, "service: mytool\nkeys:\n  platforms.instagram.token: instagram-token\ntimeout: 30s\n"), nil)
	require.NoError(t, err)

	store, err := config.NewStore(t.Context(), config.WithBackend(backend))
	require.NoError(t, err)
	assert.Equal(t, "tok", store.View().GetString("platforms.instagram.token"))
	assert.True(t, backend.Capabilities().Sensitive)
}

// A binary with no keychain linked fails the factory naming the link, so an
// optional slot drops out and a required one stops the tool (D6, D12).
func TestFactory_WithoutAKeychainLinked(t *testing.T) {
	t.Parallel()

	_, err := factoryFor(&memKeychain{})(t.Context(), settings(t, "service: mytool\n"), nil)
	require.ErrorIs(t, err, ErrNoKeychain)
	assert.Contains(t, errors.FlattenHints(err), "pkg/setup/keychain")
}

func TestFactory_NeedsAService(t *testing.T) {
	t.Parallel()

	_, err := factoryFor(&memKeychain{available: true})(t.Context(), settings(t, "keys: {}\n"), nil)
	require.ErrorIs(t, err, ErrNoService)
}
