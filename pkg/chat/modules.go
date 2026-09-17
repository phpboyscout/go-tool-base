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

// ProviderNames lists every known provider, comma separated, for help text.
func ProviderNames() string {
	names := make([]string, len(providerModules))
	for i, entry := range providerModules {
		names[i] = string(entry.Provider)
	}

	return strings.Join(names, ", ")
}

// ProviderModules returns every provider this adapter knows with its module, in
// a stable order.
func ProviderModules() []ProviderModuleEntry {
	out := make([]ProviderModuleEntry, len(providerModules))
	copy(out, providerModules)

	return out
}

// ProviderDisplay is how a provider is shown to a person: init ai's select,
// the generator's wizard and the --provider help all read this one table.
type ProviderDisplay struct {
	ID    gochat.Provider
	Label string
	// Gloss says what the provider is and what it needs, in a few words.
	Gloss string
}

// providerDisplays follows the module table's order (spec 0196 D7).
var providerDisplays = map[gochat.Provider]ProviderDisplay{
	gochat.ProviderClaude:           {Label: "Claude (Anthropic)", Gloss: "Anthropic API; needs an API key"},
	gochat.ProviderClaudeLocal:      {Label: "Claude CLI", Gloss: "the claude CLI on this machine; no API key"},
	gochat.ProviderOpenAI:           {Label: "OpenAI", Gloss: "OpenAI API; needs an API key"},
	gochat.ProviderOpenAICompatible: {Label: "OpenAI-compatible endpoint", Gloss: "any OpenAI-shaped endpoint (Ollama, xAI); needs a base URL"},
	gochat.ProviderCodexLocal:       {Label: "Codex CLI", Gloss: "the codex CLI on this machine; no API key"},
	gochat.ProviderGemini:           {Label: "Gemini (Google)", Gloss: "Google Gemini API; needs an API key"},
	gochat.ProviderGeminiVertex:     {Label: "Gemini on Vertex AI", Gloss: "Gemini through Vertex AI; Google application default credentials"},
	gochat.ProviderAgyLocal:         {Label: "agy CLI", Gloss: "the agy CLI on this machine; no API key, no tools"},
	gochat.ProviderBedrock:          {Label: "AWS Bedrock", Gloss: "AWS Bedrock; the AWS credential chain"},
	gochat.ProviderAzureOpenAI:      {Label: "Azure OpenAI", Gloss: "Azure OpenAI; a deployment endpoint, an API version and a key"},
}

// ProviderDisplays returns every known provider's display, in module-table
// order.
func ProviderDisplays() []ProviderDisplay {
	out := make([]ProviderDisplay, 0, len(providerModules))

	for _, entry := range providerModules {
		if d, ok := DisplayFor(entry.Provider); ok {
			out = append(out, d)
		}
	}

	return out
}

// DisplayFor returns a provider's display, and whether the provider is known.
func DisplayFor(provider gochat.Provider) (ProviderDisplay, bool) {
	d, ok := providerDisplays[provider]
	if !ok {
		return ProviderDisplay{}, false
	}

	d.ID = provider

	return d, true
}

// CredentialKeys is where GTB keeps a provider's credential: the config root
// and the three storage-mode keys under it, plus the well-known environment
// variable the resolver falls back to.
type CredentialKeys struct {
	Root, Env, Keychain, Literal, FallbackEnv string
}

// CredentialKeysFor derives a provider's credential keys from its root, and
// reports false for a provider that carries no GTB credential. Every list of
// these keys (init ai, config migrate, the posture descriptors) reads this
// one derivation.
func CredentialKeysFor(provider gochat.Provider) (CredentialKeys, bool) {
	root := credentialConfigRoot(provider)
	if root == "" {
		return CredentialKeys{}, false
	}

	return credentialKeysForRoot(root), true
}

func credentialKeysForRoot(root string) CredentialKeys {
	return CredentialKeys{
		Root:        root,
		Env:         root + ".env",
		Keychain:    root + ".keychain",
		Literal:     root + ".key",
		FallbackEnv: fallbackEnvForRoot[root],
	}
}

// ProviderCredentialKeys is every credential GTB keeps for a chat provider,
// one per config root in the order the wizard offers them. The migration
// and the project-trust list read it rather than naming the roots again.
func ProviderCredentialKeys() []CredentialKeys {
	out := make([]CredentialKeys, 0, len(credentialRoots))
	for _, root := range credentialRoots {
		out = append(out, credentialKeysForRoot(root))
	}

	return out
}

// credentialRoots orders the credential roots for every enumeration.
var credentialRoots = []string{configRootClaude, configRootOpenAI, configRootGemini, configRootAzure}

// fallbackEnvForRoot is the ecosystem variable each credential root falls
// back to when no key under the root is set.
var fallbackEnvForRoot = map[string]string{
	configRootClaude: EnvClaudeKey,
	configRootOpenAI: EnvOpenAIKey,
	configRootGemini: EnvGeminiKey,
	configRootAzure:  EnvAzureKey,
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

// hintUnsupportedProvider adds the missing blank import to a registry miss
// (go/chat's ErrProviderNotRegistered) and leaves every other error untouched.
func hintUnsupportedProvider(err error, provider gochat.Provider) error {
	if err == nil || !errors.Is(err, gochat.ErrProviderNotRegistered) {
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
