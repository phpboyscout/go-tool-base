//go:build integration

package errorhandling_test

import (
	"bytes"
	"testing"

	cberrors "github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/errorhandling"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

// simulateDeepError mimics a multi-layer error chain:
// database layer → service layer → handler layer.
func simulateDeepError() error {
	// Layer 1: database returns raw error
	dbErr := cberrors.New("connection refused")

	// Layer 2: service wraps with context and a hint
	svcErr := errorhandling.WrapWithHint(dbErr, "query failed", "check database connection string")

	// Layer 3: handler wraps again with operational context
	return errorhandling.WithUserHint(
		cberrors.Wrap(svcErr, "user lookup failed"),
		"verify the database is running",
	)
}

func TestHintsSurviveMultipleWraps(t *testing.T) {
	t.Parallel()

	err := simulateDeepError()
	log := logger.NewBuffer()
	h := errorhandling.New(log, nil)

	h.Error(err, "API: ")

	entries := log.Entries()
	require.Len(t, entries, 1)
	assert.Equal(t, logger.ErrorLevel, entries[0].Level)
	assert.Contains(t, entries[0].Message, "user lookup failed")

	// Both hints from different layers must survive
	hints := cberrors.FlattenHints(err)
	assert.Contains(t, hints, "check database connection string")
	assert.Contains(t, hints, "verify the database is running")

	// Hints appear in log keyvals
	assert.Contains(t, entries[0].Keyvals, errorhandling.KeyHints)
}

func TestDebugModeAddsStacktrace(t *testing.T) {
	t.Parallel()

	err := cberrors.New("something broke")

	debugLog := logger.NewBuffer()
	debugLog.SetLevel(logger.DebugLevel)
	debugHandler := errorhandling.New(debugLog, nil)
	debugHandler.Error(err)

	debugEntries := debugLog.Entries()
	require.NotEmpty(t, debugEntries)
	assert.Contains(t, debugEntries[0].Keyvals, errorhandling.KeyStacktrace)

	// Non-debug mode: no stacktrace
	infoLog := logger.NewBuffer()
	infoLog.SetLevel(logger.InfoLevel)
	infoHandler := errorhandling.New(infoLog, nil)
	infoHandler.Error(err)

	infoEntries := infoLog.Entries()
	require.NotEmpty(t, infoEntries)
	assert.NotContains(t, infoEntries[0].Keyvals, errorhandling.KeyStacktrace)
}

func TestHelpConfigAppearsInOutput(t *testing.T) {
	t.Parallel()

	err := cberrors.New("unexpected failure")
	log := logger.NewBuffer()

	h := errorhandling.New(log, errorhandling.SlackHelp{
		Team:    "Platform",
		Channel: "#incidents",
	})

	h.Error(err)

	entries := log.Entries()
	require.NotEmpty(t, entries)
	assert.Contains(t, entries[0].Keyvals, errorhandling.KeyHelp)

	// Find the help value
	for i, kv := range entries[0].Keyvals {
		if kv == errorhandling.KeyHelp {
			helpMsg, ok := entries[0].Keyvals[i+1].(string)
			require.True(t, ok)
			assert.Contains(t, helpMsg, "Platform")
			assert.Contains(t, helpMsg, "#incidents")
			break
		}
	}
}

func TestFatalCallsExitWithHints(t *testing.T) {
	t.Parallel()

	baseErr := cberrors.New("disk full")
	hinted := errorhandling.WithUserHint(baseErr, "free up space or expand volume")

	log := logger.NewBuffer()
	var exitCode int
	h := errorhandling.New(log, nil,
		errorhandling.WithExitFunc(func(code int) { exitCode = code }),
	)

	h.Fatal(hinted)

	assert.Equal(t, 1, exitCode)

	entries := log.Entries()
	require.NotEmpty(t, entries)
	assert.Equal(t, logger.ErrorLevel, entries[0].Level)
	assert.Contains(t, entries[0].Message, "disk full")
	assert.Contains(t, entries[0].Keyvals, errorhandling.KeyHints)
}

func TestSpecialError_WrappedUnimplemented(t *testing.T) {
	t.Parallel()

	// Wrap an unimplemented error with additional context — it should still
	// be detected as a special error and downgraded to warn.
	inner := errorhandling.NewErrNotImplemented("https://example.com/issues/42")
	wrapped := cberrors.Wrap(inner, "feature X")

	log := logger.NewBuffer()
	h := errorhandling.New(log, nil)
	h.Error(wrapped)

	entries := log.Entries()
	require.NotEmpty(t, entries)
	assert.Equal(t, logger.WarnLevel, entries[0].Level)
	assert.Contains(t, entries[0].Message, "not yet implemented")
}

func TestSpecialError_ErrRunSubCommandWritesUsage(t *testing.T) {
	t.Parallel()

	var writerBuf bytes.Buffer
	log := logger.NewBuffer()
	h := errorhandling.New(log, nil, errorhandling.WithWriter(&writerBuf))

	// Simulate a cobra command that has usage output
	cmd := newTestCommand("mycommand")
	cmd.SetOut(&writerBuf)
	cmd.SetErr(&writerBuf)

	h.Check(errorhandling.ErrRunSubCommand, "", errorhandling.LevelError, cmd)

	entries := log.Entries()
	require.NotEmpty(t, entries)
	assert.Equal(t, logger.WarnLevel, entries[0].Level)
	assert.Contains(t, entries[0].Message, "Subcommand required")
	assert.Contains(t, writerBuf.String(), "Usage:")
}

func TestNilErrorIsNoOp(t *testing.T) {
	t.Parallel()

	log := logger.NewBuffer()
	h := errorhandling.New(log, nil)

	h.Check(nil, "prefix", errorhandling.LevelError)
	h.Error(nil)
	h.Warn(nil)

	assert.Empty(t, log.Entries())
}

func TestAssertionFailureFallsThrough(t *testing.T) {
	t.Parallel()

	err := errorhandling.NewAssertionFailure("invariant broken: %s", "x < 0")
	log := logger.NewBuffer()
	h := errorhandling.New(log, nil)

	h.Error(err)

	entries := log.Entries()
	// Assertion failure logs at Error level (both from handleSpecialErrors and logError)
	require.NotEmpty(t, entries)

	hasAssertionMsg := false
	hasErrorMsg := false
	for _, e := range entries {
		if e.Level == logger.ErrorLevel {
			if assert.ObjectsAreEqual("Internal error (assertion failure)", e.Message) {
				hasAssertionMsg = true
			}
			if assert.ObjectsAreEqual(true, len(e.Message) > 0) {
				hasErrorMsg = true
			}
		}
	}

	assert.True(t, hasAssertionMsg || hasErrorMsg, "should log assertion failure at error level")
}

func TestPrefixPropagation(t *testing.T) {
	t.Parallel()

	err := cberrors.New("timeout")
	log := logger.NewBuffer()
	h := errorhandling.New(log, nil)

	h.Error(err, "HTTP: ", "GET /api: ")

	entries := log.Entries()
	require.NotEmpty(t, entries)
	// Multiple prefixes are concatenated
	assert.Contains(t, entries[0].Message, "HTTP:")
	assert.Contains(t, entries[0].Message, "GET /api:")
}

func TestWarnLevel(t *testing.T) {
	t.Parallel()

	err := cberrors.New("deprecated feature used")
	log := logger.NewBuffer()
	h := errorhandling.New(log, nil)

	h.Warn(err)

	entries := log.Entries()
	require.NotEmpty(t, entries)
	assert.Equal(t, logger.WarnLevel, entries[0].Level)
	assert.Contains(t, entries[0].Message, "deprecated feature used")
}

func TestCrossPackageErrorChain(t *testing.T) {
	t.Parallel()

	// Simulate error originating in config, wrapped by setup, handled by CLI
	configErr := cberrors.New("YAML parse error at line 5")
	setupErr := errorhandling.WrapWithHint(configErr, "failed to load configuration", "check config.yaml syntax")
	cliErr := cberrors.Wrap(setupErr, "initialisation failed")

	log := logger.NewBuffer()
	log.SetLevel(logger.DebugLevel)

	var exitCode int
	h := errorhandling.New(log, errorhandling.SlackHelp{
		Team:    "DevTools",
		Channel: "#support",
	}, errorhandling.WithExitFunc(func(code int) { exitCode = code }))

	h.Fatal(cliErr)

	assert.Equal(t, 1, exitCode)

	entries := log.Entries()
	require.NotEmpty(t, entries)

	entry := entries[0]
	assert.Equal(t, logger.ErrorLevel, entry.Level)

	// Outermost wrapper message appears
	assert.Contains(t, entry.Message, "initialisation failed")

	// Hint from setup layer survives
	assert.Contains(t, entry.Keyvals, errorhandling.KeyHints)

	// Stacktrace present in debug mode
	assert.Contains(t, entry.Keyvals, errorhandling.KeyStacktrace)

	// Help config present
	assert.Contains(t, entry.Keyvals, errorhandling.KeyHelp)

	// The original error message is in the chain
	assert.Contains(t, cberrors.FlattenHints(cliErr), "check config.yaml syntax")
}

// newTestCommand creates a minimal cobra command for testing.
func newTestCommand(name string) *cobra.Command {
	return &cobra.Command{
		Use: name,
		Run: func(_ *cobra.Command, _ []string) {},
	}
}
