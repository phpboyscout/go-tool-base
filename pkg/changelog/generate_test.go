package changelog

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	conventionalcommits "github.com/leodido/go-conventionalcommits"
	ccparser "github.com/leodido/go-conventionalcommits/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testAuthor is a reusable commit signature for tests.
var testAuthor = &object.Signature{
	Name:  "Test",
	Email: "test@example.com",
	When:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
}

// initTestRepo creates a bare-bones git repo in a temp dir and returns the
// repo, its path, and a helper to create commits.
func initTestRepo(t *testing.T) (*git.Repository, string) {
	t.Helper()

	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	require.NoError(t, err)

	return repo, dir
}

// commitFile stages a file and creates a commit with the given message.
func commitFile(t *testing.T, repo *git.Repository, dir, filename, content, message string) plumbing.Hash {
	t.Helper()

	err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644)
	require.NoError(t, err)

	wt, err := repo.Worktree()
	require.NoError(t, err)

	_, err = wt.Add(filename)
	require.NoError(t, err)

	hash, err := wt.Commit(message, &git.CommitOptions{
		Author: testAuthor,
	})
	require.NoError(t, err)

	return hash
}

// createTag creates a lightweight tag at the given hash.
func createTag(t *testing.T, repo *git.Repository, name string, hash plumbing.Hash) {
	t.Helper()

	ref := plumbing.NewHashReference(plumbing.NewTagReferenceName(name), hash)
	err := repo.Storer.SetReference(ref)
	require.NoError(t, err)
}

// createAnnotatedTag creates an annotated tag at the given hash.
func createAnnotatedTag(t *testing.T, repo *git.Repository, name string, hash plumbing.Hash) {
	t.Helper()

	_, err := repo.CreateTag(name, hash, &git.CreateTagOptions{
		Tagger:  testAuthor,
		Message: name,
	})
	require.NoError(t, err)
}

func TestGenerateFromRepo(t *testing.T) {
	t.Run("single release", func(t *testing.T) {
		t.Parallel()

		repo, dir := initTestRepo(t)
		h1 := commitFile(t, repo, dir, "a.txt", "a", "feat(chat): add conversation persistence")
		createTag(t, repo, "v1.0.0", h1)

		result, err := GenerateFromRepo(dir)
		require.NoError(t, err)

		assert.Contains(t, result, "# v1.0.0")
		assert.Contains(t, result, "### Features")
		assert.Contains(t, result, "* **chat:** add conversation persistence")
	})

	t.Run("multiple releases newest first", func(t *testing.T) {
		t.Parallel()

		repo, dir := initTestRepo(t)
		h1 := commitFile(t, repo, dir, "a.txt", "a", "feat: initial feature")
		createTag(t, repo, "v1.0.0", h1)

		h2 := commitFile(t, repo, dir, "b.txt", "b", "fix(http): fix timeout")
		createTag(t, repo, "v1.0.1", h2)

		result, err := GenerateFromRepo(dir)
		require.NoError(t, err)

		// v1.0.1 should appear before v1.0.0 (newest first).
		idx101 := indexOf(result, "# v1.0.1")
		idx100 := indexOf(result, "# v1.0.0")
		assert.Greater(t, idx100, idx101, "v1.0.1 should appear before v1.0.0")
		assert.Contains(t, result, "### Bug Fixes")
		assert.Contains(t, result, "* **http:** fix timeout")
	})

	t.Run("unreleased commits", func(t *testing.T) {
		t.Parallel()

		repo, dir := initTestRepo(t)
		h1 := commitFile(t, repo, dir, "a.txt", "a", "feat: released feature")
		createTag(t, repo, "v1.0.0", h1)

		commitFile(t, repo, dir, "b.txt", "b", "feat: unreleased feature")

		result, err := GenerateFromRepo(dir)
		require.NoError(t, err)

		assert.Contains(t, result, "# Unreleased")
		assert.Contains(t, result, "* unreleased feature")
	})

	t.Run("annotated tags", func(t *testing.T) {
		t.Parallel()

		repo, dir := initTestRepo(t)
		h1 := commitFile(t, repo, dir, "a.txt", "a", "feat: annotated release")
		createAnnotatedTag(t, repo, "v2.0.0", h1)

		result, err := GenerateFromRepo(dir)
		require.NoError(t, err)

		assert.Contains(t, result, "# v2.0.0")
		assert.Contains(t, result, "* annotated release")
	})

	t.Run("since tag filter", func(t *testing.T) {
		t.Parallel()

		repo, dir := initTestRepo(t)
		h1 := commitFile(t, repo, dir, "a.txt", "a", "feat: old feature")
		createTag(t, repo, "v1.0.0", h1)

		h2 := commitFile(t, repo, dir, "b.txt", "b", "feat: new feature")
		createTag(t, repo, "v2.0.0", h2)

		result, err := GenerateFromRepo(dir, WithSinceTag("v1.0.0"))
		require.NoError(t, err)

		assert.Contains(t, result, "# v2.0.0")
		assert.NotContains(t, result, "# v1.0.0")
	})

	t.Run("max releases", func(t *testing.T) {
		t.Parallel()

		repo, dir := initTestRepo(t)
		h1 := commitFile(t, repo, dir, "a.txt", "a", "feat: first")
		createTag(t, repo, "v1.0.0", h1)

		h2 := commitFile(t, repo, dir, "b.txt", "b", "feat: second")
		createTag(t, repo, "v2.0.0", h2)

		h3 := commitFile(t, repo, dir, "c.txt", "c", "feat: third")
		createTag(t, repo, "v3.0.0", h3)

		result, err := GenerateFromRepo(dir, WithMaxReleases(2))
		require.NoError(t, err)

		assert.Contains(t, result, "# v3.0.0")
		assert.Contains(t, result, "# v2.0.0")
		assert.NotContains(t, result, "# v1.0.0")
	})

	t.Run("include all non-conventional", func(t *testing.T) {
		t.Parallel()

		repo, dir := initTestRepo(t)
		h1 := commitFile(t, repo, dir, "a.txt", "a", "just a plain commit message")
		createTag(t, repo, "v1.0.0", h1)

		// Without includeAll, plain commits are excluded.
		result, err := GenerateFromRepo(dir)
		require.NoError(t, err)
		assert.NotContains(t, result, "plain commit message")

		// With includeAll, they appear under Other.
		result, err = GenerateFromRepo(dir, WithIncludeAll())
		require.NoError(t, err)
		assert.Contains(t, result, "### Other")
		assert.Contains(t, result, "* just a plain commit message")
	})

	t.Run("breaking change via exclamation", func(t *testing.T) {
		t.Parallel()

		repo, dir := initTestRepo(t)
		h1 := commitFile(t, repo, dir, "a.txt", "a", "feat(api)!: remove deprecated method")
		createTag(t, repo, "v2.0.0", h1)

		result, err := GenerateFromRepo(dir)
		require.NoError(t, err)

		assert.Contains(t, result, "### Breaking Changes")
		assert.Contains(t, result, "* **api:** remove deprecated method")
	})

	t.Run("breaking change via footer", func(t *testing.T) {
		t.Parallel()

		repo, dir := initTestRepo(t)
		h1 := commitFile(t, repo, dir, "a.txt", "a", "feat(config): rename config path\n\nBREAKING CHANGE: ConfigPath renamed to ConfigDir")
		createTag(t, repo, "v2.0.0", h1)

		result, err := GenerateFromRepo(dir)
		require.NoError(t, err)

		assert.Contains(t, result, "### Breaking Changes")
		assert.Contains(t, result, "* **config:** rename config path")
	})

	t.Run("skip test and ci commits", func(t *testing.T) {
		t.Parallel()

		repo, dir := initTestRepo(t)
		commitFile(t, repo, dir, "a.txt", "a", "test: add unit tests")
		commitFile(t, repo, dir, "b.txt", "b", "ci: update workflow")
		h3 := commitFile(t, repo, dir, "c.txt", "c", "feat: visible feature")
		createTag(t, repo, "v1.0.0", h3)

		result, err := GenerateFromRepo(dir)
		require.NoError(t, err)

		assert.NotContains(t, result, "add unit tests")
		assert.NotContains(t, result, "update workflow")
		assert.Contains(t, result, "* visible feature")
	})

	t.Run("scoped and unscoped entries", func(t *testing.T) {
		t.Parallel()

		repo, dir := initTestRepo(t)
		commitFile(t, repo, dir, "a.txt", "a", "feat(http): add middleware")
		commitFile(t, repo, dir, "b.txt", "b", "fix: resolve nil panic")
		h3 := commitFile(t, repo, dir, "c.txt", "c", "perf(cache): reduce allocations")
		createTag(t, repo, "v1.0.0", h3)

		result, err := GenerateFromRepo(dir)
		require.NoError(t, err)

		assert.Contains(t, result, "* **http:** add middleware")
		assert.Contains(t, result, "* resolve nil panic")
		assert.Contains(t, result, "* **cache:** reduce allocations")
		assert.Contains(t, result, "### Performance Improvements")
	})

	t.Run("invalid repo path", func(t *testing.T) {
		t.Parallel()

		_, err := GenerateFromRepo("/nonexistent/path")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "opening git repository")
	})

	t.Run("empty repo no commits", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		_, err := git.PlainInit(dir, false)
		require.NoError(t, err)

		result, err := GenerateFromRepo(dir)
		// Empty repo has no HEAD, so we expect an error.
		require.Error(t, err)
		assert.Empty(t, result)
	})

	t.Run("output is parseable by Parse", func(t *testing.T) {
		t.Parallel()

		repo, dir := initTestRepo(t)
		commitFile(t, repo, dir, "a.txt", "a", "feat(chat): add streaming")
		commitFile(t, repo, dir, "b.txt", "b", "fix(http): fix timeout")
		h3 := commitFile(t, repo, dir, "c.txt", "c", "feat(grpc): add interceptors")
		createTag(t, repo, "v1.0.0", h3)

		commitFile(t, repo, dir, "d.txt", "d", "feat(output): add progress bar")
		h5 := commitFile(t, repo, dir, "e.txt", "e", "fix(config): fix reload")
		createTag(t, repo, "v1.1.0", h5)

		result, err := GenerateFromRepo(dir)
		require.NoError(t, err)

		// Parse the output and verify round-trip compatibility.
		cl := Parse(result)
		require.NotNil(t, cl)
		assert.Len(t, cl.Releases, 2)

		// Releases are oldest-first after Parse().
		assert.Equal(t, "v1.0.0", cl.Releases[0].Version)
		assert.Equal(t, "v1.1.0", cl.Releases[1].Version)

		// Verify entries exist.
		features := cl.EntriesByCategory(CategoryFeature)
		assert.GreaterOrEqual(t, len(features), 3)

		fixes := cl.EntriesByCategory(CategoryFix)
		assert.GreaterOrEqual(t, len(fixes), 2)
	})

	t.Run("category ordering", func(t *testing.T) {
		t.Parallel()

		repo, dir := initTestRepo(t)
		commitFile(t, repo, dir, "a.txt", "a", "fix: a bug fix")
		commitFile(t, repo, dir, "b.txt", "b", "feat: a feature")
		commitFile(t, repo, dir, "c.txt", "c", "feat(api)!: breaking change")
		h4 := commitFile(t, repo, dir, "d.txt", "d", "perf: a performance improvement")
		createTag(t, repo, "v1.0.0", h4)

		result, err := GenerateFromRepo(dir)
		require.NoError(t, err)

		// Breaking Changes should come before Features.
		idxBreaking := indexOf(result, "### Breaking Changes")
		idxFeatures := indexOf(result, "### Features")
		idxBugFixes := indexOf(result, "### Bug Fixes")
		idxPerf := indexOf(result, "### Performance Improvements")

		assert.Greater(t, idxFeatures, idxBreaking, "Features should come after Breaking Changes")
		assert.Greater(t, idxBugFixes, idxFeatures, "Bug Fixes should come after Features")
		assert.Greater(t, idxPerf, idxBugFixes, "Performance should come after Bug Fixes")
	})
}

func TestParseCommitMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		message    string
		includeAll bool
		wantOk     bool
		wantCat    Category
		wantScope  string
		wantDesc   string
	}{
		{
			name:      "feat with scope",
			message:   "feat(chat): add streaming",
			wantOk:    true,
			wantCat:   CategoryFeature,
			wantScope: "chat",
			wantDesc:  "add streaming",
		},
		{
			name:     "fix without scope",
			message:  "fix: resolve panic",
			wantOk:   true,
			wantCat:  CategoryFix,
			wantDesc: "resolve panic",
		},
		{
			name:      "perf with scope",
			message:   "perf(cache): reduce allocations",
			wantOk:    true,
			wantCat:   CategoryPerformance,
			wantScope: "cache",
			wantDesc:  "reduce allocations",
		},
		{
			name:      "breaking via exclamation",
			message:   "feat(api)!: remove method",
			wantOk:    true,
			wantCat:   CategoryBreaking,
			wantScope: "api",
			wantDesc:  "remove method",
		},
		{
			name:      "refactor maps to other",
			message:   "refactor(http): simplify middleware",
			wantOk:    true,
			wantCat:   CategoryOther,
			wantScope: "http",
			wantDesc:  "simplify middleware",
		},
		{
			name:    "test skipped",
			message: "test: add unit tests",
			wantOk:  false,
		},
		{
			name:    "ci skipped",
			message: "ci: update pipeline",
			wantOk:  false,
		},
		{
			name:       "non-conventional excluded by default",
			message:    "just a plain message",
			includeAll: false,
			wantOk:     false,
		},
		{
			name:       "non-conventional included with flag",
			message:    "just a plain message",
			includeAll: true,
			wantOk:     true,
			wantCat:    CategoryOther,
			wantDesc:   "just a plain message",
		},
		{
			name:    "empty message",
			message: "",
			wantOk:  false,
		},
		{
			name:     "breaking test commit still included",
			message:  "test!: breaking test change",
			wantOk:   true,
			wantCat:  CategoryBreaking,
			wantDesc: "breaking test change",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create a fresh parser per subtest — leodido/go-conventionalcommits
			// Machine is not goroutine-safe.
			subParser := ccparser.NewMachine(
				ccparser.WithTypes(conventionalcommits.TypesConventional),
				ccparser.WithBestEffort(),
			)

			entry, ok := parseCommitMessage(subParser, tt.message, tt.includeAll)
			assert.Equal(t, tt.wantOk, ok)

			if ok {
				assert.Equal(t, tt.wantCat, entry.Category)
				assert.Equal(t, tt.wantScope, entry.Scope)
				assert.Equal(t, tt.wantDesc, entry.Description)
			}
		})
	}
}

func TestCanonicalSemver(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"v1.0.0", "v1.0.0"},
		{"1.0.0", "v1.0.0"},
		{"v2.3.4-beta.1", "v2.3.4-beta.1"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, canonicalSemver(tt.input))
		})
	}
}

func TestTypeToCategory(t *testing.T) {
	t.Parallel()

	assert.Equal(t, CategoryFeature, typeToCategory("feat"))
	assert.Equal(t, CategoryFix, typeToCategory("fix"))
	assert.Equal(t, CategoryPerformance, typeToCategory("perf"))
	assert.Equal(t, CategoryOther, typeToCategory("refactor"))
	assert.Equal(t, CategoryOther, typeToCategory("docs"))
	assert.Equal(t, CategoryOther, typeToCategory("chore"))
}

func TestIsSkippedType(t *testing.T) {
	t.Parallel()

	assert.True(t, isSkippedType("test"))
	assert.True(t, isSkippedType("ci"))
	assert.False(t, isSkippedType("feat"))
	assert.False(t, isSkippedType("fix"))
	assert.False(t, isSkippedType("refactor"))
}

func TestFormatGroups(t *testing.T) {
	t.Parallel()

	groups := []releaseGroup{
		{
			version: "v1.0.0",
			entries: []Entry{
				{Category: CategoryFeature, Scope: "chat", Description: "add streaming"},
				{Category: CategoryFix, Description: "resolve panic"},
			},
		},
	}

	result := formatGroups(groups)
	assert.Contains(t, result, "# v1.0.0")
	assert.Contains(t, result, "### Features")
	assert.Contains(t, result, "* **chat:** add streaming")
	assert.Contains(t, result, "### Bug Fixes")
	assert.Contains(t, result, "* resolve panic")
}

func TestCategoryHeading(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Breaking Changes", categoryHeading(CategoryBreaking))
	assert.Equal(t, "Features", categoryHeading(CategoryFeature))
	assert.Equal(t, "Bug Fixes", categoryHeading(CategoryFix))
	assert.Equal(t, "Performance Improvements", categoryHeading(CategoryPerformance))
	assert.Equal(t, "Other", categoryHeading(CategoryOther))
}

// indexOf returns the position of substr in s, or -1 if not found.
func indexOf(s, substr string) int {
	return len(s) - len(s[findIndex(s, substr):])
}

func findIndex(s, substr string) int {
	for i := range s {
		if len(s[i:]) >= len(substr) && s[i:i+len(substr)] == substr {
			return i
		}
	}

	return len(s)
}
