package logger

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCharmBackend_StructuredOutput(t *testing.T) {
	var buf bytes.Buffer
	l := NewCharm(&buf, WithLevel(DebugLevel))

	l.Info("hello", "key", "value")

	output := buf.String()
	assert.Contains(t, output, "hello")
	assert.Contains(t, output, "key")
	assert.Contains(t, output, "value")
}

func TestCharmBackend_PrintfMethods(t *testing.T) {
	var buf bytes.Buffer
	l := NewCharm(&buf, WithLevel(DebugLevel))

	l.Infof("count: %d", 42)
	l.Warnf("file: %s", "test.go")
	l.Errorf("error: %v", "bad")
	l.Debugf("debug: %t", true)

	output := buf.String()
	assert.Contains(t, output, "count: 42")
	assert.Contains(t, output, "file: test.go")
	assert.Contains(t, output, "error: bad")
	assert.Contains(t, output, "debug: true")
}

func TestCharmBackend_Print(t *testing.T) {
	var buf bytes.Buffer
	l := NewCharm(&buf, WithLevel(ErrorLevel))

	// Print should output regardless of level
	l.Print("always visible")

	output := buf.String()
	assert.Contains(t, output, "always visible")
}

func TestCharmBackend_LevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	l := NewCharm(&buf, WithLevel(WarnLevel))

	l.Debug("should not appear")
	l.Info("should not appear")
	l.Warn("should appear")
	l.Error("should also appear")

	output := buf.String()
	assert.NotContains(t, output, "should not appear")
	assert.Contains(t, output, "should appear")
	assert.Contains(t, output, "should also appear")
}

func TestCharmBackend_SetLevel(t *testing.T) {
	var buf bytes.Buffer
	l := NewCharm(&buf, WithLevel(ErrorLevel))

	l.Info("hidden")
	assert.Empty(t, buf.String())

	l.SetLevel(DebugLevel)
	l.Info("visible")
	assert.Contains(t, buf.String(), "visible")
}

func TestCharmBackend_GetLevel(t *testing.T) {
	l := NewCharm(&bytes.Buffer{}, WithLevel(WarnLevel))
	assert.Equal(t, WarnLevel, l.GetLevel())

	l.SetLevel(DebugLevel)
	assert.Equal(t, DebugLevel, l.GetLevel())
}

func TestCharmBackend_SetFormatter(t *testing.T) {
	tests := []struct {
		name      string
		formatter Formatter
		check     func(t *testing.T, output string)
	}{
		{
			name:      "JSON",
			formatter: JSONFormatter,
			check: func(t *testing.T, output string) {
				assert.Contains(t, output, `"msg"`)
			},
		},
		{
			name:      "Logfmt",
			formatter: LogfmtFormatter,
			check: func(t *testing.T, output string) {
				assert.Contains(t, output, "msg=")
			},
		},
		{
			name:      "Text",
			formatter: TextFormatter,
			check: func(t *testing.T, output string) {
				assert.Contains(t, output, "hello")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			l := NewCharm(&buf, WithLevel(DebugLevel))
			l.SetFormatter(tt.formatter)
			l.Info("hello", "k", "v")
			tt.check(t, buf.String())
		})
	}
}

func TestCharmBackend_Handler(t *testing.T) {
	l := NewCharm(&bytes.Buffer{}, WithLevel(DebugLevel))

	handler := l.Handler()
	require.NotNil(t, handler)

	// Should be usable with slog.New
	slogLogger := slog.New(handler)
	require.NotNil(t, slogLogger)
}

func TestCharmBackend_Handler_SlogIntegration(t *testing.T) {
	var buf bytes.Buffer
	l := NewCharm(&buf, WithLevel(DebugLevel))

	// Create slog.Logger from our handler
	slogLogger := slog.New(l.Handler())
	slogLogger.Info("from slog", "source", "test")

	output := buf.String()
	assert.Contains(t, output, "from slog")
}

func TestCharmBackend_With(t *testing.T) {
	var buf bytes.Buffer
	l := NewCharm(&buf, WithLevel(DebugLevel))

	child := l.With("component", "test")
	child.Info("hello")

	output := buf.String()
	assert.Contains(t, output, "component")
	assert.Contains(t, output, "test")
	assert.Contains(t, output, "hello")
}

func TestCharmBackend_WithPrefix(t *testing.T) {
	var buf bytes.Buffer
	l := NewCharm(&buf, WithLevel(DebugLevel))

	child := l.WithPrefix("myprefix")
	child.Info("hello")

	output := buf.String()
	assert.Contains(t, output, "myprefix")
	assert.Contains(t, output, "hello")
}

func TestCharmBackend_Options(t *testing.T) {
	t.Run("WithTimestamp", func(t *testing.T) {
		var buf bytes.Buffer
		l := NewCharm(&buf, WithTimestamp(true), WithLevel(DebugLevel))
		l.Info("timestamped")
		// Timestamps add a time field to the output
		output := buf.String()
		assert.Contains(t, output, "timestamped")
	})

	t.Run("WithCaller", func(t *testing.T) {
		var buf bytes.Buffer
		l := NewCharm(&buf, WithCaller(true), WithLevel(DebugLevel))
		l.Info("with caller")
		output := buf.String()
		assert.Contains(t, output, "with caller")
	})

	t.Run("WithPrefix", func(t *testing.T) {
		var buf bytes.Buffer
		l := NewCharm(&buf, WithPrefix("pfx"), WithLevel(DebugLevel))
		l.Info("prefixed")
		output := buf.String()
		assert.Contains(t, output, "pfx")
	})
}

func TestCharmBackend_LevelConversion_RoundTrip(t *testing.T) {
	levels := []Level{DebugLevel, InfoLevel, WarnLevel, ErrorLevel, FatalLevel}
	for _, l := range levels {
		charm := toCharmLevel(l)
		back := fromCharmLevel(charm)
		assert.Equal(t, l, back, "round-trip failed for %s", l)
	}
}

func TestCharmBackend_InterfaceSatisfaction(t *testing.T) {
	var _ Logger = NewCharm(&bytes.Buffer{})
}

func TestCharmBackend_Inner(t *testing.T) {
	l := NewCharm(&bytes.Buffer{})
	cl, ok := l.(*charmLogger)
	require.True(t, ok)
	assert.NotNil(t, cl.Inner())
}

func TestCharmBackend_AllLevels(t *testing.T) {
	var buf bytes.Buffer
	l := NewCharm(&buf, WithLevel(DebugLevel))

	l.Debug("debug msg")
	l.Info("info msg")
	l.Warn("warn msg")
	l.Error("error msg")

	output := buf.String()
	for _, msg := range []string{"debug msg", "info msg", "warn msg", "error msg"} {
		assert.True(t, strings.Contains(output, msg), "missing: %s", msg)
	}
}
