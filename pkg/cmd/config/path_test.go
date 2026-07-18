package config_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gtbconfig "gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func runPath(t *testing.T, p *props.Props, args ...string) string {
	t.Helper()
	cmd := config.NewCmdPath(p)
	cmd.Flags().String("output", "text", "")
	if len(args) > 0 {
		require.NoError(t, cmd.Flags().Set("output", args[0]))
		args = args[1:]
	}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs(args)
	require.NoError(t, cmd.Execute())

	return buf.String()
}

func TestCmdPath_SingleFile(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	path := "/etc/tool/config.yaml"
	require.NoError(t, afero.WriteFile(fs, path, []byte("log:\n  level: info\n"), 0o600))
	c, err := gtbconfig.LoadFilesContainer(fs, gtbconfig.WithLogger(logger.ToSlog(logger.NewNoop())),
		gtbconfig.WithConfigFiles(path))
	require.NoError(t, err)
	p := &props.Props{Config: c, FS: fs, Tool: props.Tool{Name: "tool"}}

	out := runPath(t, p, "text")
	assert.Contains(t, out, path)
	assert.Contains(t, out, "contributing")
	assert.Contains(t, out, "writable")
}

func TestCmdPath_MultiFileMergeOrder(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	first, second := "/etc/tool/config.yaml", "/home/u/.tool/config.yaml"
	require.NoError(t, afero.WriteFile(fs, first, []byte("log:\n  level: info\n"), 0o600))
	require.NoError(t, afero.WriteFile(fs, second, []byte("log:\n  level: debug\n"), 0o600))
	c, err := gtbconfig.LoadFilesContainer(fs, gtbconfig.WithLogger(logger.ToSlog(logger.NewNoop())),
		gtbconfig.WithConfigFiles(first, second))
	require.NoError(t, err)
	p := &props.Props{Config: c, FS: fs, Tool: props.Tool{Name: "tool"}}

	out := runPath(t, p, "text")
	// Merge order preserved: first appears before second.
	assert.Less(t, strings.Index(out, first), strings.Index(out, second))
}

func TestCmdPath_NoFile(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	c := gtbconfig.NewReaderContainer(fs, gtbconfig.WithLogger(logger.ToSlog(logger.NewNoop())),
		gtbconfig.WithConfigFormat("yaml"), gtbconfig.WithConfigReaders(strings.NewReader("log:\n  level: info\n")))
	p := &props.Props{Config: c, FS: fs, Tool: props.Tool{Name: "tool"}}

	out := runPath(t, p, "text")
	want := filepath.Join(setup.GetDefaultConfigDir(fs, "tool"), setup.DefaultConfigFilename)
	assert.Contains(t, out, want)
	assert.Contains(t, out, "no config file is currently loaded")
}

func TestCmdPath_Writable(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	path := "/etc/tool/config.yaml"
	require.NoError(t, afero.WriteFile(fs, path, []byte("log:\n  level: info\n"), 0o600))
	c, err := gtbconfig.LoadFilesContainer(fs, gtbconfig.WithLogger(logger.ToSlog(logger.NewNoop())),
		gtbconfig.WithConfigFiles(path))
	require.NoError(t, err)
	p := &props.Props{Config: c, FS: fs, Tool: props.Tool{Name: "tool"}}

	out := runPath(t, p, "text", "--writable")
	assert.Equal(t, path+"\n", out)
	assert.NotContains(t, out, "contributing")
}

func TestCmdPath_JSONOutput(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	path := "/etc/tool/config.yaml"
	require.NoError(t, afero.WriteFile(fs, path, []byte("log:\n  level: info\n"), 0o600))
	c, err := gtbconfig.LoadFilesContainer(fs, gtbconfig.WithLogger(logger.ToSlog(logger.NewNoop())),
		gtbconfig.WithConfigFiles(path))
	require.NoError(t, err)
	p := &props.Props{Config: c, FS: fs, Tool: props.Tool{Name: "tool"}}

	out := runPath(t, p, "json")
	assert.Contains(t, out, `"path"`)
	assert.Contains(t, out, pathRoleWritableExpected)
}

func TestCmdPath_YAMLOutput(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	path := "/etc/tool/config.yaml"
	require.NoError(t, afero.WriteFile(fs, path, []byte("log:\n  level: info\n"), 0o600))
	c, err := gtbconfig.LoadFilesContainer(fs, gtbconfig.WithLogger(logger.ToSlog(logger.NewNoop())),
		gtbconfig.WithConfigFiles(path))
	require.NoError(t, err)
	p := &props.Props{Config: c, FS: fs, Tool: props.Tool{Name: "tool"}}

	out := runPath(t, p, "yaml")
	assert.Contains(t, out, "path:")
	assert.Contains(t, out, "role:")
}

func TestCmdPath_WritableJSON(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	path := "/etc/tool/config.yaml"
	require.NoError(t, afero.WriteFile(fs, path, []byte("log:\n  level: info\n"), 0o600))
	c, err := gtbconfig.LoadFilesContainer(fs, gtbconfig.WithLogger(logger.ToSlog(logger.NewNoop())),
		gtbconfig.WithConfigFiles(path))
	require.NoError(t, err)
	p := &props.Props{Config: c, FS: fs, Tool: props.Tool{Name: "tool"}}

	out := runPath(t, p, "json", "--writable")
	assert.Contains(t, out, `"writable"`)
	assert.Contains(t, out, path)
}

func TestCmdPath_NilConfig(t *testing.T) {
	t.Parallel()

	cmd := config.NewCmdPath(&props.Props{Config: nil})
	assert.Error(t, cmd.Execute())
}

// pathRoleWritableExpected is the literal "writable" role string asserted in
// JSON output (the constant in path.go is package-private).
const pathRoleWritableExpected = "writable"
