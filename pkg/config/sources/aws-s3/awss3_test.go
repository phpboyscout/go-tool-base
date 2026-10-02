package awss3

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/awssource/awssourcetest"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

type bootstrap struct{}

func (bootstrap) View() *config.View { return nil }
func (bootstrap) FS() config.FS      { return nil }
func (bootstrap) CodecFor(path string) (config.Codec, error) {
	return setup.ConfigCodecFor(nil, path)
}

func settings(t *testing.T, yaml string) config.Reader {
	t.Helper()

	store, err := config.NewStore(t.Context(), config.WithReaders(config.NamedSource{Name: "settings", Content: []byte(yaml)}))
	require.NoError(t, err)

	return store.View()
}

// Spec 0204 D2, D18: an aws-s3 source reads one object, in the format its key
// names, through a client from the ambient chain; path-style addressing is a
// setting, for MinIO and LocalStack.
func TestFactory_ReadsTheObject(t *testing.T) {
	awssourcetest.Isolate(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/acme-config/mytool/config.yaml" {
			http.NotFound(w, r)

			return
		}

		w.Header().Set("Last-Modified", "Mon, 01 Jan 2024 00:00:00 GMT")
		w.Header().Set("ETag", `"1"`)

		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "20")

			return
		}

		_, _ = w.Write([]byte("log:\n  level: debug\n"))
	}))
	t.Cleanup(srv.Close)

	backend, err := factory(t.Context(), settings(t, "bucket: acme-config\nkey: mytool/config.yaml\npath_style: true\nendpoint: "+srv.URL+"\n"), bootstrap{})
	require.NoError(t, err)

	store, err := config.NewStore(t.Context(), config.WithBackend(backend))
	require.NoError(t, err)
	assert.Equal(t, "debug", store.View().GetString("log.level"))
}

func TestFactory_Refuses(t *testing.T) {
	awssourcetest.Isolate(t)

	_, err := factory(t.Context(), settings(t, "key: config.yaml\n"), bootstrap{})
	require.ErrorIs(t, err, ErrNoObject)

	_, err = factory(t.Context(), settings(t, "bucket: b\n"), bootstrap{})
	require.ErrorIs(t, err, ErrNoObject)

	_, err = factory(t.Context(), settings(t, "bucket: b\nkey: config.toml\n"), bootstrap{})
	require.ErrorIs(t, err, setup.ErrUnlinkedConfigFormat)
}
