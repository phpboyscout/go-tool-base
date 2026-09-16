package props

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/features"
)

// TestBuiltinsAreRegistered pins the built-in seed: every constant in the
// FeatureID block must be present as a Builtin descriptor with the ConstName the
// generator emits, and DefaultFeatures must agree with the descriptors' Default.
func TestBuiltinsAreRegistered(t *testing.T) {
	t.Parallel()

	byID := map[FeatureID]FeatureDescriptor{}
	for _, d := range DescriptorsIn(features.Default().Snapshot()) {
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
		assert.Equalf(t, PackagePath, d.ConstPackage, "ConstPackage for %q", id)
		assert.Equalf(t, w.enabled, d.Default, "Default for %q", id)
	}

	assert.Lenf(t, byID, len(want),
		"registry holds %d built-ins, expected %d — a new constant needs a descriptor", len(byID), len(want))
}

// TestDescriptorsIn_MirrorsTheSnapshot is the point of the whole change: the
// enumeration is what was declared, not a second hand-maintained list. This is
// the defect that let github/bitbucket vanish from doctor's feature matrix.
func TestDescriptorsIn_MirrorsTheSnapshot(t *testing.T) {
	t.Parallel()

	snap := features.Default().Snapshot()
	all := snap.Descriptors()
	ours := DescriptorsIn(snap)

	require.Len(t, ours, len(all), "every descriptor on the default registry is GTB's")

	for i, d := range all {
		assert.Equal(t, d.FeatureID(), ours[i].ID, "DescriptorsIn must keep the snapshot's order")
	}
}

// TestOrdering_IsTotalAndIndependentOfRegistrationOrder guards D4: built-ins
// hold their declared order, and everything else sorts by (kind, id). Go runs
// init() in dependency-then-filename order, which is stable for a fixed build
// but moves with the import graph — doctor's report and the generator's golden
// files must not.
func TestOrdering_IsTotalAndIndependentOfRegistrationOrder(t *testing.T) {
	t.Parallel()

	var ids []FeatureID
	for _, d := range features.Default().Snapshot().Descriptors() {
		ids = append(ids, d.FeatureID())
	}

	// Built-ins first, in the order the const block declares them.
	wantHead := []FeatureID{
		UpdateCmd, InitCmd, McpCmd, DocsCmd, AiCmd, DoctorCmd,
		ConfigCmd, ChangelogCmd, ManCmd, TelemetryCmd,
	}
	require.GreaterOrEqual(t, len(ids), len(wantHead))
	assert.Equal(t, wantHead, ids[:len(wantHead)], "built-ins must keep their declared order")

	// Repeated snapshots must not vary.
	var again []FeatureID
	for _, d := range features.Default().Snapshot().Descriptors() {
		again = append(again, d.FeatureID())
	}

	assert.Equal(t, ids, again, "enumeration must be stable across calls")
}

// TestRegisterFeature_RejectsDefaultOnNonBuiltin guards D9. A blank import must
// change what is *available*, never what is *on* — otherwise providers.go
// becomes a behavioural file and a regulated downstream's opt-outs turn
// load-bearing.
func TestRegisterFeature_RejectsDefaultOnNonBuiltin(t *testing.T) {
	t.Parallel()

	err := validateDescriptor(FeatureDescriptor{
		ID:           FeatureID("someforge"),
		ConstName:    "SomeforgeFeature",
		ConstPackage: "example.com/someforge",
		Kind:         KindForge,
		Default:      true,
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
			d:    FeatureDescriptor{ConstName: "X", ConstPackage: "p", Kind: KindForge},
			want: ErrInvalidDescriptor,
		},
		{
			name: "empty ConstName",
			d:    FeatureDescriptor{ID: FeatureID("x"), ConstPackage: "p", Kind: KindForge},
			want: ErrInvalidDescriptor,
		},
		{
			name: "empty ConstPackage",
			d:    FeatureDescriptor{ID: FeatureID("x"), ConstName: "X", Kind: KindForge},
			want: ErrInvalidDescriptor,
		},
		{
			name: "empty kind",
			d:    FeatureDescriptor{ID: FeatureID("x"), ConstName: "X", ConstPackage: "p"},
			want: ErrInvalidDescriptor,
		},
		{
			name: "duplicate id",
			d:    FeatureDescriptor{ID: UpdateCmd, ConstName: "UpdateCmd", ConstPackage: PackagePath, Kind: KindForge},
			want: ErrDuplicateFeature,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateDescriptor(tt.d, DescriptorsIn(features.Default().Snapshot()))
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

	snap := features.Default().Snapshot()
	assert.Len(t, snap.OfKind(KindBuiltin), len(snap.Descriptors()),
		"with only built-ins registered, the builtin kind is the whole set")

	assert.Empty(t, snap.OfKind(KindForge))
	assert.Empty(t, snap.OfKind(FeatureKind("nonexistent")))
}

// TestResolvedSet_UsesRegistryDefaults keeps the existing behaviour wired to
// the registry's defaults rather than to DefaultFeatures directly.
func TestResolvedSet_UsesRegistryDefaults(t *testing.T) {
	t.Parallel()

	set := enabledFor(t, Tool{})

	assert.True(t, set.Enabled(UpdateCmd), "update is default-enabled")
	assert.False(t, set.Enabled(AiCmd), "ai is default-disabled")
	assert.False(t, set.Enabled(FeatureID("unregistered")))
}

// TestRegisterFeature_OnAnOwnRegistry is spec 0199 D5: a test that declares a
// feature declares it on a Registry of its own, so the default registry is a
// function of the import graph and every enumeration test can run in parallel.
func TestRegisterFeature_OnAnOwnRegistry(t *testing.T) {
	t.Parallel()

	r := features.NewRegistry()
	late := FeatureDescriptor{
		ID:           FeatureID("latecomer"),
		ConstName:    "LatecomerFeature",
		ConstPackage: "example.com/latecomer",
		Kind:         KindForge,
	}

	require.NoError(t, registerFeature(r, late))
	require.ErrorIs(t, registerFeature(r, late), ErrDuplicateFeature)

	_, inDefault := features.Default().Snapshot().Lookup("latecomer")
	assert.False(t, inDefault, "the default registry never sees a test's feature")

	got := descriptorsOf(r.Snapshot())
	require.Len(t, got, 1)
	assert.Equal(t, late, got[0])
}
