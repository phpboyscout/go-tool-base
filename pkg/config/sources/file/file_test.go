package file

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	configafero "gitlab.com/phpboyscout/go/config-afero"
	configtoml "gitlab.com/phpboyscout/go/config-toml"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

type bootstrap struct {
	fs     config.FS
	codecs []setup.ConfigCodec
}

func (bootstrap) View() *config.View { return nil }
func (b bootstrap) FS() config.FS    { return b.fs }
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

// Spec 0204 D2, D3: a file source reads the file its slot names, in any
// linked format, chosen by extension.
func TestFactory_ReadsTheFileInItsFormat(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/etc/mytool/platform.toml", []byte("[region]\nname = \"eu\"\n"), 0o600))

	b := bootstrap{fs: configafero.Wrap(fs), codecs: []setup.ConfigCodec{{Format: "toml", Codec: configtoml.Codec{}, Extensions: []string{".toml"}}}}

	backend, err := factory(t.Context(), settings(t, "path: /etc/mytool/platform.toml\n"), b)
	require.NoError(t, err)
	assert.Equal(t, "eu", resolve(t, backend).GetString("region.name"))
}

func TestFactory_Refuses(t *testing.T) {
	t.Parallel()

	b := bootstrap{fs: configafero.Wrap(afero.NewMemMapFs())}

	_, err := factory(t.Context(), settings(t, "other: x\n"), b)
	require.ErrorIs(t, err, ErrNoPath)

	_, err = factory(t.Context(), settings(t, "path: /etc/mytool/platform.toml\n"), b)
	require.ErrorIs(t, err, setup.ErrUnlinkedConfigFormat, "a format the tool does not link")
}
