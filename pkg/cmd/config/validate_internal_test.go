package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cfg "gitlab.com/phpboyscout/go/config"
)

func TestEnvOriginSuffix(t *testing.T) {
	t.Parallel()

	store, err := cfg.NewStore(t.Context(), cfg.WithEnv("TOOLTEST", cfg.WithEnviron(func() []string {
		return []string{"TOOLTEST_ANNOUNCE_WHEN=always", "TOOLTEST_ANNOUNCE_FLAGS="}
	})))
	require.NoError(t, err)

	snap := store.View().Snapshot()

	assert.Equal(t, " (from environment variables TOOLTEST_ANNOUNCE_FLAGS, TOOLTEST_ANNOUNCE_WHEN)", envOriginSuffix(snap, "announce"))
	assert.Equal(t, " (from environment variable TOOLTEST_ANNOUNCE_WHEN)", envOriginSuffix(snap, "announce.when"))
	assert.Empty(t, envOriginSuffix(snap, "log.level"))
	assert.Empty(t, envOriginSuffix(snap, ""), "a whole-config error has no key to trace")
}
