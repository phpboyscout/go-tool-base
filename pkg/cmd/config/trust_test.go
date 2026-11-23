package config_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	config "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func trustTool(style props.ConfigDir) props.Tool {
	return props.Tool{Name: "mytool", Config: props.ConfigSpec{Dir: style}}
}

func trustProps(t *testing.T, style props.ConfigDir) *props.Props {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	return &props.Props{
		Tool:   trustTool(style),
		Logger: logger.NewNoop(),
		FS:     afero.NewOsFs(),
	}
}

// TestCmdTrust_TrustAndList exercises trusting an explicit file then listing it.
func TestCmdTrust_TrustAndList(t *testing.T) {
	testutil.EachConfigDir(t, testCmdTrust_TrustAndList)
}

func testCmdTrust_TrustAndList(t *testing.T, style props.ConfigDir) {
	p := trustProps(t, style)

	dir := t.TempDir()
	path := filepath.Join(dir, ".mytool.yaml")
	require.NoError(t, afero.WriteFile(p.FS, path, []byte("log:\n  level: info\n"), 0o600))

	// trust <path>
	cmd := config.NewCmdTrust(p)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{path})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "trusted")

	trusted, err := setup.IsProjectConfigTrusted(p.FS, trustTool(style), path)
	require.NoError(t, err)
	assert.True(t, trusted)

	// trust --list
	listCmd := config.NewCmdTrust(p)
	var listOut bytes.Buffer
	listCmd.SetOut(&listOut)
	listCmd.SetArgs([]string{"--list"})
	require.NoError(t, listCmd.Execute())
	abs, _ := filepath.Abs(path)
	assert.Contains(t, listOut.String(), abs)
}

// TestCmdTrust_Forget revokes trust for a file.
func TestCmdTrust_Forget(t *testing.T) {
	testutil.EachConfigDir(t, testCmdTrust_Forget)
}

func testCmdTrust_Forget(t *testing.T, style props.ConfigDir) {
	p := trustProps(t, style)

	dir := t.TempDir()
	path := filepath.Join(dir, ".mytool.yaml")
	require.NoError(t, afero.WriteFile(p.FS, path, []byte("log:\n  level: info\n"), 0o600))
	require.NoError(t, setup.TrustProjectConfig(p.FS, trustTool(style), path))

	cmd := config.NewCmdTrust(p)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--forget", path})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "revoked")

	trusted, err := setup.IsProjectConfigTrusted(p.FS, trustTool(style), path)
	require.NoError(t, err)
	assert.False(t, trusted)
}

// TestCmdTrust_NoProjectFile errors helpfully when nothing is discoverable.
func TestCmdTrust_NoProjectFile(t *testing.T) {
	p := trustProps(t, props.ConfigDirXDG)
	// cwd is the repo, which has no .mytool.yaml — but pass an explicit
	// nonexistent path to keep the test independent of the working directory.
	cmd := config.NewCmdTrust(p)
	cmd.SetArgs([]string{filepath.Join(t.TempDir(), ".mytool.yaml")})

	// Trusting a missing file surfaces the read error.
	require.Error(t, cmd.Execute())
}
