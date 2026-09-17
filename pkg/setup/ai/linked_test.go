package ai

import (
	"context"
	"testing"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"

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
// whole table and doctor's Chat providers check is what reports the gap. A
// tool that declares links (#81) narrows to the ones it declared.
func TestLinkedProviders_NothingRegisteredOffersEverything(t *testing.T) {
	t.Parallel()

	// This test binary registers no real provider, so a set with no links
	// resolves to "everything".
	empty, err := features.Resolve(features.NewRegistry().Snapshot(), nil)
	require.NoError(t, err)

	linked := linkedProviders(empty)
	assert.True(t, linked(gochat.ProviderClaude))

	// Declared links narrow to what is declared and registered.
	gochat.RegisterProvider("lp-declared", func(context.Context, gochat.Settings) (gochat.ChatClient, error) { return nil, nil })

	r := features.NewRegistry()
	require.NoError(t, props.DeclareLinksOn(r, props.ChatLinkPrefix, "lp-declared"))

	set, err := features.Resolve(r.Snapshot(), nil)
	require.NoError(t, err)

	linked = linkedProviders(set)
	assert.True(t, linked("lp-declared"))
	assert.False(t, linked(gochat.ProviderClaude))
}
