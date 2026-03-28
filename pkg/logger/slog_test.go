package logger

import (
	"bytes"
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
	buf, l := newTestSlog()

	l.Info("hello", "key", "value")

	output := buf.String()
	assert.Contains(t, output, "hello")
	assert.Contains(t, output, "key=value")
}

func TestSlogBackend_PrintfMethods(t *testing.T) {
	buf, l := newTestSlog()
	l.SetLevel(DebugLevel) // enable debug output

	l.Infof("count: %d", 42)
	l.Warnf("file: %s", "test.go")
	l.Errorf("err: %v", "bad")
	l.Debugf("debug: %t", true)

	output := buf.String()
	assert.Contains(t, output, "count: 42")
	assert.Contains(t, output, "file: test.go")
	assert.Contains(t, output, "err: bad")
	assert.Contains(t, output, "debug: true")
}

func TestSlogBackend_Print(t *testing.T) {
	buf, l := newTestSlog()

	l.Print("unlevelled output")

	// slog backend emits Print at Info level
	output := buf.String()
	assert.Contains(t, output, "unlevelled output")
	assert.Contains(t, output, "level=INFO")
}

func TestSlogBackend_LevelFiltering(t *testing.T) {
	buf, l := newTestSlog()
	l.SetLevel(WarnLevel)

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

func TestSlogBackend_SetLevel(t *testing.T) {
	buf, l := newTestSlog()
	l.SetLevel(ErrorLevel)

	l.Info("hidden")
	assert.NotContains(t, buf.String(), "hidden")

	l.SetLevel(DebugLevel)
	l.Info("visible")
	assert.Contains(t, buf.String(), "visible")
}

func TestSlogBackend_GetLevel(t *testing.T) {
	_, l := newTestSlog()

	assert.Equal(t, InfoLevel, l.GetLevel())

	l.SetLevel(WarnLevel)
	assert.Equal(t, WarnLevel, l.GetLevel())

	l.SetLevel(DebugLevel)
	assert.Equal(t, DebugLevel, l.GetLevel())
}

func TestSlogBackend_Handler(t *testing.T) {
	_, l := newTestSlog()

	handler := l.Handler()
	require.NotNil(t, handler)

	slogLogger := slog.New(handler)
	require.NotNil(t, slogLogger)
}

func TestSlogBackend_SetFormatter_NoOp(t *testing.T) {
	_, l := newTestSlog()

	// Should not panic
	l.SetFormatter(JSONFormatter)
	l.SetFormatter(LogfmtFormatter)
	l.SetFormatter(TextFormatter)
}

func TestSlogBackend_With(t *testing.T) {
	buf, l := newTestSlog()

	child := l.With("component", "test")
	child.Info("hello")

	output := buf.String()
	assert.Contains(t, output, "component=test")
	assert.Contains(t, output, "hello")
}

func TestSlogBackend_WithPrefix(t *testing.T) {
	buf, l := newTestSlog()

	child := l.WithPrefix("myprefix")
	child.Info("hello")

	output := buf.String()
	assert.Contains(t, output, "prefix=myprefix")
	assert.Contains(t, output, "hello")
}

func TestSlogBackend_InterfaceSatisfaction(t *testing.T) {
	handler := slog.NewTextHandler(&bytes.Buffer{}, nil)
	_ = NewSlog(handler)
}

func TestSlogBackend_LevelConversion_RoundTrip(t *testing.T) {
	levels := []Level{DebugLevel, InfoLevel, WarnLevel, ErrorLevel}
	for _, l := range levels {
		sl := toSlogLevel(l)
		back := fromSlogLevel(sl)
		assert.Equal(t, l, back, "round-trip failed for %s", l)
	}
}

func TestSlogBackend_FatalLevel_MapsToError(t *testing.T) {
	// Fatal maps to slog.LevelError since slog has no Fatal
	sl := toSlogLevel(FatalLevel)
	assert.Equal(t, slog.LevelError, sl)
}
