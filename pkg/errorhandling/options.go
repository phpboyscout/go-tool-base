package errorhandling

import (
	"io"
)

// Option is a functional option for configuring a StandardErrorHandler.
type Option func(*StandardErrorHandler)

// WithExitFunc allows injection of a custom exit handler (e.g. for testing).
func WithExitFunc(exit ExitFunc) Option {
	return func(eh *StandardErrorHandler) {
		eh.Exit = exit
	}
}

// WithWriter allows injection of a custom output writer.
func WithWriter(w io.Writer) Option {
	return func(eh *StandardErrorHandler) {
		eh.Writer = w
	}
}
