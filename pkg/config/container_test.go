package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
)

type TestObserver struct {
	handler func(config.Containable) error
}

func (o TestObserver) Run(c config.Containable) error {
	return o.handler(c)
}

func TestContainer_ConfigFiles(t *testing.T) {
	t.Parallel()
	log := logger.NewNoop()

	t.Run("empty for reader containers", func(t *testing.T) {
		t.Parallel()
		c := config.NewReaderContainer(afero.NewMemMapFs(), config.WithLogger(logger.ToSlog(log)),
			config.WithConfigFormat("yaml"), config.WithConfigReaders(strings.NewReader("foo: bar")))
		assert.Empty(t, c.ConfigFiles())
	})

	t.Run("returns loaded files in merge order", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "first.yaml", []byte("key: a"), 0o644))
		require.NoError(t, afero.WriteFile(fs, "second.yaml", []byte("key: b"), 0o644))
		c, err := config.LoadFilesContainer(fs, config.WithLogger(logger.ToSlog(log)),
			config.WithConfigFiles("first.yaml", "second.yaml"))
		require.NoError(t, err)

		assert.Equal(t, []string{"first.yaml", "second.yaml"}, c.ConfigFiles())
	})

	t.Run("returns a copy callers cannot mutate", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "cfg.yaml", []byte("key: a"), 0o644))
		c, err := config.LoadFilesContainer(fs, config.WithLogger(logger.ToSlog(log)),
			config.WithConfigFiles("cfg.yaml"))
		require.NoError(t, err)

		files := c.ConfigFiles()
		require.Len(t, files, 1)
		files[0] = "mutated.yaml"

		assert.Equal(t, []string{"cfg.yaml"}, c.ConfigFiles())
	})
}

// TestContainer_AddObserver asserts that a single-file container actually
// observes a change (D5: the watcher must fire for single-file containers, not
// only multi-file ones). It uses an atomic int64 observation counter and polls
// for observed > 0 rather than asserting an exact count, because fsnotify can
// coalesce or repeat events.
func TestContainer_AddObserver(t *testing.T) {
	t.Parallel()
	log := logger.NewNoop()

	t.Run("with single config file", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		filename := filepath.Join(tmpDir, "config.yml")

		require.NoError(t, os.WriteFile(filename, []byte(firstMockFilesYaml), 0o644))

		// OsFs is required: fsnotify needs real filesystem events; MemMapFs
		// cannot deliver them.
		c := config.NewFilesContainer(
			afero.NewOsFs(),
			config.WithLogger(logger.ToSlog(log)),
			config.WithConfigFiles(filename),
			config.WithReloadDebounce(20*time.Millisecond),
		)
		t.Cleanup(func() { _ = c.Close() })

		origValue := c.GetString("yaml.key")

		var observed atomic.Int64

		observeFunc := func(cc config.Containable) error {
			if cc.GetString("yaml.key") != origValue {
				observed.Add(1)
			}

			return nil
		}

		c.AddObserver(TestObserver{observeFunc})
		c.AddObserverFunc(observeFunc)

		// Give the watcher goroutine time to start.
		time.Sleep(50 * time.Millisecond)

		require.NoError(t, os.WriteFile(filename, []byte(secondMockFilesYaml), 0o644))

		require.Eventually(t, func() bool {
			return observed.Load() > 0
		}, 3*time.Second, 20*time.Millisecond, "single-file container should observe the change")

		assert.Len(t, c.GetObservers(), 2)
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
	c := config.NewReaderContainer(afero.NewMemMapFs(), config.WithLogger(logger.ToSlog(l)), config.WithConfigFormat("yaml"), config.WithConfigReaders(strings.NewReader(secondMockFilesYaml)))
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
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
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
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
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
	c := config.NewReaderContainer(afero.NewMemMapFs(), config.WithLogger(logger.ToSlog(l)), config.WithConfigFormat("yaml"), config.WithConfigReaders(strings.NewReader(firstMockFilesYaml)))
	v := c.GetViper()

	assert.Equal(t, "value", v.GetString("yaml.key"))

}
