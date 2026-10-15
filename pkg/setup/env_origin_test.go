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

// Spec 0209 D7: in an exact Store a variable lands on an existing key's own
// spelling, or on the lower-case dotted form when nothing defines the key,
// and the origin is found under the key the Store produced either way.
func TestEnvVariablesFor_ExactStore(t *testing.T) {
	t.Parallel()

	store, err := config.NewStore(t.Context(),
		config.WithReaders(config.NamedSource{Name: "file", Content: []byte("Log:\n  Level: info\n")}),
		config.WithEnv("APP", config.WithEnviron(func() []string {
			return []string{"APP_LOG_LEVEL=debug", "APP_SERVER_PORT=9090"}
		})),
		config.WithCaseSensitiveKeys(),
	)
	require.NoError(t, err)

	snap := store.View().Snapshot()

	assert.Equal(t, "debug", store.View().GetString("Log.Level"))
	assert.Equal(t, []string{"APP_LOG_LEVEL"}, setup.EnvVariablesFor(snap, "Log.Level"))
	assert.Equal(t, []string{"APP_SERVER_PORT"}, setup.EnvVariablesFor(snap, "server.port"))
	assert.Empty(t, setup.EnvVariablesFor(snap, "log.level"), "another spelling is another key")
}
