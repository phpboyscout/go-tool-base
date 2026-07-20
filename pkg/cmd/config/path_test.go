package config_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cfg "gitlab.com/phpboyscout/go/config"
	configafero "gitlab.com/phpboyscout/go/config-afero"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/config"
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

// singleFileProps returns Props whose store loaded exactly one config file.
func singleFileProps(t *testing.T) (*props.Props, string) {
	t.Helper()

	fs := afero.NewMemMapFs()
	path := "/etc/tool/config.yaml"
	require.NoError(t, afero.WriteFile(fs, path, []byte("log:\n  level: info\n"), 0o600))

	store, err := cfg.NewStore(t.Context(), cfg.WithFiles(configafero.Wrap(fs), path))
	require.NoError(t, err)

	return &props.Props{Config: store, FS: fs, Tool: props.Tool{Name: "tool"}}, path
}

func TestCmdPath_SingleFile(t *testing.T) {
	t.Parallel()

	p, path := singleFileProps(t)

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

	store, err := cfg.NewStore(t.Context(), cfg.WithFiles(configafero.Wrap(fs), first, second))
	require.NoError(t, err)
	p := &props.Props{Config: store, FS: fs, Tool: props.Tool{Name: "tool"}}

	out := runPath(t, p, "text")
	// Merge order preserved: first appears before second.
	assert.Less(t, strings.Index(out, first), strings.Index(out, second))
}

func TestCmdPath_NoFile(t *testing.T) {
	// Not parallel: pins HOME so the default writable path is deterministic
	// (and any accidental write would land on the memmap FS, not a real home).
	t.Setenv("HOME", "/home/pathtest")

	fs := afero.NewMemMapFs()
	p := &props.Props{
		Config: testutil.StoreFromYAML(t, "log:\n  level: info\n"),
		FS:     fs,
		Tool:   props.Tool{Name: "tool"},
	}

	out := runPath(t, p, "text")
	want := filepath.Join(setup.GetDefaultConfigDir(fs, "tool"), setup.DefaultConfigFilename)
	assert.Contains(t, out, want)
	assert.Contains(t, out, "no config file is currently loaded")
}

func TestCmdPath_Writable(t *testing.T) {
	t.Parallel()

	p, path := singleFileProps(t)

	out := runPath(t, p, "text", "--writable")
	assert.Equal(t, path+"\n", out)
	assert.NotContains(t, out, "contributing")
}

func TestCmdPath_JSONOutput(t *testing.T) {
	t.Parallel()

	p, _ := singleFileProps(t)

	out := runPath(t, p, "json")
	assert.Contains(t, out, `"path"`)
	assert.Contains(t, out, pathRoleWritableExpected)
}

func TestCmdPath_YAMLOutput(t *testing.T) {
	t.Parallel()

	p, _ := singleFileProps(t)

	out := runPath(t, p, "yaml")
	assert.Contains(t, out, "path:")
	assert.Contains(t, out, "role:")
}

func TestCmdPath_WritableJSON(t *testing.T) {
	t.Parallel()

	p, path := singleFileProps(t)

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
