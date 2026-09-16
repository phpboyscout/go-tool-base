package credentialposture_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
)

// The registry is the extension point spec 0189 Q6 settled on. Since spec 0199
// OQ5 a descriptor is a contribution on the feature registry under the feature
// that consumes it, so every test declares on a registry of its own and reads
// it back through a snapshot or a Set, and runs in parallel.

// testFeature is a minimal features.Descriptor for declaring the owning
// features these tests gate on.
type testFeature struct {
	id features.ID
	on bool
}

func (f testFeature) FeatureID() features.ID     { return f.id }
func (f testFeature) FeatureKind() features.Kind { return "test" }
func (f testFeature) DefaultOn() bool            { return f.on }
func (f testFeature) IsDynamic() bool            { return false }

func TestRegisterOn_AndReportAll(t *testing.T) {
	t.Setenv("REGISTRY_TEST_VAR", "s3cret")

	r := features.NewRegistry()
	credentialposture.RegisterOn(r, credentialposture.Descriptor{
		Owner:      "test:registry",
		Label:      "Registry test credential",
		EnvKey:     "registrytest.env",
		LiteralKey: "registrytest.key",
	})

	declared := credentialposture.RegisteredIn(r.Snapshot())
	require.Len(t, declared, 1)
	assert.Equal(t, "test:registry", declared[0].Owner)

	results := credentialposture.Report(context.Background(),
		fakeReader{"registrytest.env": "REGISTRY_TEST_VAR"}, declared, nil)
	require.Len(t, results, 1)
	require.NoError(t, results[0].Err)
	assert.Equal(t, credentialposture.OriginEnvRef, results[0].Posture.Origin)
}

func TestRegisteredIn_ReRegisteringReplacesRatherThanDuplicates(t *testing.T) {
	t.Parallel()

	d := credentialposture.Descriptor{Owner: "test:dedupe", Label: "first", LiteralKey: "dedupe.key"}

	r := features.NewRegistry()
	credentialposture.RegisterOn(r, d)

	d.Label = "second"
	credentialposture.RegisterOn(r, d)

	got := credentialposture.RegisteredIn(r.Snapshot())
	require.Len(t, got, 1, "the same credential must not be reported twice")
	assert.Equal(t, "second", got[0].Label, "the later registration wins")
}

func TestRegisteredIn_IsOrderedStably(t *testing.T) {
	t.Parallel()

	r := features.NewRegistry()
	credentialposture.RegisterOn(r, credentialposture.Descriptor{Owner: "test:zzz", Label: "z", LiteralKey: "z.key"})
	credentialposture.RegisterOn(r, credentialposture.Descriptor{Owner: "test:aaa", Label: "a", LiteralKey: "a.key", Feature: "forge"})

	first := credentialposture.RegisteredIn(r.Snapshot())
	second := credentialposture.RegisteredIn(r.Snapshot())

	require.Len(t, first, 2)
	assert.Equal(t, "test:aaa", first[0].Owner, "ordered by owner across features")
	assert.Equal(t, first, second, "report order must not vary between runs")
}

// TestDeclaredFor_IsTheEnabledFeaturesCredentials is OQ5: a Set hands out the
// credentials of its enabled features plus those declared under no feature,
// so a reporting surface needs no predicate for #55.
func TestDeclaredFor_IsTheEnabledFeaturesCredentials(t *testing.T) {
	t.Parallel()

	r := features.NewRegistry()
	require.NoError(t, r.Declare(testFeature{id: "ai"}))
	require.NoError(t, r.Declare(testFeature{id: "forge"}))

	credentialposture.RegisterOn(r, credentialposture.Descriptor{Owner: "ai", Label: "AI", Feature: "ai", EnvKey: "ai.env"})
	credentialposture.RegisterOn(r, credentialposture.Descriptor{Owner: "forge", Label: "Forge", Feature: "forge", EnvKey: "forge.env"})
	credentialposture.RegisterOn(r, credentialposture.Descriptor{Owner: "tool", Label: "Own", EnvKey: "own.env"})

	set, err := features.Resolve(r.Snapshot(), []features.State{{ID: "ai", Enabled: true}})
	require.NoError(t, err)

	var owners []string
	for _, d := range credentialposture.DeclaredFor(set) {
		owners = append(owners, d.Owner)
	}

	assert.Equal(t, []string{"ai", "tool"}, owners, "the enabled feature's and the feature-less credential, not the disabled forge's")

	all := credentialposture.RegisteredIn(r.Snapshot())
	assert.Len(t, all, 3, "Registered is every declaration regardless of enablement")
}

func TestReportAll_OneBrokenCredentialDoesNotLoseTheOthers(t *testing.T) {
	t.Setenv("REPORTALL_TEST_VAR", "s3cret")

	r := features.NewRegistry()
	credentialposture.RegisterOn(r, credentialposture.Descriptor{Owner: "test:broken", Label: "broken", KeychainKey: "broken.keychain"})
	credentialposture.RegisterOn(r, credentialposture.Descriptor{Owner: "test:working", Label: "working", EnvKey: "working.env"})

	results := credentialposture.Report(context.Background(), fakeReader{
		"broken.keychain": "malformed-no-slash",
		"working.env":     "REPORTALL_TEST_VAR",
	}, credentialposture.RegisteredIn(r.Snapshot()), nil)

	var sawBroken, sawWorking bool

	for _, res := range results {
		switch res.Posture.Owner {
		case "test:broken":
			sawBroken = true

			require.Error(t, res.Err, "the broken credential carries its own error")
		case "test:working":
			sawWorking = true

			require.NoError(t, res.Err)
			assert.Equal(t, credentialposture.OriginEnvRef, res.Posture.Origin)
		}
	}

	assert.True(t, sawBroken && sawWorking, "one failure must not suppress the rest of the report")
}

// TestRegister_ContributesToTheDefault pins the init-time entry point onto the
// default registry, under an owner nobody else uses; production bundles
// register this way at init.
func TestRegister_ContributesToTheDefault(t *testing.T) {
	t.Parallel()

	credentialposture.Register(credentialposture.Descriptor{Owner: "test:default-probe", Label: "probe", LiteralKey: "probe.key"})

	found := false
	for _, d := range credentialposture.Registered() {
		if d.Owner == "test:default-probe" {
			found = true
		}
	}

	assert.True(t, found)
}
