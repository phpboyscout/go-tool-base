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
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// TestNewCmdRoot_DefaultsNilVersion is the nil-Version regression: a Props built
// without a Version must not leave props.Version nil, because the update-check
// path logs props.Version.GetVersion() unconditionally and would panic. The
// default is an empty, development-flavoured Version, so the update check is
// safely skipped rather than dereferencing nil.
func TestNewCmdRoot_DefaultsNilVersion(t *testing.T) {
	// Not parallel: NewCmdRoot seals the process-global middleware registry, so
	// it must not run concurrently with the parallel registry-resetting tests
	// (TestMiddleware_IntegrationWithCobra). Reset + cleanup mirrors the sibling
	// NewCmdRoot tests.
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)

	props := &p.Props{
		Tool:   p.Tool{Name: "t"},
		Logger: logger.NewNoop(),
		FS:     afero.NewMemMapFs(),
		Assets: p.NewAssets(),
		// Version deliberately omitted.
	}

	_ = NewCmdRoot(props)

	require.NotNil(t, props.Version, "root construction must default a nil Version")
	assert.True(t, props.Version.IsDevelopment(),
		"the defaulted Version reports as development, so the update check is skipped")
	assert.NotPanics(t, func() { _ = props.Version.GetVersion() },
		"GetVersion on the defaulted Version must not panic")
}

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
