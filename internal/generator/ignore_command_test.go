package generator

import (
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// newIgnoreTestGenerator builds a Generator over fs rooted at projectPath for
// the command-level ignore tests (list/check/doctor).
func newIgnoreTestGenerator(fs afero.Fs, projectPath string) *Generator {
	return New(&props.Props{FS: fs, Logger: logger.NewNoop()}, &Config{Path: projectPath})
}

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

// --- Tests for the `gtb ignore` command primitives ---------------------------
//
// These pin the two new generator primitives issue #3 introduced: an idempotent
// comment/order-preserving writer (AppendIgnorePattern) and a winning-rule
// accessor (IgnoreRules.Explain).

// TestIgnoreAdd_Idempotent_PreservesComments covers `gtb ignore add`:
// appending a pattern already present is a no-op (no duplicate line, file
// byte-identical), and existing comments/ordering survive the edit.
func TestIgnoreAdd_Idempotent_PreservesComments(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeIgnore(t, fs, "proj", "# my rules\njustfile\n")

	before, err := afero.ReadFile(fs, "proj/.gtb/ignore")
	require.NoError(t, err)

	// Re-adding an existing pattern is a reported no-op, byte-identical file.
	changed, err := AppendIgnorePattern(fs, "proj", "justfile")
	require.NoError(t, err)
	assert.False(t, changed, "re-adding a present pattern must be a no-op")

	after, err := afero.ReadFile(fs, "proj/.gtb/ignore")
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "no-op add must leave the file byte-identical")

	// Adding a new pattern appends one line, preserving comment + prior pattern.
	changed, err = AppendIgnorePattern(fs, "proj", "Dockerfile")
	require.NoError(t, err)
	assert.True(t, changed)

	body, err := afero.ReadFile(fs, "proj/.gtb/ignore")
	require.NoError(t, err)
	assert.Equal(t, "# my rules\njustfile\nDockerfile\n", string(body),
		"append must preserve the leading comment and the pre-existing pattern")
}

// TestIgnoreAdd_WritesHeaderOnCreate covers `gtb ignore add` creating the file
// when absent: it writes an explanatory header so the next reader understands
// the syntax without hunting for the how-to, then the pattern.
func TestIgnoreAdd_WritesHeaderOnCreate(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	changed, err := AppendIgnorePattern(fs, "proj", "justfile")
	require.NoError(t, err)
	assert.True(t, changed)

	body, err := afero.ReadFile(fs, "proj/.gtb/ignore")
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(string(body), "#"),
		"a created ignore file must begin with an explanatory header comment")
	assert.Contains(t, string(body), ".gtb/ignore",
		"the header should describe the mechanism")
	assert.Contains(t, string(body), "\njustfile\n", "the pattern follows the header")

	// The header is comments-only, so the sole active rule is the pattern.
	assert.True(t, LoadIgnoreRules(fs, "proj").IsIgnored("justfile"))
}

// TestIgnoreCheck_NamesWinningRule covers `gtb ignore check <path>`: it reports
// not just ignored/not-ignored but WHICH rule decided it, correct under
// last-match-wins + ! negation.
func TestIgnoreCheck_NamesWinningRule(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeIgnore(t, fs, "proj", `# CI is hands-off
.github/workflows/**
!.github/workflows/release.yml
justfile
`)

	rules := LoadIgnoreRules(fs, "proj")

	cases := []struct {
		path    string
		rule    string
		negated bool
		matched bool
	}{
		{".github/workflows/test.yml", ".github/workflows/**", false, true},
		{".github/workflows/release.yml", "!.github/workflows/release.yml", true, true},
		{"cmd/tool/main.go", "", false, false},
	}

	for _, tc := range cases {
		rule, negated, matched := rules.Explain(tc.path)
		assert.Equalf(t, tc.rule, rule, "%s: winning rule", tc.path)
		assert.Equalf(t, tc.negated, negated, "%s: negation", tc.path)
		assert.Equalf(t, tc.matched, matched, "%s: matched", tc.path)
	}
}

// TestIgnoreConflictHint confirms the regenerate conflict warning names
// .gtb/ignore and the gtb ignore command as the remedy.
func TestIgnoreConflictHint(t *testing.T) {
	t.Parallel()

	hint := ignoreConflictHint("cmd/tool/main.go")
	assert.Contains(t, hint, ".gtb/ignore")
	assert.Contains(t, hint, "gtb ignore add cmd/tool/main.go")
}

// TestScaffoldIgnoreFile confirms the scaffold writer creates an inert,
// comment-only file and never clobbers an existing one.
func TestScaffoldIgnoreFile(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, ScaffoldIgnoreFile(fs, "proj"))

	body, err := afero.ReadFile(fs, "proj/.gtb/ignore")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(body), "#"))
	assert.False(t, LoadIgnoreRules(fs, "proj").IsIgnored("justfile"), "scaffolded file ignores nothing")

	// Idempotent: a second call must not overwrite an edited file.
	writeIgnore(t, fs, "proj", "justfile\n")
	require.NoError(t, ScaffoldIgnoreFile(fs, "proj"))
	after, err := afero.ReadFile(fs, "proj/.gtb/ignore")
	require.NoError(t, err)
	assert.Equal(t, "justfile\n", string(after), "scaffold must not clobber existing rules")
}

// TestRemoveIgnorePattern covers removal (literal-line match) and its preview,
// including the no-op cases (absent rule, missing file).
func TestRemoveIgnorePattern(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeIgnore(t, fs, "proj", "# c\njustfile\n*.yml\n")

	// Preview does not write.
	preview, changed, err := PreviewRemoveIgnorePattern(fs, "proj", "justfile")
	require.NoError(t, err)
	assert.True(t, changed)
	assert.NotContains(t, preview, "justfile")
	onDisk, _ := afero.ReadFile(fs, "proj/.gtb/ignore")
	assert.Contains(t, string(onDisk), "justfile", "preview must not write")

	// Real remove drops only the literal line.
	changed, err = RemoveIgnorePattern(fs, "proj", "justfile")
	require.NoError(t, err)
	assert.True(t, changed)
	body, _ := afero.ReadFile(fs, "proj/.gtb/ignore")
	assert.Equal(t, "# c\n*.yml\n", string(body))

	// Absent rule and missing file are reported no-ops.
	changed, err = RemoveIgnorePattern(fs, "proj", "nope")
	require.NoError(t, err)
	assert.False(t, changed)

	changed, err = RemoveIgnorePattern(afero.NewMemMapFs(), "empty", "x")
	require.NoError(t, err)
	assert.False(t, changed)
}

// TestPreviewAppendIgnorePatterns confirms multiple patterns compose in the
// dry-run preview without writing.
func TestPreviewAppendIgnorePatterns(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	content, err := PreviewAppendIgnorePatterns(fs, "proj", []string{"justfile", "Dockerfile", "justfile"})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(content, "#"), "seeds a header when creating")
	assert.Contains(t, content, "\njustfile\n")
	assert.Contains(t, content, "\nDockerfile\n")
	assert.Equal(t, 1, strings.Count(content, "\njustfile\n"), "duplicate pattern must not repeat")

	exists, _ := afero.Exists(fs, "proj/.gtb/ignore")
	assert.False(t, exists, "preview must not write")
}

// TestGeneratorIgnoreListAndCheck exercises the manifest-resolving list view,
// the fresh-read check view, and the diverged-file doctor helper.
func TestGeneratorIgnoreListAndCheck(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeIgnore(t, fs, "proj", "justfile\nDockerfile\n")

	m := &Manifest{
		Properties: ManifestProperties{Name: "tool"},
		Hashes: map[string]string{
			"justfile":         "h1",
			"cmd/tool/main.go": "h2",
		},
	}
	require.NoError(t, EncodeManifestFile(fs, ManifestPathFor("proj"), m))

	gen := newIgnoreTestGenerator(fs, "proj")

	listing, err := gen.ListIgnoreRules()
	require.NoError(t, err)
	require.Len(t, listing.Entries, 1)
	assert.Equal(t, "justfile", listing.Entries[0].Path)
	assert.True(t, listing.Entries[0].Ignored)
	assert.Equal(t, []string{"Dockerfile"}, listing.StaleRules)

	results := gen.CheckIgnorePaths([]string{"justfile", "cmd/tool/main.go"})
	require.Len(t, results, 2)
	assert.True(t, results[0].Ignored)
	assert.Equal(t, "justfile", results[0].Rule)
	assert.False(t, results[1].Ignored)
	assert.False(t, results[1].Matched)

	// list errors when no manifest exists.
	_, err = newIgnoreTestGenerator(afero.NewMemMapFs(), "none").ListIgnoreRules()
	assert.Error(t, err)
}

// TestDivergedUnignoredFiles confirms the doctor helper reports only files that
// diverged from their manifest hash AND are not ignored.
func TestDivergedUnignoredFiles(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	// justfile diverged but ignored; README diverged and NOT ignored;
	// config matches its stored hash; gone is tracked but missing on disk.
	require.NoError(t, afero.WriteFile(fs, "proj/justfile", []byte("changed"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "proj/README.md", []byte("changed"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "proj/config.yaml", []byte("same"), 0o644))
	writeIgnore(t, fs, "proj", "justfile\n")

	m := &Manifest{
		Properties: ManifestProperties{Name: "tool"},
		Hashes: map[string]string{
			"justfile":    "stale",
			"README.md":   "stale",
			"config.yaml": calculateHash([]byte("same")),
			"gone.txt":    "stale",
		},
	}
	require.NoError(t, EncodeManifestFile(fs, ManifestPathFor("proj"), m))

	diverged, err := newIgnoreTestGenerator(fs, "proj").DivergedUnignoredFiles()
	require.NoError(t, err)
	assert.Equal(t, []string{"README.md"}, diverged)
}
