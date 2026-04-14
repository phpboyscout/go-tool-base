package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phpboyscout/go-tool-base/pkg/logger"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/config"
)

type TestObserver struct {
	handler func(config.Containable, chan error)
}

func (o TestObserver) Run(c config.Containable, errs chan error) {
	o.handler(c, errs)
}

// TestContainer_AddObserver provides a convoluted test for triggering multiple observers of filesystem changes.
func TestContainer_AddObserver(t *testing.T) {
	t.Parallel()
	logger := logger.NewNoop()

	t.Run("with single config file", func(t *testing.T) {
		t.Parallel()
		// Use t.TempDir() to avoid polluting /tmp and ensure cleanup
		tmpDir := t.TempDir()
		filename := filepath.Join(tmpDir, "config.yml")

		// Create initial file
		err := os.WriteFile(filename, []byte(firstMockFilesYaml), 0644)
		require.NoError(t, err)

		// Must use OsFs because Viper's WatchConfig relies on fsnotify which requires real filesystem events.
		// MemMapFs does not support this.
		c := config.NewFilesContainer(afero.NewOsFs(), config.WithLogger(logger), config.WithConfigFiles(filename))
		origValue := c.GetString("yaml.key")
		observed := 0

		observeFunc := func(c config.Containable, errors chan error) {
			observed++
			newValue := c.GetString("yaml.key")
			// t.Logf("observed = %d, origValue = %s, newValue = %s", observed, origValue, newValue)
			if origValue == newValue {
				t.Fail()
			}
		}

		c.AddObserver(TestObserver{observeFunc})
		c.AddObserverFunc(observeFunc)

		// Give watcher time to start? Viper WatchConfig is usually async.
		time.Sleep(100 * time.Millisecond)

		// Update file
		err = os.WriteFile(filename, []byte(secondMockFilesYaml), 0644)
		require.NoError(t, err)

		time.Sleep(1 * time.Second)

		assert.Len(t, c.GetObservers(), 2)

		if observed >= 2 && observed%len(c.GetObservers()) != 0 {
			// fsnotify can at times trigger multiple times, so the test accounts for this by testing
			// for the modulus of observations to the number of observers
			t.Errorf("Expected 2 observations, Observed: %d", observed)
		}
	})
}

// func TestContainer_Dump(t *testing.T) {
// 	t.Parallel()

// 	l := logger.NewNoop()
// 	c := NewReaderContainer(l, "yaml", strings.NewReader(firstMockFilesYaml))
// 	c.Dump()
// }

func TestContainer_Sub(t *testing.T) {
	t.Parallel()

	l := logger.NewNoop()
	c := config.NewReaderContainer(afero.NewMemMapFs(), config.WithLogger(l), config.WithConfigFormat("yaml"), config.WithConfigReaders(strings.NewReader(secondMockFilesYaml)))
	s := c.Sub("yaml.more")

	assert.Equal(t, "secondfile", s.GetString("key2"))

}

func TestContainer_GetViper(t *testing.T) {
	t.Parallel()

	l := logger.NewNoop()
	c := config.NewReaderContainer(afero.NewMemMapFs(), config.WithLogger(l), config.WithConfigFormat("yaml"), config.WithConfigReaders(strings.NewReader(firstMockFilesYaml)))
	v := c.GetViper()

	assert.Equal(t, "value", v.GetString("yaml.key"))

}
