package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadFilesTool_ReadsMultiple(t *testing.T) {
	t.Parallel()

	afs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(afs, "/project/a.go", []byte("package a"), 0644))
	require.NoError(t, afero.WriteFile(afs, "/project/b.go", []byte("package b"), 0644))

	tool := ReadFilesTool(afs, "/project")
	args, err := json.Marshal(map[string][]string{"paths": {"/project/a.go", "/project/b.go"}})
	require.NoError(t, err)

	result, err := tool.Handler(context.Background(), args)
	require.NoError(t, err)

	out, ok := result.(string)
	require.True(t, ok)
	assert.Contains(t, out, "===== /project/a.go =====")
	assert.Contains(t, out, "package a")
	assert.Contains(t, out, "===== /project/b.go =====")
	assert.Contains(t, out, "package b")
}

func TestReadFilesTool_ReportsBadPathInline(t *testing.T) {
	t.Parallel()

	afs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(afs, "/project/a.go", []byte("package a"), 0644))
	require.NoError(t, afero.WriteFile(afs, "/etc/secret", []byte("topsecret"), 0644))

	tool := ReadFilesTool(afs, "/project")
	args, err := json.Marshal(map[string][]string{"paths": {"/project/a.go", "/etc/secret"}})
	require.NoError(t, err)

	// A disallowed path is reported inline, not as a tool-level failure, and
	// must not leak the outside file's contents.
	result, err := tool.Handler(context.Background(), args)
	require.NoError(t, err)

	out, ok := result.(string)
	require.True(t, ok)
	assert.Contains(t, out, "package a")
	assert.Contains(t, out, "===== /etc/secret =====")
	assert.Contains(t, out, "error:")
	assert.NotContains(t, out, "topsecret")
}

func TestTreeTool_ListsRecursively(t *testing.T) {
	t.Parallel()

	afs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(afs, "/project/main.go", []byte("x"), 0644))
	require.NoError(t, afero.WriteFile(afs, "/project/pkg/cmd/run.go", []byte("y"), 0644))

	tool := TreeTool(afs, "/project")
	args, err := json.Marshal(map[string]string{"path": "/project"})
	require.NoError(t, err)

	result, err := tool.Handler(context.Background(), args)
	require.NoError(t, err)

	out, ok := result.(string)
	require.True(t, ok)
	assert.Contains(t, out, "main.go")
	assert.Contains(t, out, "pkg/")
	assert.Contains(t, out, "pkg/cmd/run.go")
}

func TestTreeTool_RejectsOutsidePath(t *testing.T) {
	t.Parallel()

	afs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(afs, "/etc/secret", []byte("secret"), 0644))

	tool := TreeTool(afs, "/project")
	args, err := json.Marshal(map[string]string{"path": "/etc"})
	require.NoError(t, err)

	_, err = tool.Handler(context.Background(), args)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPathInvalid)
}
