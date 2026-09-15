package chat

import (
	"strings"

	gochat "gitlab.com/phpboyscout/go/chat"
	"gitlab.com/phpboyscout/go/errors"
)

// ProviderModuleEntry pairs a chat provider with the module whose blank import
// registers it.
type ProviderModuleEntry struct {
	Provider gochat.Provider
	Module   string
}

// providerModules is the one table the generator, doctor and hints read. A
// blank import is per module, so selecting one provider a module registers
// links the others it registers too.
var providerModules = []ProviderModuleEntry{
	{gochat.ProviderClaude, "gitlab.com/phpboyscout/go/chat-anthropic"},
	{gochat.ProviderClaudeLocal, "gitlab.com/phpboyscout/go/chat-anthropic"},
	{gochat.ProviderOpenAI, "gitlab.com/phpboyscout/go/chat-openai"},
	{gochat.ProviderOpenAICompatible, "gitlab.com/phpboyscout/go/chat-openai"},
	{gochat.ProviderCodexLocal, "gitlab.com/phpboyscout/go/chat-openai"},
	{gochat.ProviderGemini, "gitlab.com/phpboyscout/go/chat-gemini"},
	{gochat.ProviderGeminiVertex, "gitlab.com/phpboyscout/go/chat-gemini"},
	{gochat.ProviderAgyLocal, "gitlab.com/phpboyscout/go/chat-gemini"},
	{gochat.ProviderBedrock, "gitlab.com/phpboyscout/go/chat-bedrock"},
	{gochat.ProviderAzureOpenAI, "gitlab.com/phpboyscout/go/chat-openai-azure"},
}

// ProviderModule returns the module path whose blank import registers the
// provider, and reports whether the provider is one this adapter knows.
func ProviderModule(provider gochat.Provider) (string, bool) {
	for _, entry := range providerModules {
		if entry.Provider == provider {
			return entry.Module, true
		}
	}

	return "", false
}

// ProviderModules returns every provider this adapter knows with its module, in
// a stable order.
func ProviderModules() []ProviderModuleEntry {
	out := make([]ProviderModuleEntry, len(providerModules))
	copy(out, providerModules)

	return out
}

// NeedsCredential reports whether GTB holds a credential for the provider: a
// local CLI authenticates on its own, and bedrock authenticates through the
// AWS chain, so neither has a credential root in GTB's config.
func NeedsCredential(provider gochat.Provider) bool {
	return credentialConfigRoot(provider) != ""
}

// IsLocalCLI reports whether a provider drives a locally installed CLI, which
// authenticates on its own and so carries no credential in GTB's config.
func IsLocalCLI(provider gochat.Provider) bool {
	switch provider {
	case gochat.ProviderClaudeLocal, gochat.ProviderCodexLocal, gochat.ProviderAgyLocal:
		return true
	default:
		return false
	}
}

// unsupportedProviderWording is go/chat's registry-miss message. Matched as text
// because the module exports no sentinel for it yet (go/chat#23);
// TestUnsupportedProviderWording fails the day the wording moves.
const unsupportedProviderWording = "unsupported provider"

// hintUnsupportedProvider adds the missing blank import to a registry miss and
// leaves every other error untouched.
func hintUnsupportedProvider(err error, provider gochat.Provider) error {
	if err == nil || !strings.Contains(err.Error(), unsupportedProviderWording) {
		return err
	}

	module, ok := ProviderModule(provider)
	if !ok {
		return err
	}

	return errors.WithHintf(err,
		"Provider %q is registered by module %s, which this binary does not link. Add to the tool's main package:\n\nimport _ %q",
		provider, module, module)
}
