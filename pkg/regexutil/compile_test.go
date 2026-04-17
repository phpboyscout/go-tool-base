package regexutil_test

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/regexutil"
)

func TestCompileBounded_AcceptsValidPatterns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
	}{
		{name: "simple literal", pattern: `hello`},
		{name: "character class", pattern: `[a-z]+`},
		{name: "alternation", pattern: `foo|bar|baz`},
		{name: "anchored", pattern: `^\w+@[a-z]+\.[a-z]{2,}$`},
		{name: "case-insensitive flag", pattern: `(?i)search term`},
		{name: "realistic filename pattern", pattern: `^tool-v\d+\.\d+\.\d+-(darwin|linux|windows)-(amd64|arm64)(\.exe)?$`},
		{name: "unicode class", pattern: `\p{L}+`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			re, err := regexutil.CompileBounded(context.Background(), tc.pattern)
			require.NoError(t, err)
			require.NotNil(t, re)
			assert.Equal(t, tc.pattern, re.String())
		})
	}
}

func TestCompileBounded_RejectsOversize(t *testing.T) {
	t.Parallel()

	// One byte past the cap — forces the length branch.
	pattern := strings.Repeat("a", regexutil.MaxPatternLength+1)

	re, err := regexutil.CompileBounded(context.Background(), pattern)
	require.Error(t, err)
	require.Nil(t, re)
	require.ErrorIs(t, err, regexutil.ErrPatternTooLong)

	// The hint must reveal the length (for diagnosis) but NOT the pattern
	// itself (would let an attacker stuff logs with chosen content).
	hint := errors.FlattenHints(err)
	assert.Contains(t, hint, "1025")
	assert.NotContains(t, hint, pattern)
}

func TestCompileBounded_AcceptsExactlyMaxLength(t *testing.T) {
	t.Parallel()

	// Exactly at the cap is valid; the cap is an upper bound, not
	// strict-less-than.
	pattern := strings.Repeat("a", regexutil.MaxPatternLength)

	re, err := regexutil.CompileBounded(context.Background(), pattern)
	require.NoError(t, err)
	require.NotNil(t, re)
}

func TestCompileBounded_RejectsSyntaxError(t *testing.T) {
	t.Parallel()

	re, err := regexutil.CompileBounded(context.Background(), `[unclosed`)
	require.Error(t, err)
	require.Nil(t, re)
	require.ErrorIs(t, err, regexutil.ErrPatternInvalid)
}

func TestCompileBounded_RespectsContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the call so the select's ctx.Done() fires

	// A valid small pattern may race with the cancelled context: the
	// compile is fast and could return before the context path triggers.
	// What we assert is only that the call returns promptly and does not
	// hang — either outcome (compile win, context win) is acceptable.
	start := time.Now()
	_, _ = regexutil.CompileBounded(ctx, `hello`)
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 50*time.Millisecond, "cancelled ctx must not block the caller")
}

// TestCompileBounded_KnownPathologicalPatternsReject asserts that the
// ReDoS-ish patterns we expect to see in the wild all terminate within
// the timeout and return a typed error — the critical invariant.
// RE2 is fast, so some of these patterns will actually compile
// successfully; others will time out. Both outcomes are acceptable, so
// long as the call terminates promptly.
func TestCompileBounded_KnownPathologicalPatternsTerminate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
	}{
		{name: "nested quantifier (a+)+b", pattern: `(a+)+b`},
		{name: "alternation overlap (a|a)*b", pattern: `(a|a)*b`},
		{name: "repeated alternation (x|x|x)*y", pattern: `(x|x|x)*y`},
		{name: "deeply nested groups", pattern: strings.Repeat("(", 50) + "a" + strings.Repeat(")", 50)},
		{name: "massive alternation", pattern: strings.Repeat("a|", 400) + "a"},
		{name: "large bounded repetition", pattern: `a{1,999}b{1,999}c{1,999}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			start := time.Now()
			_, err := regexutil.CompileBounded(context.Background(), tc.pattern)
			elapsed := time.Since(start)

			// The contract: whatever happens, we return within roughly
			// the timeout budget. A small grace factor for scheduling.
			assert.LessOrEqual(t, elapsed, regexutil.DefaultCompileTimeout+100*time.Millisecond,
				"pattern must not hang beyond the compile timeout")

			if err != nil {
				// If we rejected, it must be via a typed sentinel —
				// never a raw errors.New or a *syntax.Error leak.
				typed := errors.Is(err, regexutil.ErrPatternTooLong) ||
					errors.Is(err, regexutil.ErrPatternCompileTimeout) ||
					errors.Is(err, regexutil.ErrPatternInvalid)
				assert.True(t, typed, "rejection must wrap a regexutil sentinel, got %v", err)
			}
		})
	}
}

func TestCompileBoundedTimeout_UsesCallerTimeout(t *testing.T) {
	t.Parallel()

	start := time.Now()
	re, err := regexutil.CompileBoundedTimeout(`hello`, 50*time.Millisecond)
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.NotNil(t, re)
	assert.Less(t, elapsed, 50*time.Millisecond, "a normal pattern should complete well under the timeout")
}

func TestCompileBoundedTimeout_StillEnforcesLengthCap(t *testing.T) {
	t.Parallel()

	pattern := strings.Repeat("a", regexutil.MaxPatternLength+1)

	_, err := regexutil.CompileBoundedTimeout(pattern, regexutil.DefaultCompileTimeout)
	require.ErrorIs(t, err, regexutil.ErrPatternTooLong)
}

// TestCompileBounded_ConcurrentGoroutineBound is a coarse leak check.
// We launch many concurrent compiles of pathological patterns and then
// observe the goroutine count. RE2 should eat the patterns here
// quickly, so this primarily exercises that concurrent use does not
// blow up goroutines permanently.
func TestCompileBounded_ConcurrentGoroutineBound(t *testing.T) {
	t.Parallel()

	const calls = 50

	baseline := runtime.NumGoroutine()

	var wg sync.WaitGroup

	wg.Add(calls)

	for range calls {
		go func() {
			defer wg.Done()

			_, _ = regexutil.CompileBoundedTimeout(`(a+)+b`, 20*time.Millisecond)
		}()
	}

	wg.Wait()

	// Grace period for GC-triggered goroutine teardown after timeouts.
	time.Sleep(200 * time.Millisecond)
	runtime.Gosched()

	growth := runtime.NumGoroutine() - baseline
	// Allow some slack; the point is that growth is bounded, not zero.
	assert.Less(t, growth, calls, "concurrent compiles must not leak a goroutine per call")
}

func TestCompileBounded_NoPatternInErrorString(t *testing.T) {
	t.Parallel()

	// A pattern containing a marker we can grep for in the error string.
	const marker = "SENSITIVE-TOKEN-ABC123"

	oversize := strings.Repeat(marker, regexutil.MaxPatternLength) // way over cap

	_, err := regexutil.CompileBounded(context.Background(), oversize)
	require.Error(t, err)
	require.ErrorIs(t, err, regexutil.ErrPatternTooLong)

	assert.NotContains(t, err.Error(), marker,
		"error must not echo the offending pattern (log-amplification risk)")
	assert.NotContains(t, errors.FlattenHints(err), marker,
		"hint must not echo the offending pattern (log-amplification risk)")
}
