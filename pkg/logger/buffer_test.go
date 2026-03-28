package logger

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBufferBackend_CapturesMessages(t *testing.T) {
	buf := NewBuffer()

	buf.Info("hello world")
	buf.Warn("be careful")
	buf.Error("something broke")

	assert.Equal(t, 3, buf.Len())
	msgs := buf.Messages()
	assert.Equal(t, []string{"hello world", "be careful", "something broke"}, msgs)
}

func TestBufferBackend_Contains(t *testing.T) {
	buf := NewBuffer()
	buf.Info("hello world")

	assert.True(t, buf.Contains("hello"))
	assert.True(t, buf.Contains("world"))
	assert.False(t, buf.Contains("goodbye"))
}

func TestBufferBackend_ContainsLevel(t *testing.T) {
	buf := NewBuffer()
	buf.Info("info message")
	buf.Warn("warn message")
	buf.Error("error message")

	assert.True(t, buf.ContainsLevel(InfoLevel, "info"))
	assert.False(t, buf.ContainsLevel(ErrorLevel, "info"))
	assert.True(t, buf.ContainsLevel(ErrorLevel, "error"))
}

func TestBufferBackend_PrintfMethods(t *testing.T) {
	buf := NewBuffer()

	buf.Infof("count: %d", 42)
	buf.Warnf("file: %s", "test.go")
	buf.Errorf("err: %v", "bad")
	buf.Debugf("debug: %t", true)

	assert.True(t, buf.Contains("count: 42"))
	assert.True(t, buf.Contains("file: test.go"))
	assert.True(t, buf.Contains("err: bad"))
	assert.True(t, buf.Contains("debug: true"))
}

func TestBufferBackend_Print(t *testing.T) {
	buf := NewBuffer()
	buf.Print("unlevelled")

	require.Equal(t, 1, buf.Len())
	assert.Equal(t, InfoLevel, buf.Entries()[0].Level)
	assert.Equal(t, "unlevelled", buf.Entries()[0].Message)
}

func TestBufferBackend_LevelFiltering(t *testing.T) {
	buf := NewBuffer()
	buf.SetLevel(WarnLevel)

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
	buf := NewBuffer()

	child := buf.With("component", "test")
	child.Info("hello")

	entries := buf.Entries()
	require.Len(t, entries, 1)
	assert.Contains(t, entries[0].Keyvals, "component")
	assert.Contains(t, entries[0].Keyvals, "test")
}

func TestBufferBackend_WithPrefix(t *testing.T) {
	buf := NewBuffer()

	child := buf.WithPrefix("myprefix")
	child.Info("hello")

	assert.True(t, buf.Contains("myprefix: hello"))
}

func TestBufferBackend_Reset(t *testing.T) {
	buf := NewBuffer()
	buf.Info("before reset")
	assert.Equal(t, 1, buf.Len())

	buf.Reset()
	assert.Equal(t, 0, buf.Len())
	assert.False(t, buf.Contains("before"))
}

func TestBufferBackend_String(t *testing.T) {
	buf := NewBuffer()
	buf.Info("first")
	buf.Warn("second")

	s := buf.String()
	assert.Contains(t, s, "[info] first")
	assert.Contains(t, s, "[warn] second")
}

func TestBufferBackend_Entries(t *testing.T) {
	buf := NewBuffer()
	buf.Info("msg", "key", "value")

	entries := buf.Entries()
	require.Len(t, entries, 1)
	assert.Equal(t, InfoLevel, entries[0].Level)
	assert.Equal(t, "msg", entries[0].Message)
	assert.Equal(t, []any{"key", "value"}, entries[0].Keyvals)
}

func TestBufferBackend_GetLevel(t *testing.T) {
	buf := NewBuffer()
	assert.Equal(t, DebugLevel, buf.GetLevel())

	buf.SetLevel(ErrorLevel)
	assert.Equal(t, ErrorLevel, buf.GetLevel())
}

func TestBufferBackend_ConcurrentAccess(t *testing.T) {
	buf := NewBuffer()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			buf.Infof("goroutine %d", n)
		}(i)
	}

	wg.Wait()
	assert.Equal(t, 100, buf.Len())
}

func TestBufferBackend_InterfaceSatisfaction(t *testing.T) {
	var _ Logger = NewBuffer()
}

func TestBufferBackend_Handler(t *testing.T) {
	buf := NewBuffer()
	h := buf.Handler()
	assert.NotNil(t, h)
}

func TestBufferBackend_SetFormatter_NoOp(t *testing.T) {
	buf := NewBuffer()
	// Should not panic
	buf.SetFormatter(JSONFormatter)
	buf.SetFormatter(LogfmtFormatter)
	buf.SetFormatter(TextFormatter)
}

func TestBufferBackend_Fatal(t *testing.T) {
	buf := NewBuffer()
	buf.Fatal("fatal msg")
	buf.Fatalf("fatal %s", "formatted")

	assert.True(t, buf.ContainsLevel(FatalLevel, "fatal msg"))
	assert.True(t, buf.ContainsLevel(FatalLevel, "fatal formatted"))
}
