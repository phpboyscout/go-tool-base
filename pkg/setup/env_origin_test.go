package setup_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func TestEnvVariablesFor(t *testing.T) {
	t.Parallel()

	store, err := config.NewStore(t.Context(),
		config.WithReaders(
			config.NamedSource{Name: "defaults", Content: []byte("log:\n  level: info\nannounce:\n  webhook: https://hooks.internal/a\n")},
			config.NamedSource{Name: "file", Content: []byte("server:\n  port: 8080\n")},
		),
		config.WithEnv("APP", config.WithEnviron(func() []string {
			return []string{"APP_LOG_LEVEL=debug", "APP_ANNOUNCE_FLAGS=", "APP_ANNOUNCE_WHEN=always", "OTHER_ANNOUNCE_X=1"}
		})),
	)
	require.NoError(t, err)

	snap := store.View().Snapshot()

	tests := []struct {
		name, key string
		want      []string
	}{
		{"a scalar the environment supplied", "log.level", []string{"APP_LOG_LEVEL"}},
		{"a block one variable reached into, sorted", "announce", []string{"APP_ANNOUNCE_FLAGS", "APP_ANNOUNCE_WHEN"}},
		{"the colophon scalar", "announce.flags", []string{"APP_ANNOUNCE_FLAGS"}},
		{"a key from a file", "server.port", nil},
		{"a key from defaults only", "announce.webhook", nil},
		{"a key nothing defines", "missing.key", nil},
		{"a sibling sharing a prefix but not a dot boundary", "announ", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, setup.EnvVariablesFor(snap, tt.key))
		})
	}
}
