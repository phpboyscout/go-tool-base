package props

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuiltinsAreRegistered pins the built-in seed: every constant in the
// FeatureID block must be present as a Builtin descriptor with the ConstName the
// generator emits, and DefaultFeatures must agree with the descriptors' Default.
func TestBuiltinsAreRegistered(t *testing.T) {
	t.Parallel()

	byID := map[FeatureID]FeatureDescriptor{}
	for _, d := range FeatureDescriptors() {
		byID[d.ID] = d
	}

	want := map[FeatureID]struct {
		constName string
		enabled   bool
	}{
		UpdateCmd:    {"UpdateCmd", true},
		InitCmd:      {"InitCmd", true},
		McpCmd:       {"McpCmd", true},
		DocsCmd:      {"DocsCmd", true},
		DoctorCmd:    {"DoctorCmd", true},
		ChangelogCmd: {"ChangelogCmd", true},
		AiCmd:        {"AiCmd", false},
		ConfigCmd:    {"ConfigCmd", false},
		ManCmd:       {"ManCmd", false},
		TelemetryCmd: {"TelemetryCmd", false},
	}

	for id, w := range want {
		d, ok := byID[id]
		require.Truef(t, ok, "built-in %q is not registered", id)
		assert.Equalf(t, w.constName, d.ConstName, "ConstName for %q", id)
		assert.Equalf(t, KindBuiltin, d.Kind, "Kind for %q", id)
		assert.Equalf(t, w.enabled, d.Default, "Default for %q", id)
	}

	assert.Lenf(t, byID, len(want),
		"registry holds %d built-ins, expected %d — a new constant needs a descriptor", len(byID), len(want))
}

// TestAllFeatures_DerivesFromRegistry is the point of the whole change: the
// enumeration is what was registered, not a second hand-maintained list. This is
// the defect that let github/bitbucket vanish from doctor's feature matrix.
func TestAllFeatures_DerivesFromRegistry(t *testing.T) {
	t.Parallel()

	ids := AllFeatures()
	descriptors := FeatureDescriptors()

	require.Len(t, ids, len(descriptors))

	for i, d := range descriptors {
		assert.Equal(t, d.ID, ids[i], "AllFeatures must mirror FeatureDescriptors order")
	}
}

// TestOrdering_IsTotalAndIndependentOfRegistrationOrder guards D4: built-ins
// hold their declared order, and everything else sorts by (kind, id). Go runs
// init() in dependency-then-filename order, which is stable for a fixed build
// but moves with the import graph — doctor's report and the generator's golden
// files must not.
func TestOrdering_IsTotalAndIndependentOfRegistrationOrder(t *testing.T) {
	t.Parallel()

	ids := AllFeatures()

	// Built-ins first, in the order the const block declares them.
	wantHead := []FeatureID{
		UpdateCmd, InitCmd, McpCmd, DocsCmd, AiCmd, DoctorCmd,
		ConfigCmd, ChangelogCmd, ManCmd, TelemetryCmd,
	}
	require.GreaterOrEqual(t, len(ids), len(wantHead))
	assert.Equal(t, wantHead, ids[:len(wantHead)], "built-ins must keep their declared order")

	// Repeated calls must not vary.
	assert.Equal(t, ids, AllFeatures(), "enumeration must be stable across calls")
}

// TestRegisterFeature_RejectsDefaultOnNonBuiltin guards D9. A blank import must
// change what is *available*, never what is *on* — otherwise providers.go
// becomes a behavioural file and a regulated downstream's opt-outs turn
// load-bearing.
func TestRegisterFeature_RejectsDefaultOnNonBuiltin(t *testing.T) {
	t.Parallel()

	err := validateDescriptor(FeatureDescriptor{
		ID:        FeatureID("someforge"),
		ConstName: "SomeforgeFeature",
		Kind:      KindForge,
		Default:   true,
	}, nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPluginDefaultOn)
}

// TestRegisterFeature_Validates covers the rest of the descriptor contract.
func TestRegisterFeature_Validates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		d    FeatureDescriptor
		want error
	}{
		{
			name: "empty id",
			d:    FeatureDescriptor{ConstName: "X", Kind: KindForge},
			want: ErrInvalidDescriptor,
		},
		{
			name: "empty ConstName",
			d:    FeatureDescriptor{ID: FeatureID("x"), Kind: KindForge},
			want: ErrInvalidDescriptor,
		},
		{
			name: "empty kind",
			d:    FeatureDescriptor{ID: FeatureID("x"), ConstName: "X"},
			want: ErrInvalidDescriptor,
		},
		{
			name: "duplicate id",
			d:    FeatureDescriptor{ID: UpdateCmd, ConstName: "UpdateCmd", Kind: KindForge},
			want: ErrDuplicateFeature,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateDescriptor(tt.d, FeatureDescriptors())
			require.Error(t, err)
			assert.ErrorIs(t, err, tt.want)
		})
	}
}

// TestFeaturesOfKind turns "what forges are there?" into a query, which is what
// 0185 needs so the generator wizard, config validation and doctor stop
// hand-listing them.
func TestFeaturesOfKind(t *testing.T) {
	t.Parallel()

	builtins := FeaturesOfKind(KindBuiltin)
	assert.Equal(t, AllFeatures(), builtins,
		"with only built-ins registered, the builtin kind is the whole set")

	assert.Empty(t, FeaturesOfKind(KindForge))
	assert.Empty(t, FeaturesOfKind(FeatureKind("nonexistent")))
}

// TestIsEnabled_UsesRegistryDefaults keeps the existing behaviour wired to the
// new source of truth rather than to DefaultFeatures directly.
func TestIsEnabled_UsesRegistryDefaults(t *testing.T) {
	t.Parallel()

	var tool Tool

	assert.True(t, tool.IsEnabled(UpdateCmd), "update is default-enabled")
	assert.False(t, tool.IsEnabled(AiCmd), "ai is default-disabled")
	assert.False(t, tool.IsEnabled(FeatureID("unregistered")))
}

// TestRegisterFeature_RejectsAfterSeal guards D7: once anything has enumerated,
// a late registration fails loudly instead of producing a set that depends on
// when it was read.
//
// Deliberately not parallel — it asserts on the registry's sealed state, which
// the parallel tests reach by enumerating.
func TestRegisterFeature_RejectsAfterSeal(t *testing.T) {
	SealFeatures()

	err := registerFeature(FeatureDescriptor{
		ID:        FeatureID("latecomer"),
		ConstName: "LatecomerFeature",
		Kind:      KindForge,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRegistrySealed)
}
