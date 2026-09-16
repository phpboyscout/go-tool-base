package settings_test

import (
	"bytes"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/cmd/settings"
	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

const manifest = "properties:\n  name: mytool\n  chat:\n    providers: [claude]\n    default:\n      provider: claude\n" +
	"release_source:\n  type: github\n  backend: github\n  host: github.com\n  owner: org\n  repo: mytool\n" +
	"version:\n  gtb: v1.0.0\n  go: \"1.26\"\ncommands: []\n"

func project(t *testing.T) (*props.Props, afero.Fs) {
	t.Helper()

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/work/.gtb", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/work/.gtb/manifest.yaml", []byte(manifest), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/work/go.mod", []byte("module test-mod\n"), 0o644))
	require.NoError(t, fs.MkdirAll("/work/pkg/cmd/root", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/root/cmd.go", []byte("package root\n"), 0o644))

	return &props.Props{FS: fs, Logger: logger.NewNoop(), Config: testutil.StoreFromYAML(t, ""), Version: version.NewInfo("v1.0.0", "", "")}, fs
}

// TestSetGetUnset drives the three commands end to end on an in-memory
// project (spec 0197 D6).
func TestSetGetUnset(t *testing.T) {
	t.Parallel()

	p, _ := project(t)

	set := settings.NewCmdSet(p)
	set.SetArgs([]string{"-p", "/work", "chat.default.model", "claude-opus-5"})
	require.NoError(t, set.Execute())

	var out bytes.Buffer
	get := settings.NewCmdGet(p)
	get.SetOut(&out)
	get.SetArgs([]string{"-p", "/work", "chat.default.model"})
	require.NoError(t, get.Execute())
	assert.Equal(t, "claude-opus-5\n", out.String())

	unset := settings.NewCmdUnset(p)
	unset.SetArgs([]string{"-p", "/work", "chat.default.model"})
	require.NoError(t, unset.Execute())

	out.Reset()
	get = settings.NewCmdGet(p)
	get.SetOut(&out)
	get.SetArgs([]string{"-p", "/work", "chat.default.model"})
	require.NoError(t, get.Execute())
	assert.Empty(t, out.String())

	set = settings.NewCmdSet(p)
	set.SetArgs([]string{"-p", "/work", "nonsense"})
	require.Error(t, set.Execute(), "a path needs a value")
}
