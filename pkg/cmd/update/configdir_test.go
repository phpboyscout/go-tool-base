package update

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Spec 0221 D1: an offline update records itself in the tool's directory.
func TestDefaultOfflineUpdater_KeepsTheToolsDirectory(t *testing.T) {
	t.Parallel()

	testutil.EachConfigDir(t, testDefaultOfflineUpdater_KeepsTheToolsDirectory)
}

func testDefaultOfflineUpdater_KeepsTheToolsDirectory(t *testing.T, style p.ConfigDir) {
	t.Parallel()

	tool := p.Tool{Name: "t", Config: p.ConfigSpec{Dir: style}}
	props := &p.Props{Tool: tool, Logger: logger.NewNoop(), FS: afero.NewMemMapFs()}

	updater, ok := newUpdateOptions(nil).newOfflineUpdater(props).(*setup.SelfUpdater)
	require.True(t, ok)
	assert.Equal(t, setup.ConfigDir(tool), setup.ConfigDir(updater.Tool))
}
