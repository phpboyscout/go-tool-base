package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gochat "gitlab.com/phpboyscout/go/chat"
	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/features"

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
	linked := func(p gochat.Provider) bool { return p == gochat.ProviderCodexLocal }

	// The select offers only the linked provider, so a hand-typed answer is
	// what an unlinked choice looks like; the rule holds after the form too.
	_, err := finaliseAIConfig(&AIConfig{Provider: string(gochat.ProviderBedrock)}, store.View(), linked)
	require.ErrorIs(t, err, ErrProviderNotLinked)
	assert.Contains(t, errors.FlattenHints(err), "chat-bedrock", "the hint names the module to import")
}

// TestLinkedProviders_NothingRegisteredOffersEverything: a binary that
// registers no provider at all cannot narrow, so init ai falls back to the
// whole table and doctor's Chat providers check is what reports the gap.
// The narrowing by declared links is chat's and tested there; this test
// registers nothing, because the process-wide chat registry is what the
// wizard tests in this package count providers from.
func TestLinkedProviders_NothingRegisteredOffersEverything(t *testing.T) {
	t.Parallel()

	empty, err := features.Resolve(features.NewRegistry().Snapshot(), nil)
	require.NoError(t, err)

	assert.True(t, linkedProviders(empty)(gochat.ProviderClaude))
}
