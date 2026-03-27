package changelog

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createArchive builds a gzipped tar archive from a map of filename → content.
func createArchive(t *testing.T, files map[string]string) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer

	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, content := range files {
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		require.NoError(t, tw.WriteHeader(hdr))

		_, err := tw.Write([]byte(content))
		require.NoError(t, err)
	}

	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	return &buf
}

func TestParseFromArchive_ValidChangelog(t *testing.T) {
	t.Parallel()

	changelogContent := `# v1.2.0

### Features

* **http:** add middleware chaining

### Bug Fixes

* fix startup race condition

# v1.1.0

### Features

* **grpc:** add interceptor support
`

	archive := createArchive(t, map[string]string{
		"gtb":          "fake-binary-content",
		"CHANGELOG.md": changelogContent,
	})

	cl, err := ParseFromArchive(archive)
	require.NoError(t, err)
	require.NotNil(t, cl)
	require.Len(t, cl.Releases, 2)

	// Oldest first
	assert.Equal(t, "v1.1.0", cl.Releases[0].Version)
	assert.Equal(t, "v1.2.0", cl.Releases[1].Version)

	features := cl.EntriesByCategory(CategoryFeature)
	assert.Len(t, features, 2)

	fixes := cl.EntriesByCategory(CategoryFix)
	assert.Len(t, fixes, 1)
}

func TestParseFromArchive_NoChangelog(t *testing.T) {
	t.Parallel()

	archive := createArchive(t, map[string]string{
		"gtb":       "fake-binary-content",
		"README.md": "# GTB\n",
	})

	cl, err := ParseFromArchive(archive)
	assert.NoError(t, err)
	assert.Nil(t, cl)
}

func TestParseFromArchive_NestedChangelog(t *testing.T) {
	t.Parallel()

	changelogContent := `# v1.0.0

### Features

* add initial release
`

	archive := createArchive(t, map[string]string{
		"gtb_Linux_x86_64/gtb":          "fake-binary-content",
		"gtb_Linux_x86_64/CHANGELOG.md": changelogContent,
	})

	cl, err := ParseFromArchive(archive)
	require.NoError(t, err)
	require.NotNil(t, cl)
	require.Len(t, cl.Releases, 1)
	assert.Equal(t, "v1.0.0", cl.Releases[0].Version)
}

func TestParseFromArchive_CaseInsensitiveMatch(t *testing.T) {
	t.Parallel()

	archive := createArchive(t, map[string]string{
		"gtb":          "fake-binary-content",
		"changelog.md": "# v1.0.0\n\n### Features\n\n* add feature\n",
	})

	cl, err := ParseFromArchive(archive)
	require.NoError(t, err)
	require.NotNil(t, cl)
	require.Len(t, cl.Releases, 1)
}

func TestParseFromArchive_EmptyChangelog(t *testing.T) {
	t.Parallel()

	archive := createArchive(t, map[string]string{
		"gtb":          "fake-binary-content",
		"CHANGELOG.md": "",
	})

	cl, err := ParseFromArchive(archive)
	require.NoError(t, err)
	require.NotNil(t, cl)
	assert.Empty(t, cl.Releases)
}

func TestParseFromArchive_InvalidGzip(t *testing.T) {
	t.Parallel()

	cl, err := ParseFromArchive(strings.NewReader("not-gzip-data"))
	assert.Error(t, err)
	assert.Nil(t, cl)
	assert.Contains(t, err.Error(), "gzip")
}

func TestParseFromArchive_SkipsDirectoryEntries(t *testing.T) {
	t.Parallel()

	// Create archive with a directory entry named CHANGELOG.md (unlikely but defensive)
	var buf bytes.Buffer

	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// Directory entry with the changelog name
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "CHANGELOG.md/",
		Mode:     0o755,
		Typeflag: tar.TypeDir,
	}))

	// Actual changelog as a regular file
	content := "# v1.0.0\n\n### Features\n\n* add feature\n"
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "CHANGELOG.md",
		Mode:     0o644,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	}))

	_, err := tw.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	cl, err := ParseFromArchive(&buf)
	require.NoError(t, err)
	require.NotNil(t, cl)
	require.Len(t, cl.Releases, 1)
}
