package docs

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMkDocsNav_WithNav(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"mkdocs.yml": {Data: []byte(`nav:
  - Home: index.md
  - Guide:
    - Getting Started: guide/start.md
`)},
		"index.md":       {Data: []byte("# Welcome")},
		"guide/start.md": {Data: []byte("# Getting Started")},
	}

	nodes, err := parseMkDocsNav(fsys)
	require.NoError(t, err)
	require.Len(t, nodes, 2)
	assert.Equal(t, "Home", nodes[0].Title)
	assert.Equal(t, "index.md", nodes[0].Path)
	assert.Equal(t, "Guide", nodes[1].Title)
	require.Len(t, nodes[1].Children, 1)
	assert.Equal(t, "Getting Started", nodes[1].Children[0].Title)
}

func TestParseMkDocsNav_EmptyNav(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"mkdocs.yml": {Data: []byte("nav: []")},
	}

	// Empty nav falls back to FS walk, but empty FS has no .md files
	nodes, err := parseMkDocsNav(fsys)
	require.NoError(t, err)
	assert.Empty(t, nodes)
}

func TestParseMkDocsNav_InvalidYAML(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"mkdocs.yml": {Data: []byte("not: valid: yaml: [")},
	}

	// Invalid YAML falls back to FS walk
	nodes, err := parseMkDocsNav(fsys)
	require.NoError(t, err)
	assert.Empty(t, nodes)
}

func TestParseMkDocsNav_MissingMkDocsYml(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"index.md": {Data: []byte("# Index")},
		"guide.md": {Data: []byte("# Guide")},
	}

	nodes, err := parseMkDocsNav(fsys)
	require.NoError(t, err)
	require.Len(t, nodes, 2)
}

func TestParseMkDocsNav_NestedSections(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"mkdocs.yml": {Data: []byte(`nav:
  - Home: index.md
  - Guide:
    - Getting Started: guide/start.md
    - Advanced:
      - Plugins: guide/advanced/plugins.md
`)},
		"index.md":                   {Data: []byte("# Welcome")},
		"guide/start.md":             {Data: []byte("# Getting Started")},
		"guide/advanced/plugins.md":  {Data: []byte("# Plugins")},
	}

	nodes, err := parseMkDocsNav(fsys)
	require.NoError(t, err)
	require.Len(t, nodes, 2)

	guide := nodes[1]
	assert.Equal(t, "Guide", guide.Title)
	require.Len(t, guide.Children, 2)

	advanced := guide.Children[1]
	assert.Equal(t, "Advanced", advanced.Title)
	require.Len(t, advanced.Children, 1)
	assert.Equal(t, "Plugins", advanced.Children[0].Title)
}

func TestGenerateNavFromFS(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"index.md":           {Data: []byte("# Home")},
		"about.md":           {Data: []byte("# About")},
		"guide/install.md":   {Data: []byte("# Installation")},
		"guide/quickstart.md": {Data: []byte("# Quick Start")},
		".hidden.md":         {Data: []byte("# Hidden")},
		"_private.md":        {Data: []byte("# Private")},
		"readme.txt":         {Data: []byte("not markdown")},
	}

	nodes, err := generateNavFromFS(fsys, ".")
	require.NoError(t, err)

	// Should include index.md, about.md, and guide/ directory
	// Should exclude .hidden.md, _private.md, readme.txt
	assert.GreaterOrEqual(t, len(nodes), 2)

	// Verify hidden/private files are excluded
	for _, n := range nodes {
		assert.NotEqual(t, "Hidden", n.Title)
		assert.NotEqual(t, "Private", n.Title)
	}
}

func TestFormatTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		{"getting-started", "Getting Started"},
		{"hello_world", "Hello World"},
		{"simple", "Simple"},
		{"multi-word-title", "Multi Word Title"},
		{"mixed_and-styles", "Mixed And Styles"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, formatTitle(tt.input))
		})
	}
}

func TestExtractTitle(t *testing.T) {
	t.Parallel()

	t.Run("file with h1", func(t *testing.T) {
		t.Parallel()
		fsys := fstest.MapFS{
			"page.md": {Data: []byte("# My Page Title\n\nSome content.")},
		}
		assert.Equal(t, "My Page Title", extractTitle(fsys, "page.md"))
	})

	t.Run("file without h1", func(t *testing.T) {
		t.Parallel()
		fsys := fstest.MapFS{
			"page.md": {Data: []byte("No heading here.\n\nJust content.")},
		}
		assert.Equal(t, "", extractTitle(fsys, "page.md"))
	})

	t.Run("file does not exist", func(t *testing.T) {
		t.Parallel()
		fsys := fstest.MapFS{}
		assert.Equal(t, "", extractTitle(fsys, "missing.md"))
	})

	t.Run("h1 not on first line", func(t *testing.T) {
		t.Parallel()
		fsys := fstest.MapFS{
			"page.md": {Data: []byte("---\ntitle: test\n---\n# Actual Title\n")},
		}
		assert.Equal(t, "Actual Title", extractTitle(fsys, "page.md"))
	})
}

func TestParseNavList_StringEntry(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"page.md": {Data: []byte("# My Page")},
	}

	rawList := []any{"page.md"}
	nodes := parseNavList(fsys, rawList)

	require.Len(t, nodes, 1)
	assert.Equal(t, "My Page", nodes[0].Title)
	assert.Equal(t, "page.md", nodes[0].Path)
}

func TestParseNavList_MapEntry(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{}
	rawList := []any{
		map[string]any{"Custom Title": "custom.md"},
	}

	nodes := parseNavList(fsys, rawList)
	require.Len(t, nodes, 1)
	assert.Equal(t, "Custom Title", nodes[0].Title)
	assert.Equal(t, "custom.md", nodes[0].Path)
}

func TestSortEntries_IndexFirst(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"zebra.md": {Data: []byte("")},
		"index.md": {Data: []byte("")},
		"alpha.md": {Data: []byte("")},
	}

	entries, err := fsys.ReadDir(".")
	require.NoError(t, err)

	sortEntries(entries)

	assert.Equal(t, "index.md", entries[0].Name())
}

func TestGetAllMarkdownContent(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"index.md":       {Data: []byte("# Index\nWelcome")},
		"guide/start.md": {Data: []byte("# Start\nGuide content")},
		"readme.txt":     {Data: []byte("not markdown")},
	}

	content, err := GetAllMarkdownContent(fsys)
	require.NoError(t, err)
	assert.Contains(t, content, "# Index")
	assert.Contains(t, content, "# Start")
	assert.NotContains(t, content, "not markdown")
	assert.Contains(t, content, "--- File: index.md ---")
}

func TestGetAllMarkdownContent_EmptyFS(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{}
	content, err := GetAllMarkdownContent(fsys)
	require.NoError(t, err)
	assert.Empty(t, content)
}
