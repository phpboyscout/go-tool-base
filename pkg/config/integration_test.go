package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/internal/testutil"
	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

func TestLoadFilesContainer_MergesMultipleSources(t *testing.T) {
	t.Parallel()
	testutil.SkipIfNotIntegration(t, "config")

	dir := t.TempDir()
	primary := filepath.Join(dir, "primary.yaml")
	secondary := filepath.Join(dir, "secondary.yaml")

	require.NoError(t, os.WriteFile(primary, []byte(`
server:
  port: 8080
  host: localhost
logging:
  level: info
`), 0o644))

	require.NoError(t, os.WriteFile(secondary, []byte(`
server:
  port: 9090
database:
  host: db.local
`), 0o644))

	cfg, err := config.LoadFilesContainer(afero.NewOsFs(), config.WithLogger(logger.NewNoop()), config.WithConfigFiles(primary, secondary))
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Secondary overrides primary for shared keys
	assert.Equal(t, 9090, cfg.GetInt("server.port"))

	// Primary-only keys preserved
	assert.Equal(t, "localhost", cfg.GetString("server.host"))
	assert.Equal(t, "info", cfg.GetString("logging.level"))

	// Secondary-only keys added
	assert.Equal(t, "db.local", cfg.GetString("database.host"))
}

func TestLoadFilesContainer_MissingSecondarySkipped(t *testing.T) {
	t.Parallel()
	testutil.SkipIfNotIntegration(t, "config")

	dir := t.TempDir()
	primary := filepath.Join(dir, "config.yaml")

	require.NoError(t, os.WriteFile(primary, []byte(`
app:
  name: myapp
  debug: false
`), 0o644))

	missing := filepath.Join(dir, "nonexistent.yaml")

	cfg, err := config.LoadFilesContainer(afero.NewOsFs(), config.WithLogger(logger.NewNoop()), config.WithConfigFiles(primary, missing))
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, "myapp", cfg.GetString("app.name"))
	assert.False(t, cfg.GetBool("app.debug"))
}

func TestLoadFilesContainer_MissingPrimaryReturnsNil(t *testing.T) {
	t.Parallel()
	testutil.SkipIfNotIntegration(t, "config")

	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.yaml")

	cfg, err := config.LoadFilesContainer(afero.NewOsFs(), config.WithLogger(logger.NewNoop()), config.WithConfigFiles(missing))
	require.NoError(t, err)
	assert.Nil(t, cfg)
}

func TestLoadFilesContainer_InvalidYAML(t *testing.T) {
	t.Parallel()
	testutil.SkipIfNotIntegration(t, "config")

	dir := t.TempDir()
	primary := filepath.Join(dir, "bad.yaml")

	require.NoError(t, os.WriteFile(primary, []byte(`
not: valid: yaml: [broken
`), 0o644))

	_, err := config.LoadFilesContainer(afero.NewOsFs(), config.WithLogger(logger.NewNoop()), config.WithConfigFiles(primary))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestLoadFilesContainer_InvalidSecondaryLogWarning(t *testing.T) {
	t.Parallel()
	testutil.SkipIfNotIntegration(t, "config")

	dir := t.TempDir()
	primary := filepath.Join(dir, "good.yaml")
	secondary := filepath.Join(dir, "bad.yaml")

	require.NoError(t, os.WriteFile(primary, []byte("key: value\n"), 0o644))
	require.NoError(t, os.WriteFile(secondary, []byte("not: valid: [broken\n"), 0o644))

	log := logger.NewBuffer()
	cfg, err := config.LoadFilesContainer(afero.NewOsFs(), config.WithLogger(log), config.WithConfigFiles(primary, secondary))
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Primary values survive despite secondary failure
	assert.Equal(t, "value", cfg.GetString("key"))

	// Warning logged for invalid secondary
	assert.True(t, log.Contains("Failed to merge configuration file"))
}

func TestLoadFilesContainer_EnvVarOverridesFileValue(t *testing.T) {
	// Cannot use t.Parallel — t.Setenv modifies process environment.
	testutil.SkipIfNotIntegration(t, "config")

	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")

	require.NoError(t, os.WriteFile(cfgFile, []byte(`
server:
  port: 8080
  host: filehost
`), 0o644))

	t.Setenv("SERVER_PORT", "3000")

	cfg, err := config.LoadFilesContainer(afero.NewOsFs(), config.WithLogger(logger.NewNoop()), config.WithConfigFiles(cfgFile))
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Env var wins over file value
	assert.Equal(t, 3000, cfg.GetInt("server.port"))

	// Non-overridden keys use file value
	assert.Equal(t, "filehost", cfg.GetString("server.host"))
}

func TestLoadFilesContainer_DeepNestingMerge(t *testing.T) {
	t.Parallel()
	testutil.SkipIfNotIntegration(t, "config")

	dir := t.TempDir()
	base := filepath.Join(dir, "base.yaml")
	overlay := filepath.Join(dir, "overlay.yaml")

	require.NoError(t, os.WriteFile(base, []byte(`
server:
  grpc:
    port: 50051
    reflection: true
  http:
    port: 8080
    tls:
      enabled: false
      cert: ""
`), 0o644))

	require.NoError(t, os.WriteFile(overlay, []byte(`
server:
  http:
    tls:
      enabled: true
      cert: /etc/ssl/cert.pem
`), 0o644))

	cfg, err := config.LoadFilesContainer(afero.NewOsFs(), config.WithLogger(logger.NewNoop()), config.WithConfigFiles(base, overlay))
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Overlay overrides deeply nested keys
	assert.True(t, cfg.GetBool("server.http.tls.enabled"))
	assert.Equal(t, "/etc/ssl/cert.pem", cfg.GetString("server.http.tls.cert"))

	// Untouched deep keys preserved
	assert.Equal(t, 50051, cfg.GetInt("server.grpc.port"))
	assert.True(t, cfg.GetBool("server.grpc.reflection"))
	assert.Equal(t, 8080, cfg.GetInt("server.http.port"))
}

func TestLoadEmbed_MergesWithFileConfig(t *testing.T) {
	t.Parallel()
	testutil.SkipIfNotIntegration(t, "config")

	// Simulate embedded defaults
	embeddedFS := fstest.MapFS{
		"defaults.yaml": &fstest.MapFile{
			Data: []byte(`
ai:
  provider: openai
  max_tokens: 4096
logging:
  level: warn
`),
		},
	}

	embeddedCfg, err := config.LoadEmbed([]string{"defaults.yaml"}, embeddedFS, config.WithLogger(logger.NewNoop()))
	require.NoError(t, err)
	require.NotNil(t, embeddedCfg)

	// Verify embedded defaults loaded
	assert.Equal(t, "openai", embeddedCfg.GetString("ai.provider"))
	assert.Equal(t, 4096, embeddedCfg.GetInt("ai.max_tokens"))

	// Now create a file-based config that overrides some values
	dir := t.TempDir()
	userConfig := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(userConfig, []byte(`
ai:
  provider: claude
logging:
  level: debug
`), 0o644))

	fileCfg, err := config.LoadFilesContainer(afero.NewOsFs(), config.WithLogger(logger.NewNoop()), config.WithConfigFiles(userConfig))
	require.NoError(t, err)
	require.NotNil(t, fileCfg)

	// File config overrides what it specifies
	assert.Equal(t, "claude", fileCfg.GetString("ai.provider"))
	assert.Equal(t, "debug", fileCfg.GetString("logging.level"))
}

func TestLoadEnv_DotEnvFileLoaded(t *testing.T) {
	// Cannot use t.Parallel — LoadEnv modifies process environment via gotenv.
	testutil.SkipIfNotIntegration(t, "config")

	dir := t.TempDir()
	dotenv := filepath.Join(dir, ".env")

	// Use a unique env var name to avoid collisions with real environment.
	require.NoError(t, os.WriteFile(dotenv, []byte("GTB_INT_TEST_DOTENV_VAR=from_dotenv\n"), 0o644))
	t.Cleanup(func() { _ = os.Unsetenv("GTB_INT_TEST_DOTENV_VAR") })

	// Use an OsFs rooted at the temp dir to find .env
	fs := afero.NewBasePathFs(afero.NewOsFs(), dir)
	config.LoadEnv(fs, logger.NewNoop())

	assert.Equal(t, "from_dotenv", os.Getenv("GTB_INT_TEST_DOTENV_VAR"))
}

func TestLoad_AllowEmptyConfig(t *testing.T) {
	t.Parallel()
	testutil.SkipIfNotIntegration(t, "config")

	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.yaml")

	t.Run("disallowed_returns_error", func(t *testing.T) {
		t.Parallel()
		_, err := config.Load([]string{missing}, afero.NewOsFs(), false, config.WithLogger(logger.NewNoop()))
		require.Error(t, err)
		assert.ErrorIs(t, err, config.ErrNoFilesFound)
	})

	t.Run("allowed_returns_empty_container", func(t *testing.T) {
		t.Parallel()
		cfg, err := config.Load([]string{missing}, afero.NewOsFs(), true, config.WithLogger(logger.NewNoop()))
		require.NoError(t, err)
		// Returns an empty container (not nil) — no values set
		require.NotNil(t, cfg)
		assert.Empty(t, cfg.GetString("any.key"))
	})
}

func TestLoadFilesContainer_ThreeWayMerge(t *testing.T) {
	t.Parallel()
	testutil.SkipIfNotIntegration(t, "config")

	dir := t.TempDir()
	defaults := filepath.Join(dir, "defaults.yaml")
	project := filepath.Join(dir, "project.yaml")
	local := filepath.Join(dir, "local.yaml")

	require.NoError(t, os.WriteFile(defaults, []byte(`
server:
  port: 8080
  host: 0.0.0.0
logging:
  level: warn
  format: json
database:
  host: localhost
  port: 5432
`), 0o644))

	require.NoError(t, os.WriteFile(project, []byte(`
server:
  port: 3000
logging:
  level: info
database:
  name: myapp
`), 0o644))

	require.NoError(t, os.WriteFile(local, []byte(`
logging:
  level: debug
database:
  host: 127.0.0.1
  password: secret
`), 0o644))

	cfg, err := config.LoadFilesContainer(afero.NewOsFs(), config.WithLogger(logger.NewNoop()), config.WithConfigFiles(defaults, project, local))
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Last writer wins at each level
	assert.Equal(t, 3000, cfg.GetInt("server.port"))              // project overrides defaults
	assert.Equal(t, "0.0.0.0", cfg.GetString("server.host"))      // only in defaults
	assert.Equal(t, "debug", cfg.GetString("logging.level"))      // local overrides project
	assert.Equal(t, "json", cfg.GetString("logging.format"))      // only in defaults
	assert.Equal(t, "127.0.0.1", cfg.GetString("database.host"))  // local overrides defaults
	assert.Equal(t, 5432, cfg.GetInt("database.port"))            // only in defaults
	assert.Equal(t, "myapp", cfg.GetString("database.name"))      // only in project
	assert.Equal(t, "secret", cfg.GetString("database.password")) // only in local
}

func TestLoadFilesContainerWithSchema_ValidationFailure(t *testing.T) {
	t.Parallel()
	testutil.SkipIfNotIntegration(t, "config")

	type ServerConfig struct {
		Port int    `config:"server.port" validate:"required"`
		Host string `config:"server.host" validate:"required"`
	}

	schema, err := config.NewSchema(config.WithStructSchema(ServerConfig{}))
	require.NoError(t, err)

	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")

	// Missing required "server.host" field
	require.NoError(t, os.WriteFile(cfgFile, []byte(`
server:
  port: 8080
`), 0o644))

	_, err = config.LoadFilesContainerWithSchema(afero.NewOsFs(), schema, config.WithLogger(logger.NewNoop()), config.WithConfigFiles(cfgFile))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server.host")
}
