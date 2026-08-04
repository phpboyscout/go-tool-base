package logger_test

import (
	"bytes"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

func hinted() error {
	return errors.WithHint(errors.New("boom"), "try the other thing")
}

// TestPresentingHandler_LiftsHints is the point of the wrapper. Resolution
// alone leaves the hint nested in the error group, which is a poor way to show
// the one part of an error written for the person reading it.
func TestPresentingHandler_LiftsHints(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	l := logger.ToSlog(logger.NewCharm(&buf, logger.WithLevel(logger.InfoLevel)))
	l.Log(t.Context(), slog.LevelError, "it failed", "err", hinted())

	assert.Contains(t, buf.String(), `hints="try the other thing"`)
}

// TestPresentingHandler_ErrorGroupIsDebugOnly pins the noise decision: the
// error's message is already the log message, so rendering the group beside it
// repeats every failure back at the user.
func TestPresentingHandler_ErrorGroupIsDebugOnly(t *testing.T) {
	t.Parallel()

	t.Run("at info the group is omitted, the hint is not", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		l := logger.ToSlog(logger.NewCharm(&buf, logger.WithLevel(logger.InfoLevel)))
		l.Log(t.Context(), slog.LevelError, "it failed", "err", hinted())

		assert.NotContains(t, buf.String(), "kind=", "the error group is noise at info")
		assert.Contains(t, buf.String(), "try the other thing")
	})

	t.Run("at debug everything is there", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		l := logger.ToSlog(logger.NewCharm(&buf, logger.WithLevel(logger.DebugLevel)))
		l.Log(t.Context(), slog.LevelError, "it failed", "err", hinted())

		assert.Contains(t, buf.String(), "kind=")
		assert.Contains(t, buf.String(), "try the other thing")
	})
}

// TestPresentingHandler_HintlessErrorAddsNothing guards against a stray empty
// attribute on the overwhelming majority of errors, which carry no hint.
func TestPresentingHandler_HintlessErrorAddsNothing(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	l := logger.ToSlog(logger.NewCharm(&buf, logger.WithLevel(logger.InfoLevel)))
	l.Log(t.Context(), slog.LevelError, "it failed", "err", errors.New("boom"))

	assert.NotContains(t, buf.String(), "hints=")
}

// TestPresentingHandler_BufferCarriesHintsToo closes the gap that made this
// worth wiring in two places. Without it the test double drops what production
// shows, so a test asserting a hint's ABSENCE would pass for the wrong reason.
//
// Asserted on the captured attributes rather than through Contains, which
// searches messages only — the hint is an attribute, and always was.
func TestPresentingHandler_BufferCarriesHintsToo(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	logger.ToSlog(buf).Error("it failed", "err", hinted())

	entries := buf.Entries()
	require.Len(t, entries, 1)

	assert.Contains(t, fmt.Sprint(entries[0].Keyvals...), "try the other thing",
		"the buffer logger must carry hints exactly as the charm logger does")
}

// TestPresentingHandler_PassesThroughOrdinaryAttrs covers the common path.
func TestPresentingHandler_PassesThroughOrdinaryAttrs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	l := logger.ToSlog(logger.NewCharm(&buf, logger.WithLevel(logger.InfoLevel)))
	l.Log(t.Context(), slog.LevelInfo, "hello", "count", 3, "name", "gtb")

	out := buf.String()
	assert.Contains(t, out, "count=3")
	assert.Contains(t, out, "name=gtb")
	assert.NotContains(t, out, "hints=")
}

// TestPresentingHandler_WithAttrsAndGroup covers the delegating methods.
//
// Note what the group case documents rather than prevents: with a group active
// the lifted attribute nests as "g.hints". That follows slog's own rule for
// attributes added during Handle, and GTB uses no groups today — but it means
// the key is not stable at the top level, which is worth knowing before anyone
// builds a query on it.
func TestPresentingHandler_WithAttrsAndGroup(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	base := logger.NewPresentingHandler(
		slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	withAttrs := base.WithAttrs([]slog.Attr{slog.String("scope", "test")})
	require.NotNil(t, withAttrs)

	slog.New(withAttrs).Error("failed", "err", hinted())

	out := buf.String()
	assert.Contains(t, out, "scope=test", "attrs added through WithAttrs must survive")
	assert.Contains(t, out, "try the other thing")

	var grouped bytes.Buffer

	slog.New(logger.NewPresentingHandler(
		slog.NewTextHandler(&grouped, nil)).WithGroup("g")).
		Error("failed", "err", hinted())

	assert.Contains(t, grouped.String(), "g.hints=",
		"an active group nests the lifted attribute, per slog's own rule")
}
