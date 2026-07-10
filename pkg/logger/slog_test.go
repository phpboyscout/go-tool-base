package logger

import (
	"bytes"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSlog() (*bytes.Buffer, Logger) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})

	return &buf, NewSlog(handler)
}

func TestSlogBackend_StructuredOutput(t *testing.T) {
	t.Parallel()

	buf, l := newTestSlog()

	l.Info("hello", "key", "value")

	output := buf.String()
	assert.Contains(t, output, "hello")
	assert.Contains(t, output, "key=value")
}

// TestSlogBackend_FormattedMessages replaces the removed Infof/... family: the
// caller pre-formats and the message reaches the handler.
func TestSlogBackend_FormattedMessages(t *testing.T) {
	t.Parallel()

	buf, l := newTestSlog()

	l.Info(fmt.Sprintf("count: %d", 42))
	l.Warn(fmt.Sprintf("file: %s", "test.go"))
	l.Error(fmt.Sprintf("err: %v", "bad"))
	l.Debug(fmt.Sprintf("debug: %t", true))

	output := buf.String()
	assert.Contains(t, output, "count: 42")
	assert.Contains(t, output, "file: test.go")
	assert.Contains(t, output, "err: bad")
	assert.Contains(t, output, "debug: true")
}

func TestSlogBackend_LevelFiltering(t *testing.T) {
	t.Parallel()

	// A plain slog logger owns its level via its handler options.
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	l := NewSlog(handler)

	l.Debug("debug hidden")
	l.Info("info hidden")
	l.Warn("warn visible")
	l.Error("error visible")

	output := buf.String()
	assert.NotContains(t, output, "debug hidden")
	assert.NotContains(t, output, "info hidden")
	assert.Contains(t, output, "warn visible")
	assert.Contains(t, output, "error visible")
}

// TestSlogBackend_LevelVarControl shows the slog-first way to control level at
// runtime: gate the handler with a shared *slog.LevelVar.
func TestSlogBackend_LevelVarControl(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	levelVar := new(slog.LevelVar)
	levelVar.Set(slog.LevelError)
	l := NewSlog(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: levelVar}))

	l.Info("hidden")
	assert.NotContains(t, buf.String(), "hidden")

	levelVar.Set(slog.LevelDebug)
	l.Info("visible")
	assert.Contains(t, buf.String(), "visible")
}

// TestSlogBackend_HelpersAreNoOps proves the no-op path of SetLevel/SetFormatter:
// a plain *slog.Logger implements neither Leveller nor Reformatter, so the
// helpers report that nothing was applied and leave the logger untouched.
func TestSlogBackend_HelpersAreNoOps(t *testing.T) {
	t.Parallel()

	buf, l := newTestSlog()

	assert.False(t, SetLevel(l, slog.LevelError), "plain slog must not implement Leveller")
	assert.False(t, SetFormatter(l, JSONFormatter), "plain slog must not implement Reformatter")

	// The handler's own level (debug) is unchanged, so info still emits.
	l.Info("still here")
	assert.Contains(t, buf.String(), "still here")
}

func TestSlogBackend_Handler(t *testing.T) {
	t.Parallel()

	_, l := newTestSlog()

	handler := l.Handler()
	require.NotNil(t, handler)

	slogLogger := slog.New(handler)
	require.NotNil(t, slogLogger)
}

func TestSlogBackend_With(t *testing.T) {
	t.Parallel()

	buf, l := newTestSlog()

	child := l.With("component", "test")
	child.Info("hello")

	output := buf.String()
	assert.Contains(t, output, "component=test")
	assert.Contains(t, output, "hello")
}

func TestSlogBackend_WithGroup(t *testing.T) {
	t.Parallel()

	buf, l := newTestSlog()

	child := l.WithGroup("http")
	child.Info("hello", "status", 200)

	output := buf.String()
	assert.Contains(t, output, "http.status=200")
	assert.Contains(t, output, "hello")
}

// TestLevelGate_DerivedHandlers exercises the slogLevelHandler's WithAttrs and
// WithGroup: attributes and groups added to a gated logger must survive and the
// gate must keep filtering.
func TestLevelGate_DerivedHandlers(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	levelVar := new(slog.LevelVar)
	levelVar.Set(slog.LevelInfo)
	base := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	l := slog.New(NewLevelGate(base, levelVar))

	derived := l.With("component", "gate").WithGroup("http")
	derived.Debug("suppressed")
	assert.NotContains(t, buf.String(), "suppressed", "gate must filter debug at info level")

	derived.Info("kept", "status", 200)
	output := buf.String()
	assert.Contains(t, output, "kept")
	assert.Contains(t, output, "component=gate")
	assert.Contains(t, output, "http.status=200")
}

func TestSlogBackend_InterfaceSatisfaction(t *testing.T) {
	t.Parallel()

	handler := slog.NewTextHandler(&bytes.Buffer{}, nil)
	_ = NewSlog(handler)
}

func TestSlogBackend_LevelConversion_RoundTrip(t *testing.T) {
	t.Parallel()

	levels := []Level{DebugLevel, InfoLevel, WarnLevel, ErrorLevel}
	for _, l := range levels {
		sl := toSlogLevel(l)
		back := fromSlogLevel(sl)
		assert.Equal(t, l, back, "round-trip failed for %s", l)
	}
}

func TestSlogBackend_FatalLevel_MapsToError(t *testing.T) {
	t.Parallel()

	// Fatal maps to slog.LevelError since slog has no Fatal.
	sl := toSlogLevel(FatalLevel)
	assert.Equal(t, slog.LevelError, sl)
}

func TestSlogBackend_UnknownLevelConversion(t *testing.T) {
	t.Parallel()

	// Unknown levels fall back to info in both directions.
	assert.Equal(t, slog.LevelInfo, toSlogLevel(Level(99)))
}
