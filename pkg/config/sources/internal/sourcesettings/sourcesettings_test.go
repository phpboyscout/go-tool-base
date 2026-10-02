package sourcesettings

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
)

func TestPollInterval(t *testing.T) {
	t.Parallel()

	for yaml, want := range map[string]time.Duration{"poll_interval: 90s\n": 90 * time.Second, "other: x\n": 0} {
		store, err := config.NewStore(t.Context(), config.WithReaders(config.NamedSource{Name: "s", Content: []byte(yaml)}))
		require.NoError(t, err)

		got, err := PollInterval(store.View())
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}

	store, err := config.NewStore(t.Context(), config.WithReaders(config.NamedSource{Name: "s", Content: []byte("poll_interval: soon\n")}))
	require.NoError(t, err)

	_, err = PollInterval(store.View())
	require.Error(t, err)
}
