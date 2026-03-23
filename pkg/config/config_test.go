package config_test

import (
	"strings"
	"testing"

	"github.com/phpboyscout/gtb/pkg/logger"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/gtb/pkg/config"
)

var firstMockFilesYaml = `yaml:
  key: "value"
  bool: true
  int: 1
  float: 2.4
  time: "2021-09-11 12:34:56"
  duration: 5s`

var secondMockFilesYaml = `yaml:
  key: "value2"
  more:
    key2: "secondfile"`

func TestNewFilesContainer(t *testing.T) {
	t.Parallel()
	logger := logger.NewNoop()

	t.Run("with single config file", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		err := afero.WriteFile(fs, "first.yml", []byte(firstMockFilesYaml), 0o644)
		require.NoError(t, err)

		c := config.NewFilesContainer(logger, fs, "first.yml")
		value := c.GetString("yaml.key")
		assert.Equal(t, "value", value)
	})

	t.Run("with multiple config file", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		err := afero.WriteFile(fs, "first.yml", []byte(firstMockFilesYaml), 0o644)
		require.NoError(t, err)

		err = afero.WriteFile(fs, "second.yml", []byte(secondMockFilesYaml), 0o644)
		require.NoError(t, err)

		c := config.NewFilesContainer(logger, fs, "first.yml", "second.yml")
		value := c.GetString("yaml.key")
		assert.Equal(t, "value2", value)

		value = c.GetString("yaml.more.key2")
		assert.Equal(t, "secondfile", value)
	})
}

func TestNewReaderContainer(t *testing.T) {
	t.Parallel()
	logger := logger.NewNoop()

	t.Run("with single config reader", func(t *testing.T) {
		t.Parallel()
		r := strings.NewReader(firstMockFilesYaml)
		c := config.NewReaderContainer(logger, "yaml", r)

		value := c.GetString("yaml.key")
		assert.Equal(t, "value", value)
	})

	t.Run("with multiple config readers", func(t *testing.T) {
		t.Parallel()
		c := config.NewReaderContainer(
			logger,
			"yaml",
			strings.NewReader(firstMockFilesYaml),
			strings.NewReader(secondMockFilesYaml),
		)

		value := c.GetString("yaml.key")
		assert.Equal(t, "value2", value)

		value = c.GetString("yaml.more.key2")
		assert.Equal(t, "secondfile", value)
	})
}
