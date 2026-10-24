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
	root := credentialConfigRoot(provider)
	if root == "" {
		return credentialposture.Descriptor{}, false
	}

	desc := descriptorForRoot(root)
	if provider == gochat.ProviderOpenAICompatible {
		desc.FallbackEnv = "" // OPENAI_API_KEY belongs to api.openai.com (chat-openai#10).
	}

	return desc, true
}

// ProviderKeyResolves reports whether provider's key resolves through the walk
// chat.New reads, so doctor can say what the client will find. A provider GTB
// holds no key for reports true, and a configured rung that cannot be read is
// the error.
func ProviderKeyResolves(ctx context.Context, cfg credentialposture.Reader, provider gochat.Provider) (bool, error) {
	source, ok := credentialSources(cfg)(provider)
	if !ok {
		return true, nil
	}

	key, err := source(ctx)
	if err != nil {
		return false, err
	}

	return key != "", nil
}

// ProviderKeyOptional reports whether provider runs without a key:
// openai-compatible sends none, for a host that needs no authentication.
func ProviderKeyOptional(provider gochat.Provider) bool {
	return provider == gochat.ProviderOpenAICompatible
}
