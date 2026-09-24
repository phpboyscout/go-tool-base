package dotenv_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configdotenv "gitlab.com/phpboyscout/go/config-dotenv"
	"gitlab.com/phpboyscout/go/features"

	dotenv "gitlab.com/phpboyscout/go-tool-base/pkg/config/formats/dotenv"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func TestImportLinksTheFormat(t *testing.T) {
	t.Parallel()

	set, err := features.Resolve(features.Default().Snapshot(), nil)
	require.NoError(t, err)

	codec, err := setup.ConfigCodecFor(setup.ConfigCodecsIn(set), "/etc/tool/config.env")
	require.NoError(t, err)
	assert.Equal(t, configdotenv.Codec{}, codec)
	assert.True(t, set.Enabled(setup.ConfigFormatDescriptor(dotenv.Format).ID))
}
