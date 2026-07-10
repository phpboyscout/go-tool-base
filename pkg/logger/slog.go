package logger

import (
	"context"
	"log/slog"
)

// NewSlog returns a Logger backed by an slog.Handler. Under the slog-first
// design this is simply slog.New(handler); it exists as a named constructor for
// ecosystem integration (OpenTelemetry, Datadog, custom handlers).
//
// Any library that implements or bridges to slog.Handler works here:
//
//	Zap:     logger.NewSlog(zapslog.NewHandler(zapCore))
//	Zerolog: logger.NewSlog(slogzerolog.Option{Logger: &zl}.NewHandler())
//	OTEL:    logger.NewSlog(otelslog.NewHandler(exporter))
//
// For runtime level control, wrap the handler with NewLevelGate before passing
// it here (or use NewCharmSlog for GTB's default output).
func NewSlog(handler slog.Handler) Logger {
	return slog.New(handler)
}

// toSlogLevel converts a logger.Level to an slog.Level.
func toSlogLevel(l Level) slog.Level {
	switch l {
	case DebugLevel:
		return slog.LevelDebug
	case InfoLevel:
		return slog.LevelInfo
	case WarnLevel:
		return slog.LevelWarn
	case ErrorLevel, FatalLevel:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// fromSlogLevel converts an slog.Level to a logger.Level.
func fromSlogLevel(l slog.Level) Level {
	switch {
	case l <= slog.LevelDebug:
		return DebugLevel
	case l <= slog.LevelInfo:
		return InfoLevel
	case l <= slog.LevelWarn:
		return WarnLevel
	default:
		return ErrorLevel
	}
}

// slogLevelHandler wraps an slog.Handler to respect the LevelVar from the slogLogger.
// This is used internally so that SetLevel actually filters messages.
type slogLevelHandler struct {
	level   *slog.LevelVar
	handler slog.Handler
}

func (h *slogLevelHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *slogLevelHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.handler.Handle(ctx, r)
}

func (h *slogLevelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &slogLevelHandler{level: h.level, handler: h.handler.WithAttrs(attrs)}
}

func (h *slogLevelHandler) WithGroup(name string) slog.Handler {
	return &slogLevelHandler{level: h.level, handler: h.handler.WithGroup(name)}
}
