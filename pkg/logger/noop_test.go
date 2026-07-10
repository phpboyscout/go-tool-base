package logger

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoopBackend_AllMethods(t *testing.T) {
	t.Parallel()

	l := NewNoop()
	ctx := context.Background()

	// None of these should panic and none produce output.
	l.Debug("debug")
	l.Info("info")
	l.Warn("warn")
	l.Error("error")
	l.DebugContext(ctx, "debug")
	l.InfoContext(ctx, "info")
	l.WarnContext(ctx, "warn")
	l.ErrorContext(ctx, "error")
	l.Log(ctx, slog.LevelInfo, "log")
	l.LogAttrs(ctx, slog.LevelInfo, "logattrs", slog.String("k", "v"))
	l.With("key", "value").Info("with")
	l.WithGroup("group").Info("grouped")
}

func TestNoopBackend_DiscardsEverything(t *testing.T) {
	t.Parallel()

	l := NewNoop()

	// The discard handler reports nothing enabled, at any level.
	assert.False(t, l.Enabled(context.Background(), slog.LevelError))
	assert.False(t, l.Enabled(context.Background(), slog.LevelDebug))
}

// TestNoopBackend_LevellerHelperNoOp proves the no-op path of the SetLevel /
// SetFormatter helpers: a plain *slog.Logger implements neither Leveller nor
// Reformatter, so both helpers report that no change was applied.
func TestNoopBackend_LevellerHelperNoOp(t *testing.T) {
	t.Parallel()

	l := NewNoop()

	assert.False(t, SetLevel(l, slog.LevelError), "noop must not implement Leveller")
	assert.False(t, SetFormatter(l, JSONFormatter), "noop must not implement Reformatter")
}

func TestNoopBackend_Handler(t *testing.T) {
	t.Parallel()

	l := NewNoop()
	h := l.Handler()
	require.NotNil(t, h)
}

func TestNoopBackend_InterfaceSatisfaction(t *testing.T) {
	t.Parallel()

	_ = NewNoop()
}
