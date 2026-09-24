package root

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Spec 0204 D1: the declared layer list is the precedence order. The user's
// config file and a project file both set one key; which one wins is decided
// by the declaration alone.
func TestBuildConfigStore_TheDeclaredOrderIsPrecedence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	fs := afero.NewOsFs()
	dir := t.TempDir()
	userPath := filepath.Join(dir, "config.yaml")
	projectPath := filepath.Join(dir, ".mytool.yaml")

	require.NoError(t, afero.WriteFile(fs, userPath, []byte("log:\n  level: warn\n"), 0o600))
	require.NoError(t, afero.WriteFile(fs, projectPath, []byte("log:\n  level: debug\n"), 0o600))

	tests := []struct {
		name   string
		layers []p.ConfigLayer
		want   string
	}{
		{name: "the default puts the project file above the user's", want: "debug"},
		{
			name:   "a declaration can put it below",
			layers: []p.ConfigLayer{p.LayerDefaults, p.LayerProject, p.LayerFiles, p.LayerEnv, p.LayerFlags},
			want:   "warn",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store, err := buildConfigStore(t.Context(), ConfigLoadOptions{
				Props: &p.Props{
					Tool:   p.Tool{Name: "mytool", Config: p.ConfigSpec{Layers: tc.layers}},
					Logger: logger.NewNoop(),
					FS:     fs,
				},
				CfgPaths:          []string{userPath},
				ProjectConfigPath: projectPath,
			})
			require.NoError(t, err)

			assert.Equal(t, tc.want, store.View().GetString("log.level"))
		})
	}
}

// A Tool is a plain struct and can be changed after props.New validated it,
// so the store refuses a bad order itself rather than wiring it.
func TestBuildConfigStore_RefusesABadLayerOrder(t *testing.T) {
	t.Parallel()

	_, err := buildConfigStore(t.Context(), ConfigLoadOptions{
		Props: &p.Props{
			Tool:   p.Tool{Name: "mytool", Config: p.ConfigSpec{Layers: []p.ConfigLayer{p.LayerFiles, p.LayerDefaults}}},
			Logger: logger.NewNoop(),
			FS:     afero.NewMemMapFs(),
		},
		AllowEmpty: true,
	})
	require.ErrorIs(t, err, p.ErrConfigLayerOrder)
}
