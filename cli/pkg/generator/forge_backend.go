package generator

import (
	"slices"
	"strings"

	"gitlab.com/phpboyscout/go/errors"
	"golang.org/x/mod/module"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Release channels a self-updating tool can be generated with (spec 0195 D7).
const (
	// ReleaseChannelForge releases from the project's forge backend.
	ReleaseChannelForge = "forge"
	// ReleaseChannelDirect releases from URLs the go/forge direct source reads.
	ReleaseChannelDirect = "direct"
)

// Sentinels for the backend, channel and module-path fields.
var (
	ErrInvalidForgeBackend   = errors.NewSentinel("gtb.generator.invalid_forge_backend", "unknown forge backend")
	ErrInvalidModulePath     = errors.NewSentinel("gtb.generator.invalid_module_path", "invalid Go module path")
	ErrInvalidReleaseChannel = errors.NewSentinel("gtb.generator.invalid_release_channel", "unknown release channel")
	// ErrBackendContradictsType reports a release_source.backend naming one
	// forge while release_source.type names another.
	ErrBackendContradictsType = errors.NewSentinel("gtb.generator.backend_contradicts_type", "release_source.backend contradicts release_source.type")
	ErrAmbiguousForgeBackend  = errors.NewSentinel("gtb.generator.ambiguous_forge_backend",
		"the manifest enables more than one forge and its release source names none of them; set release_source.backend")
)

// ForgeBackends lists every forge a project can be hosted on, in catalogue
// order: the forge features the framework registers profiles for. The
// wizard's chooser, --forge-backend and the manifest validator all read it.
func ForgeBackends() []props.FeatureID {
	var backends []props.FeatureID

	for _, d := range templates.Catalogue() {
		if d.Kind == props.KindForge {
			backends = append(backends, d.ID)
		}
	}

	return backends
}

// ValidateForgeBackend accepts a registered forge or empty (not hosted).
func ValidateForgeBackend(backend string) error {
	if backend == "" || slices.Contains(ForgeBackends(), props.FeatureID(backend)) {
		return nil
	}

	return errors.Wrapf(ErrInvalidForgeBackend, "%q (known: %s)", backend, joinFeatureIDs(ForgeBackends()))
}

// ValidateModulePath accepts a syntactically valid Go module path, or empty
// (derived for a hosted project).
func ValidateModulePath(path string) error {
	if path == "" {
		return nil
	}

	if err := module.CheckPath(path); err != nil {
		// A single-segment path like "myapp" is a valid module path for a
		// tool that is never imported, and CheckPath refuses it only for
		// lacking a dot in the first element.
		if !strings.Contains(path, "/") && module.CheckImportPath(path) == nil {
			return nil
		}

		return errors.Wrapf(ErrInvalidModulePath, "%q: %v", path, err)
	}

	return nil
}

// ValidateReleaseChannel accepts forge, direct or empty.
func ValidateReleaseChannel(channel string) error {
	switch channel {
	case "", ReleaseChannelForge, ReleaseChannelDirect:
		return nil
	default:
		return errors.Wrapf(ErrInvalidReleaseChannel, "%q (known: forge, direct)", channel)
	}
}

func joinFeatureIDs(ids []props.FeatureID) string {
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		names = append(names, string(id))
	}

	return strings.Join(names, ", ")
}

// releaseSourceTypeFor is the go/forge source type a project releases from:
// the backend's for the forge channel, direct for the direct channel, the
// pre-0195 host substring when neither is recorded but a host is, and empty
// for a project that is not hosted and has no channel.
func releaseSourceTypeFor(backend props.FeatureID, channel, host string) string {
	switch {
	case channel == ReleaseChannelDirect:
		return ReleaseChannelDirect
	case backend != "":
		return string(backend)
	case host != "":
		return releaseProviderForHost(host)
	default:
		return "" // not hosted, no channel: nothing to release from
	}
}

// deriveForgeBackend reads a backend out of a manifest that predates the
// field (spec 0195 D11): the single enabled forge feature; else the release
// type when it names a forge; else the host substring. Two enabled forges
// resolve through the release type, and are ambiguous when it names neither.
func deriveForgeBackend(m *Manifest) (props.FeatureID, error) {
	enabled := enabledForgeFeatures(m.Properties.Features)
	byType := forgeBackendNamed(m.ReleaseSource.Type)

	switch {
	case len(enabled) == 1:
		return enabled[0], nil
	case len(enabled) > 1:
		if byType != "" && slices.Contains(enabled, byType) {
			return byType, nil
		}

		return "", errors.WithStack(ErrAmbiguousForgeBackend)
	case byType != "":
		return byType, nil
	case m.ReleaseSource.Host != "":
		return props.FeatureID(releaseProviderForHost(m.ReleaseSource.Host)), nil
	default:
		return "", nil
	}
}

// enabledForgeFeatures lists the forge features a manifest enables.
func enabledForgeFeatures(features []ManifestFeature) []props.FeatureID {
	backends := ForgeBackends()

	var enabled []props.FeatureID

	for _, f := range features {
		if f.Enabled && slices.Contains(backends, props.FeatureID(f.Name)) {
			enabled = append(enabled, props.FeatureID(f.Name))
		}
	}

	return enabled
}

// forgeBackendNamed returns name as a backend when it is one, else "".
func forgeBackendNamed(name string) props.FeatureID {
	if slices.Contains(ForgeBackends(), props.FeatureID(name)) {
		return props.FeatureID(name)
	}

	return ""
}

// manifestModulePath is the module path a manifest records, or the pre-0195
// derivation from host and repository when it predates the field.
func manifestModulePath(m Manifest) string {
	if m.Properties.ModulePath != "" {
		return m.Properties.ModulePath
	}

	_, org, repoName := m.GetReleaseSource()
	if m.ReleaseSource.Host == "" || org == "" || repoName == "" {
		// Nothing to derive from: a project not hosted on a forge records
		// its module path, and a manifest without one is left as is rather
		// than given "//" (F12).
		return ""
	}

	return m.ReleaseSource.Host + "/" + org + "/" + repoName
}

// syncDerivedManifestFields records, on a manifest that predates them, the
// fields regenerate derives so the project states its choice from then on:
// the chat providers (spec 0194 D7), the forge backend and the module path
// (spec 0195 D11). It runs before anything renders, and writes the manifest
// only when something was derived.
func (g *Generator) syncDerivedManifestFields(m *Manifest) error {
	changed, err := deriveMissingManifestFields(m)
	if err != nil {
		return err
	}

	// After derivation, so a manifest that has just gained the default
	// provider set is told the one thing derivation cannot decide.
	g.warnUndecidedChatDefault(m)

	if !changed {
		return nil
	}

	if err := g.marshalManifestFile(ManifestPathFor(g.config.Path), m); err != nil {
		return err
	}

	g.props.Logger.Info("manifest predates some fields; recorded the derived values",
		"backend", m.ReleaseSource.Backend, "module_path", m.Properties.ModulePath,
		"chat_providers", len(m.Properties.Chat.Providers))

	return nil
}

// warnUndecidedChatDefault says what an older manifest's author has still to
// decide: with several providers linked and no default, regenerate proceeds
// without an author default rather than guessing (spec 0196 D3).
func (g *Generator) warnUndecidedChatDefault(m *Manifest) {
	c := m.Properties.Chat
	if c.Default.Provider != "" || len(c.Providers) < 2 || !featureEnabledIn(m.Properties.Features, string(props.AiCmd)) {
		return
	}

	g.props.Logger.Warn("the manifest links several chat providers and names no default; the tool ships without one",
		"set", "properties.chat.default.provider", "providers", strings.Join(c.Providers, ", "))
}

// deriveMissingManifestFields fills the derivable fields a manifest lacks and
// reports whether it changed anything.
func deriveMissingManifestFields(m *Manifest) (bool, error) {
	changed := deriveMissingChatFields(&m.Properties)

	if m.ReleaseSource.Backend == "" && m.ReleaseSource.Type != ReleaseChannelDirect {
		backend, err := deriveForgeBackend(m)
		if err != nil {
			return false, err
		}

		if backend != "" {
			m.ReleaseSource.Backend = backend
			changed = true
		}
	}

	if m.Properties.ModulePath == "" {
		if derived := manifestModulePath(*m); derived != "" {
			m.Properties.ModulePath = derived
			changed = true
		}
	}

	// A manifest from before version.go existed keeps the go line it has
	// today, which is the running toolchain's (spec 0197 D3).
	if m.Version.Go == "" {
		m.Version.Go = resolveGoVersion("")
		changed = true
	}

	return changed, nil
}

// deriveMissingChatFields fills the chat block an older manifest lacks: the
// default provider set (spec 0194 D7), and the default provider when exactly
// one is linked. Between several the generator does not choose (spec 0196 D3,
// OQ5).
func deriveMissingChatFields(p *ManifestProperties) bool {
	if !featureEnabledIn(p.Features, string(props.AiCmd)) {
		return false
	}

	changed := false

	if p.Chat.Providers == nil {
		p.Chat.Providers = DefaultChatProviders()
		changed = true
	}

	if p.Chat.Default.Provider == "" && len(p.Chat.Providers) == 1 {
		p.Chat.Default.Provider = p.Chat.Providers[0]
		changed = true
	}

	return changed
}

// ciSkeleton names an embedded CI asset set; an empty root means the backend
// has none (spec 0195 D8).
type ciSkeleton struct {
	root string
}

// ciSkeletonFor chooses the CI asset set from the backend, falling back to the
// recorded release provider for a manifest that predates the field.
func ciSkeletonFor(backend props.FeatureID, releaseProvider string) ciSkeleton {
	key := string(backend)
	if key == "" {
		key = releaseProvider
	}

	switch key {
	case "github":
		return ciSkeleton{root: "assets/skeleton-github"}
	case "gitlab":
		return ciSkeleton{root: "assets/skeleton-gitlab"}
	default:
		return ciSkeleton{}
	}
}
