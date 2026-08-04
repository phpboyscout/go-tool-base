package config

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	configafero "gitlab.com/phpboyscout/go/config-afero"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// fileBoundProps returns Props whose store loaded path as its writable file
// layer, so the writable-path resolution used by set/unset/edit resolves to
// that file. The file must exist (with parseable content) on fs.
func fileBoundProps(t *testing.T, fs afero.Fs, path string) *props.Props {
	t.Helper()

	store, err := config.NewStore(t.Context(), config.WithFiles(configafero.Wrap(fs), path))
	require.NoError(t, err)

	return &props.Props{
		FS:     fs,
		Config: store,
		Tool:   props.Tool{Name: "tool"},
	}
}

// TestLoadWritableSettings_NullContentResetsMap — a file holding "null"
// unmarshals to a nil map, which loadWritableSettings must reset to an empty,
// non-nil map so callers can mutate it. The file is rewritten to "null" after
// the store loads, since the store itself refuses to load an all-null layer.
func TestLoadWritableSettings_NullContentResetsMap(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	path := "/etc/tool/config.yaml"
	require.NoError(t, afero.WriteFile(fs, path, []byte("log:\n  level: info\n"), 0o600))

	p := fileBoundProps(t, fs, path)

	require.NoError(t, afero.WriteFile(fs, path, []byte("null\n"), 0o600))

	gotPath, settings, err := loadWritableSettings(p)
	require.NoError(t, err)
	assert.Equal(t, path, gotPath)
	require.NotNil(t, settings)
	assert.Empty(t, settings)
}

// TestLoadWritableSettings_InvalidYAML — an existing file whose content became
// unparseable after load surfaces a descriptive error rather than silently
// starting from empty.
func TestLoadWritableSettings_InvalidYAML(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	path := "/etc/tool/config.yaml"
	require.NoError(t, afero.WriteFile(fs, path, []byte("log:\n  level: info\n"), 0o600))

	p := fileBoundProps(t, fs, path)

	require.NoError(t, afero.WriteFile(fs, path, []byte("}{"), 0o600))

	_, _, err := loadWritableSettings(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing existing config")
}

// TestPersistUnset_PropagatesLoadError — persistUnset surfaces a
// loadWritableSettings failure (here: a file corrupted after the store loaded).
func TestPersistUnset_PropagatesLoadError(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	path := "/etc/tool/config.yaml"
	require.NoError(t, afero.WriteFile(fs, path, []byte("log:\n  level: info\nfeature:\n  enabled: true\n"), 0o600))

	p := fileBoundProps(t, fs, path)

	require.NoError(t, afero.WriteFile(fs, path, []byte("}{"), 0o600))

	err := persistUnset(t.Context(), p, "feature.enabled")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing existing config")
}

// TestWriteConfigAtomic_MkdirError — a read-only filesystem fails the lazy
// parent-directory create, and the error is wrapped descriptively.
func TestWriteConfigAtomic_MkdirError(t *testing.T) {
	t.Parallel()

	ro := afero.NewReadOnlyFs(afero.NewMemMapFs())

	err := writeConfigAtomic(ro, "/etc/tool/config.yaml", []byte("k: v\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create config directory")
}

// TestSeedOrRead_EmptyToolNameFallback — when the file is absent and the tool
// name is empty, the seeded header falls back to the generic "tool" label.
func TestSeedOrRead_EmptyToolNameFallback(t *testing.T) {
	t.Parallel()

	out := seedOrRead(afero.NewMemMapFs(), "/absent/config.yaml", "")
	assert.Contains(t, string(out), "tool configuration")
}

// TestResolveEnvVarName_AssumeYes — with --yes the default env var name is
// returned without prompting.
func TestResolveEnvVarName_AssumeYes(t *testing.T) {
	t.Parallel()

	name, err := resolveEnvVarName(MigrateOptions{AssumeYes: true}, literalCredential{
		Key: "github.auth.value",
	})
	require.NoError(t, err)
	assert.Equal(t, "GITHUB_TOKEN", name)
}

// TestRunEdit_BadEditorShlex — an unparseable editor command is rejected before
// any editor launch.
func TestRunEdit_BadEditorShlex(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	path := "/etc/tool/config.yaml"
	require.NoError(t, afero.WriteFile(fs, path, []byte("log:\n  level: info\n"), 0o600))

	cmd := NewCmdEdit(fileBoundProps(t, fs, path),
		WithEditorRunner(func(context.Context, []string, string) error { return nil }),
		WithInteractiveCheck(func() bool { return true }),
	)
	cmd.SetArgs([]string{"--editor", "'unbalanced"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not parse editor command")
}

// TestRunEdit_ReadEditedFails — when the editor removes the temp file, reading
// it back surfaces a descriptive error rather than a panic.
func TestRunEdit_ReadEditedFails(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	path := "/etc/tool/config.yaml"
	require.NoError(t, afero.WriteFile(fs, path, []byte("log:\n  level: info\n"), 0o600))

	runner := func(_ context.Context, _ []string, tmp string) error {
		return fs.Remove(tmp) // editor "succeeds" but the temp file is gone
	}

	cmd := NewCmdEdit(fileBoundProps(t, fs, path),
		WithEditorRunner(runner),
		WithInteractiveCheck(func() bool { return true }),
	)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading edited file")
}

// renameFailFs is an afero.Fs whose Rename always fails, to exercise
// writeConfigAtomic's temp-rename error branch.
type renameFailFs struct{ afero.Fs }

func (renameFailFs) Rename(string, string) error {
	return errors.New("rename blocked")
}

// TestWriteConfigAtomic_RenameError — a Rename failure is wrapped and the temp
// file is cleaned up.
func TestWriteConfigAtomic_RenameError(t *testing.T) {
	t.Parallel()

	fs := renameFailFs{afero.NewMemMapFs()}

	err := writeConfigAtomic(fs, "/etc/tool/config.yaml", []byte("k: v\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rename migrated config into place")
}
