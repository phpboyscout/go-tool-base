// Package logger provides a unified logging interface for GTB applications.
//
// It abstracts over multiple logging backends, allowing consumers to swap
// implementations without changing call sites. Two backends are provided:
//
//   - NewCharm: backed by charmbracelet/log, providing coloured, styled CLI output.
//     This is the default for interactive terminal applications.
//   - NewSlog: backed by any slog.Handler, providing ecosystem interoperability
//     with libraries like zap, zerolog, logrus, and OpenTelemetry.
//   - NewNoop: discards all output, useful for tests.
//
// The Logger interface exposes both structured (key-value) and printf-style
// logging methods, plus an unlevelled Print method for direct user output.
//
// For interoperability with libraries that require *slog.Logger, use:
//
//	slogLogger := slog.New(logger.Handler())
package logger
