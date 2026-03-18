package errorhandling

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
)

const (
	LevelFatal    = "fatal"
	LevelError    = "error"
	LevelWarn     = "warn"
	KeyStacktrace = "stacktrace"
	KeyHelp       = "help"
	KeyHints      = "hints"
	KeyDetails    = "details"
)

var (
	ErrNotImplemented = errors.New("command not yet implemented")
	ErrRunSubCommand  = errors.New("subcommand required")
)

type ExitFunc func(code int)

type ErrorHandler interface {
	Check(err error, prefix string, level string, cmd ...*cobra.Command)
	Fatal(err error, prefixes ...string)
	Error(err error, prefixes ...string)
	Warn(err error, prefixes ...string)
	SetUsage(usage func() error)
}

type StandardErrorHandler struct {
	Logger *log.Logger
	Help   HelpConfig
	Exit   ExitFunc
	Writer io.Writer
	Usage  func() error
}

func New(logger *log.Logger, help HelpConfig, opts ...Option) ErrorHandler {
	h := &StandardErrorHandler{
		Logger: logger,
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

		if h.Logger.GetLevel() == log.DebugLevel {
			h.Logger.Debug("Assertion detail", KeyStacktrace, fmt.Sprintf("%+v", err))
		}

		return false
	}

	return false
}

func (h *StandardErrorHandler) logError(err error, prefix, level string) {
	l := h.Logger
	if len(prefix) > 0 {
		l = l.WithPrefix(prefix)
	}

	kvPairs := []any{}

	// Stack trace in debug mode
	if h.Logger.GetLevel() == log.DebugLevel {
		kvPairs = append(kvPairs, KeyStacktrace, fmt.Sprintf("%+v", err))
	}

	// User-facing hints (always displayed when present)
	if hints := errors.FlattenHints(err); hints != "" {
		kvPairs = append(kvPairs, KeyHints, hints)
	}

	// Developer-facing details (debug mode only)
	if h.Logger.GetLevel() == log.DebugLevel {
		if details := errors.FlattenDetails(err); details != "" {
			kvPairs = append(kvPairs, KeyDetails, details)
		}
	}

	if h.Help != nil {
		if msg := h.Help.SupportMessage(); msg != "" {
			kvPairs = append(kvPairs, KeyHelp, msg)
		}
	}

	switch level {
	case LevelFatal:
		l.Error(err, kvPairs...)
		h.Exit(1)
	case LevelError:
		l.Error(err, kvPairs...)
	case LevelWarn:
		l.Warn(err, kvPairs...)
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
