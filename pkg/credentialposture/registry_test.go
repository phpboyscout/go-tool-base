package credentialposture_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
)

// The registry is the extension point spec 0189 Q6 settled on. These tests are
// deliberately not parallel: the registry is process-wide, and a parallel test
// registering a descriptor would be visible to every other one.

func TestRegistry_RegisterAndReport(t *testing.T) {
	t.Setenv("REGISTRY_TEST_VAR", "s3cret")

	credentialposture.Register(credentialposture.Descriptor{
		Owner:      "test:registry",
		Label:      "Registry test credential",
		EnvKey:     "registrytest.env",
		LiteralKey: "registrytest.key",
	})

	found := false

	for _, d := range credentialposture.Registered() {
		if d.Owner == "test:registry" {
			found = true
		}
	}

	require.True(t, found, "a registered credential must appear in the inventory")

	results := credentialposture.ReportAll(context.Background(),
		fakeReader{"registrytest.env": "REGISTRY_TEST_VAR"})

	var got *credentialposture.Result

	for i := range results {
		if results[i].Posture.Owner == "test:registry" {
			got = &results[i]
		}
	}

	require.NotNil(t, got)
	require.NoError(t, got.Err)
	assert.Equal(t, credentialposture.OriginEnvRef, got.Posture.Origin)
}

func TestRegistry_ReRegisteringReplacesRatherThanDuplicates(t *testing.T) {
	d := credentialposture.Descriptor{
		Owner:      "test:dedupe",
		Label:      "first",
		LiteralKey: "dedupe.key",
	}

	credentialposture.Register(d)

	d.Label = "second"
	credentialposture.Register(d)

	var count int

	var label string

	for _, got := range credentialposture.Registered() {
		if got.Owner == "test:dedupe" {
			count++
			label = got.Label
		}
	}

	assert.Equal(t, 1, count, "the same credential must not be reported twice")
	assert.Equal(t, "second", label, "the later registration wins")
}

func TestRegistry_ReportIsOrderedStably(t *testing.T) {
	credentialposture.Register(credentialposture.Descriptor{Owner: "test:zzz", Label: "z", LiteralKey: "z.key"})
	credentialposture.Register(credentialposture.Descriptor{Owner: "test:aaa", Label: "a", LiteralKey: "a.key"})

	first := credentialposture.Registered()
	second := credentialposture.Registered()

	require.Len(t, second, len(first))

	for i := range first {
		assert.Equal(t, first[i].Owner, second[i].Owner, "report order must not vary between runs")
	}
}

func TestReportAll_OneBrokenCredentialDoesNotLoseTheOthers(t *testing.T) {
	t.Setenv("REPORTALL_TEST_VAR", "s3cret")

	credentialposture.Register(credentialposture.Descriptor{
		Owner: "test:broken", Label: "broken", KeychainKey: "broken.keychain",
	})
	credentialposture.Register(credentialposture.Descriptor{
		Owner: "test:working", Label: "working", EnvKey: "working.env",
	})

	results := credentialposture.ReportAll(context.Background(), fakeReader{
		"broken.keychain": "malformed-no-slash",
		"working.env":     "REPORTALL_TEST_VAR",
	})

	var sawBroken, sawWorking bool

	for _, r := range results {
		switch r.Posture.Owner {
		case "test:broken":
			sawBroken = true

			require.Error(t, r.Err, "the broken credential carries its own error")
		case "test:working":
			sawWorking = true

			require.NoError(t, r.Err)
			assert.Equal(t, credentialposture.OriginEnvRef, r.Posture.Origin)
		}
	}

	assert.True(t, sawBroken && sawWorking, "one failure must not suppress the rest of the report")
}
