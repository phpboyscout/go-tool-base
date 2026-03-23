package logger

import (
	"io"
	"log/slog"
	"os"

	"github.com/charmbracelet/log"
)

// charmLogger wraps charmbracelet/log to implement the Logger interface.
type charmLogger struct {
	inner *log.Logger
}

// CharmOption configures the charmbracelet backend.
type CharmOption func(*log.Options)

// WithTimestamp enables or disables timestamps in log output.
func WithTimestamp(enabled bool) CharmOption {
	return func(o *log.Options) {
		o.ReportTimestamp = enabled
	}
}

// WithCaller enables or disables caller location in log output.
func WithCaller(enabled bool) CharmOption {
	return func(o *log.Options) {
		o.ReportCaller = enabled
	}
}

// WithLevel sets the initial log level.
func WithLevel(level Level) CharmOption {
	return func(o *log.Options) {
		o.Level = toCharmLevel(level)
	}
}

// WithPrefix sets an initial prefix on the logger.
func WithPrefix(prefix string) CharmOption {
	return func(o *log.Options) {
		o.Prefix = prefix
	}
}

// NewCharm returns a Logger backed by charmbracelet/log. This is the default
// backend for CLI applications, providing coloured, styled terminal output.
func NewCharm(w io.Writer, opts ...CharmOption) Logger {
	o := log.Options{
		Level: log.InfoLevel,
	}
	for _, opt := range opts {
		opt(&o)
	}

	return &charmLogger{inner: log.NewWithOptions(w, o)}
}

func (c *charmLogger) Debug(msg string, keyvals ...any) {
	c.inner.Debug(msg, keyvals...)
}

func (c *charmLogger) Info(msg string, keyvals ...any) {
	c.inner.Info(msg, keyvals...)
}

func (c *charmLogger) Warn(msg string, keyvals ...any) {
	c.inner.Warn(msg, keyvals...)
}

func (c *charmLogger) Error(msg string, keyvals ...any) {
	c.inner.Error(msg, keyvals...)
}

func (c *charmLogger) Fatal(msg string, keyvals ...any) {
	c.inner.Fatal(msg, keyvals...)
	os.Exit(1) // charmbracelet/log already calls os.Exit, but just in case
}

func (c *charmLogger) Debugf(format string, args ...any) {
	c.inner.Debugf(format, args...)
}

func (c *charmLogger) Infof(format string, args ...any) {
	c.inner.Infof(format, args...)
}

func (c *charmLogger) Warnf(format string, args ...any) {
	c.inner.Warnf(format, args...)
}

func (c *charmLogger) Errorf(format string, args ...any) {
	c.inner.Errorf(format, args...)
}

func (c *charmLogger) Fatalf(format string, args ...any) {
	c.inner.Fatalf(format, args...)
}

func (c *charmLogger) Print(msg any, keyvals ...any) {
	c.inner.Print(msg, keyvals...)
}

func (c *charmLogger) With(keyvals ...any) Logger {
	return &charmLogger{inner: c.inner.With(keyvals...)}
}

func (c *charmLogger) WithPrefix(prefix string) Logger {
	return &charmLogger{inner: c.inner.WithPrefix(prefix)}
}

func (c *charmLogger) SetLevel(level Level) {
	c.inner.SetLevel(toCharmLevel(level))
}

func (c *charmLogger) GetLevel() Level {
	return fromCharmLevel(c.inner.GetLevel())
}

func (c *charmLogger) SetFormatter(f Formatter) {
	switch f {
	case JSONFormatter:
		c.inner.SetFormatter(log.JSONFormatter)
	case LogfmtFormatter:
		c.inner.SetFormatter(log.LogfmtFormatter)
	case TextFormatter:
		c.inner.SetFormatter(log.TextFormatter)
	}
}

// Handler returns an slog.Handler for interoperability.
// charmbracelet/log *Logger natively implements slog.Handler.
func (c *charmLogger) Handler() slog.Handler {
	return c.inner
}

// Inner returns the underlying charmbracelet/log *Logger.
// This is an escape hatch for code that needs access to charm-specific
// features not exposed by the Logger interface (e.g., SetStyles, SetOutput).
func (c *charmLogger) Inner() *log.Logger {
	return c.inner
}

// toCharmLevel converts a logger.Level to a charmbracelet/log.Level.
func toCharmLevel(l Level) log.Level {
	switch l {
	case DebugLevel:
		return log.DebugLevel
	case InfoLevel:
		return log.InfoLevel
	case WarnLevel:
		return log.WarnLevel
	case ErrorLevel:
		return log.ErrorLevel
	case FatalLevel:
		return log.FatalLevel
	default:
		return log.InfoLevel
	}
}

// fromCharmLevel converts a charmbracelet/log.Level to a logger.Level.
func fromCharmLevel(l log.Level) Level {
	switch l {
	case log.DebugLevel:
		return DebugLevel
	case log.InfoLevel:
		return InfoLevel
	case log.WarnLevel:
		return WarnLevel
	case log.ErrorLevel:
		return ErrorLevel
	case log.FatalLevel:
		return FatalLevel
	default:
		return InfoLevel
	}
}
