package root

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestReloadLoggingObserver_ReappliesLevel proves config hot-reload re-applies
// logging: a reloaded log.level changes the live logger's verbosity.
func TestReloadLoggingObserver_ReappliesLevel(t *testing.T) {
	t.Parallel()

	log := logger.NewCharm(io.Discard)
	logger.SetLevel(log, slog.LevelInfo)
	require.False(t, log.Enabled(context.Background(), slog.LevelDebug),
		"precondition: debug is not enabled at info level")

	props := &p.Props{Tool: p.Tool{Name: "t"}, Logger: log, FS: afero.NewMemMapFs()}
	observer := reloadLoggingObserver(props, &FlagValues{Debug: false}, &slog.LevelVar{})

	// Simulate a reload whose new snapshot sets log.level: debug.
	require.NoError(t, observer(testutil.ViewFromYAML(t, "log:\n  level: debug\n")))

	assert.True(t, log.Enabled(context.Background(), slog.LevelDebug),
		"editing log.level to debug must raise the live logger's verbosity")
}

// TestReloadLoggingObserver_DebugFlagWins ensures a reload can never downgrade an
// explicit --debug: even when the reloaded config says log.level: error, the
// --debug flag keeps debug active.
func TestReloadLoggingObserver_DebugFlagWins(t *testing.T) {
	t.Parallel()

	log := logger.NewCharm(io.Discard)
	logger.SetLevel(log, slog.LevelDebug)

	props := &p.Props{Tool: p.Tool{Name: "t"}, Logger: log, FS: afero.NewMemMapFs()}
	observer := reloadLoggingObserver(props, &FlagValues{Debug: true}, &slog.LevelVar{})

	require.NoError(t, observer(testutil.ViewFromYAML(t, "log:\n  level: error\n")))

	assert.True(t, log.Enabled(context.Background(), slog.LevelDebug),
		"--debug must survive a reload that lowers log.level")
}
