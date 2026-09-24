package properties_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configproperties "gitlab.com/phpboyscout/go/config-properties"
	"gitlab.com/phpboyscout/go/features"

	properties "gitlab.com/phpboyscout/go-tool-base/pkg/config/formats/properties"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func TestImportLinksTheFormat(t *testing.T) {
	t.Parallel()

	set, err := features.Resolve(features.Default().Snapshot(), nil)
	require.NoError(t, err)

	codec, err := setup.ConfigCodecFor(setup.ConfigCodecsIn(set), "/etc/tool/config.properties")
	require.NoError(t, err)
	assert.Equal(t, configproperties.Codec{}, codec)
	assert.True(t, set.Enabled(setup.ConfigFormatDescriptor(properties.Format).ID))
}
