package awssource

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
)

func TestChainName(t *testing.T) {
	t.Parallel()

	for yaml, want := range map[string]string{
		"profile: ops\n": "AWS profile ops",
		"region: eu-1\n": "the AWS credential chain",
	} {
		store, err := config.NewStore(t.Context(), config.WithReaders(config.NamedSource{Name: "s", Content: []byte(yaml)}))
		require.NoError(t, err)
		assert.Equal(t, want, ChainName(store.View()))
	}
}
