package awsssm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/awssource/awssourcetest"
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

// Spec 0204 D18: an aws-ssm source reads every parameter under its prefix,
// through the AWS config the ambient chain resolves.
func TestFactory_ReadsThePrefix(t *testing.T) {
	awssourcetest.Isolate(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "AmazonSSM.GetParametersByPath", r.Header.Get("X-Amz-Target"))
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_ = json.NewEncoder(w).Encode(map[string]any{"Parameters": []map[string]any{
			{"Name": "/team/mytool/log/level", "Type": "String", "Value": "debug", "Version": 1},
		}})
	}))
	t.Cleanup(srv.Close)

	backend, err := factory(t.Context(), settings(t, "prefix: /team/mytool\nendpoint: "+srv.URL+"\n"), bootstrap{})
	require.NoError(t, err)

	store, err := config.NewStore(t.Context(), config.WithBackend(backend))
	require.NoError(t, err)
	assert.Equal(t, "debug", store.View().GetString("log.level"))
}

func TestFactory_NeedsAPrefix(t *testing.T) {
	awssourcetest.Isolate(t)

	_, err := factory(t.Context(), settings(t, "region: eu-west-2\n"), bootstrap{})
	require.ErrorIs(t, err, ErrNoPrefix)
}
