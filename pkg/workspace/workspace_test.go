package workspace

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetect_GTBProject(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/home/user/project/.gtb", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/home/user/project/.gtb/manifest.yaml", []byte("name: test"), 0o644))
	require.NoError(t, fs.MkdirAll("/home/user/project/pkg/cmd/root", 0o755))

	ws, err := Detect(fs, "/home/user/project/pkg/cmd/root", DefaultMarkers)
	require.NoError(t, err)

	assert.Equal(t, "/home/user/project", ws.Root)
	assert.Equal(t, ".gtb/manifest.yaml", ws.Marker)
}

func TestDetect_GoModule(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/home/user/project/go.mod", []byte("module test"), 0o644))
	require.NoError(t, fs.MkdirAll("/home/user/project/internal/pkg", 0o755))

	ws, err := Detect(fs, "/home/user/project/internal/pkg", DefaultMarkers)
	require.NoError(t, err)

	assert.Equal(t, "/home/user/project", ws.Root)
	assert.Equal(t, "go.mod", ws.Marker)
}

func TestDetect_GitRepo(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/home/user/project/.git", 0o755))
	require.NoError(t, fs.MkdirAll("/home/user/project/src/deep/nested", 0o755))

	ws, err := Detect(fs, "/home/user/project/src/deep/nested", DefaultMarkers)
	require.NoError(t, err)

	assert.Equal(t, "/home/user/project", ws.Root)
	assert.Equal(t, ".git", ws.Marker)
}

func TestDetect_MarkerPrecedence(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	// Both .gtb/manifest.yaml and go.mod exist at the same level
	require.NoError(t, fs.MkdirAll("/project/.gtb", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/project/.gtb/manifest.yaml", []byte(""), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/project/go.mod", []byte(""), 0o644))

	ws, err := Detect(fs, "/project", DefaultMarkers)
	require.NoError(t, err)

	assert.Equal(t, ".gtb/manifest.yaml", ws.Marker, "GTB marker should take precedence over go.mod")
}

func TestDetect_NotFound(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/home/user/random/dir", 0o755))

	_, err := Detect(fs, "/home/user/random/dir", DefaultMarkers)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestDetect_MaxDepth(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	// Marker is 5 levels up but max depth is 3
	require.NoError(t, afero.WriteFile(fs, "/project/go.mod", []byte(""), 0o644))
	require.NoError(t, fs.MkdirAll("/project/a/b/c/d/e", 0o755))

	_, err := Detect(fs, "/project/a/b/c/d/e", DefaultMarkers, WithMaxDepth(3))

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestDetect_MaxDepthSufficient(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/project/go.mod", []byte(""), 0o644))
	require.NoError(t, fs.MkdirAll("/project/a/b/c", 0o755))

	ws, err := Detect(fs, "/project/a/b/c", DefaultMarkers, WithMaxDepth(5))
	require.NoError(t, err)

	assert.Equal(t, "/project", ws.Root)
}

func TestDetect_StartDirIsRoot(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/go.mod", []byte(""), 0o644))

	ws, err := Detect(fs, "/", DefaultMarkers)
	require.NoError(t, err)

	assert.Equal(t, "/", ws.Root)
	assert.Equal(t, "go.mod", ws.Marker)
}

func TestDetect_CustomMarkers(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/myproject/Cargo.toml", []byte(""), 0o644))
	require.NoError(t, fs.MkdirAll("/myproject/src/lib", 0o755))

	ws, err := Detect(fs, "/myproject/src/lib", []string{"Cargo.toml"})
	require.NoError(t, err)

	assert.Equal(t, "/myproject", ws.Root)
	assert.Equal(t, "Cargo.toml", ws.Marker)
}

func TestDetect_EmptyMarkers(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/project", 0o755))

	_, err := Detect(fs, "/project", nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotFound)
}
