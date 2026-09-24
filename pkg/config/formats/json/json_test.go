package json_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configjson "gitlab.com/phpboyscout/go/config-json"
	"gitlab.com/phpboyscout/go/features"

	json "gitlab.com/phpboyscout/go-tool-base/pkg/config/formats/json"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func TestImportLinksTheFormat(t *testing.T) {
	t.Parallel()

	set, err := features.Resolve(features.Default().Snapshot(), nil)
	require.NoError(t, err)

	codec, err := setup.ConfigCodecFor(setup.ConfigCodecsIn(set), "/etc/tool/config.json")
	require.NoError(t, err)
	assert.Equal(t, configjson.Codec{}, codec)
	assert.True(t, set.Enabled(setup.ConfigFormatDescriptor(json.Format).ID))
}
