package generator

// Regression tests for the generator validation perimeter
// (docs/development/specs/2026-06-12-generator-validation-perimeter.md):
//
//   - a tampered manifest command name must never reach the
//     filepath.Join / RemoveAll sink (skip + ERROR log, valid commands
//     still regenerate);
//   - a tampered ManifestSigning field must never render into the
//     CI-executed .goreleaser.yaml (signing skipped + ERROR log);
//   - the AI doc tools must not read or list outside the project root;
//   - ErrNotGoToolBaseProject must be a matchable, placeholder-free
//     sentinel.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// logBuffer is the capturing subset of the buffer logger returned by
// logger.NewBuffer (whose concrete type is unexported).
type logBuffer interface {
	logger.Logger
	ContainsLevel(level logger.Level, substr string) bool
	Messages() []string
}

// newPerimeterTestProject scaffolds the minimal in-memory project layout the
// regenerate path needs, with the given manifest content, and returns the
// generator plus the capturing logger.
func newPerimeterTestProject(t *testing.T, manifest string) (*Generator, afero.Fs, logBuffer) {
	t.Helper()

	fs := afero.NewMemMapFs()
	buf := logger.NewBuffer()

	p := &props.Props{
		FS:      fs,
		Logger:  buf,
		Config:  emptyTestStore(t),
		Version: version.NewInfo("v1.0.0", "", ""),
	}

	root := "/work"
	require.NoError(t, fs.MkdirAll(root+"/.gtb", 0o755))
	require.NoError(t, afero.WriteFile(fs, root+"/.gtb/manifest.yaml", []byte(manifest), 0o644))
	require.NoError(t, afero.WriteFile(fs, root+"/go.mod", []byte("module test-mod\n"), 0o644))
	require.NoError(t, fs.MkdirAll(root+"/pkg/cmd/root", 0o755))
	require.NoError(t, afero.WriteFile(fs, root+"/pkg/cmd/root/cmd.go",
		[]byte("package root\nfunc NewCmdRoot(p interface{}) {}\n"), 0o644))

	g := New(p, &Config{Path: root})
	g.runCommand = func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
		return []byte("done"), nil
	}

	return g, fs, buf
}

// TestRegenerateProject_SkipsTraversalCommandName proves a tampered manifest
// command name cannot drive writes outside the project tree: the offending
// command is skipped with an ERROR log while valid commands still regenerate.
func TestRegenerateProject_SkipsTraversalCommandName(t *testing.T) {
	t.Parallel()

	manifest := "properties:\n  name: mytool\nversion:\n  gtb: v1.0.0\ncommands:\n" +
		"  - name: good\n" +
		"  - name: ../../evil\n"

	g, fs, buf := newPerimeterTestProject(t, manifest)

	require.NoError(t, g.RegenerateProject(context.Background()))

	// The valid command regenerated.
	exists, _ := afero.Exists(fs, "/work/pkg/cmd/good/cmd.go")
	assert.True(t, exists, "valid command must still regenerate")

	// The traversal target was never written: filepath.Join would have
	// cleaned /work/pkg/cmd/../../evil to /work/evil... and beyond.
	for _, escaped := range []string{"/evil", "/work/evil", "/work/pkg/evil"} {
		exists, _ := afero.Exists(fs, escaped)
		assert.False(t, exists, "no write outside pkg/cmd permitted: %s", escaped)
	}

	// The skip is surfaced at ERROR level.
	assert.True(t, buf.ContainsLevel(logger.ErrorLevel, "Skipping"),
		"expected an ERROR-level skip log, got: %v", buf.Messages())

	// The skipped command was not registered in the regenerated root command.
	rootCmd, err := afero.ReadFile(fs, "/work/pkg/cmd/root/cmd.go")
	require.NoError(t, err)
	assert.NotContains(t, string(rootCmd), "evil")
}

// TestRegenerateProject_SkipsInvalidSigning proves a tampered signing block
// cannot inject YAML into the CI-executed .goreleaser.yaml: signing-block
// rendering is skipped with an ERROR log and the rest still regenerates.
func TestRegenerateProject_SkipsInvalidSigning(t *testing.T) {
	t.Parallel()

	manifest := "properties:\n  name: mytool\n  signing:\n    enabled: true\n" +
		"    backend: aws-kms\n    kms_region: eu-west-2\n" +
		"    key_id: \"x\\\"\\n  - artifact: pwned\"\n" +
		"version:\n  gtb: v1.0.0\ncommands:\n  - name: good\n"

	g, fs, buf := newPerimeterTestProject(t, manifest)

	require.NoError(t, g.RegenerateProject(context.Background()))

	out, err := afero.ReadFile(fs, "/work/.goreleaser.yaml")
	require.NoError(t, err)
	assert.NotContains(t, string(out), "signs:", "invalid signing must skip the signs block")
	assert.NotContains(t, string(out), "artifact: pwned")

	assert.True(t, buf.ContainsLevel(logger.ErrorLevel, "signing"),
		"expected an ERROR-level signing skip log, got: %v", buf.Messages())

	exists, _ := afero.Exists(fs, "/work/pkg/cmd/good/cmd.go")
	assert.True(t, exists, "valid commands must still regenerate when signing is skipped")
}

// TestGetCommandPath_ContainedUnderPkgCmd proves the join sink itself rejects
// any resolved path that escapes <project>/pkg/cmd, regardless of entry point.
func TestGetCommandPath_ContainedUnderPkgCmd(t *testing.T) {
	t.Parallel()

	manifest := "properties:\n  name: mytool\nversion:\n  gtb: v1.0.0\n"
	g, _, _ := newPerimeterTestProject(t, manifest)

	for _, name := range []string{"../../evil", "..", "."} {
		g.config.Name = name

		_, err := g.getCommandPath()
		require.Error(t, err, "name %q must not resolve to a command path", name)
	}

	g.config.Name = "fine"
	p, err := g.getCommandPath()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/work", "pkg", "cmd", "fine"), p)
}

// TestAIDocTools_ContainedToProjectRoot proves the AI doc tools cannot read
// or list outside the project root via model-supplied relative paths.
func TestAIDocTools_ContainedToProjectRoot(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/etc/passwd", []byte("root:x:0:0"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/work/inside.txt", []byte("inside"), 0o644))

	p := &props.Props{FS: fs, Logger: logger.NewNoop()}
	g := New(p, &Config{Path: "/work"})

	ctx := context.Background()

	t.Run("read_file traversal rejected", func(t *testing.T) {
		t.Parallel()

		_, err := g.handleReadFileTool(ctx, json.RawMessage(`{"path":"../../../../etc/passwd"}`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "escapes")
	})

	t.Run("read_file inside project allowed", func(t *testing.T) {
		t.Parallel()

		out, err := g.handleReadFileTool(ctx, json.RawMessage(`{"path":"inside.txt"}`))
		require.NoError(t, err)
		assert.Equal(t, "inside", out)
	})

	t.Run("list_dir traversal rejected", func(t *testing.T) {
		t.Parallel()

		_, err := g.handleListDirTool(ctx, json.RawMessage(`{"path":"../../../../etc"}`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "escapes")
	})

	t.Run("list_dir inside project allowed", func(t *testing.T) {
		t.Parallel()

		out, err := g.handleListDirTool(ctx, json.RawMessage(`{"path":"."}`))
		require.NoError(t, err)
		assert.Contains(t, out, "inside.txt")
	})
}

// TestVerifyProject_SentinelIsMatchable proves ErrNotGoToolBaseProject is a
// placeholder-free sentinel that survives errors.Is through verifyProject,
// with the offending path attached by the call site.
func TestVerifyProject_SentinelIsMatchable(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop()}
	g := New(p, &Config{Path: "/not-a-project"})

	err := g.verifyProject()
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNotGoToolBaseProject)
	assert.NotContains(t, err.Error(), "%s", "sentinel must not render a literal format verb")
	assert.Contains(t, err.Error(), "/not-a-project")
}
