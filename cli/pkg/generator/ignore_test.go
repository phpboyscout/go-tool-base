package generator

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeIgnoreFile(t *testing.T, fs afero.Fs, content string) {
	t.Helper()

	require.NoError(t, fs.MkdirAll(".gtb", 0o755))
	require.NoError(t, afero.WriteFile(fs, ".gtb/ignore", []byte(content), 0o644))
}

func TestIgnoreRules_BasicGlob(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeIgnoreFile(t, fs, "*.yml\n")

	rules := LoadIgnoreRules(fs, ".")

	assert.True(t, rules.IsIgnored("foo.yml"))
	assert.True(t, rules.IsIgnored("bar.yml"))
	assert.False(t, rules.IsIgnored("foo.go"))
	assert.False(t, rules.IsIgnored("foo.yaml"))
}

func TestIgnoreRules_Directory(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeIgnoreFile(t, fs, ".github/**\n")

	rules := LoadIgnoreRules(fs, ".")

	assert.True(t, rules.IsIgnored(".github/workflows/release.yml"))
	assert.True(t, rules.IsIgnored(".github/CODEOWNERS"))
	assert.True(t, rules.IsIgnored(".github/renovate.json5"))
	assert.False(t, rules.IsIgnored("justfile"))
	assert.False(t, rules.IsIgnored(".goreleaser.yaml"))
}

func TestIgnoreRules_Negation(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeIgnoreFile(t, fs, ".github/**\n!.github/CODEOWNERS\n")

	rules := LoadIgnoreRules(fs, ".")

	assert.True(t, rules.IsIgnored(".github/workflows/release.yml"))
	assert.False(t, rules.IsIgnored(".github/CODEOWNERS"), "negated file should not be ignored")
	assert.True(t, rules.IsIgnored(".github/renovate.json5"))
}

func TestIgnoreRules_Comments(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeIgnoreFile(t, fs, "# This is a comment\n\njustfile\n# Another comment\n")

	rules := LoadIgnoreRules(fs, ".")

	assert.True(t, rules.IsIgnored("justfile"))
	assert.False(t, rules.IsIgnored("go.mod"))
}

func TestIgnoreRules_Empty(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	rules := LoadIgnoreRules(fs, ".")

	assert.False(t, rules.IsIgnored("justfile"))
	assert.False(t, rules.IsIgnored(".github/workflows/release.yml"))
}

func TestIgnoreRules_ExactMatch(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeIgnoreFile(t, fs, "justfile\n")

	rules := LoadIgnoreRules(fs, ".")

	assert.True(t, rules.IsIgnored("justfile"))
	assert.False(t, rules.IsIgnored("Justfile"))
	assert.False(t, rules.IsIgnored("justfile.bak"))
}

func TestIgnoreRules_PathPattern(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeIgnoreFile(t, fs, ".github/workflows/release.yml\n")

	rules := LoadIgnoreRules(fs, ".")

	assert.True(t, rules.IsIgnored(".github/workflows/release.yml"))
	assert.False(t, rules.IsIgnored(".github/workflows/test.yml"))
	assert.False(t, rules.IsIgnored("release.yml"))
}

func TestIgnoreRules_MultiplePatterns(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeIgnoreFile(t, fs, "justfile\nDockerfile\n.goreleaser.yaml\n")

	rules := LoadIgnoreRules(fs, ".")

	assert.True(t, rules.IsIgnored("justfile"))
	assert.True(t, rules.IsIgnored("Dockerfile"))
	assert.True(t, rules.IsIgnored(".goreleaser.yaml"))
	assert.False(t, rules.IsIgnored("go.mod"))
}

func TestIgnoreRules_NegationOverridesEarlier(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeIgnoreFile(t, fs, "*.yml\n!release.yml\n")

	rules := LoadIgnoreRules(fs, ".")

	assert.True(t, rules.IsIgnored("test.yml"))
	assert.False(t, rules.IsIgnored("release.yml"), "negation should re-include release.yml")
}

func TestIgnoreRules_BasenameGlobMatchesNested(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeIgnoreFile(t, fs, "*.yml\n")

	rules := LoadIgnoreRules(fs, ".")

	// Basename glob should match nested files
	assert.True(t, rules.IsIgnored(".github/workflows/release.yml"))
	assert.True(t, rules.IsIgnored("deep/nested/file.yml"))
}
