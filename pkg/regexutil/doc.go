// Package regexutil provides bounded, DoS-safe wrappers around
// [regexp.Compile] for every call path that takes a user- or
// config-supplied regex pattern.
//
// # Threat model
//
// Go's regexp engine is RE2-based and therefore does not suffer classical
// catastrophic backtracking at match time. Compilation, however, is not
// guaranteed linear: a pathological pattern (e.g. `(a+)+b`, deeply nested
// alternation, or simply very long repetition chains) can take measurable
// wall-clock time inside [regexp.Compile] and allocate an automaton of
// many thousands of states. That is sufficient to hang a CLI's update
// flow or freeze an interactive TUI.
//
// This package applies two defences uniformly:
//
//  1. A byte-length cap ([MaxPatternLength]) rejects oversize patterns
//     before any compile work begins.
//  2. A wall-clock timeout on the compile itself ([DefaultCompileTimeout])
//     bounds the worst case for anything that slips past the length cap.
//
// Call-site discipline: any regex compiled from a pattern that originates
// outside the binary (config file, CLI flag, TUI input, HTTP payload,
// message queue) MUST go through [CompileBounded] or [CompileBoundedTimeout].
// Literal patterns known at build time can — and should — continue to use
// [regexp.MustCompile] directly.
//
// # Goroutine leak tradeoff
//
// [regexp.Compile] is not context-aware, so the timeout path launches the
// compile in a goroutine and returns on timeout while that goroutine keeps
// running until the compile finishes (or forever, for truly pathological
// inputs). This is a bounded leak: the number of distinct pathological
// patterns a single process ever sees is small; the goroutine holds a
// single compile's working set; the caller gets their error immediately
// and can continue. If a future Go version exposes a context-aware
// compile, migrate.
//
// # Logging
//
// Rejection errors never include the offending pattern — only its length
// and the rejection kind. Logging the pattern would let an attacker fill
// logs with content of their choosing. Callers that wish to surface the
// pattern to the operator should do so from a trusted-source context
// where log amplification is not a concern.
package regexutil
