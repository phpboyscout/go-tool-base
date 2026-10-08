package generator

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// A generated GitHub project still carries releaser-pleaser's workflow from
// before #110. Left in place beside colophon's, both would run on every push
// to main, so regenerate removes it when it is still what the generator
// wrote, and leaves an edited one for its author.
func TestRetireSkeletonFiles(t *testing.T) {
	t.Parallel()

	const rp = ".github/workflows/releaser-pleaser.yaml"

	setup := func(t *testing.T, onDisk string, log logger.Logger) (*Generator, afero.Fs) {
		t.Helper()

		fs := afero.NewMemMapFs()
		g := New(&props.Props{FS: fs, Logger: log}, &Config{Path: "/work"})
		require.NoError(t, afero.WriteFile(fs, "/work/"+rp, []byte(onDisk), 0o644))

		return g, fs
	}

	t.Run("an untouched workflow is removed with its hash", func(t *testing.T) {
		t.Parallel()

		stored := map[string]string{rp: calculateHash([]byte("generated\n")), "justfile": "abc"}
		g, fs := setup(t, "generated\n", logger.NewNoop())

		g.retireSkeletonFiles(stored)

		exists, _ := afero.Exists(fs, "/work/"+rp)
		assert.False(t, exists)
		assert.NotContains(t, stored, rp)
		assert.Contains(t, stored, "justfile", "other hashes are untouched")
	})

	t.Run("an edited workflow stays, and the author is told", func(t *testing.T) {
		t.Parallel()

		stored := map[string]string{rp: calculateHash([]byte("generated\n"))}
		log := logger.NewBuffer()
		g, fs := setup(t, "generated\nplus my change\n", log)

		g.retireSkeletonFiles(stored)

		exists, _ := afero.Exists(fs, "/work/"+rp)
		assert.True(t, exists)
		assert.True(t, log.ContainsLevel(logger.WarnLevel, rp))
	})

	t.Run("a file the generator has no record of stays, and the author is told", func(t *testing.T) {
		t.Parallel()

		stored := map[string]string{}
		log := logger.NewBuffer()
		g, fs := setup(t, "someone's own workflow\n", log)

		g.retireSkeletonFiles(stored)

		exists, _ := afero.Exists(fs, "/work/"+rp)
		assert.True(t, exists)
		assert.True(t, log.ContainsLevel(logger.WarnLevel, rp))
	})
}
