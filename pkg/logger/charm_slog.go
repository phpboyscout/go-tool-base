package logger

import (
	"io"
	"log/slog"
)

// NewCharmHandler builds a Charm-backed slog.Handler from the given options. It
// is the slog-first construction entry point: wrap it in slog.New (or use
// NewCharmSlog) to obtain a *slog.Logger with GTB's default human-friendly
// terminal output.
func NewCharmHandler(w io.Writer, opts ...CharmOption) slog.Handler {
	return NewCharm(w, opts...).Handler()
}

// NewCharmSlog builds a *slog.Logger backed by a Charm slog.Handler. This is the
// preferred way to construct GTB's default application logger under the
// slog-first design; the returned *slog.Logger satisfies the Logger interface.
func NewCharmSlog(w io.Writer, opts ...CharmOption) *slog.Logger {
	return slog.New(NewCharmHandler(w, opts...))
}

// NewLevelGate wraps an slog.Handler so its effective level is controlled at
// runtime by level. Records below level.Level() are dropped before reaching
// inner. This is how the application logger honours --debug and log.level after
// construction: build the base handler at its most permissive level and gate it
// with a shared *slog.LevelVar that the command layer mutates.
func NewLevelGate(inner slog.Handler, level *slog.LevelVar) slog.Handler {
	return &slogLevelHandler{level: level, handler: inner}
}
