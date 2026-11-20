package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fixtureChangelog = `# Changelog

## [v1.2.0](https://example.com/releases/v1.2.0)

[Compare to previous version](https://example.com/compare/v1.1.0...v1.2.0)

### Features

- a thing

## [v1.2.0-rc.1](https://example.com/releases/v1.2.0-rc.1)

- a candidate

## [v1.1.0](https://example.com/releases/v1.1.0)

- an older thing
`

func TestChangelogSection(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		tag   string
		want  string
		found bool
	}{
		"the first section, without its heading": {
			tag:   "v1.2.0",
			want:  "[Compare to previous version](https://example.com/compare/v1.1.0...v1.2.0)\n\n### Features\n\n- a thing",
			found: true,
		},
		"a prerelease is its own section":  {tag: "v1.2.0-rc.1", want: "- a candidate", found: true},
		"the last section runs to the end": {tag: "v1.1.0", want: "- an older thing", found: true},
		"a tag with no section":            {tag: "v1.3.0"},
		"a tag that only prefixes another": {tag: "v1.2"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, found := changelogSection(fixtureChangelog, tc.tag)
			assert.Equal(t, tc.found, found)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestChangelogSection_PlainHeading(t *testing.T) {
	t.Parallel()

	got, found := changelogSection("## v1.2.0\n\n- plain\n\n## v1.1.0\n", "v1.2.0")
	assert.True(t, found)
	assert.Equal(t, "- plain", got)
}

func TestNotesFromChangelog(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "CHANGELOG.md")
	require.NoError(t, os.WriteFile(path, []byte(fixtureChangelog), 0o600))

	t.Run("the tag's section", func(t *testing.T) {
		t.Parallel()

		var warned bytes.Buffer

		notes, err := notesFromChangelog(path, &warned)("v1.1.0")
		require.NoError(t, err)
		assert.Equal(t, "- an older thing", notes)
		assert.Empty(t, warned.String())
	})

	t.Run("no section warns and gives no notes", func(t *testing.T) {
		t.Parallel()

		var warned bytes.Buffer

		notes, err := notesFromChangelog(path, &warned)("v9.9.9")
		require.NoError(t, err, "notes are optional, so a missing section never fails a release")
		assert.Empty(t, notes)
		assert.Contains(t, warned.String(), "v9.9.9")
	})

	t.Run("no file warns and gives no notes", func(t *testing.T) {
		t.Parallel()

		var warned bytes.Buffer

		notes, err := notesFromChangelog(filepath.Join(t.TempDir(), "absent.md"), &warned)("v1.2.0")
		require.NoError(t, err)
		assert.Empty(t, notes)
		assert.Contains(t, warned.String(), "absent.md")
	})
}

func TestBuild_NotesFromTheChangelog(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "CHANGELOG.md")
	require.NoError(t, os.WriteFile(path, []byte(fixtureChangelog), 0o600))

	m, _, err := fixtureBuilder().build(fixtureDist(t), base, "", notesFromChangelog(path, &bytes.Buffer{}))
	require.NoError(t, err)
	assert.Contains(t, m.Notes, "- a thing", "the fixture's tag is v1.2.0")
	assert.NotContains(t, m.Notes, "a candidate")
}

func TestNotesFlags(t *testing.T) {
	t.Parallel()

	source, err := notesFlags("", "", &bytes.Buffer{})
	require.NoError(t, err)
	assert.Nil(t, source, "neither flag: no notes")

	_, err = notesFlags("NOTES.md", "CHANGELOG.md", &bytes.Buffer{})
	require.ErrorIs(t, err, errNotesTwice)

	path := filepath.Join(t.TempDir(), "notes.md")
	require.NoError(t, os.WriteFile(path, []byte("## v1.2.0\n\n- a thing\n"), 0o600))

	source, err = notesFlags(path, "", &bytes.Buffer{})
	require.NoError(t, err)

	notes, err := source("v1.2.0")
	require.NoError(t, err)
	assert.Equal(t, "## v1.2.0\n\n- a thing\n", notes, "--notes is the file's text, unchanged")

	source, err = notesFlags("", path, &bytes.Buffer{})
	require.NoError(t, err)

	notes, err = source("v1.2.0")
	require.NoError(t, err)
	assert.Equal(t, "- a thing", notes, "--changelog takes the tag's section, without its heading")
}

func TestNotesFromFile_Unreadable(t *testing.T) {
	t.Parallel()

	_, err := notesFromFile(filepath.Join(t.TempDir(), "absent.md"))("v1.2.0")
	require.ErrorContains(t, err, "reading notes", "--notes names a file on purpose, so a missing one fails")
}
