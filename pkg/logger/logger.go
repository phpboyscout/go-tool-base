package logger

import (
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
type Logger interface {
	// Structured logging methods. keyvals are alternating key/value pairs.
	Debug(msg string, keyvals ...any)
	Info(msg string, keyvals ...any)
	Warn(msg string, keyvals ...any)
	Error(msg string, keyvals ...any)
	Fatal(msg string, keyvals ...any)

	// Printf-style logging methods.
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)

	// Print writes an unlevelled message that is not filtered by log level.
	// Used for direct user-facing output (e.g., version info, release notes).
	Print(msg any, keyvals ...any)

	// With returns a new Logger with the given key-value pairs prepended
	// to every subsequent log call.
	With(keyvals ...any) Logger

	// WithPrefix returns a new Logger with the given prefix prepended to
	// every message.
	WithPrefix(prefix string) Logger

	// SetLevel changes the minimum log level dynamically.
	SetLevel(level Level)

	// GetLevel returns the current minimum log level.
	GetLevel() Level

	// SetFormatter changes the output format (text, json, logfmt).
	// Backends that do not support a given formatter silently ignore the call.
	SetFormatter(f Formatter)

	// Handler returns an slog.Handler for interoperability with libraries
	// that require *slog.Logger. Usage: slog.New(logger.Handler())
	Handler() slog.Handler
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
