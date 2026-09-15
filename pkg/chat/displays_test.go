package chat

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gochat "gitlab.com/phpboyscout/go/chat"
)

// TestProviderDisplays_CoverEveryModuleProvider pins spec 0196 D7: one table
// labels every provider the module table knows, so init ai, docs ask's help
// and the wizard read the same names and glosses.
func TestProviderDisplays_CoverEveryModuleProvider(t *testing.T) {
	t.Parallel()

	displays := ProviderDisplays()
	require.Len(t, displays, len(ProviderModules()))

	for i, entry := range ProviderModules() {
		d := displays[i]
		assert.Equal(t, entry.Provider, d.ID, "display order follows the module table")
		assert.NotEmpty(t, d.Label, "%s has a label", entry.Provider)
		assert.NotEmpty(t, d.Gloss, "%s has a gloss", entry.Provider)

		got, ok := DisplayFor(entry.Provider)
		require.True(t, ok)
		assert.Equal(t, d, got)
	}

	_, ok := DisplayFor("bogus")
	assert.False(t, ok)
}

// TestCredentialKeysFor pins the one derivation of a provider's credential
// keys from its root, replacing the per-key switches init ai kept by hand.
func TestCredentialKeysFor(t *testing.T) {
	t.Parallel()

	keys, ok := CredentialKeysFor(gochat.ProviderAzureOpenAI)
	require.True(t, ok)
	assert.Equal(t, CredentialKeys{
		Root: configRootAzure, Env: ConfigKeyAzureEnv, Keychain: ConfigKeyAzureKeychain,
		Literal: ConfigKeyAzureKey, FallbackEnv: EnvAzureKey,
	}, keys)

	keys, ok = CredentialKeysFor(gochat.ProviderOpenAICompatible)
	require.True(t, ok)
	assert.Equal(t, configRootOpenAI, keys.Root, "openai-compatible shares OpenAI's root")

	for _, p := range []gochat.Provider{gochat.ProviderClaudeLocal, gochat.ProviderCodexLocal, gochat.ProviderAgyLocal, gochat.ProviderBedrock, "bogus"} {
		_, ok := CredentialKeysFor(p)
		assert.Falsef(t, ok, "%s carries no GTB credential", p)
	}

	// The descriptors doctor reads are the same derivation.
	for _, d := range providerCredentials() {
		assert.Equal(t, d.LiteralKey, d.EnvKey[:len(d.EnvKey)-len(".env")]+".key")
	}
}
