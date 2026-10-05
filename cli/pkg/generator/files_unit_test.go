package generator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// faultFs refuses every mutating operation on one path, standing in for a
// permission failure or a full disk at exactly that file.
type faultFs struct {
	afero.Fs

	refuse     string
	refuseStat string
}

func (f faultFs) Create(name string) (afero.File, error) {
	if name == f.refuse {
		return nil, assert.AnError
	}

	return f.Fs.Create(name)
}

func (f faultFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if name == f.refuse && flag&(os.O_WRONLY|os.O_RDWR) != 0 {
		return nil, assert.AnError
	}

	return f.Fs.OpenFile(name, flag, perm)
}

func (f faultFs) Remove(name string) error {
	if name == f.refuse {
		return assert.AnError
	}

	return f.Fs.Remove(name)
}

func (f faultFs) MkdirAll(name string, perm os.FileMode) error {
	if name == f.refuse {
		return assert.AnError
	}

	return f.Fs.MkdirAll(name, perm)
}

func (f faultFs) Stat(name string) (os.FileInfo, error) {
	if name == f.refuseStat {
		return nil, assert.AnError
	}

	return f.Fs.Stat(name)
}

const filesRoot = "/work"

func filesCmdDir() string {
	return filepath.Join(filesRoot, "pkg", "cmd", "widget")
}

func newFilesGenerator(t *testing.T, fs afero.Fs, cfg *Config) *Generator {
	t.Helper()

	require.NoError(t, fs.MkdirAll(filesCmdDir(), DefaultDirMode))

	if cfg == nil {
		cfg = &Config{}
	}

	cfg.Path = filesRoot
	cfg.Name = "widget"

	if cfg.Overwrite == "" {
		cfg.Overwrite = OverwriteAllow
	}

	return New(&props.Props{FS: fs, Logger: logger.NewNoop()}, cfg)
}

func widgetData() *templates.CommandData {
	return &templates.CommandData{
		Package:    "widget",
		Name:       "widget",
		PascalName: "Widget",
	}
}

func TestGenerateCommandFile_WritesEveryOptionalFile(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := newFilesGenerator(t, fs, nil)

	data := widgetData()
	data.WithInitializer = true
	data.WithConfigValidation = true
	data.TestCode = "package widget_test\n"

	require.NoError(t, g.GenerateCommandFile(context.Background(), filesCmdDir(), data))

	for _, name := range []string{"cmd.go", "main.go", "init.go", "config.go", "main_test.go"} {
		exists, err := afero.Exists(fs, filepath.Join(filesCmdDir(), name))
		require.NoError(t, err)
		assert.True(t, exists, name)
	}

	assert.NotEmpty(t, data.Hashes["cmd.go"])
	assert.NotEmpty(t, data.Hashes["init.go"])
	assert.Equal(t, calculateHash([]byte(data.TestCode)), data.Hashes["main_test.go"])
}

func TestGenerateCommandFile_DroppingTheInitializerRemovesIt(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := newFilesGenerator(t, fs, nil)

	initFile := filepath.Join(filesCmdDir(), "init.go")
	configFile := filepath.Join(filesCmdDir(), "config.go")
	require.NoError(t, afero.WriteFile(fs, initFile, []byte("package widget\n"), DefaultFileMode))
	require.NoError(t, afero.WriteFile(fs, configFile, []byte("package widget\n"), DefaultFileMode))

	data := widgetData()
	require.NoError(t, g.GenerateCommandFile(context.Background(), filesCmdDir(), data))

	exists, err := afero.Exists(fs, initFile)
	require.NoError(t, err)
	assert.False(t, exists, "init.go is removed once the initializer is off")

	kept, err := afero.ReadFile(fs, configFile)
	require.NoError(t, err)
	assert.Equal(t, "package widget\n", string(kept), "a disabled config.go is warned about, never deleted")
	assert.NotContains(t, data.Hashes, "init.go")
}

func TestHandleConfigValidationFile_PreservesAnExistingFile(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := newFilesGenerator(t, fs, nil)

	configFile := filepath.Join(filesCmdDir(), "config.go")
	require.NoError(t, afero.WriteFile(fs, configFile, []byte("// customised\n"), DefaultFileMode))

	data := widgetData()
	data.WithConfigValidation = true

	require.NoError(t, g.handleConfigValidationFile(context.Background(), filesCmdDir(), data))

	got, err := afero.ReadFile(fs, configFile)
	require.NoError(t, err)
	assert.Equal(t, "// customised\n", string(got))
}

func TestGenerateCommandFile_ReportsEachWriteFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		file    string
		prepare func(*templates.CommandData)
		want    string
	}{
		{file: "cmd.go", want: "failed to create registration file"},
		{file: "main.go", want: "failed to create execution file"},
		{
			file:    "init.go",
			prepare: func(d *templates.CommandData) { d.WithInitializer = true },
			want:    "failed to create initializer file",
		},
		{
			file:    "config.go",
			prepare: func(d *templates.CommandData) { d.WithConfigValidation = true },
			want:    "failed to create config validation file",
		},
		{
			file:    "main_test.go",
			prepare: func(d *templates.CommandData) { d.TestCode = "package widget_test\n" },
			want:    "failed to create test file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			t.Parallel()

			fs := faultFs{Fs: afero.NewMemMapFs(), refuse: filepath.Join(filesCmdDir(), tt.file)}
			g := newFilesGenerator(t, fs, nil)

			data := widgetData()
			if tt.prepare != nil {
				tt.prepare(data)
			}

			err := g.GenerateCommandFile(context.Background(), filesCmdDir(), data)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestHandleInitializerFile_ReportsARefusedRemoval(t *testing.T) {
	t.Parallel()

	initFile := filepath.Join(filesCmdDir(), "init.go")
	mem := afero.NewMemMapFs()
	require.NoError(t, mem.MkdirAll(filesCmdDir(), DefaultDirMode))
	require.NoError(t, afero.WriteFile(mem, initFile, []byte("package widget\n"), DefaultFileMode))

	g := newFilesGenerator(t, faultFs{Fs: mem, refuse: initFile}, nil)

	data := widgetData()
	data.Hashes = map[string]string{"init.go": "stale"}

	err := g.handleInitializerFile(filesCmdDir(), data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to remove initializer file")
}

func TestGenerateTestFile_NoTestCodeWritesNothing(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := newFilesGenerator(t, fs, nil)

	hash, err := g.generateTestFile(context.Background(), filesCmdDir(), *widgetData())
	require.NoError(t, err)
	assert.Empty(t, hash)

	exists, err := afero.Exists(fs, filepath.Join(filesCmdDir(), "main_test.go"))
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestGeneratedFiles_IgnoredPathsAreLeftAlone(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := newFilesGenerator(t, fs, nil)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(filesRoot, ".gtb", "ignore"),
		[]byte("pkg/cmd/widget/init.go\npkg/cmd/widget/main_test.go\n"), DefaultFileMode))

	for _, name := range []string{"init.go", "main_test.go"} {
		require.NoError(t, afero.WriteFile(fs, filepath.Join(filesCmdDir(), name), []byte("// hand edited\n"), DefaultFileMode))
	}

	data := widgetData()
	data.TestCode = "package widget_test\n"

	_, err := g.generateInitializerFile(filesCmdDir(), *data)
	require.NoError(t, err)

	_, err = g.generateTestFile(context.Background(), filesCmdDir(), *data)
	require.NoError(t, err)

	for _, name := range []string{"init.go", "main_test.go"} {
		got, err := afero.ReadFile(fs, filepath.Join(filesCmdDir(), name))
		require.NoError(t, err)
		assert.Equal(t, "// hand edited\n", string(got), name)
	}
}

func TestSeedAssetFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(filesCmdDir(), "assets", "config.yaml")

	t.Run("existing file is left alone", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		g := newFilesGenerator(t, fs, nil)
		require.NoError(t, fs.MkdirAll(filepath.Dir(path), DefaultDirMode))
		require.NoError(t, afero.WriteFile(fs, path, []byte("mine: true\n"), DefaultFileMode))

		require.NoError(t, g.seedAssetFile(path, "theirs: true\n"))

		got, err := afero.ReadFile(fs, path)
		require.NoError(t, err)
		assert.Equal(t, "mine: true\n", string(got))
	})

	tests := []struct {
		name string
		fs   faultFs
		want string
	}{
		{name: "directory refused", fs: faultFs{refuse: filepath.Dir(path)}, want: "failed to create asset directory"},
		{name: "create refused", fs: faultFs{refuse: path}, want: "failed to create config file"},
		{name: "stat refused", fs: faultFs{refuseStat: path}, want: "failed to check for config file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fs := tt.fs
			fs.Fs = afero.NewMemMapFs()
			g := newFilesGenerator(t, fs, nil)

			err := g.seedAssetFile(path, "x: 1\n")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestGenerateAssetFiles_SurfacesASeedFailure(t *testing.T) {
	t.Parallel()

	refuse := filepath.Join(filesCmdDir(), "assets", "init")
	g := newFilesGenerator(t, faultFs{Fs: afero.NewMemMapFs(), refuse: refuse}, nil)

	err := g.generateAssetFiles(filesCmdDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create asset directory")
}

func TestWriteVerb(t *testing.T) {
	t.Parallel()

	g := newFilesGenerator(t, afero.NewMemMapFs(), &Config{DryRun: true})
	assert.Equal(t, "Would write", g.writeVerb())

	g.config.DryRun = false
	assert.Equal(t, "Writing", g.writeVerb())
}
