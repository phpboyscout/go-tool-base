package docs

import (
	"strings"
	"testing"
	"testing/fstest"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/regexutil"
)

// TestPerformSearch_OversizeRegexQueryFailsFast covers the H-3 call
// site in tui.go — a user typing a regex query longer than
// [regexutil.MaxPatternLength] must not hang the TUI. The bounded
// compile returns an error; the search returns an empty result set.
func TestPerformSearch_OversizeRegexQueryFailsFast(t *testing.T) {
	t.Parallel()

	m := &Model{
		fs:       fstest.MapFS{"readme.md": &fstest.MapFile{Data: []byte("# readme\n")}},
		useRegex: true,
	}

	oversize := strings.Repeat("a", regexutil.MaxPatternLength+1)

	start := time.Now()
	msg := m.performSearch(oversize)()
	elapsed := time.Since(start)

	require.Less(t, elapsed, 50*time.Millisecond, "oversize query must fail fast — no FS walk")

	res, ok := msg.(SearchResultMessage)
	require.True(t, ok)
	assert.Empty(t, res.Results)
	assert.Equal(t, oversize, res.Query)
}

// TestPerformSearch_PathologicalRegexTerminates covers the timeout
// path: a pattern that is in-bounds length-wise but might take longer
// than the budget to compile. RE2 handles most ReDoS-ish patterns
// quickly, so this primarily asserts the call terminates within the
// budget rather than a specific error outcome.
func TestPerformSearch_PathologicalRegexTerminates(t *testing.T) {
	t.Parallel()

	m := &Model{
		fs:       fstest.MapFS{},
		useRegex: true,
	}

	// Nested quantifiers — RE2 will generally compile this in well
	// under the budget, but the invariant we care about is that the
	// call returns within the compile-timeout window either way.
	pattern := "(a+)+" + strings.Repeat("(b|c)", 30) + "z"

	start := time.Now()
	_ = m.performSearch(pattern)()
	elapsed := time.Since(start)

	require.LessOrEqual(t, elapsed,
		regexutil.DefaultCompileTimeout+100*time.Millisecond,
		"pathological query must not hang the TUI")
}

// Compile-time assertion that performSearch returns a tea.Cmd — guards
// against a future refactor that accidentally changes the signature.
var _ tea.Cmd = (&Model{}).performSearch("")
