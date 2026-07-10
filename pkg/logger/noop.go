package logger

import (
	"context"
	"log/slog"
)

// NewNoop returns a Logger that discards all output. Useful for tests where log
// output is irrelevant. It is a *slog.Logger over a discard handler, so it
// satisfies the Logger interface directly.
func NewNoop() Logger {
	return slog.New(noopHandler{})
}

// noopHandler implements slog.Handler and discards all records.
type noopHandler struct{}

func (noopHandler) Enabled(_ context.Context, _ slog.Level) bool  { return false }
func (noopHandler) Handle(_ context.Context, _ slog.Record) error { return nil }
func (h noopHandler) WithAttrs(_ []slog.Attr) slog.Handler        { return h }
func (h noopHandler) WithGroup(_ string) slog.Handler             { return h }
