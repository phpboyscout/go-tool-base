package generator

// BreakingChanges is a map of version strings to descriptions of breaking changes introduced in that version.
// The keys should be valid semantic version strings (e.g., "v2.10.0").
// The values are messages displayed to the user when they upgrade across these versions.
var BreakingChanges = map[string]string{
	"v2.10.0": "Breaking changes to Assets interface and command signatures. Please refer to the migration guide.",
}
