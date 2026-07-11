package templates

import "gitlab.com/phpboyscout/go-tool-base/pkg/props"

// FeatureDescriptor carries the three facts the generator needs to round-trip a
// built-in feature between its config/manifest name, the Go source token that
// names its props FeatureCmd constant, and its default-enabled state.
type FeatureDescriptor struct {
	// Cmd is the feature's props.FeatureCmd; its string value is the
	// config/manifest name (e.g. "ai").
	Cmd props.FeatureCmd
	// ConstName is the exported Go identifier of the constant as it appears in
	// generated source, e.g. "AiCmd". It cannot be derived reliably from the
	// value (mcp -> McpCmd), so it is recorded explicitly.
	ConstName string
	// Default is the framework default-enabled state, mirroring
	// props.DefaultFeatures.
	Default bool
}

// FeatureCatalogue is the ordered, canonical name<->constant<->default table for
// every built-in props.FeatureCmd feature. It is generator-internal tooling (not
// public API): the SetFeatures renderer and the manifest scanner both derive
// from it, so the mapping has a single origin and the two sides cannot drift.
// The historical bug it fixes: the scanner froze at the original four features
// while the set grew. A test guards this list against props.AllFeatures so a new
// framework feature cannot silently omit its generator handling.
//
// keychain is intentionally absent: it has no FeatureCmd and is a build-time
// blank-import decision, recovered from its artefact rather than SetFeatures.
var FeatureCatalogue = []FeatureDescriptor{
	{props.UpdateCmd, "UpdateCmd", true},
	{props.InitCmd, "InitCmd", true},
	{props.McpCmd, "McpCmd", true},
	{props.DocsCmd, "DocsCmd", true},
	{props.DoctorCmd, "DoctorCmd", true},
	{props.ChangelogCmd, "ChangelogCmd", true},
	{props.AiCmd, "AiCmd", false},
	{props.ConfigCmd, "ConfigCmd", false},
	{props.TelemetryCmd, "TelemetryCmd", false},
	{props.ManCmd, "ManCmd", false},
}
