package agent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
)

// invalidJSON is a payload that fails json.Unmarshal into any of the tools'
// parameter structs (a bare array where an object is expected).
const invalidJSON = `[`

// dirToolFactories enumerates the constructors whose handler reads a "dir"
// argument and runs a fixed subprocess. Exercising them through the
// invalid-args and disallowed-path branches covers the otherwise-uncovered
// GoBuildTool/GoTestTool/LinterTool/GoModTidyTool wrappers without invoking a
// subprocess (which would need a network or external binary).
func dirToolFactories() map[string]func(afero.Fs, string) chat.Tool {
	return map[string]func(afero.Fs, string) chat.Tool{
		"go_build":    GoBuildTool,
		"go_test":     GoTestTool,
		"golangci":    LinterTool,
		"go_mod_tidy": GoModTidyTool,
	}
}

// TestDirTools_InvalidArguments covers the json.Unmarshal failure branch in
// each fixed-command directory tool (go_build/go_test/golangci_lint/go_mod_tidy).
func TestDirTools_InvalidArguments(t *testing.T) {
	t.Parallel()

	afs := afero.NewMemMapFs()
	require.NoError(t, afs.MkdirAll("/work", 0o755))

	for name, factory := range dirToolFactories() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tool := factory(afs, "/work")
			_, err := tool.Handler(context.Background(), json.RawMessage(invalidJSON))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid arguments")
		})
	}
}

// TestDirTools_RejectsOutsidePath covers the ensurePathAllowed rejection branch
// in each fixed-command directory tool — the dir argument escapes basePath, so
// the handler errors before any subprocess is started.
func TestDirTools_RejectsOutsidePath(t *testing.T) {
	t.Parallel()

	afs := afero.NewMemMapFs()
	require.NoError(t, afs.MkdirAll("/work", 0o755))
	require.NoError(t, afs.MkdirAll("/elsewhere", 0o755))

	for name, factory := range dirToolFactories() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tool := factory(afs, "/work")
			args, err := json.Marshal(map[string]string{"dir": "/elsewhere"})
			require.NoError(t, err)

			_, err = tool.Handler(context.Background(), args)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrPathInvalid)
		})
	}
}

// TestGoBuildTool_SuccessOnTrivialModule drives the success path of
// createSingleDirTool through GoBuildTool against a dependency-free module, so
// it builds offline and deterministically. This and the go_test twin are the
// only tests that actually spawn the toolchain; both skip when `go` is absent.
func TestGoBuildTool_SuccessOnTrivialModule(t *testing.T) {
	// Not parallel: spawns a subprocess and uses the real OS filesystem.
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}

	base := t.TempDir()
	writeTrivialModule(t, base)

	tool := GoBuildTool(afero.NewOsFs(), base)

	args, err := json.Marshal(map[string]string{"dir": base})
	require.NoError(t, err)

	result, err := tool.Handler(context.Background(), args)
	require.NoError(t, err)
	assert.Equal(t, "Build successful", result)
}

// TestGoTestTool_SuccessOnTrivialModule mirrors the build test for go_test, so
// the GoTestTool wrapper's success path is exercised offline.
func TestGoTestTool_SuccessOnTrivialModule(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}

	base := t.TempDir()
	writeTrivialModule(t, base)

	tool := GoTestTool(afero.NewOsFs(), base)

	args, err := json.Marshal(map[string]string{"dir": base})
	require.NoError(t, err)

	result, err := tool.Handler(context.Background(), args)
	require.NoError(t, err)
	assert.Equal(t, "Tests passed", result)
}

// writeTrivialModule lays down a minimal, dependency-free Go module so the
// toolchain tools run offline.
func writeTrivialModule(t *testing.T, dir string) {
	t.Helper()

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module trivial\n\ngo 1.21\n"),
		0o600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "main.go"),
		[]byte("package main\n\nfunc main() {}\n"),
		0o600,
	))
}

// TestTruncateOutput_Empty covers the early-return branch for empty subprocess
// output.
func TestTruncateOutput_Empty(t *testing.T) {
	t.Parallel()

	assert.Empty(t, truncateOutput(nil))
	assert.Empty(t, truncateOutput([]byte{}))
}

// TestReadFileTool_InvalidArguments covers the json.Unmarshal failure branch in
// ReadFileTool.
func TestReadFileTool_InvalidArguments(t *testing.T) {
	t.Parallel()

	tool := ReadFileTool(afero.NewMemMapFs(), "/project")
	_, err := tool.Handler(context.Background(), json.RawMessage(invalidJSON))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid arguments")
}

// TestReadFileTool_MissingFile covers the afero.ReadFile error branch (path is
// allowed but the file does not exist).
func TestReadFileTool_MissingFile(t *testing.T) {
	t.Parallel()

	afs := afero.NewMemMapFs()
	require.NoError(t, afs.MkdirAll("/project", 0o755))

	tool := ReadFileTool(afs, "/project")
	args, err := json.Marshal(map[string]string{"path": "/project/absent.txt"})
	require.NoError(t, err)

	_, err = tool.Handler(context.Background(), args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read file")
}

// TestWriteFileTool_InvalidArguments covers the json.Unmarshal failure branch
// in WriteFileTool.
func TestWriteFileTool_InvalidArguments(t *testing.T) {
	t.Parallel()

	tool := WriteFileTool(afero.NewMemMapFs(), "/project")
	_, err := tool.Handler(context.Background(), json.RawMessage(invalidJSON))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid arguments")
}

// TestWriteFileTool_WriteError covers the afero.WriteFile error branch: the
// path is allowed, but the underlying filesystem refuses the write (read-only).
func TestWriteFileTool_WriteError(t *testing.T) {
	t.Parallel()

	mem := afero.NewMemMapFs()
	require.NoError(t, mem.MkdirAll("/project", 0o755))

	// A read-only overlay rejects writes while still resolving the allowed
	// path, so the handler reaches afero.WriteFile and fails there.
	ro := afero.NewReadOnlyFs(mem)

	tool := WriteFileTool(ro, "/project")
	args, err := json.Marshal(map[string]string{"path": "/project/out.txt", "content": "x"})
	require.NoError(t, err)

	_, err = tool.Handler(context.Background(), args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write file")
}

// TestListDirTool_InvalidArguments covers the json.Unmarshal failure branch in
// ListDirTool.
func TestListDirTool_InvalidArguments(t *testing.T) {
	t.Parallel()

	tool := ListDirTool(afero.NewMemMapFs(), "/project")
	_, err := tool.Handler(context.Background(), json.RawMessage(invalidJSON))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid arguments")
}

// TestListDirTool_MissingDir covers the afero.ReadDir error branch.
func TestListDirTool_MissingDir(t *testing.T) {
	t.Parallel()

	afs := afero.NewMemMapFs()
	require.NoError(t, afs.MkdirAll("/project", 0o755))

	tool := ListDirTool(afs, "/project")
	args, err := json.Marshal(map[string]string{"path": "/project/absent"})
	require.NoError(t, err)

	_, err = tool.Handler(context.Background(), args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list directory")
}

// TestReadFilesTool_InvalidArguments covers the json.Unmarshal failure branch
// in ReadFilesTool.
func TestReadFilesTool_InvalidArguments(t *testing.T) {
	t.Parallel()

	tool := ReadFilesTool(afero.NewMemMapFs(), "/project")
	_, err := tool.Handler(context.Background(), json.RawMessage(invalidJSON))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid arguments")
}

// TestReadFilesTool_MissingFileReportedInline covers the afero.ReadFile error
// branch inside the batch loop (allowed path, but the file is absent), which is
// reported inline rather than failing the whole call.
func TestReadFilesTool_MissingFileReportedInline(t *testing.T) {
	t.Parallel()

	afs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(afs, "/project/a.go", []byte("package a"), 0o644))

	tool := ReadFilesTool(afs, "/project")
	args, err := json.Marshal(map[string][]string{"paths": {"/project/a.go", "/project/absent.go"}})
	require.NoError(t, err)

	result, err := tool.Handler(context.Background(), args)
	require.NoError(t, err)

	out, ok := result.(string)
	require.True(t, ok)
	assert.Contains(t, out, "package a")
	assert.Contains(t, out, "===== /project/absent.go =====")
	assert.Contains(t, out, "error:")
}

// TestTreeTool_InvalidArguments covers the json.Unmarshal failure branch in
// TreeTool.
func TestTreeTool_InvalidArguments(t *testing.T) {
	t.Parallel()

	tool := TreeTool(afero.NewMemMapFs(), "/project")
	_, err := tool.Handler(context.Background(), json.RawMessage(invalidJSON))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid arguments")
}

// TestTreeTool_Truncates covers the >maxTreeEntries truncation branch.
func TestTreeTool_Truncates(t *testing.T) {
	t.Parallel()

	afs := afero.NewMemMapFs()
	for i := range maxTreeEntries + 10 {
		p := filepath.Join("/project", "f"+strconv.Itoa(i)+".txt")
		require.NoError(t, afero.WriteFile(afs, p, []byte("x"), 0o644))
	}

	tool := TreeTool(afs, "/project")
	args, err := json.Marshal(map[string]string{"path": "/project"})
	require.NoError(t, err)

	result, err := tool.Handler(context.Background(), args)
	require.NoError(t, err)

	out, ok := result.(string)
	require.True(t, ok)
	assert.Contains(t, out, "truncated at")
}

// TestGoGetTool_InvalidArguments covers the json.Unmarshal failure branch in
// GoGetTool.
func TestGoGetTool_InvalidArguments(t *testing.T) {
	t.Parallel()

	tool := GoGetTool(afero.NewMemMapFs(), "/project")
	_, err := tool.Handler(context.Background(), json.RawMessage(invalidJSON))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid arguments")
}

// TestGoGetTool_RejectsOutsidePath covers the ensurePathAllowed rejection
// branch in GoGetTool (dir escapes basePath, before validation/exec).
func TestGoGetTool_RejectsOutsidePath(t *testing.T) {
	t.Parallel()

	afs := afero.NewMemMapFs()
	require.NoError(t, afs.MkdirAll("/work", 0o755))
	require.NoError(t, afs.MkdirAll("/elsewhere", 0o755))

	tool := GoGetTool(afs, "/work")
	args, err := json.Marshal(map[string]string{"package": "github.com/spf13/cobra", "dir": "/elsewhere"})
	require.NoError(t, err)

	_, err = tool.Handler(context.Background(), args)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPathInvalid)
}

// TestQueryUserTool_InvalidArguments covers the json.Unmarshal failure branch
// in QueryUserTool.
func TestQueryUserTool_InvalidArguments(t *testing.T) {
	t.Parallel()

	tool := QueryUserTool(func(context.Context, string, []string) (string, error) { return "", nil })
	_, err := tool.Handler(context.Background(), json.RawMessage(invalidJSON))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid arguments")
}
