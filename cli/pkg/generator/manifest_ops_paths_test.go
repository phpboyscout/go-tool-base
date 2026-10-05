package generator

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyTree(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/src/a.txt", []byte("a"), DefaultFileMode))
	require.NoError(t, afero.WriteFile(fs, "/src/nested/b.txt", []byte("b"), DefaultFileMode))
	require.NoError(t, fs.MkdirAll("/src/empty", DefaultDirMode))

	require.NoError(t, copyTree(fs, "/src", "/dst"))

	for path, want := range map[string]string{"/dst/a.txt": "a", "/dst/nested/b.txt": "b"} {
		got, err := afero.ReadFile(fs, path)
		require.NoError(t, err)
		assert.Equal(t, want, string(got))
	}

	isDir, err := afero.IsDir(fs, "/dst/empty")
	require.NoError(t, err)
	assert.True(t, isDir)

	require.Error(t, copyTree(fs, "/absent", "/dst2"))
}

func TestCopyTree_RefusedWriteFails(t *testing.T) {
	t.Parallel()

	mem := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(mem, "/src/nested/b.txt", []byte("b"), DefaultFileMode))

	require.Error(t, copyTree(faultFs{Fs: mem, refuse: "/dst/nested"}, "/src", "/dst"))
}

func TestRuleState_String(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "sealed", StateSealed.String())
	assert.Equal(t, "ignored", StateIgnored.String())
	assert.Equal(t, "managed", StateManaged.String())
	assert.Equal(t, "managed", RuleState(99).String())
}

func TestSealedTrackedFiles(t *testing.T) {
	t.Parallel()

	g, fs := newPureGenerator(t, &Config{Path: "/work"})

	_, err := g.SealedTrackedFiles()
	require.Error(t, err, "no manifest")

	require.NoError(t, afero.WriteFile(fs, "/work/.gtb/manifest.yaml", []byte(
		"properties:\n  name: tool\nhashes:\n  README.md: abc\n  zensical.toml: def\n"+
			"commands:\n  - name: widget\n    hashes:\n      cmd.go: 123\n      main.go: 456\n"), DefaultFileMode))
	require.NoError(t, afero.WriteFile(fs, "/work/.gtb/ignore", []byte(
		"pkg/cmd/widget/cmd.go sealed\nREADME.md sealed\nzensical.toml\n"), DefaultFileMode))

	got, err := g.SealedTrackedFiles()
	require.NoError(t, err)
	assert.Equal(t, []string{"README.md", "pkg/cmd/widget/cmd.go"}, got)
}

func TestTemplateSourceOps_RejectUnknownSources(t *testing.T) {
	t.Parallel()

	g, fs := newPureGenerator(t, &Config{Path: "/work"})

	_, err := g.ListTemplateSources()
	require.Error(t, err, "no manifest")
	require.Error(t, g.UpdateTemplateSource(context.Background(), "house"))
	require.Error(t, g.RemoveTemplateSource(context.Background(), "house"))
	require.Error(t, g.AddTemplateSource(context.Background(), TemplateSource{Location: "./overlay"}))

	require.NoError(t, afero.WriteFile(fs, "/work/.gtb/manifest.yaml", []byte("properties:\n  name: tool\n"), DefaultFileMode))

	err = g.UpdateTemplateSource(context.Background(), "house")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no template source named "house"`)

	err = g.RemoveTemplateSource(context.Background(), "house")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no template source named "house"`)
}

func TestExternalCommandOps_RequireAProject(t *testing.T) {
	t.Parallel()

	g, _ := newPureGenerator(t, &Config{Path: "/nowhere"})
	ctx := context.Background()

	require.ErrorIs(t, g.AttachExternalCommand(ctx, sigillumSpec()), ErrNotGoToolBaseProject)
	require.ErrorIs(t, g.AttachExternalAdapter(ctx), ErrNotGoToolBaseProject)
	require.ErrorIs(t, g.DetachExternalCommand(ctx, "gitlab.com/x"), ErrNotGoToolBaseProject)

	_, _, err := g.ListExternalCommands()
	require.Error(t, err)
}

func TestScaffoldExternalAdapter_Failures(t *testing.T) {
	t.Parallel()

	adapter := filepath.Join("/work", externalAdapterRelPath)

	for want, refuse := range map[string]string{
		"failed to create external adapter directory": filepath.Dir(adapter),
		"failed to write external adapter":            adapter,
	} {
		t.Run(want, func(t *testing.T) {
			t.Parallel()

			err := newFaultGenerator(faultFs{Fs: afero.NewMemMapFs(), refuse: refuse}).scaffoldExternalAdapter()
			require.Error(t, err)
			assert.Contains(t, err.Error(), want)
		})
	}
}
