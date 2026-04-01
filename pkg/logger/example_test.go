package logger_test

import (
	"os"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

func ExampleNewCharm() {
	// Create a charmbracelet-based logger for CLI output.
	l := logger.NewCharm(os.Stderr,
		logger.WithTimestamp(true),
		logger.WithLevel(logger.InfoLevel),
	)

	l.Info("Application started", "version", "1.0.0")
	l.Debug("This won't appear at InfoLevel")
}

func ExampleNewNoop() {
	// Create a silent logger for tests.
	l := logger.NewNoop()

	// All calls are no-ops — no output produced.
	l.Info("This produces no output")
	l.Error("Neither does this")
}

func ExampleNewBuffer() {
	// Create a buffer logger for testing log output.
	buf := logger.NewBuffer()

	buf.Info("test message", "key", "value")

	if buf.Contains("test message") {
		// Message was logged
	}
}
