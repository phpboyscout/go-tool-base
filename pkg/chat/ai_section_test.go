package chat

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gochat "gitlab.com/phpboyscout/go/chat"

	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

const aiSectionYAML = `
ai:
  provider: openai-compatible
  model: local-model
  base_url: https://llm.internal/v1
  api_version: 2026-01-01
  project: proj-1
  location: eu-west-2
`

// TestSettingsFromProps_TransfersTheWholeAISection pins spec 0196 D6: every
// ai.* key that go/chat's Config accepts from a file reaches the Config.
// applyRuntimeConfig used to decode ai.provider and ai.request_timeout only,
// so an openai-compatible tool with ai.base_url set failed construction with
// an empty BaseURL.
func TestSettingsFromProps_TransfersTheWholeAISection(t *testing.T) {
	t.Parallel()

	p := &props.Props{Logger: logger.NewNoop(), Config: chatStoreFromYAML(t, aiSectionYAML)}

	settings, err := SettingsFromProps(p, gochat.Config{})
	require.NoError(t, err)

	cfg := settings.Config
	assert.Equal(t, gochat.ProviderOpenAICompatible, cfg.Provider)
	assert.Equal(t, "local-model", cfg.Model)
	assert.Equal(t, "https://llm.internal/v1", cfg.BaseURL)
	assert.Equal(t, "2026-01-01", cfg.APIVersion)
	assert.Equal(t, "proj-1", cfg.Project)
	assert.Equal(t, "eu-west-2", cfg.Location)
}

// TestSettingsFromProps_CallerFieldsWinOverConfig: a Config the caller filled
// is not overwritten by the file, the same rule applyRuntimeConfig already
// applied to Provider and RequestTimeout.
func TestSettingsFromProps_CallerFieldsWinOverConfig(t *testing.T) {
	t.Parallel()

	p := &props.Props{Logger: logger.NewNoop(), Config: chatStoreFromYAML(t, aiSectionYAML)}

	settings, err := SettingsFromProps(p, gochat.Config{Model: "pinned", BaseURL: "https://other.internal/v1"})
	require.NoError(t, err)

	assert.Equal(t, "pinned", settings.Config.Model)
	assert.Equal(t, "https://other.internal/v1", settings.Config.BaseURL)
	assert.Equal(t, "proj-1", settings.Config.Project, "an empty caller field is still filled")
}

// TestNewWithFallbackFromProps_MembersSelfResolveTheirAddressing pins the D6
// delegation: the primary keeps the model and addressing the operator
// configured for it, and every other chain member starts clean. GTB's own
// derivation cleared Model and BaseURL for the primary too, and would have
// carried Project, Location and APIVersion to every member once they were
// transferred at all.
func TestNewWithFallbackFromProps_MembersSelfResolveTheirAddressing(t *testing.T) {
	captured := map[gochat.Provider]gochat.Config{}
	for _, name := range []gochat.Provider{"fbt-ok", "fbt-ok2"} {
		gochat.RegisterProvider(name, func(_ context.Context, s gochat.Settings) (gochat.ChatClient, error) {
			captured[s.Config.Provider] = s.Config

			return &fakeClient{chatReply: "ok"}, nil
		})
	}

	// The tests that share these fake names re-register plain factories.
	t.Cleanup(func() { registerTestProviders(t) })

	p := &props.Props{
		Logger: logger.NewNoop(),
		Config: chatStoreFromYAML(t, `
ai:
  provider: fbt-ok
  model: primary-model
  base_url: https://llm.internal/v1
  api_version: 2026-01-01
  project: proj-1
  location: eu-west-2
  fallback:
    enabled: true
    providers: [fbt-ok, fbt-ok2]
`),
	}

	_, err := NewWithFallbackFromProps(context.Background(), p, gochat.Config{})
	require.NoError(t, err)

	primary, ok := captured["fbt-ok"]
	require.True(t, ok, "the primary was constructed")
	assert.Equal(t, "primary-model", primary.Model)
	assert.Equal(t, "https://llm.internal/v1", primary.BaseURL)
	assert.Equal(t, "2026-01-01", primary.APIVersion)
	assert.Equal(t, "proj-1", primary.Project)
	assert.Equal(t, "eu-west-2", primary.Location)

	member, ok := captured["fbt-ok2"]
	require.True(t, ok, "the fallback member was constructed")
	assert.Empty(t, member.Model, "a model name is provider-specific")
	assert.Empty(t, member.BaseURL, "an endpoint is provider-specific")
	assert.Empty(t, member.APIVersion)
	assert.Empty(t, member.Project)
	assert.Empty(t, member.Location)
}

// TestCredentialConfigRoot_CoversEveryAPIProvider pins D6's credential roots:
// every provider that carries a GTB credential has a root, and the ones
// authenticated elsewhere (a local CLI, the AWS chain) have none.
func TestCredentialConfigRoot_CoversEveryAPIProvider(t *testing.T) {
	t.Parallel()

	want := map[gochat.Provider]string{
		gochat.ProviderClaude:           configRootClaude,
		gochat.ProviderOpenAI:           configRootOpenAI,
		gochat.ProviderOpenAICompatible: configRootOpenAI,
		gochat.ProviderAzureOpenAI:      configRootAzure,
		gochat.ProviderGemini:           configRootGemini,
		gochat.ProviderGeminiVertex:     configRootGemini,
		gochat.ProviderBedrock:          "",
		gochat.ProviderClaudeLocal:      "",
		gochat.ProviderCodexLocal:       "",
		gochat.ProviderAgyLocal:         "",
	}

	for _, entry := range ProviderModules() {
		root, known := want[entry.Provider]
		require.Truef(t, known, "provider %q is not classified by this test", entry.Provider)
		assert.Equalf(t, root, credentialConfigRoot(entry.Provider), "credential root for %q", entry.Provider)
		assert.Equalf(t, root != "", NeedsCredential(entry.Provider), "NeedsCredential(%q) follows the root", entry.Provider)
	}
}

// TestProviderCredentials_OnePerRoot: doctor reports an Azure key the way it
// reports an OpenAI key, so every credential root declares a descriptor.
func TestProviderCredentials_OnePerRoot(t *testing.T) {
	t.Parallel()

	literals := map[string]bool{}
	for _, d := range providerCredentials() {
		assert.Equal(t, string(props.AiCmd), d.Feature)
		literals[d.LiteralKey] = true
	}

	for _, root := range []string{configRootClaude, configRootOpenAI, configRootGemini, configRootAzure} {
		assert.Truef(t, literals[root+".key"], "no descriptor declares %s.key", root)
	}

	var _ credentialposture.Descriptor
}

// TestAISection_APIVersionSurvivesYAMLTyping: Azure's versions are dates, and
// YAML reads an unquoted 2024-10-21 as a timestamp. Quoted or not, the
// provider gets the dated string.
func TestAISection_APIVersionSurvivesYAMLTyping(t *testing.T) {
	t.Parallel()

	for name, yaml := range map[string]string{
		"unquoted": "ai:\n  provider: azure-openai\n  api_version: 2024-10-21\n",
		"quoted":   "ai:\n  provider: azure-openai\n  api_version: \"2024-10-21\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p := &props.Props{Logger: logger.NewNoop(), Config: chatStoreFromYAML(t, yaml)}
			settings, err := SettingsFromProps(p, gochat.Config{})
			require.NoError(t, err)
			assert.Equal(t, "2024-10-21", settings.Config.APIVersion)
		})
	}
}
