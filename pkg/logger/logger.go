package logger

import (
	"context"
	// Deliberate exception to the repo-wide cockroachdb/errors rule: logger is a
	// foundational, near-leaf package that every other package depends on, so it
	// must not pull in the heavier cockroachdb/errors tree. The sentinel and the
	// single fmt.Errorf %w wrap below stay on the stdlib.
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// Logger is the unified logging interface for GTB. All packages accept this
// interface instead of a concrete logger type.
//
// Logger is NOT safe for concurrent use unless the underlying backend
// documents otherwise. The charmbracelet and slog backends provided by
// this package are both safe for concurrent use.
// Logger is GTB's logging boundary. Its method set mirrors *slog.Logger exactly,
// so a *slog.Logger satisfies it directly and can be passed anywhere a Logger is
// expected — while consumers remain free to supply their own implementation.
//
// Runtime level/format control (SetLevel/SetFormatter) is deliberately NOT part
// of this interface: those are application concerns, exposed via the optional
// Leveller and Reformatter interfaces and the SetLevel/SetFormatter helpers, so
// only the command layer adjusts them. Process termination (Fatal) belongs to
// command/errorhandling layers, and direct user output (Print) to pkg/output or
// Cobra writers — neither belongs on the logging boundary.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)

	DebugContext(ctx context.Context, msg string, args ...any)
	InfoContext(ctx context.Context, msg string, args ...any)
	WarnContext(ctx context.Context, msg string, args ...any)
	ErrorContext(ctx context.Context, msg string, args ...any)

	Log(ctx context.Context, level slog.Level, msg string, args ...any)
	LogAttrs(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr)

	With(args ...any) *slog.Logger
	WithGroup(name string) *slog.Logger

	Enabled(ctx context.Context, level slog.Level) bool
	Handler() slog.Handler
}

// Leveller is optionally implemented by a Logger whose minimum level can be
// changed at runtime. GTB's default (Charm-backed) logger implements it; a plain
// *slog.Logger does not.
type Leveller interface {
	SetLevel(level slog.Level)
}

// Reformatter is optionally implemented by a Logger whose output format can be
// changed at runtime.
type Reformatter interface {
	SetFormatter(f Formatter)
}

// SetLevel changes log's minimum level when it implements Leveller, reporting
// whether the change was applied. A plain *slog.Logger does not implement
// Leveller, so SetLevel is a no-op for it (it owns its own level via its
// handler).
func SetLevel(log Logger, level slog.Level) bool {
	if l, ok := log.(Leveller); ok {
		l.SetLevel(level)

		return true
	}

	return false
}

// SetFormatter changes log's output format when it implements Reformatter,
// reporting whether the change was applied.
func SetFormatter(log Logger, f Formatter) bool {
	if l, ok := log.(Reformatter); ok {
		l.SetFormatter(f)

		return true
	}

	return false
}

// Level represents a logging severity level.
type Level int

const (
	// DebugLevel is the most verbose level.
	DebugLevel Level = iota
	// InfoLevel is the default level.
	InfoLevel
	// WarnLevel is for potentially harmful situations.
	WarnLevel
	// ErrorLevel is for error conditions.
	ErrorLevel
	// FatalLevel is for fatal conditions that terminate the process.
	FatalLevel
)

// ErrInvalidLevel is returned when parsing an unrecognised level string.
var ErrInvalidLevel = errors.New("invalid level")

// ParseLevel converts a level string ("debug", "info", "warn", "error", "fatal")
// to a Level value.
func ParseLevel(s string) (Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return DebugLevel, nil
	case "info":
		return InfoLevel, nil
	case "warn":
		return WarnLevel, nil
	case "error":
		return ErrorLevel, nil
	case "fatal":
		return FatalLevel, nil
	default:
		return 0, fmt.Errorf("%w: %q", ErrInvalidLevel, s)
	}
}

// String returns the level name.
func (l Level) String() string {
	switch l {
	case DebugLevel:
		return "debug"
	case InfoLevel:
		return "info"
	case WarnLevel:
		return "warn"
	case ErrorLevel:
		return "error"
	case FatalLevel:
		return "fatal"
	default:
		return "unknown"
	}
}

// Formatter represents a log output format.
type Formatter int

const (
	// TextFormatter formats log messages as human-readable text.
	TextFormatter Formatter = iota
	// JSONFormatter formats log messages as JSON.
	JSONFormatter
	// LogfmtFormatter formats log messages as logfmt.
	LogfmtFormatter
)

// ErrInvalidFormat is returned when parsing an unrecognised formatter string.
var ErrInvalidFormat = errors.New("invalid format")

// String returns the formatter name ("text", "json", "logfmt").
func (f Formatter) String() string {
	switch f {
	case TextFormatter:
		return "text"
	case JSONFormatter:
		return "json"
	case LogfmtFormatter:
		return "logfmt"
	default:
		return "unknown"
	}
}

// ParseFormatter converts a formatter string ("text", "json", "logfmt") to a
// Formatter value. The comparison is case-insensitive.
func ParseFormatter(s string) (Formatter, error) {
	switch strings.ToLower(s) {
	case "text":
		return TextFormatter, nil
	case "json":
		return JSONFormatter, nil
	case "logfmt":
		return LogfmtFormatter, nil
	default:
		return 0, fmt.Errorf("%w: %q", ErrInvalidFormat, s)
	}
}
