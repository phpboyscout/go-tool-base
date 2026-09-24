package ini_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configini "gitlab.com/phpboyscout/go/config-ini"
	"gitlab.com/phpboyscout/go/features"

	ini "gitlab.com/phpboyscout/go-tool-base/pkg/config/formats/ini"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func TestImportLinksTheFormat(t *testing.T) {
	t.Parallel()

	set, err := features.Resolve(features.Default().Snapshot(), nil)
	require.NoError(t, err)

	codec, err := setup.ConfigCodecFor(setup.ConfigCodecsIn(set), "/etc/tool/config.ini")
	require.NoError(t, err)
	assert.Equal(t, configini.Codec{}, codec)
	assert.True(t, set.Enabled(setup.ConfigFormatDescriptor(ini.Format).ID))
}
