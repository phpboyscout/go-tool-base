package config_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cfg "gitlab.com/phpboyscout/go/config"
	configafero "gitlab.com/phpboyscout/go/config-afero"

	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
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

// TestCmdSet_HardensPreexistingFilePerms pins the R4 hardening invariant on
// the write path: go/config preserves an existing file's mode, so a config
// file that arrived group/world-readable would keep those bits after a
// credential write. config set must re-tighten it to 0600. Runs against a real
// OS temp dir because afero's memmap FS does not model chmod faithfully.
func TestCmdSet_HardensPreexistingFilePerms(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("log:\n  level: info\n"), 0o644)) //nolint:gosec // deliberately loose: the test asserts the write tightens it

	fs := afero.NewOsFs()
	store, err := cfg.NewStore(t.Context(), cfg.WithFiles(configafero.Wrap(fs), path))
	require.NoError(t, err)

	p := &props.Props{Config: store, FS: fs, Tool: props.Tool{Name: "tool"}, Logger: logger.NewNoop()}

	cmd := config.NewCmdSet(p)
	cmd.SetArgs([]string{"github.auth.value", "ghp_secret"})
	require.NoError(t, cmd.Execute())

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(),
		"config set must re-tighten a pre-existing config file to owner-only")
}

// projectLocalConfig builds Props whose store's writable file is a
// project-local ".<tool>.yaml", so set routes credential writes there.
func projectLocalConfig(t *testing.T, contents string) (*props.Props, afero.Fs, string) {
	t.Helper()

	fs := afero.NewMemMapFs()
	path := "/repo/.tool.yaml"
	require.NoError(t, afero.WriteFile(fs, path, []byte(contents), 0o600))

	store, err := cfg.NewStore(t.Context(), cfg.WithFiles(configafero.Wrap(fs), path))
	require.NoError(t, err)

	return &props.Props{
		Config: store,
		FS:     fs,
		Tool:   props.Tool{Name: "tool"},
		Logger: logger.NewNoop(),
	}, fs, path
}

// TestCmdSet_SensitiveProjectLocalWrite_WarnsAndProceeds covers the credential
// guard on a committable file. Under `go test` stdin is not a terminal, so the
// non-interactive branch runs: a warning is emitted to stderr and the write
// proceeds (a project-local secret can be legitimate — it is never blocked).
func TestCmdSet_SensitiveProjectLocalWrite_WarnsAndProceeds(t *testing.T) {
	t.Parallel()

	p, fs, path := projectLocalConfig(t, "log:\n  level: info\n")

	cmd := config.NewCmdSet(p)
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetIn(&bytes.Buffer{}) // force the non-interactive branch
	cmd.SetArgs([]string{"github.auth.value", "ghp_committed_secret"})

	require.NoError(t, cmd.Execute())

	assert.Contains(t, errBuf.String(), "Warning")
	assert.Contains(t, errBuf.String(), "project-local")
	assert.NotContains(t, out.String(), "aborted")

	// The write still lands — the guard warns, it does not block.
	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "ghp_committed_secret")
}

// TestCmdSet_NonSensitiveProjectLocalWrite_NoWarning confirms the guard is
// scoped to credentials: an ordinary key written to the same project-local
// file produces no warning.
func TestCmdSet_NonSensitiveProjectLocalWrite_NoWarning(t *testing.T) {
	t.Parallel()

	p, _, _ := projectLocalConfig(t, "log:\n  level: info\n")

	cmd := config.NewCmdSet(p)
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetIn(&bytes.Buffer{})
	cmd.SetArgs([]string{"log.level", "debug"})

	require.NoError(t, cmd.Execute())
	assert.NotContains(t, errBuf.String(), "Warning")
}

// TestCmdSet_SensitiveGlobalWrite_NoWarning confirms the guard does not fire
// for a credential written to the user's private (non-project-local) config —
// that is the normal, safe destination.
func TestCmdSet_SensitiveGlobalWrite_NoWarning(t *testing.T) {
	t.Parallel()

	p, _, _ := newFileConfig(t, "log:\n  level: info\n") // path base is config.yaml

	cmd := config.NewCmdSet(p)
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetIn(&bytes.Buffer{})
	cmd.SetArgs([]string{"github.auth.value", "ghp_private_ok"})

	require.NoError(t, cmd.Execute())
	assert.NotContains(t, errBuf.String(), "Warning")
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
