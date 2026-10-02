package azureblob

import (
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

type bootstrap struct{ codecs []setup.ConfigCodec }

func (bootstrap) View() *config.View { return nil }
func (bootstrap) FS() config.FS      { return nil }
func (b bootstrap) CodecFor(path string) (config.Codec, error) {
	return setup.ConfigCodecFor(b.codecs, path)
}

func settings(t *testing.T, yaml string) config.Reader {
	t.Helper()

	store, err := config.NewStore(t.Context(), config.WithReaders(config.NamedSource{Name: "settings", Content: []byte(yaml)}))
	require.NoError(t, err)

	return store.View()
}

func resolve(t *testing.T, b config.Backend) *config.View {
	t.Helper()

	store, err := config.NewStore(t.Context(), config.WithBackend(b))
	require.NoError(t, err)

	return store.View()
}
