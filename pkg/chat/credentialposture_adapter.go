package chat

import (
	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// The provider credentials declared where their keys are defined.
//
// Spec 0189 R1/R4: `doctor` reported resolution for forges only, so an operator
// could be told exactly which rung supplies their GitHub token and nothing at
// all about the Anthropic key sitting in the same file. These registrations
// close that, and they live here rather than in doctor because this package owns
// the keys — a fourth hand-maintained list somewhere else is the problem, not
// the fix.
func init() {
	for _, d := range providerCredentials() {
		credentialposture.Register(d)
	}
}

// providerCredentials is the declaration itself, split out so it is testable
// without relying on init having run. One descriptor per credential root,
// derived the way CredentialKeysFor derives the keys.
func providerCredentials() []credentialposture.Descriptor {
	roots := []struct{ root, owner, label string }{
		{configRootClaude, "chat:anthropic", "Anthropic API key"},
		{configRootOpenAI, "chat:openai", "OpenAI API key"},
		{configRootGemini, "chat:gemini", "Gemini API key"},
		{configRootAzure, "chat:azure", "Azure OpenAI API key"},
	}

	out := make([]credentialposture.Descriptor, 0, len(roots))

	for _, r := range roots {
		keys := credentialKeysForRoot(r.root)
		out = append(out, credentialposture.Descriptor{
			Owner:       r.owner,
			Feature:     string(props.AiCmd),
			Providers:   providersUnderRoot(r.root),
			Label:       r.label,
			EnvKey:      keys.Env,
			KeychainKey: keys.Keychain,
			LiteralKey:  keys.Literal,
			FallbackEnv: keys.FallbackEnv,
		})
	}

	return out
}

// providersUnderRoot lists the providers whose credential lives under root,
// in module-table order.
func providersUnderRoot(root string) []string {
	var out []string

	for _, entry := range providerModules {
		if credentialConfigRoot(entry.Provider) == root {
			out = append(out, string(entry.Provider))
		}
	}

	return out
}
