package toml_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configtoml "gitlab.com/phpboyscout/go/config-toml"
	"gitlab.com/phpboyscout/go/features"

	toml "gitlab.com/phpboyscout/go-tool-base/pkg/config/formats/toml"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func TestImportLinksTheFormat(t *testing.T) {
	t.Parallel()

	set, err := features.Resolve(features.Default().Snapshot(), nil)
	require.NoError(t, err)

	codec, err := setup.ConfigCodecFor(setup.ConfigCodecsIn(set), "/etc/tool/config.toml")
	require.NoError(t, err)
	assert.Equal(t, configtoml.Codec{}, codec)
	assert.True(t, set.Enabled(setup.ConfigFormatDescriptor(toml.Format).ID))
}
