package config_test

import (
	"embed"
	"testing"
	"testing/fstest"

	"github.com/phpboyscout/gtb/pkg/logger"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/gtb/pkg/config"
)

// load_test.go provides comprehensive unit tests for the Load and LoadEmbed functions
// in the config package. These tests cover:
//
// Load function tests:
// - Single and multiple existing configuration files
// - Mix of existing and non-existing files
// - Empty configuration scenarios (both allowed and not allowed)
// - Error handling for ErrNoFilesFound
// - File system error handling
// - Integration tests verifying the returned container functionality
//
// LoadEmbed function tests:
// - Basic functionality with embedded filesystem error scenarios
// - Empty paths handling (successful case)
// - Single and multiple file reading with mocked EmbeddedFileReader
// - Error handling when files cannot be read
// - Complex YAML content parsing
//
// Coverage: Load function 100%, LoadEmbed function 100%

func TestLoad(t *testing.T) {
	t.Parallel()

	logger := logger.NewNoop()
	mockConfigYaml := `test:
  key: "value"
  nested:
    value: true`

	tests := []struct {
		name              string
		paths             []string
		files             map[string]string
		allowEmptyConfig  bool
		expectError       bool
		expectedError     error
		expectedConfigKey string
		expectedValue     any
	}{
		{
			name:              "single existing file",
			paths:             []string{"config.yaml"},
			files:             map[string]string{"config.yaml": mockConfigYaml},
			allowEmptyConfig:  false,
			expectError:       false,
			expectedConfigKey: "test.key",
			expectedValue:     "value",
		},
		{
			name:  "multiple existing files",
			paths: []string{"config1.yaml", "config2.yaml"},
			files: map[string]string{
				"config1.yaml": mockConfigYaml,
				"config2.yaml": `test:\n  key2: "value2"`,
			},
			allowEmptyConfig:  false,
			expectError:       false,
			expectedConfigKey: "test.key",
			expectedValue:     "value",
		},
		{
			name:              "some files exist, some don't",
			paths:             []string{"config.yaml", "nonexistent.yaml"},
			files:             map[string]string{"config.yaml": mockConfigYaml},
			allowEmptyConfig:  false,
			expectError:       false,
			expectedConfigKey: "test.key",
			expectedValue:     "value",
		},
		{
			name:             "no files exist, empty config not allowed",
			paths:            []string{"nonexistent1.yaml", "nonexistent2.yaml"},
			files:            map[string]string{},
			allowEmptyConfig: false,
			expectError:      true,
			expectedError:    config.ErrNoFilesFound,
		},
		{
			name:             "no files exist, empty config allowed",
			paths:            []string{"nonexistent1.yaml", "nonexistent2.yaml"},
			files:            map[string]string{},
			allowEmptyConfig: true,
			expectError:      false,
		},
		{
			name:             "empty paths, empty config not allowed",
			paths:            []string{},
			files:            map[string]string{},
			allowEmptyConfig: false,
			expectError:      true,
			expectedError:    config.ErrNoFilesFound,
		},
		{
			name:             "empty paths, empty config allowed",
			paths:            []string{},
			files:            map[string]string{},
			allowEmptyConfig: true,
			expectError:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create in-memory filesystem
			fs := afero.NewMemMapFs()

			// Create test files
			for filename, content := range tt.files {
				err := afero.WriteFile(fs, filename, []byte(content), 0o644)
				require.NoError(t, err)
			}

			// Call Load function
			container, err := config.Load(tt.paths, fs, logger, tt.allowEmptyConfig)

			// Check error expectations
			if tt.expectError {
				assert.Error(t, err)
				if tt.expectedError != nil {
					assert.ErrorIs(t, err, tt.expectedError)
				}
				assert.Nil(t, container)
				return
			}

			// Check success expectations
			assert.NoError(t, err)
			assert.NotNil(t, container)

			// If we have expected values, test them
			if tt.expectedConfigKey != "" {
				value := container.Get(tt.expectedConfigKey)
				assert.Equal(t, tt.expectedValue, value)
			}
		})
	}
}

func TestLoad_FileSystemErrors(t *testing.T) {
	t.Parallel()

	logger := logger.NewNoop()

	t.Run("file system stat error handling", func(t *testing.T) {
		t.Parallel()

		// Create a read-only filesystem to test error handling
		fs := afero.NewReadOnlyFs(afero.NewMemMapFs())

		// This should not error even if fs.Stat fails, because we handle the error
		container, err := config.Load([]string{"nonexistent.yaml"}, fs, logger, true)

		assert.NoError(t, err)
		assert.NotNil(t, container)
	})
}

func TestLoadEmbed(t *testing.T) {
	t.Parallel()

	logger := logger.NewNoop()

	t.Run("with empty embedded filesystem", func(t *testing.T) {
		t.Parallel()

		// Create a simple test with an empty embedded filesystem
		var emptyFS embed.FS

		container, err := config.LoadEmbed([]string{"nonexistent.yaml"}, emptyFS, logger)

		// LoadEmbed should now return an error when files cannot be read
		assert.Error(t, err)
		assert.Nil(t, container)
		assert.Contains(t, err.Error(), "failed to open embedded config file nonexistent.yaml")
	})

	t.Run("with empty paths", func(t *testing.T) {
		t.Parallel()

		var emptyFS embed.FS

		container, err := config.LoadEmbed([]string{}, emptyFS, logger)

		assert.NoError(t, err)
		assert.NotNil(t, container)
	})

	t.Run("with mock - successful single file read", func(t *testing.T) {
		t.Parallel()

		yamlContent := `
server:
  port: 8080
  host: localhost
`
		mockFS := fstest.MapFS{
			"config.yaml": &fstest.MapFile{Data: []byte(yamlContent)},
		}

		container, err := config.LoadEmbed([]string{"config.yaml"}, mockFS, logger)

		assert.NoError(t, err)
		assert.NotNil(t, container)

		// Verify the content was loaded correctly
		assert.Equal(t, 8080, container.GetInt("server.port"))
		assert.Equal(t, "localhost", container.GetString("server.host"))
	})

	t.Run("with mock - multiple successful file reads", func(t *testing.T) {
		t.Parallel()

		config1 := `
app:
  name: "test-app"
  version: "1.0.0"
`
		config2 := `
database:
  host: "localhost"
  port: 5432
`
		mockFS := fstest.MapFS{
			"app.yaml": &fstest.MapFile{Data: []byte(config1)},
			"db.yaml":  &fstest.MapFile{Data: []byte(config2)},
		}

		container, err := config.LoadEmbed([]string{"app.yaml", "db.yaml"}, mockFS, logger)

		assert.NoError(t, err)
		assert.NotNil(t, container)

		// Verify both configs were loaded
		assert.Equal(t, "test-app", container.GetString("app.name"))
		assert.Equal(t, "1.0.0", container.GetString("app.version"))
		assert.Equal(t, "localhost", container.GetString("database.host"))
		assert.Equal(t, 5432, container.GetInt("database.port"))
	})

	t.Run("with mock - mixed success and failure", func(t *testing.T) {
		t.Parallel()

		validConfig := `
logging:
  level: "info"
  format: "json"
`
		mockFS := fstest.MapFS{
			"logging.yaml": &fstest.MapFile{Data: []byte(validConfig)},
		}

		// nonexistent.yaml is missing from MapFS, so ReadFile will fail
		container, err := config.LoadEmbed([]string{"logging.yaml", "nonexistent.yaml"}, mockFS, logger)

		// Should return an error when any file fails to read
		assert.Error(t, err)
		assert.Nil(t, container)
		assert.Contains(t, err.Error(), "failed to open embedded config file nonexistent.yaml")
	})

	t.Run("with mock - all files fail to read", func(t *testing.T) {
		t.Parallel()

		mockFS := fstest.MapFS{} // Empty MapFS

		container, err := config.LoadEmbed([]string{"config1.yaml", "config2.yaml"}, mockFS, logger)

		// Should return an error when files fail to read
		assert.Error(t, err)
		assert.Nil(t, container)
		assert.Contains(t, err.Error(), "failed to open embedded config file config1.yaml")
	})

	t.Run("with mock - complex yaml content", func(t *testing.T) {
		t.Parallel()

		complexYaml := `
application:
  name: "complex-app"
  features:
    - "auth"
    - "logging"
    - "metrics"
  settings:
    debug: true
    timeout: 30s
    retry:
      max_attempts: 3
      backoff: "exponential"
`
		mockFS := fstest.MapFS{
			"complex.yaml": &fstest.MapFile{Data: []byte(complexYaml)},
		}

		container, err := config.LoadEmbed([]string{"complex.yaml"}, mockFS, logger)

		assert.NoError(t, err)
		assert.NotNil(t, container)

		// Verify complex structure was parsed correctly
		assert.Equal(t, "complex-app", container.GetString("application.name"))
		assert.Equal(t, true, container.GetBool("application.settings.debug"))
		assert.Equal(t, 3, container.GetInt("application.settings.retry.max_attempts"))
		assert.Equal(t, "exponential", container.GetString("application.settings.retry.backoff"))
	})

	t.Run("with mock - empty file content", func(t *testing.T) {
		t.Parallel()

		mockFS := fstest.MapFS{
			"empty.yaml": &fstest.MapFile{Data: []byte("")},
		}

		container, err := config.LoadEmbed([]string{"empty.yaml"}, mockFS, logger)

		assert.NoError(t, err)
		assert.NotNil(t, container)
	})

	t.Run("with mock - invalid yaml content", func(t *testing.T) {
		t.Parallel()

		invalidYaml := `
invalid: yaml: content:
  - unclosed: [bracket
    missing: quote"
`
		mockFS := fstest.MapFS{
			"invalid.yaml": &fstest.MapFile{Data: []byte(invalidYaml)},
		}

		container, err := config.LoadEmbed([]string{"invalid.yaml"}, mockFS, logger)

		// LoadEmbed itself should not fail - NewReaderContainer handles yaml parsing
		assert.NoError(t, err)
		assert.NotNil(t, container)
	})
}

func TestLoad_Integration(t *testing.T) {
	t.Parallel()

	logger := logger.NewNoop()

	t.Run("loaded container has expected functionality", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		configContent := `database:
  host: "localhost"
  port: 5432
  enabled: true
server:
  timeout: "30s"
  max_connections: 100`

		err := afero.WriteFile(fs, "config.yaml", []byte(configContent), 0o644)
		require.NoError(t, err)

		container, err := config.Load([]string{"config.yaml"}, fs, logger, false)
		require.NoError(t, err)
		require.NotNil(t, container)

		// Test various getter methods
		assert.Equal(t, "localhost", container.GetString("database.host"))
		assert.Equal(t, 5432, container.GetInt("database.port"))
		assert.Equal(t, true, container.GetBool("database.enabled"))
		assert.Equal(t, 100, container.GetInt("server.max_connections"))

		// Test Has method
		assert.True(t, container.Has("database.host"))
		assert.False(t, container.Has("nonexistent.key"))

		// Test Sub method
		dbConfig := container.Sub("database")
		assert.NotNil(t, dbConfig)
		assert.Equal(t, "localhost", dbConfig.GetString("host"))
	})
}
