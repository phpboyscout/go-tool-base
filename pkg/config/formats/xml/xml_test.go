package xml_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configxml "gitlab.com/phpboyscout/go/config-xml"
	"gitlab.com/phpboyscout/go/features"

	xml "gitlab.com/phpboyscout/go-tool-base/pkg/config/formats/xml"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func TestImportLinksTheFormat(t *testing.T) {
	t.Parallel()

	set, err := features.Resolve(features.Default().Snapshot(), nil)
	require.NoError(t, err)

	codec, err := setup.ConfigCodecFor(setup.ConfigCodecsIn(set), "/etc/tool/config.xml")
	require.NoError(t, err)
	assert.Equal(t, configxml.Codec{}, codec)
	assert.True(t, set.Enabled(setup.ConfigFormatDescriptor(xml.Format).ID))
}
