package agent

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsPathAllowed_WithinBase(t *testing.T) {
	t.Parallel()

	afs := afero.NewMemMapFs()
	_ = afs.MkdirAll("/base/sub", 0755)

	resolved, err := isPathAllowed(afs, "/base", "/base/sub/file.txt")
	require.NoError(t, err)
	assert.Equal(t, "/base/sub/file.txt", resolved)
}

func TestIsPathAllowed_OutsideBase(t *testing.T) {
	t.Parallel()

	afs := afero.NewMemMapFs()

	_, err := isPathAllowed(afs, "/base", "/etc/passwd")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPathInvalid)
}

func TestIsPathAllowed_ExactBase(t *testing.T) {
	t.Parallel()

	afs := afero.NewMemMapFs()
	_ = afs.MkdirAll("/base", 0755)

	resolved, err := isPathAllowed(afs, "/base", "/base")
	require.NoError(t, err)
	assert.Equal(t, "/base", resolved)
}

func TestIsPathAllowed_SymlinkBypass(t *testing.T) {
	// This test uses the real filesystem since symlinks need OS support
	baseDir := t.TempDir()
	outsideDir := t.TempDir()

	secretFile := filepath.Join(outsideDir, "secret.txt")
	require.NoError(t, os.WriteFile(secretFile, []byte("secret"), 0644))

	// Create symlink inside base pointing outside
	symlink := filepath.Join(baseDir, "escape")
	require.NoError(t, os.Symlink(outsideDir, symlink))

	afs := afero.NewOsFs()

	_, err := isPathAllowed(afs, baseDir, filepath.Join(symlink, "secret.txt"))
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrPathInvalid)
}

func TestIsPathAllowed_SymlinkWithinBase(t *testing.T) {
	baseDir := t.TempDir()

	// Create real subdirectory and file
	realDir := filepath.Join(baseDir, "real")
	require.NoError(t, os.MkdirAll(realDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(realDir, "data.txt"), []byte("ok"), 0644))

	// Create symlink within base pointing to another location within base
	symlink := filepath.Join(baseDir, "link")
	require.NoError(t, os.Symlink(realDir, symlink))

	afs := afero.NewOsFs()

	resolved, err := isPathAllowed(afs, baseDir, filepath.Join(symlink, "data.txt"))
	require.NoError(t, err)

	// Resolve expected path through EvalSymlinks to handle OS-level symlinks (e.g., macOS /var → /private/var)
	expectedPath, err := filepath.EvalSymlinks(filepath.Join(realDir, "data.txt"))
	require.NoError(t, err)
	assert.Equal(t, expectedPath, resolved)
}

func TestIsPathAllowed_NonexistentTarget(t *testing.T) {
	t.Parallel()

	afs := afero.NewMemMapFs()
	_ = afs.MkdirAll("/base/sub", 0755)

	// Writing to a new file in an existing directory should be allowed
	resolved, err := isPathAllowed(afs, "/base", "/base/sub/newfile.txt")
	require.NoError(t, err)
	assert.Equal(t, "/base/sub/newfile.txt", resolved)
}

func TestResolveSymlinks_NoSymlinkSupport(t *testing.T) {
	t.Parallel()

	// MemMapFs doesn't support symlinks — should return absolute path unchanged
	afs := afero.NewMemMapFs()

	resolved, err := resolveSymlinks(afs, "/some/path")
	require.NoError(t, err)
	assert.Equal(t, "/some/path", resolved)
}

func TestResolveSymlinks_ChainedSymlinks(t *testing.T) {
	baseDir := t.TempDir()

	// Create: base/a -> base/b -> base/c (real dir)
	realDir := filepath.Join(baseDir, "c")
	require.NoError(t, os.MkdirAll(realDir, 0755))
	require.NoError(t, os.Symlink(realDir, filepath.Join(baseDir, "b")))
	require.NoError(t, os.Symlink(filepath.Join(baseDir, "b"), filepath.Join(baseDir, "a")))

	afs := afero.NewOsFs()

	resolved, err := resolveSymlinks(afs, filepath.Join(baseDir, "a"))
	require.NoError(t, err)

	// Use EvalSymlinks to get the expected canonical path (handles OS-level symlinks like macOS /var → /private/var)
	expectedPath, err := filepath.EvalSymlinks(realDir)
	require.NoError(t, err)
	assert.Equal(t, expectedPath, resolved)
}

func TestReadFileTool_UsesAfero(t *testing.T) {
	t.Parallel()

	afs := afero.NewMemMapFs()
	basePath := "/project"

	_ = afs.MkdirAll(basePath, 0755)
	require.NoError(t, afero.WriteFile(afs, "/project/test.txt", []byte("hello world"), 0644))

	tool := ReadFileTool(afs, basePath)

	args, _ := json.Marshal(map[string]string{"path": "/project/test.txt"})
	result, err := tool.Handler(context.Background(), args)

	require.NoError(t, err)
	assert.Equal(t, "hello world", result)
}

func TestReadFileTool_RejectsOutsidePath(t *testing.T) {
	t.Parallel()

	afs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(afs, "/etc/secret", []byte("secret"), 0644))

	tool := ReadFileTool(afs, "/project")

	args, _ := json.Marshal(map[string]string{"path": "/etc/secret"})
	_, err := tool.Handler(context.Background(), args)

	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrPathInvalid)
}

func TestWriteFileTool_UsesAfero(t *testing.T) {
	t.Parallel()

	afs := afero.NewMemMapFs()
	basePath := "/project"

	_ = afs.MkdirAll(basePath, 0755)

	tool := WriteFileTool(afs, basePath)

	args, _ := json.Marshal(map[string]string{
		"path":    "/project/output.txt",
		"content": "written content",
	})
	result, err := tool.Handler(context.Background(), args)

	require.NoError(t, err)
	assert.Contains(t, result, "Successfully wrote")

	// Verify file was written via afero
	content, err := afero.ReadFile(afs, "/project/output.txt")
	require.NoError(t, err)
	assert.Equal(t, "written content", string(content))

	// Verify file permissions
	info, err := afs.Stat("/project/output.txt")
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0600), info.Mode().Perm())
}

func TestWriteFileTool_RejectsOutsidePath(t *testing.T) {
	t.Parallel()

	afs := afero.NewMemMapFs()

	tool := WriteFileTool(afs, "/project")

	args, _ := json.Marshal(map[string]string{
		"path":    "/etc/evil",
		"content": "bad",
	})
	_, err := tool.Handler(context.Background(), args)

	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrPathInvalid)
}

func TestListDirTool_UsesAfero(t *testing.T) {
	t.Parallel()

	afs := afero.NewMemMapFs()
	basePath := "/project"

	_ = afs.MkdirAll("/project/subdir", 0755)
	require.NoError(t, afero.WriteFile(afs, "/project/file1.txt", []byte("a"), 0644))
	require.NoError(t, afero.WriteFile(afs, "/project/file2.go", []byte("b"), 0644))

	tool := ListDirTool(afs, basePath)

	args, _ := json.Marshal(map[string]string{"path": "/project"})
	result, err := tool.Handler(context.Background(), args)

	require.NoError(t, err)

	resultStr, ok := result.(string)
	require.True(t, ok)
	assert.Contains(t, resultStr, "file1.txt")
	assert.Contains(t, resultStr, "file2.go")
	assert.Contains(t, resultStr, "subdir/")
}

func TestListDirTool_RejectsOutsidePath(t *testing.T) {
	t.Parallel()

	afs := afero.NewMemMapFs()
	_ = afs.MkdirAll("/etc", 0755)

	tool := ListDirTool(afs, "/project")

	args, _ := json.Marshal(map[string]string{"path": "/etc"})
	_, err := tool.Handler(context.Background(), args)

	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrPathInvalid)
}
