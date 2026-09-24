package root

import (
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	configtoml "gitlab.com/phpboyscout/go/config-toml"
	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// tomlLinkedProps is a tool whose binary links the TOML format, built on a
// registry of its own rather than the default one.
func tomlLinkedProps(t *testing.T, fs afero.Fs, assets *p.Assets) *p.Props {
	t.Helper()

	reg := features.NewRegistry()
	for _, d := range p.DescriptorsIn(features.Default().Snapshot()) {
		require.NoError(t, reg.Declare(d))
	}

	require.NoError(t, reg.Declare(setup.ConfigFormatDescriptor("toml")))
	setup.RegisterConfigCodecOn(reg, "toml", configtoml.Codec{}, ".toml")

	opts := []p.Option{p.WithFeatures(reg.Snapshot())}
	if assets != nil {
		opts = append(opts, p.WithAssets(assets))
	}

	props, err := p.New(p.Tool{Name: "mytool"}, logger.NewNoop(), fs, opts...)
	require.NoError(t, err)

	return props
}

// Spec 0204 D2: a --config file is read through the codec its extension
// names, and a writable format stays a write target.
func TestBuildConfigStore_ReadsALinkedFormatByExtension(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/etc/mytool/config.toml", []byte("[log]\nlevel = \"warn\"\n"), 0o600))

	store, err := buildConfigStore(t.Context(), ConfigLoadOptions{
		Props:    tomlLinkedProps(t, fs, nil),
		CfgPaths: []string{"/etc/mytool/config.toml"},
	})
	require.NoError(t, err)
	assert.Equal(t, "warn", store.View().GetString("log.level"))

	_, err = store.Apply(t.Context(), config.Set("log.level", "debug"))
	require.NoError(t, err)

	written, err := afero.ReadFile(fs, "/etc/mytool/config.toml")
	require.NoError(t, err)
	assert.Contains(t, string(written), `level = "debug"`, "the write lands in the TOML file as TOML")
}

// An extension the tool does not link is refused before any read, whether or
// not the file exists yet: an absent one would become the write target.
func TestBuildConfigStore_RefusesAnUnlinkedFormat(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/etc/mytool/config.yaml", []byte("log:\n  level: warn\n"), 0o600))

	for _, path := range []string{"/work/present.json", "/work/absent.json"} {
		if path == "/work/present.json" {
			require.NoError(t, afero.WriteFile(fs, path, []byte(`{"log":{"level":"debug"}}`), 0o600))
		}

		_, err := buildConfigStore(t.Context(), ConfigLoadOptions{
			Props:    tomlLinkedProps(t, fs, nil),
			CfgPaths: []string{"/etc/mytool/config.yaml", path},
		})
		require.ErrorIs(t, err, setup.ErrUnlinkedConfigFormat, path)
	}
}

// An embedded ConfigPaths asset is decoded by its extension too. props.Assets
// merges and re-emits a .toml asset as TOML, which the store used to read as
// YAML.
func TestBuildConfigStore_DecodesAnEmbeddedAssetByExtension(t *testing.T) {
	t.Parallel()

	assets := p.NewAssets(p.AssetMap{
		"embedded": fstest.MapFS{
			"extra.toml": &fstest.MapFile{Data: []byte("[main]\nextra = \"value\"\n")},
		},
	})

	store, err := buildConfigStore(t.Context(), ConfigLoadOptions{
		Props:       tomlLinkedProps(t, afero.NewMemMapFs(), assets),
		ConfigPaths: []string{"extra.toml"},
		AllowEmpty:  true,
	})
	require.NoError(t, err)
	assert.Equal(t, "value", store.View().GetString("main.extra"))
}

// The default search paths name the tool's own file for its format (spec 0204
// D23), so a TOML tool looks for config.toml in /etc and in the user's
// config directory.
func TestSetupRootFlags_DefaultPathsNameTheOwnFormat(t *testing.T) {
	t.Parallel()

	props := &p.Props{
		Tool:   p.Tool{Name: "mytool", Config: p.ConfigSpec{Format: "toml"}},
		Logger: logger.NewNoop(),
		FS:     afero.NewMemMapFs(),
	}

	rootCmd := &cobra.Command{Use: "mytool"}
	setupRootFlags(rootCmd, props, &rootState{})

	defaults, err := rootCmd.PersistentFlags().GetStringArray("config")
	require.NoError(t, err)
	require.Len(t, defaults, 2)

	for _, path := range defaults {
		assert.Equal(t, "config.toml", filepath.Base(path))
	}
}
