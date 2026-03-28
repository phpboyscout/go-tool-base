package errorhandling

import (
	"bytes"
	"errors"
	"os"
	"testing"

	cberrors "github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

func TestErrorHandler_Check(t *testing.T) {
	newHandler := func(buf *bytes.Buffer) *StandardErrorHandler {
		l := logger.NewCharm(buf)
		return &StandardErrorHandler{
			Logger: l,
			Exit:   os.Exit,
			Writer: buf,
		}
	}

	t.Run("Error_logs_message_with_prefix", func(t *testing.T) {
		var buf bytes.Buffer
		h := newHandler(&buf)
		h.Error(errors.New("simple error"), "Prefix: ")
		assert.Contains(t, buf.String(), "simple error")
		assert.Contains(t, buf.String(), "Prefix:")
	})

	t.Run("Warn_logs_warning", func(t *testing.T) {
		var buf bytes.Buffer
		h := newHandler(&buf)
		h.Warn(errors.New("simple warning"), "Prefix: ")
		assert.Contains(t, buf.String(), "simple warning")
	})

	t.Run("ErrNotImplemented_downgrades_to_warn", func(t *testing.T) {
		var buf bytes.Buffer
		h := newHandler(&buf)
		h.Check(ErrNotImplemented, "", LevelError)
		assert.Contains(t, buf.String(), "WARN")
		assert.Contains(t, buf.String(), "Command not yet implemented")
	})

	t.Run("ErrRunSubCommand_with_cmd_override", func(t *testing.T) {
		var buf bytes.Buffer
		h := newHandler(&buf)
		cmd := &cobra.Command{
			Use: "testcmd",
			Run: func(cmd *cobra.Command, args []string) {},
		}
		h.Check(ErrRunSubCommand, "", LevelError, cmd)
		assert.Contains(t, buf.String(), "WARN")
		assert.Contains(t, buf.String(), "Subcommand required")
		assert.Contains(t, buf.String(), "Usage:")
	})

	t.Run("ErrRunSubCommand_with_usage_property", func(t *testing.T) {
		var buf bytes.Buffer
		h := newHandler(&buf)
		cmd := &cobra.Command{
			Use: "testcmd",
			Run: func(cmd *cobra.Command, args []string) {},
		}
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		h.SetUsage(cmd.Usage)
		h.Check(ErrRunSubCommand, "", LevelError)
		assert.Contains(t, buf.String(), "WARN")
		assert.Contains(t, buf.String(), "Subcommand required")
		assert.Contains(t, buf.String(), "Usage:")
	})

	t.Run("ErrRunSubCommand_via_Error_wrapper", func(t *testing.T) {
		var buf bytes.Buffer
		h := newHandler(&buf)
		cmd := &cobra.Command{
			Use: "testcmd",
			Run: func(cmd *cobra.Command, args []string) {},
		}
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		h.SetUsage(cmd.Usage)
		h.Error(ErrRunSubCommand)
		assert.Contains(t, buf.String(), "WARN")
		assert.Contains(t, buf.String(), "Subcommand required")
		assert.Contains(t, buf.String(), "Usage:")
	})
}

func TestNewErrNotImplemented(t *testing.T) {
	t.Parallel()
	err := NewErrNotImplemented("https://github.com/org/repo/issues/1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not yet implemented")
	links := cberrors.GetAllIssueLinks(err)
	require.Len(t, links, 1)
	assert.Equal(t, "https://github.com/org/repo/issues/1", links[0].IssueURL)
}

func TestHandleSpecialErrors_UnimplementedWithIssueLink(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	l := logger.NewCharm(&buf)
	h := &StandardErrorHandler{Logger: l, Exit: os.Exit, Writer: &buf}

	err := NewErrNotImplemented("https://example.com/issue/99")
	handled := h.handleSpecialErrors(err)
	assert.True(t, handled)
	assert.Contains(t, buf.String(), "not yet implemented")
	assert.Contains(t, buf.String(), "https://example.com/issue/99")
}

func TestHandleSpecialErrors_AssertionFailure(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	l := logger.NewCharm(&buf, logger.WithLevel(logger.DebugLevel))
	h := &StandardErrorHandler{Logger: l, Exit: os.Exit, Writer: &buf}

	err := NewAssertionFailure("invariant violated: %s", "x must be positive")
	handled := h.handleSpecialErrors(err)
	assert.False(t, handled) // assertion failures fall through to logError
	assert.Contains(t, buf.String(), "Internal error")
}

func TestHandleSpecialErrors_ErrRunSubCommand_NilCmd(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	l := logger.NewCharm(&buf)
	h := &StandardErrorHandler{Logger: l, Exit: os.Exit, Writer: &buf}

	// No cmd argument, no Usage set — still returns true
	handled := h.handleSpecialErrors(ErrRunSubCommand)
	assert.True(t, handled)
}

func TestWithUserHint(t *testing.T) {
	t.Parallel()
	base := cberrors.New("base error")
	hinted := WithUserHint(base, "try restarting")
	assert.Contains(t, cberrors.FlattenHints(hinted), "try restarting")
}

func TestWithUserHintf(t *testing.T) {
	t.Parallel()
	base := cberrors.New("base error")
	hinted := WithUserHintf(base, "try %s", "again")
	assert.Contains(t, cberrors.FlattenHints(hinted), "try again")
}

func TestWrapWithHint(t *testing.T) {
	t.Parallel()
	base := cberrors.New("root cause")
	wrapped := WrapWithHint(base, "operation failed", "check your config")
	assert.Contains(t, wrapped.Error(), "operation failed")
	assert.Contains(t, cberrors.FlattenHints(wrapped), "check your config")
}

func TestNewAssertionFailure(t *testing.T) {
	t.Parallel()
	err := NewAssertionFailure("unexpected state: %d", 42)
	require.Error(t, err)
	assert.True(t, cberrors.HasAssertionFailure(err))
	assert.Contains(t, err.Error(), "unexpected state: 42")
}

func TestErrorHandler_Fatal(t *testing.T) {
	var buf bytes.Buffer
	l := logger.NewCharm(&buf)

	exitCalled := false
	exitCode := 0
	mockExit := func(code int) {
		exitCalled = true
		exitCode = code
	}

	h := &StandardErrorHandler{
		Logger: l,
		Exit:   mockExit,
		Writer: &buf,
	}

	err := errors.New("fatal error")
	h.Fatal(err, "FATAL: ")

	assert.True(t, exitCalled)
	assert.Equal(t, 1, exitCode)
	assert.Contains(t, buf.String(), "fatal error")
}
