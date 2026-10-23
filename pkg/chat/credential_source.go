package chat

import (
	"context"

	gochat "gitlab.com/phpboyscout/go/chat"

	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
)

// credentialSources resolves each provider's key through the walk doctor
// reports from (#131), so the order is stated once and a broken keychain
// entry fails chat.New instead of reading as a missing key. cfg may be nil:
// the fallback variable still applies.
func credentialSources(cfg credentialposture.Reader) gochat.ProviderCredentialSources {
	return func(provider gochat.Provider) (gochat.CredentialSource, bool) {
		desc, ok := providerDescriptor(provider)
		if !ok {
			return nil, false
		}

		return func(ctx context.Context) (string, error) {
			value, _, err := credentialposture.ResolveCredential(ctx, cfg, desc)

			return value, err
		}, true
	}
}

// providerDescriptor is the provider's root descriptor, narrowed where the
// provider module reads less than the root offers.
func providerDescriptor(provider gochat.Provider) (credentialposture.Descriptor, bool) {
	switch provider {
	case gochat.ProviderGeminiVertex:
		// Vertex authenticates with application default credentials and
		// refuses a key (chat-gemini spec 0017 D8).
		return credentialposture.Descriptor{}, false
	case gochat.ProviderOpenAICompatible:
		desc := descriptorForRoot(configRootOpenAI)
		// OPENAI_API_KEY belongs to api.openai.com (chat-openai#10).
		desc.FallbackEnv = ""

		return desc, true
	default:
		root := credentialConfigRoot(provider)
		if root == "" {
			return credentialposture.Descriptor{}, false
		}

		return descriptorForRoot(root), true
	}
}
