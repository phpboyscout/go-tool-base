package logger

import (
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBufferBackend_CapturesMessages(t *testing.T) {
	t.Parallel()

	buf := NewBuffer()

	buf.Info("hello world")
	buf.Warn("be careful")
	buf.Error("something broke")

	assert.Equal(t, 3, buf.Len())
	msgs := buf.Messages()
	assert.Equal(t, []string{"hello world", "be careful", "something broke"}, msgs)
}

func TestBufferBackend_Contains(t *testing.T) {
	t.Parallel()

	buf := NewBuffer()
	buf.Info("hello world")

	assert.True(t, buf.Contains("hello"))
	assert.True(t, buf.Contains("world"))
	assert.False(t, buf.Contains("goodbye"))
}

func TestBufferBackend_ContainsLevel(t *testing.T) {
	t.Parallel()

	buf := NewBuffer()
	buf.Info("info message")
	buf.Warn("warn message")
	buf.Error("error message")

	assert.True(t, buf.ContainsLevel(InfoLevel, "info"))
	assert.False(t, buf.ContainsLevel(ErrorLevel, "info"))
	assert.True(t, buf.ContainsLevel(ErrorLevel, "error"))
}

// TestBufferBackend_FormattedMessages replaces the removed Debugf/Infof/... family:
// callers now pre-format with fmt.Sprintf (or, preferably, log structured
// key/value pairs) and the buffer still captures the rendered message.
func TestBufferBackend_FormattedMessages(t *testing.T) {
	t.Parallel()

	buf := NewBuffer()

	buf.Info(fmt.Sprintf("count: %d", 42))
	buf.Warn(fmt.Sprintf("file: %s", "test.go"))
	buf.Error(fmt.Sprintf("err: %v", "bad"))
	buf.Debug(fmt.Sprintf("debug: %t", true))

	assert.True(t, buf.Contains("count: 42"))
	assert.True(t, buf.Contains("file: test.go"))
	assert.True(t, buf.Contains("err: bad"))
	assert.True(t, buf.Contains("debug: true"))
}

// TestBufferBackend_StructuredMessage shows the preferred slog-first form: a
// stable message plus typed key/value attributes, asserted via Entries().
func TestBufferBackend_StructuredMessage(t *testing.T) {
	t.Parallel()

	buf := NewBuffer()
	buf.Info("event", "component", "auth", "outcome", "denied")

	entries := buf.Entries()
	require.Len(t, entries, 1)
	assert.Equal(t, "event", entries[0].Message)
	assert.Equal(t, []any{"component", "auth", "outcome", "denied"}, entries[0].Keyvals)
}

func TestBufferBackend_LevelFiltering(t *testing.T) {
	t.Parallel()

	buf := NewBuffer()
	buf.SetLevel(slog.LevelWarn)

	buf.Debug("hidden debug")
	buf.Info("hidden info")
	buf.Warn("visible warn")
	buf.Error("visible error")

	assert.Equal(t, 2, buf.Len())
	assert.False(t, buf.Contains("hidden"))
	assert.True(t, buf.Contains("visible warn"))
	assert.True(t, buf.Contains("visible error"))
}

func TestBufferBackend_With(t *testing.T) {
	t.Parallel()

	buf := NewBuffer()

	// With now returns *slog.Logger; the derived logger shares the buffer's
	// capture store, so its records land back in the parent buffer.
	child := buf.With("component", "test")
	child.Info("hello")

	entries := buf.Entries()
	require.Len(t, entries, 1)
	assert.Contains(t, entries[0].Keyvals, "component")
	assert.Contains(t, entries[0].Keyvals, "test")
}

func TestBufferBackend_WithGroup(t *testing.T) {
	t.Parallel()

	buf := NewBuffer()

	// WithGroup also returns *slog.Logger over the shared store.
	child := buf.WithGroup("http")
	child.Info("grouped")

	assert.True(t, buf.Contains("grouped"))
}

func TestBufferBackend_Reset(t *testing.T) {
	t.Parallel()

	buf := NewBuffer()
	buf.Info("before reset")
	assert.Equal(t, 1, buf.Len())

	buf.Reset()
	assert.Equal(t, 0, buf.Len())
	assert.False(t, buf.Contains("before"))
}

func TestBufferBackend_String(t *testing.T) {
	t.Parallel()

	buf := NewBuffer()
	buf.Info("first")
	buf.Warn("second")

	s := buf.String()
	assert.Contains(t, s, "[info] first")
	assert.Contains(t, s, "[warn] second")
}

func TestBufferBackend_Entries(t *testing.T) {
	t.Parallel()

	buf := NewBuffer()
	buf.Info("msg", "key", "value")

	entries := buf.Entries()
	require.Len(t, entries, 1)
	assert.Equal(t, InfoLevel, entries[0].Level)
	assert.Equal(t, "msg", entries[0].Message)
	assert.Equal(t, []any{"key", "value"}, entries[0].Keyvals)
}

// TestBufferBackend_SetLevelGates replaces the removed GetLevel accessor: there
// is no level getter any more, so level state is asserted by observing what the
// buffer does and does not capture after SetLevel.
func TestBufferBackend_SetLevelGates(t *testing.T) {
	t.Parallel()

	buf := NewBuffer()

	// Default captures debug and above.
	buf.Debug("debug default")
	assert.True(t, buf.Contains("debug default"))

	buf.Reset()
	buf.SetLevel(slog.LevelError)

	buf.Debug("debug gated")
	buf.Info("info gated")
	buf.Warn("warn gated")
	buf.Error("error visible")

	assert.Equal(t, 1, buf.Len())
	assert.False(t, buf.Contains("gated"))
	assert.True(t, buf.Contains("error visible"))
}

// TestBufferBackend_LevellerHelper proves the buffer satisfies the optional
// Leveller interface so the package-level SetLevel helper drives it.
func TestBufferBackend_LevellerHelper(t *testing.T) {
	t.Parallel()

	buf := NewBuffer()

	require.True(t, SetLevel(buf, slog.LevelWarn), "buffer must implement Leveller")

	buf.Info("suppressed")
	buf.Warn("kept")

	assert.False(t, buf.Contains("suppressed"))
	assert.True(t, buf.Contains("kept"))
}

// TestBufferBackend_SetFormatterNoOp documents that the buffer does not
// implement Reformatter, so the SetFormatter helper is a no-op for it.
func TestBufferBackend_SetFormatterNoOp(t *testing.T) {
	t.Parallel()

	buf := NewBuffer()
	assert.False(t, SetFormatter(buf, JSONFormatter), "buffer must not implement Reformatter")
}

func TestBufferBackend_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	buf := NewBuffer()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			buf.Info(fmt.Sprintf("goroutine %d", n))
		}(i)
	}

	wg.Wait()
	assert.Equal(t, 100, buf.Len())
}

func TestBufferBackend_ConcurrentLevelAccess(t *testing.T) {
	t.Parallel()

	// Run under -race: concurrent SetLevel / record / read on the same logger
	// must be data-race clean now that the level is an atomic slog.LevelVar.
	buf := NewBuffer()

	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(3)

		go func() {
			defer wg.Done()
			buf.SetLevel(slog.LevelWarn)
		}()

		go func() {
			defer wg.Done()
			_ = buf.Len()
		}()

		go func() {
			defer wg.Done()
			buf.Info("concurrent")
		}()
	}

	wg.Wait()
}

func TestBufferBackend_InterfaceSatisfaction(t *testing.T) {
	t.Parallel()

	var _ Logger = NewBuffer()
}

func TestBufferBackend_Handler(t *testing.T) {
	t.Parallel()

	buf := NewBuffer()
	h := buf.Handler()
	assert.NotNil(t, h)
}
