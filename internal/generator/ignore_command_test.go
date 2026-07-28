package generator

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeIgnore is a small helper that lays down a .gtb/ignore file in a
// MemMapFs project root for the rule-evaluation tests below.
func writeIgnore(t *testing.T, fs afero.Fs, projectPath, body string) {
	t.Helper()
	require.NoError(t, fs.MkdirAll(projectPath+"/.gtb", 0o755))
	require.NoError(t, afero.WriteFile(fs, projectPath+"/.gtb/ignore", []byte(body), 0o644))
}

// TestIgnoreRules_EvaluationSemantics locks in the rule-evaluation behaviour
// that `gtb ignore check` and `gtb ignore list` must faithfully report. This
// is the EXISTING mechanism (internal/generator/ignore.go) and passes today —
// it is the contract the proposed command surface has to explain to the user:
// rules are applied top-to-bottom, and a later `!negation` re-includes a file
// an earlier rule excluded. `check` naming "the winning rule" is only coherent
// because of this last-match-wins ordering.
func TestIgnoreRules_EvaluationSemantics(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeIgnore(t, fs, "proj", `# CI is hands-off
.github/workflows/**
# ...except the release workflow, which the generator still owns
!.github/workflows/release.yml
justfile
`)

	rules := LoadIgnoreRules(fs, "proj")

	cases := []struct {
		path    string
		ignored bool
		why     string
	}{
		{".github/workflows/test.yml", true, "matched by .github/workflows/**"},
		{".github/workflows/release.yml", false, "re-included by the ! negation, which wins as the later rule"},
		{"justfile", true, "matched by the basename rule justfile"},
		{"cmd/tool/main.go", false, "matched by nothing"},
	}

	for _, tc := range cases {
		assert.Equalf(t, tc.ignored, rules.IsIgnored(tc.path), "%s: %s", tc.path, tc.why)
	}
}

// TestIgnoreRules_EmptyAndMissing confirms the "absent file is valid" contract
// the scaffolded commented file must preserve: a header-only (comments +
// blanks) ignore file ignores nothing, exactly like a missing one.
func TestIgnoreRules_EmptyAndMissing(t *testing.T) {
	t.Parallel()

	// Missing file.
	missing := LoadIgnoreRules(afero.NewMemMapFs(), "proj")
	assert.False(t, missing.IsIgnored("justfile"))

	// Header/comments only — the shape a scaffolded .gtb/ignore would ship.
	fs := afero.NewMemMapFs()
	writeIgnore(t, fs, "proj", "# gtb ignore — mark files hands-off for regenerate\n# see https://gtb.phpboyscout.uk\n\n")
	commented := LoadIgnoreRules(fs, "proj")
	assert.False(t, commented.IsIgnored("justfile"),
		"a comments-only ignore file must ignore nothing, matching a missing file")
}

// --- Pending (red) tests for the not-yet-existing `gtb ignore` command -------
//
// These describe behaviour the issue specifies but which no code implements
// yet. They are Skip-pending so the package still builds; each references the
// helper/method the feature is expected to introduce. Remove the t.Skip and
// wire the real call when implementing.

// TestIgnoreAdd_Idempotent_PreservesComments_PENDING covers `gtb ignore add`:
// appending a pattern already present is a no-op (no duplicate line), and
// existing comments/ordering survive the edit.
func TestIgnoreAdd_Idempotent_PreservesComments_PENDING(t *testing.T) {
	t.Skip("PENDING issue #3: needs a generator ignore-writer (e.g. AppendIgnorePattern) " +
		"that appends idempotently and preserves comments/ordering")

	// Expected once implemented:
	//   fs seeded with a .gtb/ignore containing a comment + one pattern.
	//   AppendIgnorePattern(fs, "proj", "justfile") when "justfile" already
	//     present -> file unchanged (byte-identical), reported as a no-op.
	//   AppendIgnorePattern(fs, "proj", "Dockerfile") -> one new line appended,
	//     the leading comment and the pre-existing pattern still intact.
}

// TestIgnoreAdd_WritesHeaderOnCreate_PENDING covers `gtb ignore add` creating
// the file when absent: it must write an explanatory header so the next reader
// understands the syntax without hunting for the how-to.
func TestIgnoreAdd_WritesHeaderOnCreate_PENDING(t *testing.T) {
	t.Skip("PENDING issue #3: `ignore add` on a project with no .gtb/ignore must " +
		"create it with an explanatory header comment, then the pattern")

	// Expected once implemented:
	//   AppendIgnorePattern(fs, "proj", "justfile") on a fresh project ->
	//     .gtb/ignore begins with a "# gtb ignore" style header (comment lines),
	//     followed by "justfile"; LoadIgnoreRules then reports justfile ignored.
}

// TestIgnoreCheck_NamesWinningRule_PENDING covers `gtb ignore check <path>`:
// it must report not just ignored/not-ignored but WHICH rule decided it, which
// the current file format cannot answer on its own. This requires a new
// last-match-wins lookup on IgnoreRules (IsIgnored returns only a bool today).
func TestIgnoreCheck_NamesWinningRule_PENDING(t *testing.T) {
	t.Skip("PENDING issue #3: needs an IgnoreRules accessor that returns the " +
		"winning rule (pattern + negation) for a path, backing `ignore check`")

	// Expected once implemented, against the ruleset in
	// TestIgnoreRules_EvaluationSemantics:
	//   check(".github/workflows/test.yml")   -> ignored, rule ".github/workflows/**"
	//   check(".github/workflows/release.yml")-> NOT ignored, rule "!.github/workflows/release.yml"
	//   check("cmd/tool/main.go")             -> NOT ignored, no matching rule
}
