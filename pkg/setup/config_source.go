package setup

import (
	"context"
	"slices"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// SlotConfigSource is where a source kind's link package contributes its
// factory and initialiser (spec 0204 D3).
const SlotConfigSource features.Slot = "config-source"

// slotConfigSourceOverride is where OverrideConfigSource contributes.
const slotConfigSourceOverride features.Slot = "config-source-override"

// ConfigSourcePrefix prefixes a linked source kind's feature ID; see
// props.LinkedNames.
const ConfigSourcePrefix = "config-source-"

// configSourceOverrides is the link feature an author's override declares on
// first use, so an override needs no import beyond pkg/setup.
const configSourceOverrides = props.FeatureID("config-source-overrides")

// overrideOnlyKinds are the kinds no factory can build from configuration
// alone: etcd and sftp have no provider convention to adopt, and billy, iofs
// and afero wrap an in-process value (spec 0204 D3, D19).
var overrideOnlyKinds = []string{"etcd", "sftp", "billy", "iofs", "afero"}

var (
	// ErrConfigSourceUnconfigured is a required slot nobody has configured.
	ErrConfigSourceUnconfigured = errors.NewSentinel("gtb.setup.config_source_unconfigured", "config source not configured")
	// ErrConfigSourceNeedsOverride is an override-only slot with no override.
	ErrConfigSourceNeedsOverride = errors.NewSentinel("gtb.setup.config_source_needs_override", "config source needs an author override")
	// ErrConfigSourceKindNotLinked is a slot whose kind the binary does not
	// link.
	ErrConfigSourceKindNotLinked = errors.NewSentinel("gtb.setup.config_source_kind_not_linked", "config source kind not linked")
	// ErrConfigSourceOverrideUndeclared is an override for a slot the tool
	// does not declare, refused so a stale override cannot add a layer.
	ErrConfigSourceOverrideUndeclared = errors.NewSentinel("gtb.setup.config_source_override_undeclared", "override for an undeclared config source")
	// ErrConfigSourceUnavailable is a required slot whose backend could not
	// be built.
	ErrConfigSourceUnavailable = errors.NewSentinel("gtb.setup.config_source_unavailable", "config source unavailable")
)

// ConfigBootstrap is the view a factory may read: the layers a repository
// cannot plant, embedded defaults, the tool's own files, the environment and
// flags (spec 0204 D5).
type ConfigBootstrap interface{ View() *config.View }

// SourceFactory builds one slot's backend from its settings, the
// config.sources.<name> subtree of the bootstrap view. Settings are nil for
// an override of a slot nobody configured.
type SourceFactory func(ctx context.Context, settings config.Reader, b ConfigBootstrap) (config.Backend, error)

// ConfigSourceInitialiser builds the initialiser that asks for a slot's
// settings and writes them under config.sources.<slot name>. A kind is told
// the slot because a tool may declare the same kind twice.
type ConfigSourceInitialiser func(p *props.Props, slot props.ConfigSource) Initialiser

// ConfigSourceKind is what a kind's link package registers.
type ConfigSourceKind struct {
	Kind              string
	Factory           SourceFactory
	Initialiser       ConfigSourceInitialiser
	WritableByDefault bool
}

// SourceKindOption adjusts a kind's registration.
type SourceKindOption func(*ConfigSourceKind)

// WritableByDefault makes a kind's slots writable unless they say otherwise,
// which only the keychain is (spec 0204 D12).
func WritableByDefault() SourceKindOption {
	return func(k *ConfigSourceKind) { k.WritableByDefault = true }
}

type configSourceOverride struct {
	name    string
	factory SourceFactory
}

// ConfigSourceDescriptor is the link feature a source kind's package
// declares. Like every link it defaults on.
func ConfigSourceDescriptor(kind string) props.FeatureDescriptor {
	return props.FeatureDescriptor{ID: props.FeatureID(ConfigSourcePrefix + kind), Kind: props.KindLink, Default: true}
}

// RegisterConfigSourceKind declares a kind's link feature on the default
// registry and contributes its factory and initialiser. A kind's link
// package calls it from init.
func RegisterConfigSourceKind(kind string, f SourceFactory, init ConfigSourceInitialiser, opts ...SourceKindOption) {
	props.RegisterFeature(ConfigSourceDescriptor(kind))
	RegisterConfigSourceKindOn(features.Default(), kind, f, init, opts...)
}

// RegisterConfigSourceKindOn contributes a kind to a caller's registry, which
// must already declare ConfigSourceDescriptor(kind).
func RegisterConfigSourceKindOn(r features.Registry, kind string, f SourceFactory, init ConfigSourceInitialiser, opts ...SourceKindOption) {
	k := ConfigSourceKind{Kind: kind, Factory: f, Initialiser: init}
	for _, opt := range opts {
		opt(&k)
	}

	r.Contribute(ConfigSourceDescriptor(kind).ID, SlotConfigSource, k)
}

// ConfigSourceKindsIn returns every enabled kind in s, by kind.
func ConfigSourceKindsIn(s features.Set) map[string]ConfigSourceKind {
	out := map[string]ConfigSourceKind{}

	for _, d := range s.EnabledDescriptors() {
		kinds, _ := features.ContributionsOf[ConfigSourceKind](s, d.FeatureID(), SlotConfigSource)
		for _, k := range kinds {
			out[k.Kind] = k
		}
	}

	return out
}

// OverrideConfigSource replaces the factory for one named slot (spec 0204
// D19). Call it from hand-written code in the tool's main package; the
// generator never writes one. It panics only on a registry the framework
// itself corrupted, as feature registration at init does.
func OverrideConfigSource(name string, f SourceFactory) {
	if err := OverrideConfigSourceOn(features.Default(), name, f); err != nil {
		panic(err)
	}
}

// OverrideConfigSourceOn is OverrideConfigSource against a caller's registry.
func OverrideConfigSourceOn(r features.Registry, name string, f SourceFactory) error {
	if _, declared := r.Snapshot().Lookup(configSourceOverrides); !declared {
		d := props.FeatureDescriptor{ID: configSourceOverrides, Kind: props.KindLink, Default: true}
		if err := r.Declare(d); err != nil {
			return errors.Wrap(err, "declaring config source overrides")
		}
	}

	r.Contribute(configSourceOverrides, slotConfigSourceOverride, configSourceOverride{name: name, factory: f})

	return nil
}

// ConfigSourceOverridesIn returns the overrides registered in s, by slot name.
func ConfigSourceOverridesIn(s features.Set) map[string]SourceFactory {
	out := map[string]SourceFactory{}

	overrides, _ := features.ContributionsOf[configSourceOverride](s, configSourceOverrides, slotConfigSourceOverride)
	for _, o := range overrides {
		out[o.name] = o.factory
	}

	return out
}

// IsOverrideOnlyKind reports whether only an author override can build kind.
func IsOverrideOnlyKind(kind string) bool {
	return slices.Contains(overrideOnlyKinds, kind)
}

// ConfigSourceUnconfiguredError refuses a required slot nobody configured,
// naming the command that configures it (spec 0204 D6).
func ConfigSourceUnconfiguredError(p *props.Props, slot props.ConfigSource) error {
	return errors.WithHintf(errors.Wrapf(ErrConfigSourceUnconfigured, "%q (%s)", slot.Name, slot.Kind),
		"run `%s init config %s`, or set config.sources.%s in your config file or the tool's defaults",
		p.Tool.Name, slot.Name, slot.Name)
}

// RunConfigSourceInit configures one declared slot: its kind's initialiser
// asks for the settings and writes them under config.sources.<name> in the
// tool's config file in dir (spec 0204 D3). An override-only slot has nothing
// to ask, since the tool's own code builds it.
func RunConfigSourceInit(ctx context.Context, p *props.Props, slot props.ConfigSource, dir string) error {
	if IsOverrideOnlyKind(slot.Kind) {
		return errors.WithHintf(errors.Wrapf(ErrConfigSourceNeedsOverride, "%q (%s)", slot.Name, slot.Kind),
			"the %s source is built by the tool's own code, so there is nothing to configure here", slot.Name)
	}

	kind, ok := ConfigSourceKindsIn(p.GetFeatures())[slot.Kind]
	if !ok || kind.Initialiser == nil {
		return errors.WithHintf(errors.Wrapf(ErrConfigSourceKindNotLinked, "%q (%s)", slot.Name, slot.Kind),
			"blank-import gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/%s, or regenerate", slot.Kind)
	}

	editor, _, err := OpenConfigEditor(ctx, p, dir, false)
	if err != nil {
		return err
	}

	return kind.Initialiser(p, slot).Configure(ctx, p, editor)
}
