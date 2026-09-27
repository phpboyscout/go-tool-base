package consul

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
)

type bootstrap struct{}

func (bootstrap) View() *config.View                    { return nil }
func (bootstrap) FS() config.FS                         { return nil }
func (bootstrap) CodecFor(string) (config.Codec, error) { return config.YAMLCodec{}, nil }

func settings(t *testing.T, yaml string) config.Reader {
	t.Helper()

	store, err := config.NewStore(t.Context(), config.WithReaders(config.NamedSource{Name: "settings", Content: []byte(yaml)}))
	require.NoError(t, err)

	return store.View()
}

// Spec 0204 D18: a Consul source is built from capi's documented defaults with
// the slot's address, reads its prefix, and takes its token from a GTB rung
// when one is set. Not parallel: the environment is process-wide.
func TestFactory_ReadsThePrefix(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("TEAM_CONSUL_TOKEN", "c.team")

	var token atomic.Value

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token.Store(r.Header.Get("X-Consul-Token"))

		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"Key": "team/mytool/log/level", "Value": []byte("debug"), "ModifyIndex": 1},
		})
	}))
	t.Cleanup(srv.Close)

	backend, err := factory(t.Context(), settings(t, "address: "+srv.URL+"\nprefix: team/mytool/\nauth:\n  env: TEAM_CONSUL_TOKEN\n"), bootstrap{})
	require.NoError(t, err)

	store, err := config.NewStore(t.Context(), config.WithBackend(backend))
	require.NoError(t, err)
	assert.Equal(t, "debug", store.View().GetString("log.level"))
	assert.Equal(t, "c.team", token.Load())
}

func TestFactory_NeedsAPrefix(t *testing.T) {
	t.Setenv("CI", "")

	_, err := factory(t.Context(), settings(t, "address: https://consul.internal\n"), bootstrap{})
	require.ErrorIs(t, err, ErrNoPrefix)
}
