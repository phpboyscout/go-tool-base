package generator

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dave/jennifer/jen"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func newFaultGenerator(fs afero.Fs) *Generator {
	return New(&props.Props{FS: fs, Logger: logger.NewNoop()}, &Config{Path: "/work"})
}

func TestRenderGoFiles(t *testing.T) {
	t.Parallel()

	valid := func() map[string]*jen.File {
		f := jen.NewFile("main")
		f.Func().Id("main").Params().Block()

		return map[string]*jen.File{"cmd/tool/main.go": f}
	}

	t.Run("writes the file", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		require.NoError(t, newFaultGenerator(fs).renderGoFiles("/work", valid()))

		got, err := afero.ReadFile(fs, "/work/cmd/tool/main.go")
		require.NoError(t, err)
		assert.Contains(t, string(got), "func main()")
	})

	t.Run("render failure", func(t *testing.T) {
		t.Parallel()

		f := jen.NewFile("main")
		f.Id("not valid go (")

		err := newFaultGenerator(afero.NewMemMapFs()).renderGoFiles("/work", map[string]*jen.File{"bad.go": f})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to render jennifer file")
	})

	for name, refuse := range map[string]string{
		"failed to create directory": "/work/cmd/tool",
		"failed to create file":      "/work/cmd/tool/main.go",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := newFaultGenerator(faultFs{Fs: afero.NewMemMapFs(), refuse: refuse}).renderGoFiles("/work", valid())
			require.Error(t, err)
			assert.Contains(t, err.Error(), name)
		})
	}
}

func TestWriteGitkeep(t *testing.T) {
	t.Parallel()

	dir := "/work/internal/trustkeys/keys"
	gitkeep := filepath.Join(dir, ".gitkeep")

	t.Run("existing file is kept", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll(dir, DefaultDirMode))
		require.NoError(t, afero.WriteFile(fs, gitkeep, []byte("note"), DefaultFileMode))

		require.NoError(t, newFaultGenerator(fs).writeGitkeep(dir))

		got, err := afero.ReadFile(fs, gitkeep)
		require.NoError(t, err)
		assert.Equal(t, "note", string(got))
	})

	for name, refuse := range map[string]string{
		"failed to create directory": dir,
		"failed to write":            gitkeep,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := newFaultGenerator(faultFs{Fs: afero.NewMemMapFs(), refuse: refuse}).writeGitkeep(dir)
			require.Error(t, err)
			assert.Contains(t, err.Error(), name)
		})
	}
}

func TestRunSkeletonCommand_UnresolvableBinaryFails(t *testing.T) {
	t.Parallel()

	g := newFaultGenerator(afero.NewMemMapFs())

	require.Error(t, g.runSkeletonCommand(context.Background(), t.TempDir(), "gtb-generator-test-no-such-binary"))
}
