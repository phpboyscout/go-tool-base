package chat

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gochat "gitlab.com/phpboyscout/go/chat"
	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestApplyDefaultProvider_OnlyRegisteredProviderIsTheDefault pins spec 0196
// D5's registry rung: a tool that links exactly one provider needs no
// ai.provider at all, and one that links several is told to choose.
func TestApplyDefaultProvider_OnlyRegisteredProviderIsTheDefault(t *testing.T) {
	t.Setenv(EnvAIProvider, "")

	log := slog.New(slog.DiscardHandler)

	cfg := gochat.Config{}
	require.NoError(t, applyDefaultProvider(log, &cfg, []gochat.Provider{gochat.ProviderCodexLocal}))
	assert.Equal(t, gochat.ProviderCodexLocal, cfg.Provider)

	cfg = gochat.Config{}
	err := applyDefaultProvider(log, &cfg, []gochat.Provider{gochat.ProviderClaude, gochat.ProviderOpenAI})
	require.ErrorIs(t, err, ErrProviderUnset)
	assert.Contains(t, errors.FlattenHints(err), "claude, openai", "the hint names what is linked")

	cfg = gochat.Config{}
	require.ErrorIs(t, applyDefaultProvider(log, &cfg, nil), ErrProviderUnset)
}

// TestHintUnsupportedProvider_MatchesTheSentinel: the registry miss is
// recognised with errors.Is on go/chat's ErrProviderNotRegistered (v0.24.0),
// not by its wording.
func TestHintUnsupportedProvider_MatchesTheSentinel(t *testing.T) {
	t.Parallel()

	miss := errors.Wrapf(gochat.ErrProviderNotRegistered, "%s", gochat.ProviderBedrock)
	assert.Contains(t, errors.FlattenHints(hintUnsupportedProvider(miss, gochat.ProviderBedrock)), "chat-bedrock")

	lookalike := errors.New("unsupported provider: bedrock")
	assert.Same(t, lookalike, hintUnsupportedProvider(lookalike, gochat.ProviderBedrock), "wording alone is not a registry miss")

	_, err := gochat.New(context.Background(), gochat.Settings{Config: gochat.Config{Provider: "no-such-provider-for-test"}})
	require.ErrorIs(t, err, gochat.ErrProviderNotRegistered, "the module's own miss is the sentinel")
}

// TestProviderCredentials_NameTheirProviders: every chat descriptor says which
// providers consume it, so doctor can report only the linked ones (D12).
func TestProviderCredentials_NameTheirProviders(t *testing.T) {
	t.Parallel()

	for _, d := range providerCredentials() {
		require.NotEmptyf(t, d.Providers, "%s names no provider", d.Label)

		for _, p := range d.Providers {
			keys, ok := CredentialKeysFor(gochat.Provider(p))
			require.Truef(t, ok, "%s is not a credential-bearing provider", p)
			assert.Equal(t, d.LiteralKey, keys.Literal, "the provider's root is the descriptor's")
		}
	}
}

// TestLinkedProvidersIn_DeclaredLinksNarrowTheRegistry (#81): declared links
// narrow to what is declared and registered, name what is declared but not
// registered, and a tool declaring none is the registry.
func TestLinkedProvidersIn_DeclaredLinksNarrowTheRegistry(t *testing.T) {
	t.Parallel()

	registered := []gochat.Provider{gochat.ProviderClaude, gochat.ProviderClaudeLocal}

	none, err := features.Resolve(features.NewRegistry().Snapshot(), nil)
	require.NoError(t, err)

	linked, unregistered := LinkedProvidersIn(none, registered)
	assert.Equal(t, registered, linked)
	assert.Empty(t, unregistered)

	r := features.NewRegistry()
	require.NoError(t, props.DeclareLinksOn(r, props.ChatLinkPrefix, string(gochat.ProviderClaude), string(gochat.ProviderGemini)))

	set, err := features.Resolve(r.Snapshot(), nil)
	require.NoError(t, err)

	linked, unregistered = LinkedProvidersIn(set, registered)
	assert.Equal(t, []gochat.Provider{gochat.ProviderClaude}, linked, "the module's other provider is not the author's choice")
	assert.Equal(t, []gochat.Provider{gochat.ProviderGemini}, unregistered, "declared but its module is not linked")
}

// TestDefaultCandidates_AreTheDeclaredProviders (F14 of the v0.43.0 manual
// round): the default-provider rung counted module registrations, so a tool
// generated with --chat-providers claude alone was told "this binary links:
// claude, claude-local" (chat-anthropic registers both) while its own doctor
// said "claude linked". The rung now chooses from the providers the tool
// declares, the same view doctor and init ai use, so one declared provider
// is its own default.
func TestDefaultCandidates_AreTheDeclaredProviders(t *testing.T) {
	t.Setenv(EnvAIProvider, "")

	registered := []gochat.Provider{gochat.ProviderClaude, gochat.ProviderClaudeLocal}

	r := features.NewRegistry()
	require.NoError(t, props.DeclareLinksOn(r, props.ChatLinkPrefix, string(gochat.ProviderClaude)))

	set, err := features.Resolve(r.Snapshot(), nil)
	require.NoError(t, err)

	candidates := defaultCandidates(set, registered)
	assert.Equal(t, []gochat.Provider{gochat.ProviderClaude}, candidates)

	cfg := gochat.Config{}
	require.NoError(t, applyDefaultProvider(slog.New(slog.DiscardHandler), &cfg, candidates))
	assert.Equal(t, gochat.ProviderClaude, cfg.Provider, "one declared provider is its own default, whatever else its module registers")

	none, err := features.Resolve(features.NewRegistry().Snapshot(), nil)
	require.NoError(t, err)
	assert.Equal(t, registered, defaultCandidates(none, registered), "a binary that declares no links chooses from the registry")
}
