package initialise

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	propstest "gitlab.com/phpboyscout/go-tool-base/pkg/props/test"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

// TestNewCmdInit_FlagsFollowEnabledFeatures pins #55: a feature's init flags
// (--skip-gitlab and friends) are registered only when the feature is
// enabled, the way its subcommands already are. A tool with no forge feature
// used to advertise every forge's skip flag.
func TestNewCmdInit_FlagsFollowEnabledFeatures(t *testing.T) {
	t.Parallel()

	narrow := NewCmdInit(propstest.New(propstest.WithTool(p.Tool{Name: "test-tool"})))
	require.Nil(t, narrow.Command.Flags().Lookup("skip-gitlab"), "no forge feature: no --skip-gitlab")
	require.Nil(t, narrow.Command.Flags().Lookup("skip-github"))
	assert.NotContains(t, narrow.Long, "GitHub, and Bitbucket", "the description names no forge the tool lacks")

	wide := NewCmdInit(propstest.New(propstest.WithTool(p.Tool{
		Name:     "test-tool",
		Features: p.SetFeatures(p.Enable(forge.GitlabFeature)),
	})))
	require.NotNil(t, wide.Command.Flags().Lookup("skip-gitlab"), "the enabled forge's flag is registered")
	require.Nil(t, wide.Command.Flags().Lookup("skip-github"), "a forge that is not enabled still registers no flag")
}
