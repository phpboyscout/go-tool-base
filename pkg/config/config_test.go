package config_test

import (
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
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

func TestLoadFilesContainer_NoFiles(t *testing.T) {
	t.Parallel()
	_, err := config.LoadFilesContainer(logger.NewNoop(), afero.NewMemMapFs())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no config files specified")
}

func TestLoadFilesContainer_NonExistentFile(t *testing.T) {
	t.Parallel()
	c, err := config.LoadFilesContainer(logger.NewNoop(), afero.NewMemMapFs(), "/does/not/exist.yaml")
	require.NoError(t, err)
	assert.Nil(t, c)
}

func TestLoadFilesContainer_ExistingFile(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "cfg.yaml", []byte("key: hello"), 0o644))
	c, err := config.LoadFilesContainer(logger.NewNoop(), fs, "cfg.yaml")
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, "hello", c.GetString("key"))
}

func TestContainer_IsSet_Set(t *testing.T) {
	t.Parallel()
	c := config.NewReaderContainer(logger.NewNoop(), "yaml", strings.NewReader("foo: bar"))
	assert.True(t, c.IsSet("foo"))
	assert.False(t, c.IsSet("nonexistent"))
	c.Set("newkey", "newval")
	assert.True(t, c.IsSet("newkey"))
	assert.Equal(t, "newval", c.GetString("newkey"))
}

func TestContainer_WriteConfigAs(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	c := config.NewFilesContainer(logger.NewNoop(), fs)
	c.Set("written", "yes")
	tmpName := "/tmp/test-write-cfg.yaml"
	require.NoError(t, c.WriteConfigAs(tmpName))
	contents, err := afero.ReadFile(fs, tmpName)
	require.NoError(t, err)
	assert.Contains(t, string(contents), "written")
}

func TestContainer_Sub_Nil(t *testing.T) {
	t.Parallel()
	c := config.NewReaderContainer(logger.NewNoop(), "yaml", strings.NewReader("foo: bar"))
	sub := c.Sub("nonexistent")
	assert.Nil(t, sub)
}

func TestContainer_ToJSON_Dump(t *testing.T) {
	t.Parallel()
	c := config.NewReaderContainer(logger.NewNoop(), "yaml", strings.NewReader("name: myapp\nversion: 1"))
	j := c.ToJSON()
	assert.Contains(t, j, "myapp")
	var buf strings.Builder
	c.Dump(&buf)
	assert.Contains(t, buf.String(), "myapp")
}

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
