package hcl_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	confighcl "gitlab.com/phpboyscout/go/config-hcl"
	"gitlab.com/phpboyscout/go/features"

	hcl "gitlab.com/phpboyscout/go-tool-base/pkg/config/formats/hcl"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func TestImportLinksTheFormat(t *testing.T) {
	t.Parallel()

	set, err := features.Resolve(features.Default().Snapshot(), nil)
	require.NoError(t, err)

	codec, err := setup.ConfigCodecFor(setup.ConfigCodecsIn(set), "/etc/tool/config.hcl")
	require.NoError(t, err)
	assert.Equal(t, confighcl.Codec{}, codec)
	assert.True(t, set.Enabled(setup.ConfigFormatDescriptor(hcl.Format).ID))
}
