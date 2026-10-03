package vault

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func settings(t *testing.T, yaml string) config.Reader {
	t.Helper()

	store, err := config.NewStore(t.Context(), config.WithReaders(config.NamedSource{Name: "settings", Content: []byte(yaml)}))
	require.NoError(t, err)

	return store.View()
}

// kvV2 answers a KV v2 read of secret/data/app, recording the token it saw.
func kvV2(t *testing.T, token *atomic.Value) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token.Store(r.Header.Get("X-Vault-Token"))

		if r.URL.Path != "/v1/secret/data/app" {
			http.NotFound(w, r)

			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"data":     map[string]any{"db": map[string]any{"host": "db.internal"}},
			"metadata": map[string]any{"version": 3},
		}})
	}))
	t.Cleanup(srv.Close)

	return srv
}

// Spec 0204 D18: a Vault source is built through vaultclient's ambient chain
// with the slot's address, reads its KV v2 secret, and takes its token from a
// GTB rung when one is set. Not parallel: the environment is process-wide.
func TestFactory_ReadsTheSecret(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("TEAM_VAULT_TOKEN", "s.team")

	var token atomic.Value

	srv := kvV2(t, &token)

	backend, err := factory(t.Context(), settings(t, "address: "+srv.URL+"\npath: app\nauth:\n  env: TEAM_VAULT_TOKEN\n"), nil)
	require.NoError(t, err)

	store, err := config.NewStore(t.Context(), config.WithBackend(backend))
	require.NoError(t, err)
	assert.Equal(t, "db.internal", store.View().GetString("db.host"))
	assert.Equal(t, "s.team", token.Load())
	assert.True(t, backend.Capabilities().Sensitive)
}

func TestFactory_NeedsAPathOrAPrefix(t *testing.T) {
	t.Setenv("CI", "")

	_, err := factory(t.Context(), settings(t, "address: https://vault.internal\n"), nil)
	require.ErrorIs(t, err, ErrNoPath)

	_, err = factory(t.Context(), settings(t, "address: https://vault.internal\npath: a\nprefix: b\n"), nil)
	require.ErrorIs(t, err, ErrNoPath, "one or the other, not both")
}

// init config offers the mount the factory falls back to.
func TestTheCatalogueMountIsTheFactorysDefault(t *testing.T) {
	t.Parallel()

	for _, s := range setup.ConfigSourceSettings(Kind, "mytool", "v") {
		if s.Key == "mount" {
			assert.Equal(t, defaultMount, s.Default)

			return
		}
	}

	t.Fatal("the catalogue declares no mount")
}
