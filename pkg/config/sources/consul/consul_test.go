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
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/sourceauth"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
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

type unlinked struct{ bootstrap }

func (unlinked) CodecFor(path string) (config.Codec, error) {
	return nil, errors.Wrapf(setup.ErrUnlinkedConfigFormat, "%s", path)
}

// The slot's datacenter reaches Consul, and a value format decodes a
// document stored under one key into a subtree.
func TestFactory_DatacenterAndAValueFormat(t *testing.T) {
	t.Setenv("CI", "")

	var dc atomic.Value

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dc.Store(r.URL.Query().Get("dc"))

		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"Key": "team/mytool/db", "Value": []byte(`{"host": "db.internal"}`), "ModifyIndex": 1},
		})
	}))
	t.Cleanup(srv.Close)

	backend, err := factory(t.Context(), settings(t, "address: "+srv.URL+"\nprefix: team/mytool/\ndatacenter: dc2\nvalue_format: json\n"), bootstrap{})
	require.NoError(t, err)

	store, err := config.NewStore(t.Context(), config.WithBackend(backend))
	require.NoError(t, err)
	assert.Equal(t, "db.internal", store.View().GetString("db.host"))
	assert.Equal(t, "dc2", dc.Load())
}

func TestFactory_RefusesAnUnlinkedValueFormat(t *testing.T) {
	t.Setenv("CI", "")

	_, err := factory(t.Context(), settings(t, "address: https://consul.internal\nprefix: team/\nvalue_format: hcl\n"), unlinked{})
	require.ErrorIs(t, err, setup.ErrUnlinkedConfigFormat)
}

func TestFactory_RefusesALiteralTokenUnderCI(t *testing.T) {
	t.Setenv("CI", "true")

	_, err := factory(t.Context(), settings(t, "address: https://consul.internal\nprefix: team/\nauth:\n  value: c.literal\n"), bootstrap{})
	require.ErrorIs(t, err, sourceauth.ErrLiteralUnderCI)
}
