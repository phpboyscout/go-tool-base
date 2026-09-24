package config_test

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cfg "gitlab.com/phpboyscout/go/config"
	configafero "gitlab.com/phpboyscout/go/config-afero"
	configjson "gitlab.com/phpboyscout/go/config-json"
	configtoml "gitlab.com/phpboyscout/go/config-toml"
	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// ownFormatProps is a tool whose own file is in format, linking the TOML and
// JSON codecs, with contents at /etc/tool/config.<format>. Empty contents
// leaves the file absent and the store without layers.
func ownFormatProps(t *testing.T, format, contents string) (*props.Props, afero.Fs, string) {
	t.Helper()

	reg := features.NewRegistry()
	for _, d := range props.DescriptorsIn(features.Default().Snapshot()) {
		require.NoError(t, reg.Declare(d))
	}

	codecs := map[string]cfg.Codec{"toml": configtoml.Codec{}, "json": configjson.Codec{}}
	for f, c := range codecs {
		require.NoError(t, reg.Declare(setup.ConfigFormatDescriptor(f)))
		setup.RegisterConfigCodecOn(reg, f, c, "."+f)
	}

	fs := afero.NewMemMapFs()
	path := "/etc/tool/config." + format

	var opts []cfg.StoreOption
	if contents != "" {
		require.NoError(t, afero.WriteFile(fs, path, []byte(contents), 0o600))
		opts = append(opts, cfg.WithBackend(cfg.NewCodecBackend(configafero.Wrap(fs), path, codecs[format])))
	} else {
		opts = append(opts, cfg.WithReaders(cfg.NamedSource{Name: "defaults", Content: []byte("log:\n  level: info\n")}))
	}

	store, err := cfg.NewStore(t.Context(), opts...)
	require.NoError(t, err)

	p, err := props.New(props.Tool{Name: "tool", Config: props.ConfigSpec{Format: format}},
		logger.NewNoop(), fs, props.WithFeatures(reg.Snapshot()), props.WithConfig(store))
	require.NoError(t, err)

	return p, fs, path
}

// Spec 0204 D23: unset reads the file through its own codec. It used to parse
// every file as YAML.
func TestCmdUnset_OwnFormatFile(t *testing.T) {
	t.Parallel()

	p, fs, path := ownFormatProps(t, "toml", "[log]\nlevel = \"info\"\n[feature]\nenabled = true\n")

	cmd := config.NewCmdUnset(p)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"feature.enabled"})
	require.NoError(t, cmd.Execute())

	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "enabled")
	assert.Contains(t, string(data), `level = "info"`, "the file stays TOML")
}

func TestCmdEdit_OwnFormatFile(t *testing.T) {
	t.Parallel()

	p, fs, path := ownFormatProps(t, "toml", "[log]\nlevel = \"info\"\n")

	cmd := config.NewCmdEdit(atATerminal(p), config.WithEditorRunner(fakeEditor(fs, "[log]\nlevel = \"debug\"\n")))
	cmd.SetOut(&bytes.Buffer{})
	require.NoError(t, cmd.Execute())

	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `level = "debug"`)
	assert.Equal(t, "debug", p.Config.View().GetString("log.level"))
}

// An edit that is not valid in the file's own format is refused naming that
// format, and the edit is kept.
func TestCmdEdit_InvalidOwnFormatAborts(t *testing.T) {
	t.Parallel()

	p, fs, path := ownFormatProps(t, "toml", "[log]\nlevel = \"info\"\n")

	cmd := config.NewCmdEdit(atATerminal(p), config.WithEditorRunner(fakeEditor(fs, "[log\nlevel = = debug\n")))
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "valid TOML")

	exists, _ := afero.Exists(fs, path+".edit.tmp")
	assert.True(t, exists, "the edit is kept")
}

// With no file yet, edit seeds one named for the own format, and a JSON seed
// is a valid empty document rather than a comment JSON cannot carry.
func TestCmdEdit_SeedsTheOwnFormat(t *testing.T) {
	// Not parallel: pins HOME so the default writable path is deterministic.
	t.Setenv("HOME", "/home/edithome")

	p, fs, _ := ownFormatProps(t, "json", "")

	var seenSeed string

	runner := func(_ context.Context, _ []string, path string) error {
		data, _ := afero.ReadFile(fs, path)
		seenSeed = string(data)

		return afero.WriteFile(fs, path, []byte(`{"log":{"level":"warn"}}`), 0o600)
	}

	cmd := config.NewCmdEdit(atATerminal(p), config.WithEditorRunner(runner))
	cmd.SetOut(&bytes.Buffer{})
	require.NoError(t, cmd.Execute())

	_, err := configjson.Codec{}.Decode("seed.json", []byte(seenSeed))
	require.NoError(t, err, "the seed is valid JSON: %q", seenSeed)

	want := filepath.Join(setup.GetDefaultConfigDir(fs, "tool"), "config.json")
	data, err := afero.ReadFile(fs, want)
	require.NoError(t, err)
	assert.Contains(t, string(data), "warn")
}
