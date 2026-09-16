package ai

import (
	"testing"

	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gochat "gitlab.com/phpboyscout/go/chat"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
)

// TestProviderOptions_OfferOnlyWhatIsLinked pins spec 0196 D7 (phase 3): the
// select offers the providers the binary registers, and every one of them.
func TestProviderOptions_OfferOnlyWhatIsLinked(t *testing.T) {
	t.Parallel()

	linked := func(p gochat.Provider) bool {
		return p == gochat.ProviderClaudeLocal || p == gochat.ProviderAzureOpenAI
	}

	opts := providerOptions(linked)
	require.Len(t, opts, 2)
	assert.Equal(t, string(gochat.ProviderClaudeLocal), opts[0].Value)
	assert.Equal(t, string(gochat.ProviderAzureOpenAI), opts[1].Value)
}

// TestRunAIForms_RefusesAProviderTheBinaryDoesNotLink: a form injected with
// an unlinked provider (or a hand-typed one) is refused with the module to
// import, rather than written to config to fail at first use.
func TestRunAIForms_RefusesAProviderTheBinaryDoesNotLink(t *testing.T) {
	t.Parallel()

	store := testutil.StoreFromYAML(t, "")
	choose := func(c *formConfig) {
		c.linked = func(p gochat.Provider) bool { return p == gochat.ProviderCodexLocal }
		c.providerFormCreator = func(cfg *AIConfig) *huh.Form {
			cfg.Provider = string(gochat.ProviderBedrock)

			return nil
		}
	}

	_, err := runAIForms(store.View(), choose)
	require.ErrorIs(t, err, ErrProviderNotLinked)
	assert.Contains(t, errors.FlattenHints(err), "chat-bedrock", "the hint names the module to import")
}

// TestLinkedProviders_NothingRegisteredOffersEverything: a binary that
// registers no provider at all cannot narrow, so init ai falls back to the
// whole table and doctor's Chat providers check is what reports the gap.
func TestLinkedProviders_NothingRegisteredOffersEverything(t *testing.T) {
	t.Parallel()

	linked := linkedProviders(func() []gochat.Provider { return nil })
	assert.True(t, linked(gochat.ProviderClaude))

	linked = linkedProviders(func() []gochat.Provider { return []gochat.Provider{gochat.ProviderGemini} })
	assert.True(t, linked(gochat.ProviderGemini))
	assert.False(t, linked(gochat.ProviderClaude))
}
