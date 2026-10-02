package awssecrets

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	configjson "gitlab.com/phpboyscout/go/config-json"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/awssource/awssourcetest"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

type bootstrap struct{ codecs []setup.ConfigCodec }

func (bootstrap) View() *config.View { return nil }
func (bootstrap) FS() config.FS      { return nil }
func (b bootstrap) CodecFor(path string) (config.Codec, error) {
	return setup.ConfigCodecFor(b.codecs, path)
}

var withJSON = bootstrap{codecs: []setup.ConfigCodec{{Format: "json", Codec: configjson.Codec{}, Extensions: []string{".json"}}}}

func settings(t *testing.T, yaml string) config.Reader {
	t.Helper()

	store, err := config.NewStore(t.Context(), config.WithReaders(config.NamedSource{Name: "settings", Content: []byte(yaml)}))
	require.NoError(t, err)

	return store.View()
}

// Spec 0204 D18: an aws-secrets source reads one secret whose value is a
// document, decoded in its value format (JSON by default), and is sensitive.
func TestFactory_ReadsOneSecret(t *testing.T) {
	awssourcetest.Isolate(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "secretsmanager.GetSecretValue", r.Header.Get("X-Amz-Target"))
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_ = json.NewEncoder(w).Encode(map[string]any{ //nolint:gosec // a fake secret in an httptest double
			"ARN": "arn:aws:secretsmanager:eu-west-2:1:secret:app", "Name": "app",
			"SecretString": `{"db":{"host":"db.internal"}}`, "VersionId": "v1", "VersionStages": []string{"AWSCURRENT"},
		})
	}))
	t.Cleanup(srv.Close)

	backend, err := factory(t.Context(), settings(t, "name: app\nendpoint: "+srv.URL+"\n"), withJSON)
	require.NoError(t, err)

	store, err := config.NewStore(t.Context(), config.WithBackend(backend))
	require.NoError(t, err)
	assert.Equal(t, "db.internal", store.View().GetString("db.host"))
	assert.True(t, backend.Capabilities().Sensitive)
}

func TestFactory_Refuses(t *testing.T) {
	awssourcetest.Isolate(t)

	_, err := factory(t.Context(), settings(t, "region: eu-west-2\n"), withJSON)
	require.ErrorIs(t, err, ErrNoName, "neither a name nor a prefix")

	_, err = factory(t.Context(), settings(t, "name: app\nprefix: team/\n"), withJSON)
	require.ErrorIs(t, err, ErrNoName, "both")

	_, err = factory(t.Context(), settings(t, "name: app\n"), bootstrap{})
	require.ErrorIs(t, err, setup.ErrUnlinkedConfigFormat, "a JSON secret needs the json format linked")
}
