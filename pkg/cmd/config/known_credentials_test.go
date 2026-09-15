package config

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
)

// TestKnownCredentials_CoverEveryDeclaredChatKey keeps migrate's hand-kept
// table in step with what the chat adapter declares: a literal key doctor
// warns about is one migrate can move (spec 0196 D6 added the Azure root).
func TestKnownCredentials_CoverEveryDeclaredChatKey(t *testing.T) {
	t.Parallel()

	known := map[string]credentialDescriptor{}
	for _, c := range knownCredentials {
		known[c.key] = c
	}

	for _, d := range credentialposture.Registered() {
		if d.Feature != "ai" || d.LiteralKey == "" {
			continue
		}

		c, ok := known[d.LiteralKey]
		if !assert.Truef(t, ok, "migrate does not know the declared chat credential %s", d.LiteralKey) {
			continue
		}

		assert.Equal(t, d.EnvKey, c.envTargetKey)
		assert.Equal(t, d.KeychainKey, c.keychainTargetKey)
		assert.Equal(t, d.FallbackEnv, defaultAIEnvVarName(d.LiteralKey), "the fallback variable migrate suggests is the one the resolver reads")
	}
}
