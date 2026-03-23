package logger

import (
	"context"
	"log/slog"
)

// noopLogger is a Logger that discards all output.
type noopLogger struct {
	level Level
}

// NewNoop returns a Logger that discards all output. Useful for tests where
// log output is irrelevant.
func NewNoop() Logger {
	return &noopLogger{level: InfoLevel}
}

func (n *noopLogger) Debug(_ string, _ ...any) {}
func (n *noopLogger) Info(_ string, _ ...any)  {}
func (n *noopLogger) Warn(_ string, _ ...any)  {}
func (n *noopLogger) Error(_ string, _ ...any) {}
func (n *noopLogger) Fatal(_ string, _ ...any) {}

func (n *noopLogger) Debugf(_ string, _ ...any) {}
func (n *noopLogger) Infof(_ string, _ ...any)  {}
func (n *noopLogger) Warnf(_ string, _ ...any)  {}
func (n *noopLogger) Errorf(_ string, _ ...any) {}
func (n *noopLogger) Fatalf(_ string, _ ...any) {}

func (n *noopLogger) Print(_ any, _ ...any) {}

func (n *noopLogger) With(_ ...any) Logger       { return n }
func (n *noopLogger) WithPrefix(_ string) Logger { return n }
func (n *noopLogger) SetLevel(level Level)       { n.level = level }
func (n *noopLogger) GetLevel() Level            { return n.level }
func (n *noopLogger) SetFormatter(_ Formatter)   {}
func (n *noopLogger) Handler() slog.Handler      { return noopHandler{} }

// noopHandler implements slog.Handler and discards all records.
type noopHandler struct{}

func (noopHandler) Enabled(_ context.Context, _ slog.Level) bool  { return false }
func (noopHandler) Handle(_ context.Context, _ slog.Record) error { return nil }
func (h noopHandler) WithAttrs(_ []slog.Attr) slog.Handler        { return h }
func (h noopHandler) WithGroup(_ string) slog.Handler             { return h }
