package generator

import (
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

// ErrUnknownChatProvider names a chat provider the framework cannot configure.
var ErrUnknownChatProvider = errors.NewSentinel("gtb.generator.unknown_chat_provider", "chat provider is not one the framework can configure")

// ErrChatProvidersRequired is the ai feature selected with no provider to
// serve it (spec 0194 OQ5).
var ErrChatProvidersRequired = errors.NewSentinel("gtb.generator.chat_providers_required", "the ai feature needs at least one chat provider")

// DefaultChatProviders is what a new project ships when the operator does not
// narrow the list: every provider the framework can configure, because a
// generated tool is configured by its consumers the way gtb itself is (spec
// 0194 OQ4).
func DefaultChatProviders() []string {
	providers := chat.ConfigurableProviders()

	out := make([]string, len(providers))
	for i, p := range providers {
		out[i] = string(p)
	}

	return out
}

// ValidateChatProviders rejects names the framework cannot configure, and an
// empty list when the ai feature is enabled.
func ValidateChatProviders(providers []string, features []ManifestFeature) error {
	configurable := chat.ConfigurableProviders()

	for _, p := range providers {
		if !slices.Contains(configurable, gochat.Provider(p)) {
			return errors.Wrapf(ErrUnknownChatProvider, "%q (configurable: %s)", p, joinProviders(configurable))
		}
	}

	if len(providers) == 0 && featureEnabledIn(features, string(props.AiCmd)) {
		return errors.WithHint(ErrChatProvidersRequired,
			"Pass --chat-providers with at least one of "+joinProviders(configurable)+", or drop ai from --features.")
	}

	return nil
}

func joinProviders(providers []gochat.Provider) string {
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = string(p)
	}

	return strings.Join(names, ", ")
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
	ps := make([]gochat.Provider, len(providers))
	for i, p := range providers {
		ps[i] = gochat.Provider(p)
	}

	modules, _ := chat.ModulesForProviders(ps)

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

		module, ok := forge.ModuleForFeature(d.Cmd)
		if !ok || slices.Contains(modules, module) {
			continue
		}

		modules = append(modules, module)
	}

	slices.Sort(modules)

	return modules
}

// syncAdapterFiles rewrites cmd/<name>/chat.go and forge.go from the manifest
// on regenerate. A manifest with no chat block is a project generated before
// the block existed; it is read as every provider the framework could
// configure and the list is written out so the project states its own choice
// from then on (spec 0194 D7). The manifest is the source of truth and the
// files follow it, so a provider removed from the manifest leaves the binary
// on the next regenerate.
func (g *Generator) syncAdapterFiles(m *Manifest) error {
	if m.Properties.Chat.Providers == nil && featureEnabledIn(m.Properties.Features, string(props.AiCmd)) {
		m.Properties.Chat.Providers = DefaultChatProviders()

		if err := g.marshalManifestFile(ManifestPathFor(g.config.Path), m); err != nil {
			return err
		}

		g.props.Logger.Info("manifest had no chat block; recorded the default providers",
			"providers", m.Properties.Chat.Providers)
	}

	name := m.Properties.Name

	chatFile := filepath.Join("cmd", name, "chat.go")
	if err := g.writeGeneratedGoFile(chatFile, templates.SkeletonChatProviders(chatModulesFor(m.Properties.Chat.Providers, m.Properties.Features))); err != nil {
		return err
	}

	forgeFile := filepath.Join("cmd", name, "forge.go")

	return g.writeGeneratedGoFile(forgeFile, templates.SkeletonForgeAdapters(forgeModules(m.Properties.Features)))
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
