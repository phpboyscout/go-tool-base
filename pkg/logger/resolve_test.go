package logger_test

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// notAnError is a LogValuer that is not an error, so resolution can be told
// apart from anything error-specific.
type notAnError struct{}

func (notAnError) LogValue() slog.Value {
	return slog.GroupValue(slog.String("inner", "resolved"))
}

// TestResolvingHandler_ResolvesLogValuer covers the slog contract charm's
// handler does not honour: an attribute implementing slog.LogValuer must be
// Resolve()d, or it renders as whatever its Go value happens to print as.
func TestResolvingHandler_ResolvesLogValuer(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	l := logger.ToSlog(logger.NewCharm(&buf, logger.WithLevel(logger.DebugLevel)))
	l.Log(t.Context(), slog.LevelError, "msg", "v", notAnError{})

	assert.Contains(t, buf.String(), "inner=resolved")
	assert.NotContains(t, buf.String(), "v={}", "an unresolved LogValuer prints as its bare Go value")
}

// TestResolvingHandler_ErrorReachesTheRecordWhole is what the migration turned
// on. errorhandling v0.2.0 hands the error to slog and expects the handler to
// take it apart; unresolved, everything below the message was lost.
func TestResolvingHandler_ErrorReachesTheRecordWhole(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	err := errors.WithHint(errors.New("boom"), "try the other thing")

	l := logger.ToSlog(logger.NewCharm(&buf, logger.WithLevel(logger.InfoLevel)))
	l.Log(t.Context(), slog.LevelError, "it failed", "err", err)

	out := buf.String()
	assert.Contains(t, out, "try the other thing", "the hint must survive into the record")
	assert.Contains(t, out, "kind=", "and so must everything else the error carries")
}

// TestResolvingHandler_PassesThroughOrdinaryAttrs covers the common path: a
// record with nothing to resolve must be unchanged.
func TestResolvingHandler_PassesThroughOrdinaryAttrs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	l := logger.ToSlog(logger.NewCharm(&buf, logger.WithLevel(logger.InfoLevel)))
	l.Log(t.Context(), slog.LevelInfo, "hello", "count", 3, "name", "gtb")

	out := buf.String()
	assert.Contains(t, out, "count=3")
	assert.Contains(t, out, "name=gtb")
}

// TestResolvingHandler_WithAttrsAndGroup covers the delegating methods, so a
// logger built through With/WithGroup keeps resolving rather than quietly
// reverting to the unwrapped handler.
func TestResolvingHandler_WithAttrsAndGroup(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	base := logger.NewResolvingHandler(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	withAttrs := base.WithAttrs([]slog.Attr{slog.String("scope", "test")})
	require.NotNil(t, withAttrs)

	slog.New(withAttrs).Error("failed", "v", notAnError{})

	out := buf.String()
	assert.Contains(t, out, "scope=test", "attrs added through WithAttrs must survive")
	assert.Contains(t, out, "inner=resolved", "and the wrapper must still resolve")

	assert.NotNil(t, base.WithGroup("g"))
}
