package generator

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/afero"

	gochat "gitlab.com/phpboyscout/go/chat"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

// ErrUnknownChatProvider names a chat provider no known module registers.
var ErrUnknownChatProvider = errors.NewSentinel("gtb.generator.unknown_chat_provider", "chat provider is not one a known module registers")

// ErrChatProvidersRequired is the ai feature selected with no provider to
// serve it (spec 0194 OQ5).
var ErrChatProvidersRequired = errors.NewSentinel("gtb.generator.chat_providers_required", "the ai feature needs at least one chat provider")

// ErrChatDefaultRequired is several providers linked and no default named:
// the generator does not guess which the author wants (spec 0196 D1).
var ErrChatDefaultRequired = errors.NewSentinel("gtb.generator.chat_default_required", "several chat providers are linked; name the default")

// ErrChatDefaultNotLinked is a default provider outside the linked set.
var ErrChatDefaultNotLinked = errors.NewSentinel("gtb.generator.chat_default_not_linked", "the default chat provider is not one the tool links")

// ErrChatEndpointRequired is a default provider whose module refuses to
// construct without addressing the author has not given (spec 0196 D2).
var ErrChatEndpointRequired = errors.NewSentinel("gtb.generator.chat_endpoint_required", "the default chat provider needs an endpoint")

// KnownChatProviders is every provider a known module registers, in the
// framework's table order. The generator emits an import; whether the running
// tool can be configured for the provider through GTB's wizard is that tool's
// concern, not a precondition for linking it (spec 0194 D5, revised).
func KnownChatProviders() []string {
	entries := chat.ProviderModules()

	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = string(e.Provider)
	}

	return out
}

// DefaultChatProviders is what a new project ships when the operator does not
// narrow the list: every known provider, because a generated tool is
// configured by its consumers the way gtb itself is (spec 0194 OQ4).
func DefaultChatProviders() []string {
	return KnownChatProviders()
}

// ValidateChatProviders rejects names no module registers, and an empty list
// when the ai feature is enabled.
func ValidateChatProviders(providers []string, features []ManifestFeature) error {
	known := KnownChatProviders()

	for _, p := range providers {
		if !slices.Contains(known, p) {
			return errors.Wrapf(ErrUnknownChatProvider, "%q (known: %s)", p, strings.Join(known, ", "))
		}
	}

	if len(providers) == 0 && featureEnabledIn(features, string(props.AiCmd)) {
		return errors.WithHint(ErrChatProvidersRequired,
			"Pass --chat-providers with at least one of "+strings.Join(known, ", ")+", or drop ai from --features.")
	}

	return nil
}

// ValidateChatDefault applies spec 0196's rules for the author's default: with
// ai enabled and several providers linked a default is required; it must be a
// linked provider; and the providers that refuse to construct without an
// endpoint must be given one (openai-compatible and azure-openai a base URL,
// azure-openai an API version, as their modules document). Project and
// location stay optional because gemini-vertex and bedrock fall back to the
// environment for them.
func ValidateChatDefault(d ManifestChatDefault, providers []string, features []ManifestFeature) error {
	if !featureEnabledIn(features, string(props.AiCmd)) {
		return nil
	}

	if err := ValidateChatDefaultProvider(d.Provider, providers, features); err != nil {
		return err
	}

	if d.Provider == "" {
		if len(providers) != 1 {
			return nil
		}

		// One provider is its own default, and its endpoint rules apply.
		d.Provider = providers[0]
	}

	return validateChatEndpoint(d)
}

// ValidateChatDefaultProvider is the provider half of ValidateChatDefault, for
// the wizard's select: required between several providers, and one of them.
// The endpoint half is a later page's.
func ValidateChatDefaultProvider(provider string, providers []string, features []ManifestFeature) error {
	if !featureEnabledIn(features, string(props.AiCmd)) {
		return nil
	}

	if provider == "" {
		if len(providers) > 1 {
			return errors.WithHint(ErrChatDefaultRequired,
				"Pass --chat-default-provider with one of "+strings.Join(providers, ", ")+".")
		}

		return nil
	}

	if !slices.Contains(providers, provider) {
		return errors.Wrapf(ErrChatDefaultNotLinked, "%q (linked: %s)", provider, strings.Join(providers, ", "))
	}

	return nil
}

func validateChatEndpoint(d ManifestChatDefault) error {
	provider := gochat.Provider(d.Provider)

	if provider == gochat.ProviderOpenAICompatible || provider == gochat.ProviderAzureOpenAI {
		if d.BaseURL == "" {
			return errors.Wrapf(ErrChatEndpointRequired, "%s needs --chat-base-url", d.Provider)
		}
	}

	if provider == gochat.ProviderAzureOpenAI && d.APIVersion == "" {
		return errors.Wrapf(ErrChatEndpointRequired, "%s needs --chat-api-version", d.Provider)
	}

	if d.BaseURL != "" {
		if err := gochat.ValidateBaseURL(d.BaseURL, false); err != nil {
			return errors.Wrapf(ErrChatEndpointRequired, "base URL: %v", err)
		}
	}

	return nil
}

// chatDefaultsYAML renders the author's default as the `ai:` section the
// framework reads. The keys are GTB's config schema (pkg/chat/constants.go);
// they are spelled here because the cli builds against the released
// framework and cannot name constants a release ahead of it (Refs #63).
func chatDefaultsYAML(d ManifestChatDefault) []byte {
	var b strings.Builder

	b.WriteString("ai:\n")

	for _, kv := range [][2]string{
		{"provider", d.Provider},
		{"model", d.Model},
		{"base_url", d.BaseURL},
		{"api_version", d.APIVersion},
		{"project", d.Project},
		{"location", d.Location},
	} {
		if kv[1] != "" {
			b.WriteString("  " + kv[0] + ": " + escapeYAML(kv[1]) + "\n")
		}
	}

	return []byte(b.String())
}

// chatModulesFor is chatModules gated on the ai feature: a tool without ai
// links no chat provider whatever the manifest lists, because the feature is
// what chooses the adapter (spec 0194 D4).
func chatModulesFor(providers []string, features []ManifestFeature) []string {
	if !featureEnabledIn(features, string(props.AiCmd)) {
		return nil
	}

	return chatModules(providers)
}

// chatModules maps the manifest's provider names to the modules whose blank
// imports register them. Unknown names are dropped here rather than failed:
// validation is the generate command's job, and a manifest edited by hand to
// name a provider this framework cannot configure still regenerates.
func chatModules(providers []string) []string {
	var modules []string

	for _, p := range providers {
		module, ok := chat.ProviderModule(gochat.Provider(p))
		if !ok || slices.Contains(modules, module) {
			continue
		}

		modules = append(modules, module)
	}

	slices.Sort(modules)

	return modules
}

// forgeModules maps the enabled forge features to their adapter modules, in
// catalogue order, deduplicated (gitea and codeberg share one).
func forgeModules(features []ManifestFeature) []string {
	var modules []string

	for _, d := range templates.FeatureCatalogue {
		if !featureEnabledIn(features, string(d.Cmd)) {
			continue
		}

		// A forge feature's ID is its forge type (github, gitlab, gitea,
		// codeberg, bitbucket), so the type table answers for the feature.
		module, ok := forge.ModuleFor(string(d.Cmd))
		if !ok || slices.Contains(modules, module) {
			continue
		}

		modules = append(modules, module)
	}

	slices.Sort(modules)

	return modules
}

// syncAdapterFiles rewrites cmd/<name>/chat.go and forge.go from the manifest
// on regenerate. The manifest is the source of truth and the files follow it,
// so a provider removed from the manifest leaves the binary on the next
// regenerate. A manifest with no chat block had the default recorded by
// syncDerivedManifestFields before rendering (spec 0194 D7).
func (g *Generator) syncAdapterFiles(m *Manifest) error {
	name := m.Properties.Name
	withDefaults := chatDefaultsFor(m.Properties)

	chatFile := filepath.Join("cmd", name, "chat.go")
	if err := g.writeGeneratedGoFile(chatFile, templates.SkeletonChatProviders(chatModulesFor(m.Properties.Chat.Providers, m.Properties.Features), !withDefaults.IsZero())); err != nil {
		return err
	}

	if err := g.syncChatDefaultsBundle(g.config.Path, name, withDefaults); err != nil {
		return err
	}

	forgeFile := filepath.Join("cmd", name, "forge.go")
	if err := g.writeGeneratedGoFile(forgeFile, templates.SkeletonForgeAdapters(forgeModules(m.Properties.Features))); err != nil {
		return err
	}

	return g.syncKeychainFile(name, m.Properties.Features)
}

// syncKeychainFile writes cmd/<name>/keychain.go while the keychain feature
// is enabled and removes it when it is not (spec 0197 D8). The file used to
// be emitted on generate and never touched again, so deleting it was the
// only durable way to drop the keychain, and the manifest did not know.
func (g *Generator) syncKeychainFile(name string, features []ManifestFeature) error {
	path := filepath.Join(g.config.Path, "cmd", name, "keychain.go")

	if !featureEnabledIn(features, KeychainFeature) {
		if err := g.props.FS.Remove(path); err != nil && !os.IsNotExist(err) {
			return errors.Newf("failed to remove %s: %w", path, err)
		}

		return nil
	}

	return g.writeGeneratedGoFile(filepath.Join("cmd", name, "keychain.go"), templates.SkeletonKeychain())
}

// chatDefaultsFor is the author's default gated on the ai feature, the way
// chatModulesFor gates the imports: no ai, no bundle.
func chatDefaultsFor(p ManifestProperties) ManifestChatDefault {
	if !featureEnabledIn(p.Features, string(props.AiCmd)) {
		return ManifestChatDefault{}
	}

	return p.Chat.Default
}

// chatBundleDir is where a tool's author defaults for chat live, relative to
// the project: cmd/<name>/chat, re-rooted by chat.go so that assets/config.yaml
// sits at the path the framework's defaults layer opens (spec 0196 D4).
func chatBundleDir(name string) string {
	return filepath.Join("cmd", name, "chat")
}

// syncChatDefaultsBundle writes the defaults bundle (embedded defaults and the
// init template, same content) under the project root, or removes it when the
// manifest has no default. The root is a parameter because generate writes to
// its destination path and regenerate to g.config.Path.
func (g *Generator) syncChatDefaultsBundle(root, name string, d ManifestChatDefault) error {
	dir := filepath.Join(root, chatBundleDir(name))

	if d.IsZero() {
		if err := g.props.FS.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
			return errors.Newf("failed to remove %s: %w", dir, err)
		}

		return nil
	}

	content := chatDefaultsYAML(d)

	for _, rel := range []string{"assets/config.yaml", "assets/init/config.yaml"} {
		full := filepath.Join(dir, rel)

		if err := g.props.FS.MkdirAll(filepath.Dir(full), DefaultDirMode); err != nil {
			return errors.Newf("failed to create directory %s: %w", filepath.Dir(full), err)
		}

		if err := afero.WriteFile(g.props.FS, full, content, DefaultFileMode); err != nil {
			return errors.Newf("failed to write %s: %w", full, err)
		}
	}

	return nil
}

// recoverChatProviders reads cmd/<name>/chat.go on a from-scratch rebuild and
// returns every provider the imported modules register. The file records
// modules, not the operator's narrower choice within a module, so the
// recovered list is the widest reading of what the binary links. Nil when the
// file is absent.
func (g *Generator) recoverChatProviders() []string {
	matches, err := afero.Glob(g.props.FS, filepath.Join(g.config.Path, "cmd", "*", "chat.go"))
	if err != nil || len(matches) == 0 {
		return nil
	}

	src, err := afero.ReadFile(g.props.FS, matches[0])
	if err != nil {
		return nil
	}

	var providers []string

	for _, entry := range chat.ProviderModules() {
		if strings.Contains(string(src), `"`+entry.Module+`"`) {
			providers = append(providers, string(entry.Provider))
		}
	}

	return providers
}
