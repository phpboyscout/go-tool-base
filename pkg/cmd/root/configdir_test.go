package root

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Spec 0221 D1.
func TestPromptTelemetryConsent_CreatesTheToolsDirectory(t *testing.T) {
	testutil.EachConfigDir(t, testPromptTelemetryConsent_CreatesTheToolsDirectory)
}

func testPromptTelemetryConsent_CreatesTheToolsDirectory(t *testing.T, style p.ConfigDir) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("CI", "")

	props := consentProps(t, "", true)
	props.Tool.Config.Dir = style
	props.IO = promptIO("n")

	promptTelemetryConsent(t.Context(), props)

	exists, err := afero.DirExists(props.FS, setup.ConfigDir(props.Tool))
	require.NoError(t, err)
	assert.True(t, exists)
}

// Spec 0221 D1.
func TestCheckForUpdates_ReadsTheLastCheckFromTheToolsDirectory(t *testing.T) {
	testutil.EachConfigDir(t, testCheckForUpdates_ReadsTheLastCheckFromTheToolsDirectory)
}

func testCheckForUpdates_ReadsTheLastCheckFromTheToolsDirectory(t *testing.T, style p.ConfigDir) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("CI", "")

	props := outdatedProps(t, p.UpdatePolicyPrompt, promptIO("n"))
	props.Tool.Config.Dir = style
	require.NoError(t, setup.SetTimeSinceLast(props.FS, props.Tool, setup.CheckedKey))

	stale := time.Now().Add(-30 * 24 * time.Hour)
	require.NoError(t, props.FS.Chtimes(filepath.Join(setup.ConfigDir(props.Tool), "last_checked"), stale, stale))

	checkForUpdates(context.Background(), mkUpdateCmd(t), props, newRootState())

	assert.True(t, loggedAt(t, props, logger.DebugLevel, "time since last update check"))
}
