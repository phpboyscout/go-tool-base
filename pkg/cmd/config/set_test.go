package config_test

import (
	"bytes"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cfg "gitlab.com/phpboyscout/go/config"
	configafero "gitlab.com/phpboyscout/go/config-afero"

	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestCmdSet_EmbeddedMergeContainerPersists proves config set works in the
// embedded-merge configuration GTB produces for InitCmd-disabled tools: the
// store carries an embedded-defaults reader layer plus a declared config file
// that does not exist yet. Apply must route the write to that file layer and
// create the file.
func TestCmdSet_EmbeddedMergeContainerPersists(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	path := "/home/u/.settool/config.yaml"

	store, err := cfg.NewStore(t.Context(),
		cfg.WithReaders(cfg.NamedSource{Name: "embedded", Content: []byte("existing:\n  key: value\n")}),
		cfg.WithFiles(configafero.Wrap(fs), path),
	)
	require.NoError(t, err)

	p := &props.Props{Config: store, FS: fs, Tool: props.Tool{Name: "settool"}}
	cmd := config.NewCmdSet(p)
	cmd.SetArgs([]string{"new.key", "hello"})

	require.NoError(t, cmd.Execute())

	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "hello")
	// The embedded default is not dumped into the created file.
	assert.NotContains(t, string(data), "existing")
	// The live store already reflects the write — no reload needed.
	assert.Equal(t, "hello", p.Config.View().GetString("new.key"))
	assert.Equal(t, "value", p.Config.View().GetString("existing.key"))
}

func TestCmdSet_WritesValue(t *testing.T) {
	t.Parallel()

	p, fs, path := newFileConfig(t, "# keep me\nlog:\n  level: info\n")

	cmd := config.NewCmdSet(p)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"log.level", "debug"})

	require.NoError(t, cmd.Execute())

	assert.Contains(t, buf.String(), "log.level")
	assert.Contains(t, buf.String(), "debug")

	// The document is edited in place: the value changed and the comment
	// survived the write.
	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "level: debug")
	assert.Contains(t, string(data), "# keep me")

	// Apply publishes the new snapshot — the live store reflects the change.
	assert.Equal(t, "debug", p.Config.View().GetString("log.level"))
}

func TestCmdSet_NilConfig(t *testing.T) {
	t.Parallel()

	p := &props.Props{Config: nil}
	cmd := config.NewCmdSet(p)
	cmd.SetArgs([]string{"log.level", "debug"})

	err := cmd.Execute()
	assert.Error(t, err)
}

func TestCmdSet_CoerceBool(t *testing.T) {
	t.Parallel()

	p, fs, path := newFileConfig(t, "feature:\n  enabled: false\n")

	cmd := config.NewCmdSet(p)
	cmd.SetArgs([]string{"feature.enabled", "true"})

	require.NoError(t, cmd.Execute())

	// Stored as a native bool, not the string "true".
	assert.Equal(t, true, p.Config.View().Get("feature.enabled"))

	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "enabled: true")
}

func TestCmdSet_CoerceInt(t *testing.T) {
	t.Parallel()

	p, fs, path := newFileConfig(t, "server:\n  port: 8080\n")

	cmd := config.NewCmdSet(p)
	cmd.SetArgs([]string{"server.port", "9090"})

	require.NoError(t, cmd.Execute())

	assert.Equal(t, 9090, p.Config.View().GetInt("server.port"))

	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "port: 9090")
}
