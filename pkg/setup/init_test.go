package setup

import (
	"bytes"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestInitialise_MkdirFailure_PreservesCause proves the init-newf-drops-error-cause
// fix: when the directory create fails, the returned error must wrap the
// underlying cause so errors.Is keeps matching it. A ReadOnlyFs.MkdirAll returns
// syscall.EPERM; the old errors.Newf("...: %s", err) form would flatten that into
// a string and break the chain.
func TestInitialise_MkdirFailure_PreservesCause(t *testing.T) {
	t.Parallel()

	p := &props.Props{
		Logger: logger.NewNoop(),
		FS:     afero.NewReadOnlyFs(afero.NewMemMapFs()),
	}

	_, err := Initialise(t.Context(), p, InitOptions{Dir: "/cannot/create"})
	require.Error(t, err)
	require.ErrorIs(t, err, syscall.EPERM,
		"the wrapped error must preserve the underlying MkdirAll cause")
	assert.Contains(t, err.Error(), "Failed to create directory")
}

func TestOpenConfigEditor(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "config.yaml")

	l := logger.NewNoop()
	p := &props.Props{
		Logger: l,
		FS:     afero.NewMemMapFs(),
	}

	// Test case 1: New config creation
	t.Run("Create new config", func(t *testing.T) {
		editor, path, err := OpenConfigEditor(t.Context(), p, tmpDir, false)
		require.NoError(t, err)
		assert.NotNil(t, editor)
		assert.Equal(t, targetFile, path)

		exists, err := afero.Exists(p.FS, targetFile)
		require.NoError(t, err)
		assert.True(t, exists, "the config file must be materialised")
	})

	// Test case 2: Existing config values survive the template merge
	t.Run("Merge existing config", func(t *testing.T) {
		require.NoError(t, afero.WriteFile(p.FS, targetFile, []byte("existing: value\n"), 0o644))

		editor, _, err := OpenConfigEditor(t.Context(), p, tmpDir, false)
		require.NoError(t, err)
		assert.Equal(t, "value", editor.View().GetString("existing"))
	})
}

func TestWriteGitignore_NewDir(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	configDir := "/home/user/.mytool"
	_ = fs.MkdirAll(configDir, 0755)

	err := writeGitignore(fs, configDir)
	require.NoError(t, err)

	content, err := afero.ReadFile(fs, filepath.Join(configDir, ".gitignore"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "*.env")
	assert.Contains(t, string(content), "*.secret")
	assert.Contains(t, string(content), "*.key")
}

func TestWriteGitignore_ExistingPreserved(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	configDir := "/home/user/.mytool"
	_ = fs.MkdirAll(configDir, 0755)

	existingContent := "# my custom gitignore\n*.log\n"
	require.NoError(t, afero.WriteFile(fs, filepath.Join(configDir, ".gitignore"), []byte(existingContent), 0644))

	err := writeGitignore(fs, configDir)
	require.NoError(t, err)

	// Should not overwrite existing .gitignore
	content, err := afero.ReadFile(fs, filepath.Join(configDir, ".gitignore"))
	require.NoError(t, err)
	assert.Equal(t, existingContent, string(content))
}

func TestWarnIfAPIKeysInGitRepo_Warns(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	configDir := "/project/.mytool"
	_ = fs.MkdirAll(configDir, 0755)

	// Create .git dir in parent to simulate git repo
	_ = fs.MkdirAll("/project/.git", 0755)

	// Write config with an API key pattern
	require.NoError(t, afero.WriteFile(fs, filepath.Join(configDir, "config.yaml"), []byte("api_key: sk-abc123"), 0644))

	l := logger.NewBuffer()

	p := &props.Props{
		Logger: l,
		FS:     fs,
	}

	warnIfAPIKeysInGitRepo(p, configDir)

	entries := l.Entries()
	require.Len(t, entries, 1)
	assert.Equal(t, logger.WarnLevel, entries[0].Level)
	assert.Contains(t, entries[0].Message, "API keys")
}

func TestWarnIfAPIKeysInGitRepo_NoGitRepo(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	configDir := "/project/.mytool"
	_ = fs.MkdirAll(configDir, 0755)

	// No .git directory
	require.NoError(t, afero.WriteFile(fs, filepath.Join(configDir, "config.yaml"), []byte("api_key: sk-abc123"), 0644))

	var buf bytes.Buffer
	l := logger.NewCharm(&buf)

	p := &props.Props{
		Logger: l,
		FS:     fs,
	}

	warnIfAPIKeysInGitRepo(p, configDir)
	assert.Empty(t, buf.String())
}

// recordingInitialiser counts Configure calls so the interactivity guard can be
// asserted without driving a real wizard.
type recordingInitialiser struct {
	calls int
}

func (r *recordingInitialiser) Name() string                    { return "Fake provider" }
func (r *recordingInitialiser) IsConfigured(config.Reader) bool { return false }
func (r *recordingInitialiser) Configure(*props.Props, Editor) error {
	r.calls++

	return nil
}

func TestInitialise_SkipsWizardsWhenNonInteractive(t *testing.T) {
	t.Parallel()

	p := &props.Props{Logger: logger.NewNoop(), FS: afero.NewMemMapFs()}
	rec := &recordingInitialiser{}
	notInteractive := false

	_, err := Initialise(t.Context(), p, InitOptions{
		Dir:          t.TempDir(),
		Initialisers: []Initialiser{rec},
		Interactive:  &notInteractive,
	})
	require.NoError(t, err)
	assert.Zero(t, rec.calls, "credential wizard must not run without an interactive terminal")
}

func TestInitialise_RunsWizardsWhenInteractive(t *testing.T) {
	t.Parallel()

	p := &props.Props{Logger: logger.NewNoop(), FS: afero.NewMemMapFs()}
	rec := &recordingInitialiser{}
	interactive := true

	_, err := Initialise(t.Context(), p, InitOptions{
		Dir:          t.TempDir(),
		Initialisers: []Initialiser{rec},
		Interactive:  &interactive,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, rec.calls, "an unconfigured initialiser must run in an interactive terminal")
}
