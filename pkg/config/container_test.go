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

// TestContainer_Sub_PreservesEnvBinding covers the regression
// guarded by the env-aware Sub implementation: calling GetString on
// a sub-container must still pick up prefixed env vars that Viper's
// AutomaticEnv wired to the parent. A previous implementation used
// Viper's native Sub and lost the env binding silently.
//
// Sub() retains Viper's "nil on truly-missing" semantic, so the
// test supplies a minimal YAML stub that gives the prefix structural
// presence. Env-bound values then surface through the sub's Get
// methods because they are qualified back onto the root's Viper.
func TestContainer_Sub_PreservesEnvBinding(t *testing.T) {
	t.Setenv("GTB_GITHUB_AUTH_VALUE", "env-bound-token")

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFormat("yaml"),
		// minimal structure so Viper's Sub returns non-nil
		config.WithConfigReaders(strings.NewReader("github:\n  placeholder: x\n")),
		config.WithEnvPrefix("GTB"),
	)

	sub := c.Sub("github")
	assert.NotNil(t, sub)
	assert.Equal(t, "env-bound-token", sub.GetString("auth.value"),
		"sub-container must route Get through the root's Viper so AutomaticEnv fires")
	assert.True(t, sub.IsSet("auth.value"),
		"sub-container IsSet must also see env-bound values")
}

// TestContainer_Sub_NestedPreservesEnvBinding exercises the
// prefix-accumulation path: cfg.Sub("a").Sub("b") must qualify
// lookups with the full "a.b.<key>" path when delegating to the
// root, not just the last segment.
func TestContainer_Sub_NestedPreservesEnvBinding(t *testing.T) {
	t.Setenv("GTB_BITBUCKET_AUTH_TOKEN", "nested-env-token")

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader("bitbucket:\n  auth:\n    placeholder: x\n")),
		config.WithEnvPrefix("GTB"),
	)

	bitbucket := c.Sub("bitbucket")
	assert.NotNil(t, bitbucket)

	auth := bitbucket.Sub("auth")
	assert.NotNil(t, auth)
	assert.Equal(t, "nested-env-token", auth.GetString("token"),
		"nested Sub must accumulate the full prefix")
}

func TestContainer_GetViper(t *testing.T) {
	t.Parallel()

	l := logger.NewNoop()
	c := config.NewReaderContainer(afero.NewMemMapFs(), config.WithLogger(l), config.WithConfigFormat("yaml"), config.WithConfigReaders(strings.NewReader(firstMockFilesYaml)))
	v := c.GetViper()

	assert.Equal(t, "value", v.GetString("yaml.key"))

}
