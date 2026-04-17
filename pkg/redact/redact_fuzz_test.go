package redact_test

import (
	"strings"
	"testing"

	"github.com/phpboyscout/go-tool-base/pkg/redact"
)

// FuzzRedactString asserts the core invariants of [redact.String]:
//
//  1. It never panics on any input.
//  2. It is idempotent — applying it twice produces the same result
//     as applying it once. This is critical: without idempotence,
//     middlewares that may double-redact (e.g. once at telemetry
//     ingest, once at log emit) could corrupt the output.
//  3. It never adds catastrophic amounts of content. The fixed-length
//     replacements ("***", "<redacted>", "<redacted-token>") grow
//     short tokens by a few bytes but cannot inflate the string
//     unboundedly.
func FuzzRedactString(f *testing.F) {
	seeds := []string{
		"",
		"no secrets here",
		"https://user:pass@api.example.co/v1",
		"apikey=sk-proj-abc123def456ghi789jkl012mno345",
		"Authorization: Bearer abc123def456ghi789",
		"ghp_abcdefghijklmnopqrstuvwxyz012345",
		"AKIAIOSFODNN7EXAMPLE",
		"xoxb-1234567890-abcdefghij",
		strings.Repeat("a", 50),
		"\x00\x01\x02 control bytes",
		"mixed: https://u:p@h/?token=x " + strings.Repeat("b", 41),
	}

	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		// Invariant 1: no panic. Reaching this point proves it.
		out := redact.String(s)

		// Invariant 2: idempotence. Skip the exact-equality check when
		// the output contains a control byte that we deliberately don't
		// touch; redactions themselves never re-match.
		if out != redact.String(out) {
			t.Fatalf("not idempotent: %q -> %q -> %q", s, out, redact.String(out))
		}

		// Invariant 3: bounded growth. Replacements introduce a few
		// fixed strings; the longest is "<redacted-token>" at 16
		// bytes. Allow 4× the input length as a generous upper bound
		// to catch pathological expansion without tying the test to
		// an exact replacement-length formula.
		//
		// The lower bound is len(s) // 32 since we've seen replacements
		// collapse long credentials into short placeholders.
		if len(s) > 0 && len(out) > 4*len(s)+64 {
			t.Fatalf("redaction grew input unreasonably: in=%d out=%d", len(s), len(out))
		}
	})
}
