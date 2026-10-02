package azuresource_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/azuresource"
)

// The credential is resolved without contacting Azure, so building a source
// never needs the network until its first read.
func TestCredential_ResolvesWithoutTheNetwork(t *testing.T) {
	t.Setenv("AZURE_TENANT_ID", "")

	store, err := config.NewStore(t.Context(), config.WithReaders(config.NamedSource{Name: "s", Content: []byte("tenant_id: 00000000-0000-0000-0000-000000000000\n")}))
	require.NoError(t, err)

	cred, err := azuresource.Credential(t.Context(), store.View())
	require.NoError(t, err)
	assert.NotNil(t, cred)
}
