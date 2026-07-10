package logger

import (
	"bytes"
	"fmt"
	"log/slog"
	"testing"

	charmlog "charm.land/log/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCharmBackend_StructuredOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := NewCharm(&buf, WithLevel(DebugLevel))

	l.Info("hello", "key", "value")

	output := buf.String()
	assert.Contains(t, output, "hello")
	assert.Contains(t, output, "key")
	assert.Contains(t, output, "value")
}

// TestCharmBackend_FormattedMessages replaces the removed Debugf/Infof/... family:
// callers pre-format with fmt.Sprintf and the message still reaches the writer.
func TestCharmBackend_FormattedMessages(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := NewCharm(&buf, WithLevel(DebugLevel))

	l.Info(fmt.Sprintf("count: %d", 42))
	l.Warn(fmt.Sprintf("file: %s", "test.go"))
	l.Error(fmt.Sprintf("error: %v", "bad"))
	l.Debug(fmt.Sprintf("debug: %t", true))

	output := buf.String()
	assert.Contains(t, output, "count: 42")
	assert.Contains(t, output, "file: test.go")
	assert.Contains(t, output, "error: bad")
	assert.Contains(t, output, "debug: true")
}

func TestCharmBackend_LevelFiltering(t *testing.T) {
	t.Parallel()

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

// TestCharmBackend_SetLevel exercises runtime level control via the Leveller
// helper (the interface no longer carries SetLevel). The charm logger implements
// Leveller, so the helper returns true and the new level takes effect.
func TestCharmBackend_SetLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := NewCharm(&buf, WithLevel(ErrorLevel))

	l.Info("hidden")
	assert.Empty(t, buf.String())

	require.True(t, SetLevel(l, slog.LevelDebug), "charm logger must implement Leveller")
	l.Info("visible")
	assert.Contains(t, buf.String(), "visible")
}

// TestCharmBackend_SetLevelConcrete exercises the concrete Leveller method on
// the returned *charmLogger directly.
func TestCharmBackend_SetLevelConcrete(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := NewCharm(&buf, WithLevel(ErrorLevel))

	cl, ok := l.(*charmLogger)
	require.True(t, ok)

	cl.SetLevel(slog.LevelDebug)
	cl.Debug("debug now visible")
	assert.Contains(t, buf.String(), "debug now visible")
}

// TestCharmBackend_SetFormatter drives runtime format switching via the
// Reformatter helper and observes the effect on emitted output.
func TestCharmBackend_SetFormatter(t *testing.T) {
	t.Parallel()

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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			l := NewCharm(&buf, WithLevel(DebugLevel))
			require.True(t, SetFormatter(l, tt.formatter), "charm logger must implement Reformatter")
			l.Info("hello", "k", "v")
			tt.check(t, buf.String())
		})
	}
}

func TestCharmBackend_Handler(t *testing.T) {
	t.Parallel()

	l := NewCharm(&bytes.Buffer{}, WithLevel(DebugLevel))

	handler := l.Handler()
	require.NotNil(t, handler)

	// Should be usable with slog.New
	slogLogger := slog.New(handler)
	require.NotNil(t, slogLogger)
}

func TestCharmBackend_Handler_SlogIntegration(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := NewCharm(&buf, WithLevel(DebugLevel))

	// Create slog.Logger from our handler
	slogLogger := slog.New(l.Handler())
	slogLogger.Info("from slog", "source", "test")

	output := buf.String()
	assert.Contains(t, output, "from slog")
}

func TestCharmBackend_With(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := NewCharm(&buf, WithLevel(DebugLevel))

	// With now returns *slog.Logger; it still writes to the same backend.
	child := l.With("component", "test")
	child.Info("hello")

	output := buf.String()
	assert.Contains(t, output, "component")
	assert.Contains(t, output, "test")
	assert.Contains(t, output, "hello")
}

func TestCharmBackend_Options(t *testing.T) {
	t.Parallel()

	t.Run("WithTimestamp", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		l := NewCharm(&buf, WithTimestamp(true), WithLevel(DebugLevel))
		l.Info("timestamped")
		// Timestamps add a time field to the output
		output := buf.String()
		assert.Contains(t, output, "timestamped")
	})

	t.Run("WithCaller", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		l := NewCharm(&buf, WithCaller(true), WithLevel(DebugLevel))
		l.Info("with caller")
		output := buf.String()
		assert.Contains(t, output, "with caller")
	})

	t.Run("WithPrefix", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		l := NewCharm(&buf, WithPrefix("pfx"), WithLevel(DebugLevel))
		l.Info("prefixed")
		output := buf.String()
		assert.Contains(t, output, "pfx")
	})
}

func TestCharmBackend_LevelMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		level Level
		want  charmlog.Level
	}{
		{DebugLevel, charmlog.DebugLevel},
		{InfoLevel, charmlog.InfoLevel},
		{WarnLevel, charmlog.WarnLevel},
		{ErrorLevel, charmlog.ErrorLevel},
		{FatalLevel, charmlog.FatalLevel},
		{Level(99), charmlog.InfoLevel}, // unknown falls back to info
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.level.String(), func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, toCharmLevel(tt.level))
		})
	}
}

func TestCharmBackend_FormatterMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		formatter Formatter
		want      charmlog.Formatter
	}{
		{TextFormatter, charmlog.TextFormatter},
		{JSONFormatter, charmlog.JSONFormatter},
		{LogfmtFormatter, charmlog.LogfmtFormatter},
		{Formatter(99), charmlog.TextFormatter}, // unknown falls back to text
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.formatter.String(), func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, toCharmFormatter(tt.formatter))
		})
	}
}

func TestCharmBackend_InterfaceSatisfaction(t *testing.T) {
	t.Parallel()

	_ = NewCharm(&bytes.Buffer{})
}

func TestCharmBackend_Inner(t *testing.T) {
	t.Parallel()

	l := NewCharm(&bytes.Buffer{})
	cl, ok := l.(*charmLogger)
	require.True(t, ok)
	assert.NotNil(t, cl.Inner())
}

func TestCharmBackend_AllLevels(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := NewCharm(&buf, WithLevel(DebugLevel))

	l.Debug("debug msg")
	l.Info("info msg")
	l.Warn("warn msg")
	l.Error("error msg")

	output := buf.String()
	for _, msg := range []string{"debug msg", "info msg", "warn msg", "error msg"} {
		assert.Contains(t, output, msg, "missing: %s", msg)
	}
}
