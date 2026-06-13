package config_test

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// testDebounce is a small debounce window so hot-reload tests stay fast while
// still exercising the coalescing path. Assertions poll with require.Eventually
// rather than sleeping the debounce exactly.
const testDebounce = 20 * time.Millisecond

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// TestHotReload_MergePreserved asserts that changing file[1] re-merges all
// files and preserves file[0]'s values (the original bug discarded the earlier
// merged layers by replacing the whole map with file[1]).
func TestHotReload_MergePreserved(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	base := filepath.Join(dir, "base.yml")
	over := filepath.Join(dir, "over.yml")

	writeFile(t, base, "base:\n  only: \"from-base\"\nshared: \"base-shared\"\n")
	writeFile(t, over, "shared: \"over-shared\"\nover:\n  only: \"v1\"\n")

	c := config.NewFilesContainer(
		afero.NewOsFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFiles(base, over),
		config.WithReloadDebounce(testDebounce),
	)
	t.Cleanup(func() { _ = c.Close() })

	require.Equal(t, "from-base", c.GetString("base.only"))
	require.Equal(t, "over-shared", c.GetString("shared"))

	var reloaded atomic.Bool

	c.AddObserverFunc(func(config.Containable) error {
		reloaded.Store(true)

		return nil
	})

	time.Sleep(50 * time.Millisecond)

	// Change only the override file.
	writeFile(t, over, "shared: \"over-shared\"\nover:\n  only: \"v2\"\n")

	require.Eventually(t, func() bool {
		return reloaded.Load() && c.GetString("over.only") == "v2"
	}, 3*time.Second, 20*time.Millisecond, "override change should be observed")

	// The critical assertion: file[0]'s value SURVIVES the reload.
	assert.Equal(t, "from-base", c.GetString("base.only"), "base layer must survive a re-merge")
	assert.Equal(t, "over-shared", c.GetString("shared"))
}

// TestHotReload_ValidationRollback asserts that a reload producing a config
// that fails schema validation is rejected: Get* keeps serving the
// last-known-good values and the bad value is never visible.
func TestHotReload_ValidationRollback(t *testing.T) {
	t.Parallel()

	type appConfig struct {
		Level string `config:"log.level" enum:"debug,info,warn,error"`
	}

	schema, err := config.NewSchema(config.WithStructSchema(appConfig{}))
	require.NoError(t, err)

	dir := t.TempDir()
	file := filepath.Join(dir, "config.yml")
	writeFile(t, file, "log:\n  level: \"info\"\n")

	c := config.NewFilesContainer(
		afero.NewOsFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFiles(file),
		config.WithSchema(schema),
		config.WithReloadDebounce(testDebounce),
	)
	t.Cleanup(func() { _ = c.Close() })
	c.SetSchema(schema)

	require.Equal(t, "info", c.GetString("log.level"))

	var observed atomic.Bool

	c.AddObserverFunc(func(config.Containable) error {
		observed.Store(true)

		return nil
	})

	time.Sleep(50 * time.Millisecond)

	// Write an invalid value (not in the enum). The reload must be rejected.
	writeFile(t, file, "log:\n  level: \"not-a-level\"\n")

	// Give the watcher ample time to attempt (and reject) the reload.
	require.Never(t, func() bool {
		return c.GetString("log.level") == "not-a-level"
	}, 1*time.Second, 50*time.Millisecond, "invalid value must never be served")

	// Last-known-good is still served, and observers were not notified of a swap.
	assert.Equal(t, "info", c.GetString("log.level"))
	assert.False(t, observed.Load(), "observers must not fire for a rejected reload")

	// A subsequent VALID reload must still work (the watcher did not stall).
	writeFile(t, file, "log:\n  level: \"warn\"\n")

	require.Eventually(t, func() bool {
		return c.GetString("log.level") == "warn"
	}, 3*time.Second, 20*time.Millisecond, "a valid reload after a rejected one must succeed")
}

// TestHotReload_PartialMergeFailClosed asserts D7: if file[0] is valid but a
// later file is malformed during a reload, the WHOLE reload is rejected and the
// last-known-good config is served. No half-merged state is ever visible.
func TestHotReload_PartialMergeFailClosed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	f0 := filepath.Join(dir, "f0.yml")
	f1 := filepath.Join(dir, "f1.yml")
	f2 := filepath.Join(dir, "f2.yml")

	writeFile(t, f0, "a: \"a0\"\n")
	writeFile(t, f1, "b: \"b0\"\n")
	writeFile(t, f2, "c: \"c0\"\n")

	c := config.NewFilesContainer(
		afero.NewOsFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFiles(f0, f1, f2),
		config.WithReloadDebounce(testDebounce),
	)
	t.Cleanup(func() { _ = c.Close() })

	require.Equal(t, "a0", c.GetString("a"))
	require.Equal(t, "c0", c.GetString("c"))

	var swapped atomic.Bool

	c.AddObserverFunc(func(config.Containable) error {
		swapped.Store(true)

		return nil
	})

	time.Sleep(50 * time.Millisecond)

	// Make f2 malformed YAML; the reload must reject the entire merge.
	writeFile(t, f2, "c: \"c1\"\n  : : not valid : :\n}{\n")

	require.Never(t, func() bool {
		return c.GetString("c") == "c1" || swapped.Load()
	}, 1*time.Second, 50*time.Millisecond, "a malformed later file must reject the whole reload")

	// Last-known-good for every layer is preserved.
	assert.Equal(t, "a0", c.GetString("a"))
	assert.Equal(t, "b0", c.GetString("b"))
	assert.Equal(t, "c0", c.GetString("c"))
}

// TestHotReload_ObserverErrorNoDeadlock asserts that an observer returning an
// error does not stall subsequent reloads (the original unbuffered-channel
// deadlock). The first reload's observer errors; a second reload must still
// fire its observer.
func TestHotReload_ObserverErrorNoDeadlock(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "config.yml")
	writeFile(t, file, "n: \"0\"\n")

	c := config.NewFilesContainer(
		afero.NewOsFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFiles(file),
		config.WithReloadDebounce(testDebounce),
	)
	t.Cleanup(func() { _ = c.Close() })

	var calls atomic.Int64

	c.AddObserverFunc(func(config.Containable) error {
		calls.Add(1)

		return assert.AnError // always returns an error
	})

	time.Sleep(50 * time.Millisecond)

	writeFile(t, file, "n: \"1\"\n")

	require.Eventually(t, func() bool {
		return calls.Load() >= 1 && c.GetString("n") == "1"
	}, 3*time.Second, 20*time.Millisecond, "first reload should fire the observer")

	first := calls.Load()

	// A second save must still trigger a reload despite the prior error.
	writeFile(t, file, "n: \"2\"\n")

	require.Eventually(t, func() bool {
		return calls.Load() > first && c.GetString("n") == "2"
	}, 3*time.Second, 20*time.Millisecond, "an observer error must not stall subsequent reloads")
}

// TestHotReload_PrimaryFileRemovedFailClosed asserts that if file[0] vanishes
// during a reload (ErrConfigFileNotFound contract), the reload is rejected and
// the last-known-good config is retained.
func TestHotReload_PrimaryFileRemovedFailClosed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	f0 := filepath.Join(dir, "f0.yml")
	f1 := filepath.Join(dir, "f1.yml")
	writeFile(t, f0, "a: \"a0\"\n")
	writeFile(t, f1, "b: \"b0\"\n")

	c := config.NewFilesContainer(
		afero.NewOsFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFiles(f0, f1),
		config.WithReloadDebounce(testDebounce),
	)
	t.Cleanup(func() { _ = c.Close() })

	require.Equal(t, "a0", c.GetString("a"))

	time.Sleep(50 * time.Millisecond)

	// Removing the primary file makes the candidate build fail-closed.
	require.NoError(t, os.Remove(f0))
	// Touch f1 to trigger a reload attempt.
	writeFile(t, f1, "b: \"b1\"\n")

	require.Never(t, func() bool {
		return c.GetString("b") == "b1"
	}, 1*time.Second, 50*time.Millisecond, "reload must be rejected when primary file is gone")

	assert.Equal(t, "a0", c.GetString("a"), "last-known-good retained")
	assert.Equal(t, "b0", c.GetString("b"))
}

// TestHotReload_EnvPrefixPreservedAcrossReload asserts the candidate viper is
// built with the same env prefix as the original, so prefixed env vars still
// resolve after a reload.
func TestHotReload_EnvPrefixPreservedAcrossReload(t *testing.T) {
	dir := t.TempDir()
	f0 := filepath.Join(dir, "f0.yml")
	f1 := filepath.Join(dir, "f1.yml")
	writeFile(t, f0, "service:\n  name: \"file\"\n")
	writeFile(t, f1, "other: \"v1\"\n")

	t.Setenv("GTBTEST_SERVICE_PORT", "9999")

	c := config.NewFilesContainer(
		afero.NewOsFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithEnvPrefix("GTBTEST"),
		config.WithConfigFiles(f0, f1),
		config.WithReloadDebounce(testDebounce),
	)
	t.Cleanup(func() { _ = c.Close() })

	require.Equal(t, 9999, c.GetInt("service.port"))

	var reloaded atomic.Bool

	c.AddObserverFunc(func(config.Containable) error {
		reloaded.Store(true)

		return nil
	})

	time.Sleep(50 * time.Millisecond)

	writeFile(t, f1, "other: \"v2\"\n")

	require.Eventually(t, func() bool {
		return reloaded.Load() && c.GetString("other") == "v2"
	}, 3*time.Second, 20*time.Millisecond)

	// Env prefix still resolves after the swap.
	assert.Equal(t, 9999, c.GetInt("service.port"))
}

// TestHotReload_CloseIdempotent asserts Close can be called more than once and
// on a non-watching container without panicking.
func TestHotReload_CloseIdempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	f := filepath.Join(dir, "c.yml")
	writeFile(t, f, "k: \"v\"\n")

	c := config.NewFilesContainer(
		afero.NewOsFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFiles(f),
		config.WithReloadDebounce(testDebounce),
	)

	require.NoError(t, c.Close())
	require.NoError(t, c.Close())
}

// TestHotReload_RaceConcurrentObservers exercises the watcher under -race with
// concurrent AddObserver registration during active reloads over a multi-file
// (merged) container.
func TestHotReload_RaceConcurrentObservers(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	f0 := filepath.Join(dir, "f0.yml")
	f1 := filepath.Join(dir, "f1.yml")
	writeFile(t, f0, "base: \"b\"\nn: \"0\"\n")
	writeFile(t, f1, "n: \"0\"\n")

	c := config.NewFilesContainer(
		afero.NewOsFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFiles(f0, f1),
		config.WithReloadDebounce(testDebounce),
	)
	t.Cleanup(func() { _ = c.Close() })

	var wg sync.WaitGroup

	stop := make(chan struct{})

	// Concurrently register observers while reloads are happening.
	wg.Add(1)

	go func() {
		defer wg.Done()

		for {
			select {
			case <-stop:
				return
			default:
				c.AddObserverFunc(func(cc config.Containable) error {
					_ = cc.GetString("n")

					return nil
				})

				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	// Concurrently read while reloads are happening.
	wg.Add(1)

	go func() {
		defer wg.Done()

		for {
			select {
			case <-stop:
				return
			default:
				_ = c.GetString("n")
				_ = c.GetString("base")

				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	// Drive several reloads by rewriting the merged file.
	for i := 1; i <= 5; i++ {
		writeFile(t, f1, "n: \""+time.Now().Format("150405.000000000")+"\"\n")
		time.Sleep(2 * testDebounce)
	}

	close(stop)
	wg.Wait()

	// base layer must still be intact after the churn.
	assert.Equal(t, "b", c.GetString("base"))
}
