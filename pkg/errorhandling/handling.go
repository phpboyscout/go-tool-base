package errorhandling

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

const (
	LevelFatal = "fatal"
	// LevelFatalQuiet exits the process exactly like LevelFatal — honouring any
	// exit code attached via WithExitCode — but logs the message at debug rather
	// than error. It exists for expected, user-initiated terminations (e.g. a
	// SIGINT/SIGTERM interrupt) where the non-zero exit code is the signal and an
	// error-level log line would be noise. The message is still emitted at debug
	// so `--debug` continues to surface it.
	LevelFatalQuiet = "fatal-quiet"
	LevelError      = "error"
	LevelWarn       = "warn"
	KeyStacktrace   = "stacktrace"
	KeyHelp         = "help"
	KeyHints        = "hints"
	KeyDetails      = "details"
)

var (
	ErrNotImplemented = errors.New("command not yet implemented")
	ErrRunSubCommand  = errors.New("subcommand required")
)

// ExitFunc is the function called to terminate the process. Defaults to os.Exit.
// Override via WithExitFunc for testing.
type ExitFunc func(code int)

// ErrorHandler defines the interface for structured error reporting. It
// formats errors with hints, stack traces, and help channel information,
// then routes them to the appropriate output (logger, writer, or exit).
type ErrorHandler interface {
	Check(err error, prefix string, level string, cmd ...*cobra.Command)
	Fatal(err error, prefixes ...string)
	Error(err error, prefixes ...string)
	Warn(err error, prefixes ...string)
	SetUsage(usage func() error)
}

// StandardErrorHandler is the default ErrorHandler implementation.
// It extracts hints, details, and stack traces from cockroachdb/errors
// and formats them for terminal or structured output.
type StandardErrorHandler struct {
	Logger logger.Logger
	Help   HelpConfig
	Exit   ExitFunc
	Writer io.Writer
	Usage  func() error
}

// New creates an ErrorHandler with the given logger and help config.
// Options can override the exit function, output writer, or other defaults.
func New(l logger.Logger, help HelpConfig, opts ...Option) ErrorHandler {
	h := &StandardErrorHandler{
		Logger: l,
		Help:   help,
		Exit:   os.Exit,
		Writer: os.Stderr,
	}
	for _, opt := range opts {
		opt(h)
	}

	return h
}

// NewErrNotImplemented creates an unimplemented error with an optional issue tracker link.
func NewErrNotImplemented(issueURL string) error {
	return errors.UnimplementedError(
		errors.IssueLink{IssueURL: issueURL},
		"command not yet implemented",
	)
}

func (h *StandardErrorHandler) Check(err error, prefix string, level string, cmd ...*cobra.Command) {
	if err == nil {
		return
	}

	if h.handleSpecialErrors(err, cmd...) {
		return
	}

	h.logError(err, prefix, level)
}

func (h *StandardErrorHandler) handleSpecialErrors(err error, cmd ...*cobra.Command) bool {
	if errors.Is(err, ErrNotImplemented) || errors.HasUnimplementedError(err) {
		h.Logger.Warn("Command not yet implemented")

		if links := errors.GetAllIssueLinks(err); len(links) > 0 {
			h.Logger.Info("Track progress", "url", links[0].IssueURL)
		}

		return true
	}

	if errors.Is(err, ErrRunSubCommand) {
		if len(cmd) > 0 && cmd[0] != nil {
			cmd[0].SetOut(h.Writer)
			_ = cmd[0].Usage()
		} else if h.Usage != nil {
			_ = h.Usage()
		}

		h.Logger.Warn("Subcommand required")

		return true
	}

	if errors.HasAssertionFailure(err) {
		h.Logger.Error("Internal error (assertion failure)", "error", err)

		if h.Logger.Enabled(context.Background(), slog.LevelDebug) {
			h.Logger.Debug("Assertion detail", KeyStacktrace, fmt.Sprintf("%+v", err))
		}

		return false
	}

	return false
}

func (h *StandardErrorHandler) buildLogKVPairs(err error) []any {
	kvPairs := []any{}
	isDebug := h.Logger.Enabled(context.Background(), slog.LevelDebug)

	if isDebug {
		kvPairs = append(kvPairs, KeyStacktrace, extractStackTrace(err))
	}

	if hints := errors.FlattenHints(err); hints != "" {
		kvPairs = append(kvPairs, KeyHints, hints)
	}

	if isDebug {
		if details := errors.FlattenDetails(err); details != "" {
			kvPairs = append(kvPairs, KeyDetails, details)
		}
	}

	if h.Help != nil {
		if msg := h.Help.SupportMessage(); msg != "" {
			kvPairs = append(kvPairs, KeyHelp, msg)
		}
	}

	return kvPairs
}

func (h *StandardErrorHandler) logError(err error, prefix, level string) {
	l := h.Logger
	if len(prefix) > 0 {
		l = l.With("prefix", prefix)
	}

	kvPairs := h.buildLogKVPairs(err)

	switch level {
	case LevelFatal:
		l.Error(err.Error(), kvPairs...)
		h.Exit(ExitCode(err))
	case LevelFatalQuiet:
		l.Debug(err.Error(), kvPairs...)
		h.Exit(ExitCode(err))
	case LevelError:
		l.Error(err.Error(), kvPairs...)
	case LevelWarn:
		l.Warn(err.Error(), kvPairs...)
	default:
		// An unrecognised level must never silently swallow the error;
		// fall back to logging at Error so the failure is still surfaced.
		l.Error(err.Error(), kvPairs...)
	}
}

func (h *StandardErrorHandler) Fatal(err error, prefixes ...string) {
	h.Check(err, handlePrefix(prefixes...), LevelFatal)
}

func (h *StandardErrorHandler) Error(err error, prefixes ...string) {
	h.Check(err, handlePrefix(prefixes...), LevelError)
}

func (h *StandardErrorHandler) Warn(err error, prefixes ...string) {
	h.Check(err, handlePrefix(prefixes...), LevelWarn)
}

func (h *StandardErrorHandler) SetUsage(usage func() error) {
	h.Usage = usage
}

func handlePrefix(prefixes ...string) string {
	var prefix strings.Builder

	for _, p := range prefixes {
		prefix.WriteString(p)
	}

	return prefix.String()
}
