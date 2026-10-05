package generator

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func TestDryRunResult_PrintActions(t *testing.T) {
	t.Parallel()

	t.Run("actions alone", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		(&DryRunResult{Actions: []string{"git init"}}).Print(&buf)

		assert.Equal(t, "Post-generation actions:\n  * git init\n", buf.String())
	})

	t.Run("actions after files", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		(&DryRunResult{
			Created: []FilePreview{{Path: "a.go"}},
			Actions: []string{"git commit"},
		}).Print(&buf)

		assert.Contains(t, buf.String(), "  + a.go\n\nPost-generation actions:\n  * git commit\n")
	})
}

func TestProduceDryRunResult_RejectsABadPathCount(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	_, err := produceDryRunResult(fs, fs)
	require.Error(t, err)

	_, err = produceDryRunResult(fs, fs, "/a", "/b", "/c")
	require.Error(t, err)
}

func TestProduceDryRunResult_MissingOverlayRootFails(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	_, err := produceDryRunResult(fs, fs, "/absent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to walk overlay filesystem")
}

// TestWithDryRunOverlay_MaterialisesAnOSProject drives the on-disk branch: the
// overlay is copied to a temp tree and diffed against the real project. The
// only post-processing step is empty, so nothing is executed.
func TestWithDryRunOverlay_MaterialisesAnOSProject(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(project, "kept.txt"), []byte("same\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(project, "changed.txt"), []byte("old\n"), 0o600))

	g := New(&props.Props{FS: afero.NewOsFs(), Logger: logger.NewNoop()}, &Config{Path: project})

	result, err := g.withDryRunOverlay(context.Background(), project, func() error {
		if err := afero.WriteFile(g.props.FS, filepath.Join(project, "changed.txt"), []byte("new\n"), DefaultFileMode); err != nil {
			return err
		}

		if err := g.props.FS.MkdirAll(filepath.Join(project, "sub"), DefaultDirMode); err != nil {
			return err
		}

		return afero.WriteFile(g.props.FS, filepath.Join(project, "sub", "added.txt"), []byte("added\n"), DefaultFileMode)
	}, &dryRunPostProcess{commands: [][]string{{}}})
	require.NoError(t, err)

	require.Len(t, result.Modified, 1)
	assert.Equal(t, "changed.txt", result.Modified[0].Path)
	assert.Contains(t, result.Modified[0].Diff, "+new")

	require.Len(t, result.Created, 1)
	assert.Equal(t, filepath.Join("sub", "added.txt"), result.Created[0].Path)

	got, err := os.ReadFile(filepath.Join(project, "changed.txt"))
	require.NoError(t, err)
	assert.Equal(t, "old\n", string(got), "the real project is untouched")
}

func TestWithDryRunOverlay_PropagatesTheGenerationError(t *testing.T) {
	t.Parallel()

	base := afero.NewMemMapFs()
	g := New(&props.Props{FS: base, Logger: logger.NewNoop()}, &Config{Path: "/work"})

	_, err := g.withDryRunOverlay(context.Background(), "/work", func() error { return assert.AnError }, nil)
	require.ErrorIs(t, err, assert.AnError)
	assert.Same(t, base, g.props.FS, "the base filesystem is restored")
}

func TestMaterialiseOverlay_MissingRootFails(t *testing.T) {
	t.Parallel()

	g := New(&props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop()}, &Config{Path: "/work"})

	require.Error(t, g.materialiseOverlay(afero.NewMemMapFs(), "/absent", t.TempDir()))
}
