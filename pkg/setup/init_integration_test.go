package setup_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	configafero "gitlab.com/phpboyscout/go/config-afero"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func newInitProps(fs afero.Fs) *props.Props {
	return &props.Props{
		Tool:   props.Tool{Name: "test-tool"},
		Logger: logger.NewNoop(),
		FS:     fs,
		Assets: props.NewAssets(),
	}
}

// loadWritten pins a view over the written config file so assertions read it
// the way a running tool would.
func loadWritten(t *testing.T, fs afero.Fs, path string) *config.View {
	t.Helper()

	store, err := config.NewStore(t.Context(), config.WithFiles(configafero.Wrap(fs), path))
	require.NoError(t, err)

	return store.View()
}

func TestInitialise_CreatesDirectoryAndConfigFile(t *testing.T) {
	t.Parallel()
	testutil.SkipIfNotIntegration(t, "setup")

	fs := afero.NewMemMapFs()
	p := newInitProps(fs)
	dir := "/home/testuser/.test-tool"

	path, err := setup.Initialise(t.Context(), p, setup.InitOptions{Dir: dir})
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(dir, "config.yaml"), path)

	// Directory created
	dirExists, _ := afero.DirExists(fs, dir)
	assert.True(t, dirExists)

	// Config file created and readable
	exists, _ := afero.Exists(fs, path)
	assert.True(t, exists)

	content, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.Contains(t, string(content), "log:")
}

func TestInitialise_WritesGitignore(t *testing.T) {
	t.Parallel()
	testutil.SkipIfNotIntegration(t, "setup")

	fs := afero.NewMemMapFs()
	p := newInitProps(fs)
	dir := "/home/testuser/.test-tool"

	_, err := setup.Initialise(t.Context(), p, setup.InitOptions{Dir: dir})
	require.NoError(t, err)

	gitignorePath := filepath.Join(dir, ".gitignore")
	exists, _ := afero.Exists(fs, gitignorePath)
	assert.True(t, exists)

	content, err := afero.ReadFile(fs, gitignorePath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "*.env")
	assert.Contains(t, string(content), "*.secret")
	assert.Contains(t, string(content), "*.key")
}

func TestInitialise_GitignoreNotOverwritten(t *testing.T) {
	t.Parallel()
	testutil.SkipIfNotIntegration(t, "setup")

	fs := afero.NewMemMapFs()
	p := newInitProps(fs)
	dir := "/home/testuser/.test-tool"

	// Pre-create a custom .gitignore
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".gitignore"), []byte("custom content\n"), 0o644))

	_, err := setup.Initialise(t.Context(), p, setup.InitOptions{Dir: dir})
	require.NoError(t, err)

	content, err := afero.ReadFile(fs, filepath.Join(dir, ".gitignore"))
	require.NoError(t, err)
	assert.Equal(t, "custom content\n", string(content), "existing .gitignore should not be overwritten")
}

// TestInitialise_WrittenConfigIsTemplateNotDefaults pins the segregated-defaults
// design (spec D9): the written file is the merged init template — the
// framework's log/update stanzas — and NOT a dump of the defaults layer.
// github.* ships as a feature defaults bundle that applies at runtime, so it
// must not be pinned into the user's file.
func TestInitialise_WrittenConfigIsTemplateNotDefaults(t *testing.T) {
	t.Parallel()
	testutil.SkipIfNotIntegration(t, "setup")

	fs := afero.NewMemMapFs()
	p := newInitProps(fs)
	dir := "/home/testuser/.test-tool"

	path, err := setup.Initialise(t.Context(), p, setup.InitOptions{Dir: dir})
	require.NoError(t, err)

	cfg := loadWritten(t, fs, path)

	// Framework template keys are present.
	assert.Equal(t, "info", cfg.GetString("log.level"))
	assert.True(t, cfg.Has("update.policy"), "the framework template ships the update stanza")

	// Feature defaults are a runtime layer, not file content.
	assert.False(t, cfg.Has("github.url.api"),
		"github defaults must not be written into the user's file")
}

func TestInitialise_CleanReplacesExistingConfig(t *testing.T) {
	t.Parallel()
	testutil.SkipIfNotIntegration(t, "setup")

	fs := afero.NewMemMapFs()
	p := newInitProps(fs)
	dir := "/home/testuser/.test-tool"
	cfgPath := filepath.Join(dir, "config.yaml")

	// Pre-create config with custom values
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, cfgPath, []byte("log:\n  level: debug\ncustom:\n  key: value\n"), 0o644))

	// Init with --clean
	_, err := setup.Initialise(t.Context(), p, setup.InitOptions{Dir: dir, Clean: true})
	require.NoError(t, err)

	cfg := loadWritten(t, fs, cfgPath)

	// Clean replaces — custom key should be gone, defaults restored
	assert.Equal(t, "info", cfg.GetString("log.level"), "should reset to default")
	assert.Empty(t, cfg.GetString("custom.key"), "custom key should be removed")
}

func TestInitialise_MergePreservesExistingValues(t *testing.T) {
	t.Parallel()
	testutil.SkipIfNotIntegration(t, "setup")

	fs := afero.NewMemMapFs()
	p := newInitProps(fs)
	dir := "/home/testuser/.test-tool"
	cfgPath := filepath.Join(dir, "config.yaml")

	// Pre-create config with custom values
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, cfgPath, []byte("log:\n  level: debug\ncustom:\n  key: preserved\n"), 0o644))

	// Init without --clean (merge mode)
	_, err := setup.Initialise(t.Context(), p, setup.InitOptions{Dir: dir, Clean: false})
	require.NoError(t, err)

	cfg := loadWritten(t, fs, cfgPath)

	// Merge preserves existing values
	assert.Equal(t, "debug", cfg.GetString("log.level"), "existing value should be preserved")
	assert.Equal(t, "preserved", cfg.GetString("custom.key"), "custom key should survive merge")

	// Template keys the file did not have are gained from the merge.
	assert.True(t, cfg.Has("update.check_interval"), "re-running init gains new template keys")
}

// testInitialiser is a simple Initialiser for testing the callback cascade.
type testInitialiser struct {
	name         string
	configured   bool
	configureErr error
	called       bool
	configFn     func(setup.Editor)
}

func (ti *testInitialiser) Name() string                      { return ti.name }
func (ti *testInitialiser) IsConfigured(_ config.Reader) bool { return ti.configured }
func (ti *testInitialiser) Configure(_ context.Context, _ *props.Props, c setup.Editor) error {
	ti.called = true
	if ti.configFn != nil {
		ti.configFn(c)
	}

	return ti.configureErr
}

// interactive returns a pointer to true so initialiser cascades exercise the
// wizard path; without it, Initialise's non-interactive guard would skip the
// fake initialisers under a non-terminal test runner.
func interactive() *bool {
	v := true

	return &v
}

func TestInitialise_InitialisersCalled(t *testing.T) {
	t.Parallel()
	testutil.SkipIfNotIntegration(t, "setup")

	fs := afero.NewMemMapFs()
	p := newInitProps(fs)
	dir := "/home/testuser/.test-tool"

	init1 := &testInitialiser{
		name: "feature-a",
		configFn: func(c setup.Editor) {
			require.NoError(t, c.Set("feature_a.enabled", true))
		},
	}
	init2 := &testInitialiser{
		name: "feature-b",
		configFn: func(c setup.Editor) {
			require.NoError(t, c.Set("feature_b.key", "from-init"))
		},
	}

	path, err := setup.Initialise(t.Context(), p, setup.InitOptions{
		Dir:          dir,
		Initialisers: []setup.Initialiser{init1, init2},
		Interactive:  interactive(),
	})
	require.NoError(t, err)

	assert.True(t, init1.called, "first initialiser should be called")
	assert.True(t, init2.called, "second initialiser should be called")

	// Values set by initialisers should be in the written config
	cfg := loadWritten(t, fs, path)

	assert.True(t, cfg.GetBool("feature_a.enabled"))
	assert.Equal(t, "from-init", cfg.GetString("feature_b.key"))
}

func TestInitialise_AlreadyConfiguredInitialiserSkipped(t *testing.T) {
	t.Parallel()
	testutil.SkipIfNotIntegration(t, "setup")

	fs := afero.NewMemMapFs()
	p := newInitProps(fs)
	dir := "/home/testuser/.test-tool"

	init1 := &testInitialiser{name: "already-done", configured: true}
	init2 := &testInitialiser{name: "needs-config"}

	_, err := setup.Initialise(t.Context(), p, setup.InitOptions{
		Dir:          dir,
		Initialisers: []setup.Initialiser{init1, init2},
		Interactive:  interactive(),
	})
	require.NoError(t, err)

	assert.False(t, init1.called, "already-configured initialiser should be skipped")
	assert.True(t, init2.called, "unconfigured initialiser should be called")
}

func TestInitialise_InitialiserFailureContinues(t *testing.T) {
	t.Parallel()
	testutil.SkipIfNotIntegration(t, "setup")

	fs := afero.NewMemMapFs()
	log := logger.NewBuffer()
	p := &props.Props{
		Tool:   props.Tool{Name: "test-tool"},
		Logger: log,
		FS:     fs,
		Assets: props.NewAssets(),
	}
	dir := "/home/testuser/.test-tool"

	failing := &testInitialiser{
		name:         "broken",
		configureErr: assert.AnError,
	}
	passing := &testInitialiser{
		name: "works",
		configFn: func(c setup.Editor) {
			require.NoError(t, c.Set("works.key", "yes"))
		},
	}

	path, err := setup.Initialise(t.Context(), p, setup.InitOptions{
		Dir:          dir,
		Initialisers: []setup.Initialiser{failing, passing},
		Interactive:  interactive(),
	})
	require.NoError(t, err, "overall init should succeed despite initialiser failure")

	assert.True(t, failing.called)
	assert.True(t, passing.called, "second initialiser should still run after first fails")

	// Warning logged for the failure
	assert.True(t, log.Contains("configuration skipped"))

	// Passing initialiser's values written
	cfg := loadWritten(t, fs, path)
	assert.Equal(t, "yes", cfg.GetString("works.key"))
}

func TestInitialise_APIKeyWarningInGitRepo(t *testing.T) {
	t.Parallel()
	testutil.SkipIfNotIntegration(t, "setup")

	fs := afero.NewMemMapFs()
	log := logger.NewBuffer()
	p := &props.Props{
		Tool:   props.Tool{Name: "test-tool"},
		Logger: log,
		FS:     fs,
		Assets: props.NewAssets(),
	}

	// Simulate being inside a git repo
	require.NoError(t, fs.MkdirAll("/project/.git", 0o755))
	dir := "/project/.test-tool"

	initWithToken := &testInitialiser{
		name: "token-provider",
		configFn: func(c setup.Editor) {
			require.NoError(t, c.Set("auth.token", "sk-supersecret123"))
		},
	}

	_, err := setup.Initialise(t.Context(), p, setup.InitOptions{
		Dir:          dir,
		Initialisers: []setup.Initialiser{initWithToken},
		Interactive:  interactive(),
	})
	require.NoError(t, err)

	// Should warn about API keys in git repo
	assert.True(t, log.Contains("API keys"))
}
