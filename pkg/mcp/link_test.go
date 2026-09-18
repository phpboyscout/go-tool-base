package mcp_test

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/mcp"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// TestImportDeclaresTheLinkFeature: importing this package is the whole
// activation. The mcp feature is a link kind that defaults on (its presence
// is its enablement, spec 0202 D1, D3), the descriptor names this package as
// the one to blank-import, and exactly one root-command provider is
// contributed under it.
func TestImportDeclaresTheLinkFeature(t *testing.T) {
	t.Parallel()

	snapshot := features.Default().Snapshot()

	d, ok := snapshot.Lookup(props.McpCmd)
	require.True(t, ok, "the blank import must declare mcp")
	assert.Equal(t, props.KindLink, d.FeatureKind())
	assert.True(t, d.DefaultOn(), "a link's presence is its enablement, so it defaults on")

	fd, isGTB := d.(props.FeatureDescriptor)
	require.True(t, isGTB)
	assert.Equal(t, "Feature", fd.ConstName)
	assert.Equal(t, mcp.PackagePath, fd.ConstPackage)
	assert.Equal(t, props.McpCmd, mcp.Feature)

	providers := setup.RootCommandsIn(snapshot)[props.McpCmd]
	require.Len(t, providers, 1, "one provider hands the root the mcp command")

	p, err := props.New(props.Tool{Name: "linked"}, logger.NewNoop(), afero.NewMemMapFs())
	require.NoError(t, err)

	cmd := providers[0](p)
	require.NotNil(t, cmd)
	assert.Equal(t, "mcp", cmd.Name())
	assert.Equal(t, props.McpCmd, cmd.Feature)
	assert.True(t, setup.IsProtocolStdout(cmd.Command))
	assert.True(t, setup.SkipsUpdateCheck(cmd.Command))
}
