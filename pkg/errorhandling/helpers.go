package errorhandling

import "github.com/cockroachdb/errors"

// WithUserHint attaches a user-facing recovery suggestion to an error.
func WithUserHint(err error, hint string) error {
	return errors.WithHint(err, hint)
}

// WithUserHintf attaches a formatted user-facing recovery suggestion.
func WithUserHintf(err error, format string, args ...any) error {
	return errors.WithHintf(err, format, args...)
}

// WrapWithHint wraps an error with a message and attaches a user-facing hint.
func WrapWithHint(err error, msg string, hint string) error {
	return errors.WithHint(errors.Wrap(err, msg), hint)
}

// NewAssertionFailure creates an error denoting a programming bug.
func NewAssertionFailure(format string, args ...any) error {
	return errors.AssertionFailedf(format, args...)
}
