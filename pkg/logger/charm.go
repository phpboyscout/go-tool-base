package logger

import (
	"io"
	"log/slog"

	"charm.land/log/v2"
)

// charmLogger is GTB's default application logger. It embeds a *slog.Logger
// (built over charmbracelet/log's slog.Handler) so it satisfies the Logger
// interface directly, and retains the underlying *log.Logger to implement the
// optional Leveller and Reformatter interfaces for runtime reconfiguration.
type charmLogger struct {
	*slog.Logger
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

// WithFormatter sets the initial output formatter.
func WithFormatter(f Formatter) CharmOption {
	return func(o *log.Options) {
		o.Formatter = toCharmFormatter(f)
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

	inner := log.NewWithOptions(w, o)

	return &charmLogger{Logger: slog.New(inner), inner: inner}
}

// SetLevel implements Leveller. charmbracelet/log's Level shares slog.Level's
// numeric values, so the conversion is direct.
func (c *charmLogger) SetLevel(level slog.Level) {
	c.inner.SetLevel(log.Level(level))
}

// SetFormatter implements Reformatter.
func (c *charmLogger) SetFormatter(f Formatter) {
	c.inner.SetFormatter(toCharmFormatter(f))
}

// toCharmFormatter maps a logger.Formatter to the charmbracelet equivalent.
// Unknown values fall back to the human-readable text formatter.
func toCharmFormatter(f Formatter) log.Formatter {
	switch f {
	case JSONFormatter:
		return log.JSONFormatter
	case LogfmtFormatter:
		return log.LogfmtFormatter
	case TextFormatter:
		return log.TextFormatter
	default:
		return log.TextFormatter
	}
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
