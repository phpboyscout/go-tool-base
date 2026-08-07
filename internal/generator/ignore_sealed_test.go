package generator

// Coverage for spec 0188 — the render/wiring split and the `sealed` attribute.

import (
	"testing"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rulesFrom compiles an ignore file body without touching the filesystem.
func rulesFrom(t *testing.T, body string) *IgnoreRules {
	t.Helper()

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/p/.gtb", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/p/.gtb/ignore", []byte(body), 0o644))

	return LoadIgnoreRules(fs, "/p")
}

// D3/D4 — the three states and how rules move a path between them.
func TestSealed_StateResolution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		path string
		want RuleState
	}{
		{
			name: "no rules leaves a path managed",
			body: "", path: "pkg/cmd/p/cmd.go", want: StateManaged,
		},
		{
			name: "a bare rule ignores",
			body: "pkg/cmd/p/cmd.go\n", path: "pkg/cmd/p/cmd.go", want: StateIgnored,
		},
		{
			name: "the sealed attribute seals",
			body: "pkg/cmd/p/cmd.go sealed\n", path: "pkg/cmd/p/cmd.go", want: StateSealed,
		},
		{
			name: "sealing implies ignoring, so one line is enough",
			body: "pkg/cmd/p/*  sealed\n", path: "pkg/cmd/p/cmd.go", want: StateSealed,
		},
		{
			name: "a later negation returns a sealed path to managed",
			body: "pkg/cmd/p/cmd.go sealed\n!pkg/cmd/p/cmd.go\n",
			path: "pkg/cmd/p/cmd.go", want: StateManaged,
		},
		{
			name: "-sealed drops to ignored without re-managing",
			body: "docs/** sealed\ndocs/index.md -sealed\n",
			path: "docs/index.md", want: StateIgnored,
		},
		{
			name: "-sealed leaves siblings sealed",
			body: "docs/** sealed\ndocs/index.md -sealed\n",
			path: "docs/other.md", want: StateSealed,
		},
		{
			name: "a later bare rule does not un-seal",
			body: "pkg/cmd/p/cmd.go sealed\npkg/cmd/**\n",
			path: "pkg/cmd/p/cmd.go", want: StateSealed,
		},
		{
			name: "a negation then a re-ignore lands on ignored",
			body: "pkg/cmd/p/cmd.go sealed\n!pkg/cmd/p/cmd.go\npkg/cmd/p/cmd.go\n",
			path: "pkg/cmd/p/cmd.go", want: StateIgnored,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := rulesFrom(t, tt.body).State(tt.path)
			assert.Equal(t, tt.want, got, "state for %q", tt.path)
		})
	}
}

// IsIgnored must keep meaning exactly what it meant before 0188 — "do not
// render" — so the eleven existing call sites stay correct. A sealed path is
// ignored too.
func TestSealed_IsIgnoredCoversBothNonManagedStates(t *testing.T) {
	t.Parallel()

	r := rulesFrom(t, "a.yaml\nb.yaml sealed\n")

	assert.True(t, r.IsIgnored("a.yaml"))
	assert.True(t, r.IsIgnored("b.yaml"), "a sealed path is also not rendered")
	assert.False(t, r.IsIgnored("c.yaml"))

	assert.False(t, r.IsSealed("a.yaml"), "a bare rule must not seal")
	assert.True(t, r.IsSealed("b.yaml"))
	assert.False(t, r.IsSealed("c.yaml"))
}

// D5 — the regression this rule exists to prevent. Before attributes, the whole
// trimmed line was the pattern, so a path containing a space was a valid rule.
func TestSealed_PathWithSpaceIsStillOnePattern(t *testing.T) {
	t.Parallel()

	r := rulesFrom(t, "my file.yaml\n")

	assert.Equal(t, StateIgnored, r.State("my file.yaml"),
		"a path containing a space must still parse as a single pattern")
	assert.Equal(t, StateManaged, r.State("my"),
		"the first word must not become a pattern of its own")
}

// D5 — an unknown attribute is not silently dropped; the line stays a pattern.
func TestSealed_UnknownAttributeLeavesTheLineAsAPattern(t *testing.T) {
	t.Parallel()

	r := rulesFrom(t, "docs/index.md frozen\n")

	assert.Equal(t, StateManaged, r.State("docs/index.md"),
		"an unknown attribute must not be treated as `sealed`, nor silently dropped")
	assert.Equal(t, StateIgnored, r.State("docs/index.md frozen"),
		"the whole line remains the pattern, so it is visibly matching the wrong thing")
}

// D5 — a path with a space that *is* sealed still resolves, because the
// trailing token is a known attribute.
func TestSealed_PathWithSpaceCanStillCarryAnAttribute(t *testing.T) {
	t.Parallel()

	r := rulesFrom(t, "my file.yaml sealed\n")

	assert.Equal(t, StateSealed, r.State("my file.yaml"))
}

// Attributes must survive round-tripping through the rule listing, since
// `gtb ignore list` prints raw lines and `remove` matches on them.
func TestSealed_RawLineIsPreservedVerbatim(t *testing.T) {
	t.Parallel()

	r := rulesFrom(t, "pkg/cmd/p/cmd.go  sealed\n")

	require.Len(t, r.Rules(), 1)
	assert.Equal(t, "pkg/cmd/p/cmd.go  sealed", r.Rules()[0])
}

// Explain backs `gtb ignore check`, which must name the deciding rule.
func TestSealed_ExplainNamesTheSealingRule(t *testing.T) {
	t.Parallel()

	r := rulesFrom(t, "docs/**\ndocs/index.md sealed\n")

	rule, negated, matched := r.Explain("docs/index.md")
	assert.True(t, matched)
	assert.False(t, negated)
	assert.Equal(t, "docs/index.md sealed", rule)
}

// D9 — the data-loss sequence this decision exists to close. Ignoring a file no
// longer adopts its on-disk content as the new baseline, so un-ignoring raises a
// conflict instead of silently overwriting the developer's work.
func TestSealed_IgnoringAPathDoesNotAdoptItsContentAsTheBaseline(t *testing.T) {
	t.Parallel()

	g, fs, _ := newIssue13Project(t, "ask", "pkg/cmd/alpha/cmd.go\n")

	require.NoError(t, g.RegenerateProject(t.Context()))

	// While ignored, the manifest must still carry the hash from before the
	// rule was added — not the hash of the hand-edited content on disk.
	recorded := alphaRecordedHash(t, fs)
	assert.Equal(t, issue13StaleHash, recorded,
		"an ignored path keeps its stored hash, so un-ignoring can still detect the edit")
	assert.NotEqual(t, CalculateHash([]byte(issue13HandEdited)), recorded,
		"adopting the on-disk content as the baseline is what silently destroys the edit")
}

// D2 — a bare rule must not stop the wiring writers. Refusing subcommand
// registration compiles cleanly and silently drops the command from the CLI,
// which is why it must not be the default.
func TestSealed_BareRuleStillAllowsSubcommandWiring(t *testing.T) {
	t.Parallel()

	g, fs, _ := newSealedNestedProject(t, "pkg/cmd/alpha/cmd.go\n")

	require.NoError(t, g.RegenerateProject(t.Context()))

	parent, err := afero.ReadFile(fs, "/work/pkg/cmd/alpha/cmd.go")
	require.NoError(t, err)

	assert.Contains(t, string(parent), "hand-edited",
		"a bare rule must still stop the file being re-rendered")
	assert.Contains(t, string(parent), "NewCmdNested",
		"a bare rule must NOT stop the child being wired in — the command would vanish from the CLI")
}

// D3/D6 — a sealed rule stops the wiring too, and the run says what it could
// not do rather than leaving a build failure to explain it.
func TestSealed_SealedRuleBlocksWiringAndSaysSo(t *testing.T) {
	t.Parallel()

	g, fs, buf := newSealedNestedProject(t, "pkg/cmd/alpha/cmd.go sealed\n")

	require.NoError(t, g.RegenerateProject(t.Context()), "a seal is a request, not a failure")

	parent, err := afero.ReadFile(fs, "/work/pkg/cmd/alpha/cmd.go")
	require.NoError(t, err)

	assert.Equal(t, sealedParentSource, string(parent),
		"a sealed file must be byte-identical after the run")
	assert.NotContains(t, string(parent), "NewCmdNested")

	assert.True(t, buf.ContainsLevel(logger.WarnLevel, "sealed, not wired"),
		"the skipped wiring must be reported, got: %v", buf.Messages())
	assert.True(t, buf.ContainsLevel(logger.InfoLevel, "sealed"),
		"the summary must count sealed paths, got: %v", buf.Messages())
}

const sealedParentSource = "package alpha\n\n// hand-edited\nfunc NewCmdAlpha() {}\n"

// newSealedNestedProject builds a project whose `alpha` command has a `nested`
// child and a hand-edited parent cmd.go, under the given ignore rules.
func newSealedNestedProject(t *testing.T, ignoreBody string) (*Generator, afero.Fs, logBuffer) {
	t.Helper()

	manifest := "properties:\n  name: mytool\nversion:\n  gtb: v1.0.0\ncommands:\n" +
		"  - name: alpha\n    hashes:\n      cmd.go: " + issue13StaleHash + "\n" +
		"    commands:\n      - name: nested\n"

	g, fs, buf := newPerimeterTestProject(t, manifest)

	require.NoError(t, fs.MkdirAll("/work/pkg/cmd/alpha", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/alpha/cmd.go", []byte(sealedParentSource), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/work/.gtb/ignore", []byte(ignoreBody), 0o644))

	return g, fs, buf
}

// D8 — seal appends the attributed rule and is idempotent.
func TestSealed_SealIsIdempotentAndPreservesComments(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/p/.gtb", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/p/.gtb/ignore",
		[]byte("# keep me\njustfile\n"), 0o644))

	changed, err := SealIgnorePattern(fs, "/p", "pkg/cmd/p/cmd.go")
	require.NoError(t, err)
	assert.True(t, changed)

	body, err := afero.ReadFile(fs, "/p/.gtb/ignore")
	require.NoError(t, err)
	assert.Equal(t, "# keep me\njustfile\npkg/cmd/p/cmd.go sealed\n", string(body))

	changed, err = SealIgnorePattern(fs, "/p", "pkg/cmd/p/cmd.go")
	require.NoError(t, err)
	assert.False(t, changed, "re-sealing must be a reported no-op")
}

// D8 — unseal leaves the path ignored rather than handing it back to the
// generator, which is what dropping the line outright would do.
func TestSealed_UnsealLeavesThePathIgnored(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/p/.gtb", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/p/.gtb/ignore",
		[]byte("# header\npkg/cmd/p/cmd.go sealed\n"), 0o644))

	changed, err := UnsealIgnorePattern(fs, "/p", "pkg/cmd/p/cmd.go")
	require.NoError(t, err)
	assert.True(t, changed)

	assert.Equal(t, StateIgnored, LoadIgnoreRules(fs, "/p").State("pkg/cmd/p/cmd.go"),
		"unsealing must not silently re-manage the file")

	body, err := afero.ReadFile(fs, "/p/.gtb/ignore")
	require.NoError(t, err)
	assert.Contains(t, string(body), "# header", "comments must survive")

	changed, err = UnsealIgnorePattern(fs, "/p", "pkg/cmd/p/cmd.go")
	require.NoError(t, err)
	assert.False(t, changed, "unsealing an unsealed path is a no-op")
}
