package changelog

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_EmptyInput(t *testing.T) {
	t.Parallel()

	cl := Parse("")
	assert.Empty(t, cl.Releases)
	assert.Empty(t, cl.FromVersion)
	assert.Empty(t, cl.ToVersion)
}

func TestParse_WhitespaceOnly(t *testing.T) {
	t.Parallel()

	cl := Parse("   \n\n  \t  \n")
	assert.Empty(t, cl.Releases)
}

func TestParse_SingleRelease(t *testing.T) {
	t.Parallel()

	input := `# v1.0.0

### Features

* **http:** add middleware chaining
* **grpc:** add interceptor support

### Bug Fixes

* fix startup race condition
`

	cl := Parse(input)
	require.Len(t, cl.Releases, 1)
	assert.Equal(t, "v1.0.0", cl.Releases[0].Version)
	assert.Equal(t, "v1.0.0", cl.FromVersion)
	assert.Equal(t, "v1.0.0", cl.ToVersion)

	entries := cl.Releases[0].Entries
	require.Len(t, entries, 3)

	assert.Equal(t, CategoryFeature, entries[0].Category)
	assert.Equal(t, "http", entries[0].Scope)
	assert.Equal(t, "add middleware chaining", entries[0].Description)

	assert.Equal(t, CategoryFeature, entries[1].Category)
	assert.Equal(t, "grpc", entries[1].Scope)
	assert.Equal(t, "add interceptor support", entries[1].Description)

	assert.Equal(t, CategoryFix, entries[2].Category)
	assert.Equal(t, "", entries[2].Scope)
	assert.Equal(t, "fix startup race condition", entries[2].Description)
}

func TestParse_MultipleReleases(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/multi_release.md")
	require.NoError(t, err)

	cl := Parse(string(data))
	require.Len(t, cl.Releases, 3)

	// Oldest first
	assert.Equal(t, "v1.1.0", cl.Releases[0].Version)
	assert.Equal(t, "v1.2.0", cl.Releases[1].Version)
	assert.Equal(t, "v1.3.0", cl.Releases[2].Version)

	assert.Equal(t, "v1.1.0", cl.FromVersion)
	assert.Equal(t, "v1.3.0", cl.ToVersion)
}

func TestParse_BreakingChanges(t *testing.T) {
	t.Parallel()

	input := `# v2.0.0

### BREAKING CHANGES

* **config:** rename ConfigPath to ConfigDir

### Features

* **http:** add TLS support
`

	cl := Parse(input)
	require.Len(t, cl.Releases, 1)

	breaking := cl.BreakingChanges()
	require.Len(t, breaking, 1)
	assert.Equal(t, "config", breaking[0].Scope)
	assert.Equal(t, "rename ConfigPath to ConfigDir", breaking[0].Description)
	assert.True(t, cl.HasBreakingChanges())
}

func TestParse_BreakingChangeFooter(t *testing.T) {
	t.Parallel()

	input := `# v2.0.0

### Features

* **config:** BREAKING CHANGE: remove deprecated SetPath method
`

	cl := Parse(input)
	require.Len(t, cl.Releases, 1)

	entries := cl.Releases[0].Entries
	require.Len(t, entries, 1)
	assert.Equal(t, CategoryBreaking, entries[0].Category)
	assert.Equal(t, "config", entries[0].Scope)
	assert.Equal(t, "remove deprecated SetPath method", entries[0].Description)
}

func TestParse_ScopedEntries(t *testing.T) {
	t.Parallel()

	input := `# v1.0.0

### Features

* **http:** add middleware chaining support
* **grpc:** add interceptor chaining support
`

	cl := Parse(input)
	entries := cl.Releases[0].Entries
	require.Len(t, entries, 2)

	assert.Equal(t, "http", entries[0].Scope)
	assert.Equal(t, "add middleware chaining support", entries[0].Description)
	assert.Equal(t, "grpc", entries[1].Scope)
	assert.Equal(t, "add interceptor chaining support", entries[1].Description)
}

func TestParse_UnscopedEntries(t *testing.T) {
	t.Parallel()

	input := `# v1.0.0

### Bug Fixes

* fix startup race condition
* resolve panic on nil logger
`

	cl := Parse(input)
	entries := cl.Releases[0].Entries
	require.Len(t, entries, 2)

	assert.Equal(t, "", entries[0].Scope)
	assert.Equal(t, "fix startup race condition", entries[0].Description)
	assert.Equal(t, "", entries[1].Scope)
	assert.Equal(t, "resolve panic on nil logger", entries[1].Description)
}

func TestParse_MalformedInput(t *testing.T) {
	t.Parallel()

	input := `This is just some random text
that doesn't follow any convention.
Nothing parseable here.`

	cl := Parse(input)
	assert.Empty(t, cl.Releases)
}

func TestParse_VersionHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		version string
	}{
		{
			name:    "with v prefix",
			input:   "# v1.2.3\n\n### Features\n\n* add feature",
			version: "v1.2.3",
		},
		{
			name:    "without v prefix",
			input:   "# 1.2.3\n\n### Features\n\n* add feature",
			version: "1.2.3",
		},
		{
			name:    "with suffix",
			input:   "# v1.2.3-beta.1\n\n### Features\n\n* add feature",
			version: "v1.2.3-beta.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cl := Parse(tt.input)
			require.Len(t, cl.Releases, 1)
			assert.Equal(t, tt.version, cl.Releases[0].Version)
		})
	}
}

func TestParse_PerformanceCategory(t *testing.T) {
	t.Parallel()

	input := `# v1.0.0

### Performance Improvements

* **cache:** reduce allocations in LRU eviction
`

	cl := Parse(input)
	entries := cl.Releases[0].Entries
	require.Len(t, entries, 1)
	assert.Equal(t, CategoryPerformance, entries[0].Category)
}

func TestParse_RawFieldPreserved(t *testing.T) {
	t.Parallel()

	input := `# v1.0.0

### Features

* **http:** add middleware chaining support
`

	cl := Parse(input)
	entries := cl.Releases[0].Entries
	require.Len(t, entries, 1)
	assert.Equal(t, "* **http:** add middleware chaining support", entries[0].Raw)
}

func TestChangelog_HasBreakingChanges_False(t *testing.T) {
	t.Parallel()

	cl := &Changelog{
		Releases: []Release{
			{Version: "v1.0.0", Entries: []Entry{
				{Category: CategoryFeature, Description: "new feature"},
			}},
		},
	}
	assert.False(t, cl.HasBreakingChanges())
}

func TestChangelog_EntriesByCategory(t *testing.T) {
	t.Parallel()

	cl := &Changelog{
		Releases: []Release{
			{Version: "v1.0.0", Entries: []Entry{
				{Category: CategoryFeature, Description: "feat 1"},
				{Category: CategoryFix, Description: "fix 1"},
				{Category: CategoryFeature, Description: "feat 2"},
			}},
			{Version: "v1.1.0", Entries: []Entry{
				{Category: CategoryFeature, Description: "feat 3"},
				{Category: CategoryBreaking, Description: "breaking 1"},
			}},
		},
	}

	features := cl.EntriesByCategory(CategoryFeature)
	assert.Len(t, features, 3)

	fixes := cl.EntriesByCategory(CategoryFix)
	assert.Len(t, fixes, 1)

	breaking := cl.EntriesByCategory(CategoryBreaking)
	assert.Len(t, breaking, 1)

	perf := cl.EntriesByCategory(CategoryPerformance)
	assert.Empty(t, perf)
}

func TestParse_OtherSection(t *testing.T) {
	t.Parallel()

	input := `# v1.0.0

### Other

* update go.mod dependencies
`

	cl := Parse(input)
	entries := cl.Releases[0].Entries
	require.Len(t, entries, 1)
	assert.Equal(t, CategoryOther, entries[0].Category)
}
