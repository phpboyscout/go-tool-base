package azureblob

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	configazureblob "gitlab.com/phpboyscout/go/config-azure-blob"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

type fakeBlobs map[string][]byte

func (f fakeBlobs) Download(_ context.Context, name string) ([]byte, error) { return f[name], nil }
func (f fakeBlobs) Upload(_ context.Context, name string, data []byte) error {
	f[name] = data

	return nil
}

func (f fakeBlobs) GetProperties(_ context.Context, name string) (configazureblob.BlobInfo, error) {
	return configazureblob.BlobInfo{Size: int64(len(f[name])), LastModified: time.Unix(0, 0), ETag: "1"}, nil
}
func (f fakeBlobs) Copy(context.Context, string, string) error { return nil }
func (f fakeBlobs) Delete(context.Context, string) error       { return nil }

// Spec 0204 D2, D18: an azure-blob source reads one blob, in the format its
// name says.
func TestFactory_ReadsTheBlob(t *testing.T) {
	t.Parallel()

	blobs := fakeBlobs{"mytool/config.yaml": []byte("log:\n  level: debug\n")}
	open := func(_ context.Context, _ config.Reader) (config.FS, error) { return configazureblob.New(blobs), nil }

	backend, err := factoryWith(open)(t.Context(),
		settings(t, "service_url: https://acme.blob.core.windows.net\ncontainer: config\nblob: mytool/config.yaml\n"), bootstrap{})
	require.NoError(t, err)
	assert.Equal(t, "debug", resolve(t, backend).GetString("log.level"))
}

func TestFactory_Refuses(t *testing.T) {
	t.Parallel()

	_, err := factoryWith(openContainer)(t.Context(), settings(t, "container: config\nblob: c.yaml\n"), bootstrap{})
	require.ErrorIs(t, err, ErrNoBlob)

	_, err = factoryWith(openContainer)(t.Context(),
		settings(t, "service_url: https://acme.blob.core.windows.net\ncontainer: config\nblob: c.toml\n"), bootstrap{})
	require.ErrorIs(t, err, setup.ErrUnlinkedConfigFormat)

	errUnreachable := errors.New("container unreachable")
	unreachable := func(context.Context, config.Reader) (config.FS, error) { return nil, errUnreachable }
	_, err = factoryWith(unreachable)(t.Context(),
		settings(t, "service_url: https://acme.blob.core.windows.net\ncontainer: config\nblob: c.yaml\n"), bootstrap{})
	require.ErrorIs(t, err, errUnreachable, "a container that cannot be opened refuses the source")
}

func TestOpenContainer_BuildsWithoutTheNetwork(t *testing.T) {
	t.Parallel()

	fsys, err := openContainer(t.Context(), settings(t, "service_url: https://acme.blob.core.windows.net\ncontainer: config\n"))
	require.NoError(t, err)
	assert.NotNil(t, fsys)
}
