package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
)

// slogLogger wraps an slog.Handler to implement the Logger interface.
type slogLogger struct {
	handler slog.Handler
	inner   *slog.Logger
	level   *slog.LevelVar
}

// NewSlog returns a Logger backed by an slog.Handler. Use this when you need
// ecosystem integration (OpenTelemetry, Datadog, custom handlers).
//
// Any library that implements or bridges to slog.Handler works here:
//
//	Zap:     logger.NewSlog(zapslog.NewHandler(zapCore))
//	Zerolog: logger.NewSlog(slogzerolog.Option{Logger: &zl}.NewHandler())
//	OTEL:    logger.NewSlog(otelslog.NewHandler(exporter))
func NewSlog(handler slog.Handler) Logger {
	levelVar := &slog.LevelVar{}
	levelVar.Set(slog.LevelInfo)
	leveled := &slogLevelHandler{level: levelVar, handler: handler}

	return &slogLogger{
		handler: handler,
		inner:   slog.New(leveled),
		level:   levelVar,
	}
}

func (s *slogLogger) Debug(msg string, keyvals ...any) {
	s.inner.Debug(msg, keyvals...)
}

func (s *slogLogger) Info(msg string, keyvals ...any) {
	s.inner.Info(msg, keyvals...)
}

func (s *slogLogger) Warn(msg string, keyvals ...any) {
	s.inner.Warn(msg, keyvals...)
}

func (s *slogLogger) Error(msg string, keyvals ...any) {
	s.inner.Error(msg, keyvals...)
}

func (s *slogLogger) Fatal(msg string, keyvals ...any) {
	s.inner.Error(msg, keyvals...)
	os.Exit(1)
}

func (s *slogLogger) Debugf(format string, args ...any) {
	s.inner.Debug(fmt.Sprintf(format, args...))
}

func (s *slogLogger) Infof(format string, args ...any) {
	s.inner.Info(fmt.Sprintf(format, args...))
}

func (s *slogLogger) Warnf(format string, args ...any) {
	s.inner.Warn(fmt.Sprintf(format, args...))
}

func (s *slogLogger) Errorf(format string, args ...any) {
	s.inner.Error(fmt.Sprintf(format, args...))
}

func (s *slogLogger) Fatalf(format string, args ...any) {
	s.inner.Error(fmt.Sprintf(format, args...))
	os.Exit(1)
}

// Print emits at Info level; slog has no unlevelled output concept.
func (s *slogLogger) Print(msg any, keyvals ...any) {
	s.inner.Info(fmt.Sprint(msg), keyvals...)
}

func (s *slogLogger) With(keyvals ...any) Logger {
	return &slogLogger{
		handler: s.handler,
		inner:   s.inner.With(keyvals...),
		level:   s.level,
	}
}

func (s *slogLogger) WithPrefix(prefix string) Logger {
	return &slogLogger{
		handler: s.handler,
		inner:   s.inner.With("prefix", prefix),
		level:   s.level,
	}
}

func (s *slogLogger) SetLevel(level Level) {
	s.level.Set(toSlogLevel(level))
}

func (s *slogLogger) GetLevel() Level {
	return fromSlogLevel(s.level.Level())
}

// SetFormatter is a no-op for the slog backend.
// The output format is determined by the handler at construction time.
func (s *slogLogger) SetFormatter(_ Formatter) {}

// Handler returns the underlying slog.Handler for interoperability.
func (s *slogLogger) Handler() slog.Handler {
	return s.handler
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
