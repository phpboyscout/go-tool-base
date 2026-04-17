package chat

import (
	"testing"

	"github.com/cockroachdb/errors"
)

// FuzzValidateSnapshotID exercises the snapshot identifier validator
// against a corpus of canonical UUIDs, obvious traversal strings,
// Unicode/encoding tricks, and raw random bytes. The security invariants
// asserted here are:
//
//  1. ValidateSnapshotID never panics, regardless of input.
//  2. Any non-empty input that passes validation can only be one of the
//     strictly-shaped canonical UUIDs accepted by uuidCanonicalPattern —
//     i.e. 36 characters, lowercase hex, hyphens at positions 8-13-18-23.
//  3. Any rejection wraps [ErrInvalidSnapshotID] so callers can
//     distinguish validation failures from I/O failures.
//
// Seed the corpus with values that have historically been hard to
// validate correctly: ReDoS triggers, long repetitive strings,
// attacker-controlled traversal sequences, and the full zero UUID.
func FuzzValidateSnapshotID(f *testing.F) {
	seeds := []string{
		// Canonical accepted inputs.
		"00000000-0000-0000-0000-000000000000",
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		"0123abcd-4567-89ab-cdef-0123456789ab",

		// Canonical shape rejections.
		"",
		"AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA",
		"gggggggg-gggg-4ggg-8ggg-gggggggggggg",
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaaa", // one char too long
		"aaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",   // 7-char first group

		// Traversal and path-injection strings.
		"../../etc/passwd",
		"..",
		"/etc/passwd",
		"a/b",
		"aaaaaaaa/aaaa/4aaa/8aaa/aaaaaaaaaaaa",
		"aaaaaaaa\\aaaa\\4aaa\\8aaa\\aaaaaaaaaaaa",

		// Encoding tricks.
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa\x00",
		"\x00aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa",
		"ааааaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", // Cyrillic look-alike
		" aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa ",
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa\n",
	}

	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, id string) {
		err := ValidateSnapshotID(id)
		if err == nil {
			// Acceptance must imply canonical shape. This check is
			// redundant with the implementation today, but it guards
			// against future relaxation of the regex silently widening
			// what Save/Load/Delete will accept.
			if !uuidCanonicalPattern.MatchString(id) {
				t.Fatalf("ValidateSnapshotID accepted non-canonical input %q", id)
			}

			return
		}

		if !errors.Is(err, ErrInvalidSnapshotID) {
			t.Fatalf("ValidateSnapshotID rejected %q with non-sentinel error: %v", id, err)
		}
	})
}
