package regexutil

import (
	"context"
	"regexp"
	"time"

	"github.com/cockroachdb/errors"
)

// MaxPatternLength is the maximum accepted pattern length, in bytes.
// Patterns longer than this are rejected without being compiled.
// Legitimate filename patterns and search queries are short; 1 KiB is
// generous for the former and ample for the latter.
const MaxPatternLength = 1024

// DefaultCompileTimeout is the wall-clock timeout applied to a single
// [regexp.Compile] call. Compiling a well-behaved 1 KiB pattern takes
// well under 1 ms on typical hardware; 100 ms is two orders of
// magnitude above normal and still imperceptible for interactive use.
const DefaultCompileTimeout = 100 * time.Millisecond

// ErrPatternTooLong is returned when a pattern exceeds
// [MaxPatternLength]. Distinguish with [errors.Is].
var ErrPatternTooLong = errors.New("regex pattern exceeds maximum length")

// ErrPatternCompileTimeout is returned when [regexp.Compile] does not
// finish within the configured timeout — typically a sign of a
// pathological pattern.
var ErrPatternCompileTimeout = errors.New("regex pattern compile timed out")

// ErrPatternInvalid is returned when [regexp.Compile] fails for a
// reason other than length or timeout (i.e. syntax errors).
var ErrPatternInvalid = errors.New("regex pattern is invalid")

// CompileBounded compiles pattern with a byte-length cap of
// [MaxPatternLength] and a wall-clock timeout of whichever of
// [DefaultCompileTimeout] or the deadline already present on ctx
// expires first.
//
// The returned error wraps one of [ErrPatternTooLong],
// [ErrPatternCompileTimeout], or [ErrPatternInvalid] so callers can
// distinguish the failure mode via [errors.Is].
//
// Use this wherever a pattern originates outside the binary: config
// file, CLI flag, HTTP payload, TUI input. Literal patterns known at
// build time should keep using [regexp.MustCompile].
func CompileBounded(ctx context.Context, pattern string) (*regexp.Regexp, error) {
	if len(pattern) > MaxPatternLength {
		return nil, errors.WithHintf(ErrPatternTooLong,
			"pattern has %d bytes; max is %d", len(pattern), MaxPatternLength)
	}

	ctx, cancel := context.WithTimeout(ctx, DefaultCompileTimeout)
	defer cancel()

	type result struct {
		re  *regexp.Regexp
		err error
	}

	// Buffered size-1 so the goroutine never blocks if the caller has
	// already returned on the timeout path.
	done := make(chan result, 1)

	go func() {
		re, err := regexp.Compile(pattern)
		done <- result{re: re, err: err}
	}()

	select {
	case r := <-done:
		if r.err != nil {
			return nil, errors.Wrap(ErrPatternInvalid, r.err.Error())
		}

		return r.re, nil
	case <-ctx.Done():
		// The goroutine keeps running until regexp.Compile returns —
		// an intentional bounded leak; see package doc.
		return nil, errors.WithHint(ErrPatternCompileTimeout,
			"The pattern is too complex to compile safely. Simplify it or use a different match strategy.")
	}
}

// CompileBoundedTimeout is a convenience wrapper that applies a
// caller-supplied timeout via [context.WithTimeout] on top of
// [context.Background]. Use this from call sites that do not naturally
// carry a [context.Context] (e.g. a Bubble Tea TUI loop).
//
// The effective timeout is the minimum of timeout and
// [DefaultCompileTimeout], so you cannot accidentally widen the
// package's wall-clock bound.
func CompileBoundedTimeout(pattern string, timeout time.Duration) (*regexp.Regexp, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return CompileBounded(ctx, pattern)
}
