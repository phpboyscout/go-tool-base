package templates

import (
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

// FeatureDescriptor carries the four facts the generator needs to round-trip a
// feature between its config/manifest name, the Go source token that names its
// props FeatureID constant, the package that declares that constant, and its
// default-enabled state.
type FeatureDescriptor struct {
	// Cmd is the feature's props.FeatureID; its string value is the
	// config/manifest name (e.g. "ai").
	Cmd props.FeatureID
	// ConstPackage is the import path of the package declaring ConstName.
	// Built-ins live in props; forge features live in pkg/setup/forge. The
	// emitter qualifies against this rather than assuming props, which is what
	// previously made forge features unemittable.
	ConstPackage string
	// ConstName is the exported Go identifier of the constant as it appears in
	// generated source, e.g. "AiCmd". It cannot be derived reliably from the
	// value (mcp -> McpCmd), so it is recorded explicitly.
	ConstName string
	// Default is the framework default-enabled state, mirroring
	// props.DefaultFeatures. Forge features are never default-enabled: a blank
	// import changes what is available, never what is on.
	Default bool
}

// FeatureCatalogue is the ordered, canonical name<->constant<->default table for
// every scaffoldable props.FeatureID feature. It is generator-internal tooling
// (not public API): the SetFeatures renderer and the manifest scanner both
// derive from it, so the mapping has a single origin and the two sides cannot
// drift. The historical bug it fixes: the scanner froze at the original four
// features while the set grew. A test guards this list against the props
// registry so a new framework feature cannot silently omit its generator
// handling.
//
// The table is written out rather than derived from the props registry, and it
// duplicates data props.FeatureDescriptor already carries — the guard test
// asserts the two agree field by field. That is a known cost, not a claim the
// duplication is free. Deriving it needs three things this package does not do
// yet:
//
//   - Lazy evaluation. props.FeatureDescriptors seals the registry on read, so
//     computing this at package init would make any later RegisterFeature panic
//     with ErrRegistrySealed.
//   - A Kind filter. The registry answers what this binary linked; the generator
//     needs the complete set a scaffolded tool may select, and a downstream
//     tool's own registrations are not GTB's to scaffold.
//   - Somewhere for keychain, which has no FeatureID to derive from.
//
// Note the ordering argument does NOT hold: FeatureDescriptors guarantees a
// stable total order derived from data, and names this generator's golden files
// as the reason it does. Whether the duplication is worth collapsing is being
// settled separately — see go-tool-base#11, which asks whether feature
// management belongs in a module of its own rather than split between props and
// the generator.
//
// keychain is intentionally absent: it has no FeatureID and is a build-time
// blank-import decision, recovered from its artefact rather than SetFeatures.
var FeatureCatalogue = []FeatureDescriptor{
	{props.UpdateCmd, props.PackagePath, "UpdateCmd", true},
	{props.InitCmd, props.PackagePath, "InitCmd", true},
	{props.McpCmd, props.PackagePath, "McpCmd", true},
	{props.DocsCmd, props.PackagePath, "DocsCmd", true},
	{props.DoctorCmd, props.PackagePath, "DoctorCmd", true},
	{props.ChangelogCmd, props.PackagePath, "ChangelogCmd", true},
	{props.AiCmd, props.PackagePath, "AiCmd", false},
	{props.ConfigCmd, props.PackagePath, "ConfigCmd", false},
	{props.TelemetryCmd, props.PackagePath, "TelemetryCmd", false},
	{props.ManCmd, props.PackagePath, "ManCmd", false},
	{forge.GithubFeature, forge.PackagePath, "GithubFeature", false},
	{forge.GitlabFeature, forge.PackagePath, "GitlabFeature", false},
	{forge.GiteaFeature, forge.PackagePath, "GiteaFeature", false},
	{forge.CodebergFeature, forge.PackagePath, "CodebergFeature", false},
	{forge.BitbucketFeature, forge.PackagePath, "BitbucketFeature", false},
}
