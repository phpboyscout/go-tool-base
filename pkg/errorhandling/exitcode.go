package errorhandling

import (
	"github.com/cockroachdb/errors"
)

// exitCodeError attaches a process exit code to an error without altering its
// message or identity. It is transparent to errors.Is/As via Unwrap.
type exitCodeError struct {
	cause error
	code  int
}

func (e *exitCodeError) Error() string { return e.cause.Error() }

func (e *exitCodeError) Unwrap() error { return e.cause }

// WithExitCode attaches a process exit code to err. The ErrorHandler's fatal
// path uses the attached code instead of the default 1, letting callers thread
// non-standard exit codes (for example the Unix 128+signum convention for
// signal-terminated runs) through the single exit path without a parallel
// os.Exit call site. Returns nil when err is nil.
func WithExitCode(err error, code int) error {
	if err == nil {
		return nil
	}

	return &exitCodeError{cause: err, code: code}
}

// ExitCode returns the exit code attached to err via WithExitCode. It returns
// 0 for a nil error and 1 for any error without an attached code. When codes
// are attached at multiple levels, the outermost (most recently applied)
// attachment wins, matching errors.As traversal order.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}

	var coded *exitCodeError
	if errors.As(err, &coded) {
		return coded.code
	}

	return 1
}
