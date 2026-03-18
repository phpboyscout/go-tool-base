package errorhandling

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestErrorHandler_Check(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewWithOptions(&buf, log.Options{
		Level:     log.InfoLevel,
		Formatter: log.TextFormatter,
	})

	h := &StandardErrorHandler{
		Logger: logger,
		Exit:   os.Exit,
		Writer: &buf,
	}
	// Mock exit to panic so we can catch it or verify it wasn't called (default is os.Exit)
	// But Check() only exits on Fatal.

	// Error case (should not exit)
	h.Error(errors.New("simple error"), "Prefix: ")
	assert.Contains(t, buf.String(), "simple error")
	assert.Contains(t, buf.String(), "Prefix:")

	// Warn case
	buf.Reset()
	h.Warn(errors.New("simple warning"), "Prefix: ")
	assert.Contains(t, buf.String(), "simple warning")

	// ErrNotImplemented case
	buf.Reset()
	h.Check(ErrNotImplemented, "", LevelError)
	assert.Contains(t, buf.String(), "WARN")
	assert.Contains(t, buf.String(), "Command not yet implemented")

	// ErrRunSubCommand case with cmd override
	buf.Reset()
	cmd := &cobra.Command{
		Use: "testcmd",
		Run: func(cmd *cobra.Command, args []string) {},
	}
	h.Check(ErrRunSubCommand, "", LevelError, cmd)
	assert.Contains(t, buf.String(), "WARN")
	assert.Contains(t, buf.String(), "Subcommand required")
	assert.Contains(t, buf.String(), "Usage:")

	// ErrRunSubCommand case with property
	buf.Reset()
	h.SetUsage(cmd.Usage)
	h.Check(ErrRunSubCommand, "", LevelError)
	assert.Contains(t, buf.String(), "WARN")
	assert.Contains(t, buf.String(), "Subcommand required")
	assert.Contains(t, buf.String(), "Usage:")

	// ErrRunSubCommand case via Error wrapper
	buf.Reset()
	h.Error(ErrRunSubCommand)
	assert.Contains(t, buf.String(), "WARN")
	assert.Contains(t, buf.String(), "Subcommand required")
	assert.Contains(t, buf.String(), "Usage:")
}

func TestErrorHandler_Fatal(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewWithOptions(&buf, log.Options{
		Level:     log.InfoLevel,
		Formatter: log.TextFormatter,
	})

	exitCalled := false
	exitCode := 0
	mockExit := func(code int) {
		exitCalled = true
		exitCode = code
	}

	h := &StandardErrorHandler{
		Logger: logger,
		Exit:   mockExit,
		Writer: &buf,
	}

	err := errors.New("fatal error")
	h.Fatal(err, "FATAL: ")

	assert.True(t, exitCalled)
	assert.Equal(t, 1, exitCode)
	assert.Contains(t, buf.String(), "fatal error")
}

