package regexutil_test

import (
	"context"
	"testing"
	"time"

	"github.com/cockroachdb/errors"

	"github.com/phpboyscout/go-tool-base/pkg/regexutil"
)

// FuzzCompileBounded exercises [regexutil.CompileBounded] against a
// corpus of canonical patterns, known ReDoS-ish shapes, oversize
// inputs, and raw random bytes. The security invariants asserted here:
//
//  1. No call ever panics, regardless of input.
//  2. Every call returns within a grace period of the compile timeout —
//     the whole point of the package.
//  3. Every rejection is wrapped in one of the exported sentinel errors
//     so callers can switch on kind via [errors.Is].
func FuzzCompileBounded(f *testing.F) {
	seeds := []string{
		// Canonical accepted patterns.
		"",
		"hello",
		`[a-z]+`,
		`^\w+@[a-z]+\.[a-z]{2,}$`,
		`(?i)search`,
		`foo|bar|baz`,

		// Known ReDoS-ish shapes (RE2 handles these, but they're in the
		// corpus to ensure nothing regresses to a hang).
		`(a+)+b`,
		`(a|a)*b`,
		`(x|x|x)*y`,
		`a{1,999}b{1,999}`,

		// Syntax errors — ErrPatternInvalid territory.
		`[unclosed`,
		`*invalid-anchor`,
		`(?x`,

		// Edge encodings.
		"\x00",
		"\xff\xfe",
	}

	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, pattern string) {
		start := time.Now()

		re, err := regexutil.CompileBounded(context.Background(), pattern)

		elapsed := time.Since(start)

		// Invariant 2: always returns within a grace window over the
		// timeout. 250 ms is DefaultCompileTimeout + scheduling slack.
		if elapsed > 250*time.Millisecond {
			t.Fatalf("CompileBounded took %v for pattern of len %d; expected <= 250ms", elapsed, len(pattern))
		}

		if err == nil {
			if re == nil {
				t.Fatalf("nil regexp with nil error for pattern len %d", len(pattern))
			}

			return
		}

		// Invariant 3: rejection must wrap a known sentinel.
		typed := errors.Is(err, regexutil.ErrPatternTooLong) ||
			errors.Is(err, regexutil.ErrPatternCompileTimeout) ||
			errors.Is(err, regexutil.ErrPatternInvalid)
		if !typed {
			t.Fatalf("rejection for pattern len %d wraps no regexutil sentinel: %v", len(pattern), err)
		}
	})
}
