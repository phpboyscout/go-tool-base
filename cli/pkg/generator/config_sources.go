package generator

import (
	"slices"
	"strings"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// configSourcePackage is where each source kind's link package lives in the
// framework (spec 0204 D20).
const configSourcePackage = "gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/"

// configSourceKinds are the kinds the framework ships a link package for.
var configSourceKinds = []string{
	"file", "keychain", "vault", "consul",
	"aws-s3", "aws-ssm", "aws-secrets",
	"azure-blob", "azure-keyvault", "azure-appconfig",
	"gcp-gcs", "gcp-secret", "gcp-parameter",
}

// overrideOnlySourceKinds are built by an author's override alone, so they
// have no package to link (spec 0204 D19).
var overrideOnlySourceKinds = []string{"etcd", "sftp", "billy", "iofs", "afero"}

// ManifestConfigSource is one declared config source slot. Where it connects
// is runtime configuration under config.sources.<name>, never recorded here
// (spec 0204 R1).
type ManifestConfigSource struct {
	Name string `yaml:"name"`
	Kind string `yaml:"kind"`
	// Required, when absent, is true (spec 0204 D6).
	Required *bool `yaml:"required,omitempty"`
	// Writable, when absent, is the kind's default (spec 0204 D7, D12).
	Writable *bool `yaml:"writable,omitempty"`
}

// DeriveEnvPrefix is the environment prefix a project name gives: upper-case,
// hyphens as underscores (my-tool becomes MY_TOOL).
func DeriveEnvPrefix(name string) string {
	return strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
}

// defaultSourceLayers places declared sources when no order is stated: above
// the defaults and below the user's own files, so a source is a shared
// baseline those files, the environment and flags can override (spec 0204
// R9). Nil when there are no sources: the framework default applies.
func defaultSourceLayers(sources []ManifestConfigSource) []string {
	if len(sources) == 0 {
		return nil
	}

	layers := []string{string(props.LayerDefaults)}
	for _, s := range sources {
		layers = append(layers, s.Name)
	}

	return append(layers, string(props.LayerFiles), string(props.LayerProject), string(props.LayerEnv), string(props.LayerFlags))
}

// withSourceImplications applies what declaring a source implies before
// anything renders: a placement when none is stated (R9), an environment
// prefix when none is set (D21), and the keychain feature for a keychain
// source (D12).
func withSourceImplications(config SkeletonConfig) SkeletonConfig {
	if len(config.ConfigSources) == 0 {
		return config
	}

	if len(config.ConfigLayers) == 0 {
		config.ConfigLayers = defaultSourceLayers(config.ConfigSources)
	}

	if config.EnvPrefix == "" {
		config.EnvPrefix = DeriveEnvPrefix(config.Name)
	}

	config.Features = withKeychainForSources(config.Features, config.ConfigSources)

	return config
}

// withKeychainForSources enables the keychain feature when a keychain source
// is declared and nothing says otherwise. An explicit disable is left for
// validation to refuse.
func withKeychainForSources(features []ManifestFeature, sources []ManifestConfigSource) []ManifestFeature {
	if !slices.ContainsFunc(sources, func(s ManifestConfigSource) bool { return s.Kind == "keychain" }) {
		return features
	}

	if slices.ContainsFunc(features, func(f ManifestFeature) bool { return f.Name == KeychainFeature }) {
		return features
	}

	return append(features, ManifestFeature{Name: KeychainFeature, Enabled: true})
}

// configLinkModules are the packages cmd/<name>/config.go blank-imports: one
// per linked format and one per linked source kind, each once.
func configLinkModules(formats []string, sources []ManifestConfigSource) []string {
	modules := configFormatModules(formats)

	for _, kind := range configSourceKinds {
		if slices.ContainsFunc(sources, func(s ManifestConfigSource) bool { return s.Kind == kind }) {
			modules = append(modules, configSourcePackage+kind)
		}
	}

	return modules
}

// ValidateConfigSources refuses an unknown kind, a slot D15 refuses, and a
// keychain source beside an explicitly disabled keychain feature (D12).
func ValidateConfigSources(sources []ManifestConfigSource, layers []string, features []ManifestFeature) error {
	if len(sources) == 0 {
		return nil
	}

	if len(layers) == 0 {
		layers = defaultSourceLayers(sources)
	}

	spec := props.ConfigSpec{}
	for _, l := range layers {
		spec.Layers = append(spec.Layers, props.ConfigLayer(l))
	}

	for _, s := range sources {
		if !slices.Contains(configSourceKinds, s.Kind) && !slices.Contains(overrideOnlySourceKinds, s.Kind) {
			return rejectf("ConfigSources", "unknown config source kind (valid: "+
				strings.Join(append(slices.Clone(configSourceKinds), overrideOnlySourceKinds...), ", ")+")", s.Kind)
		}

		if s.Kind == "keychain" && featureExplicitlyDisabled(features, KeychainFeature) {
			return rejectf("ConfigSources", "a keychain source needs the keychain feature, which is disabled", s.Name)
		}

		spec.Sources = append(spec.Sources, props.ConfigSource{Name: s.Name, Kind: s.Kind})
	}

	if err := props.ValidateConfigSpec(spec); err != nil {
		return rejectf("ConfigSources", err.Error()+": "+errors.FlattenHints(err), "")
	}

	return nil
}

// ValidateConfigStack checks a declared layer list and its source slots
// together, since a source's layer is named for its slot and the builtin-only
// layer check would refuse it.
func ValidateConfigStack(layers []string, sources []ManifestConfigSource, features []ManifestFeature) error {
	if len(sources) == 0 {
		return ValidateConfigLayers(layers)
	}

	return ValidateConfigSources(sources, layers, features)
}

func featureExplicitlyDisabled(features []ManifestFeature, name string) bool {
	return slices.ContainsFunc(features, func(f ManifestFeature) bool { return f.Name == name && !f.Enabled })
}

// sourceTemplateData carries the declared slots into the root template.
func sourceTemplateData(sources []ManifestConfigSource) []templates.ConfigSourceData {
	out := make([]templates.ConfigSourceData, len(sources))
	for i, s := range sources {
		out[i] = templates.ConfigSourceData{Name: s.Name, Kind: s.Kind, Required: s.Required, Writable: s.Writable}
	}

	return out
}

// ConfigSourceKinds returns every kind a slot may declare: the shipped kinds,
// then the override-only ones.
func ConfigSourceKinds() []string {
	return append(slices.Clone(configSourceKinds), overrideOnlySourceKinds...)
}

// IsOverrideOnlySourceKind reports whether only an author's override builds
// kind.
func IsOverrideOnlySourceKind(kind string) bool {
	return slices.Contains(overrideOnlySourceKinds, kind)
}
