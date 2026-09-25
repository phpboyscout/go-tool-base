package setup_test

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Spec 0204 D14: a tool whose own format changed finds its config in the old
// format and none in the new. The old file is stranded; anything else is not.
func TestStrandedConfig(t *testing.T) {
	t.Parallel()

	toml := props.Tool{Name: "mytool", Config: props.ConfigSpec{Format: "toml"}}
	yaml := props.Tool{Name: "mytool"}

	tests := []struct {
		name     string
		tool     props.Tool
		files    []string
		wantOld  string
		wantWant string
	}{
		{name: "old format only", tool: toml, files: []string{"/cfg/config.yaml"}, wantOld: "/cfg/config.yaml", wantWant: "/cfg/config.toml"},
		{name: "changed back to yaml", tool: yaml, files: []string{"/cfg/config.json"}, wantOld: "/cfg/config.json", wantWant: "/cfg/config.yaml"},
		{name: "already converted", tool: toml, files: []string{"/cfg/config.yaml", "/cfg/config.toml"}},
		{name: "own format only", tool: toml, files: []string{"/cfg/config.toml"}},
		{name: "nothing yet", tool: toml},
		{name: "a read-only format is never an own file", tool: toml, files: []string{"/cfg/config.ini"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			for _, f := range tc.files {
				require.NoError(t, afero.WriteFile(fs, f, []byte("a: 1\n"), 0o600))
			}

			old, want := setup.StrandedConfig(fs, tc.tool, []string{"/cfg/" + tc.tool.ConfigFilename()})
			assert.Equal(t, tc.wantOld, old)
			assert.Equal(t, tc.wantWant, want)
		})
	}
}

// A path that is not the tool's own filename, such as an explicit --config,
// is never checked: the user named that file.
func TestStrandedConfig_OnlyTheOwnFilename(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/work/config.yaml", []byte("a: 1\n"), 0o600))

	old, _ := setup.StrandedConfig(fs, props.Tool{Name: "mytool", Config: props.ConfigSpec{Format: "toml"}},
		[]string{"/work/settings.toml"})
	assert.Empty(t, old)
}

func TestDefaultConfigPaths(t *testing.T) {
	t.Parallel()

	p := &props.Props{Tool: props.Tool{Name: "mytool", Config: props.ConfigSpec{Format: "toml"}}, FS: afero.NewMemMapFs()}

	paths := setup.DefaultConfigPaths(p)
	require.Len(t, paths, 2)
	assert.Equal(t, filepath.Join(string(filepath.Separator), "etc", "mytool", "config.toml"), paths[0])
	assert.Equal(t, filepath.Join(setup.GetDefaultConfigDir(p.FS, "mytool"), "config.toml"), paths[1])
}

// The refusal names both files, and the way out: config convert when the tool
// has the config command, by hand when it does not.
func TestConfigFormatChangedError(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		states  []features.State
		mention string
	}{
		{name: "with the config command", states: []features.State{{ID: props.ConfigCmd, Enabled: true}}, mention: "mytool config convert --from /cfg/config.yaml --to /cfg/config.toml"},
		{name: "without it", mention: "by hand"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			set, err := features.Resolve(features.Default().Snapshot(), tc.states)
			require.NoError(t, err)

			p := &props.Props{Tool: props.Tool{Name: "mytool"}, Logger: logger.NewNoop(), Features: set}

			err = setup.ConfigFormatChangedError(p, "/cfg/config.yaml", "/cfg/config.toml")
			require.ErrorIs(t, err, setup.ErrConfigFormatChanged)
			assert.Contains(t, err.Error(), "/cfg/config.yaml")
			assert.Contains(t, err.Error(), "/cfg/config.toml")
			assert.Contains(t, errors.FlattenHints(err), tc.mention)
		})
	}
}
