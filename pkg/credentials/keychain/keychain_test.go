package keychain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/phpboyscout/go-tool-base/pkg/credentials"
	kc "github.com/phpboyscout/go-tool-base/pkg/credentials/keychain"
)

// TestBackend_RoundTrip exercises Store → Retrieve → Delete against
// go-keyring's in-memory mock backend. The mock is installed by
// keyring.MockInit which makes the suite deterministic on every
// platform — the real OS keychain interaction is covered by the
// INT_TEST_CREDENTIALS integration suite.
func TestBackend_RoundTrip(t *testing.T) {
	keyring.MockInit()

	const (
		service = "gtb-test"
		account = "round.trip"
		secret  = "test-secret-xyzzy"
	)

	b := kc.Backend{}

	require.NoError(t, b.Store(service, account, secret))

	got, err := b.Retrieve(service, account)
	require.NoError(t, err)
	assert.Equal(t, secret, got)

	require.NoError(t, b.Delete(service, account))

	// Deleting twice must be idempotent (no error, ErrNotFound
	// suppressed by Delete itself).
	require.NoError(t, b.Delete(service, account))
}

// TestBackend_RetrieveMissing verifies that a functional keychain
// with no matching entry surfaces ErrCredentialNotFound — the
// sentinel resolvers rely on to distinguish "fall through" from
// "abort". See R3 in the spec.
func TestBackend_RetrieveMissing(t *testing.T) {
	keyring.MockInit()

	_, err := kc.Backend{}.Retrieve("gtb-test", "never.stored")
	require.ErrorIs(t, err, credentials.ErrCredentialNotFound)
}

// TestBackend_OverwriteExisting pins the semantic that a second
// Store for the same service/account replaces the prior secret
// rather than erroring. Important for re-running setup.
func TestBackend_OverwriteExisting(t *testing.T) {
	keyring.MockInit()

	const (
		service = "gtb-test"
		account = "overwrite"
	)

	b := kc.Backend{}

	require.NoError(t, b.Store(service, account, "first"))
	require.NoError(t, b.Store(service, account, "second"))

	got, err := b.Retrieve(service, account)
	require.NoError(t, err)
	assert.Equal(t, "second", got)
}

// TestBlankImport_RegistersBackend asserts that importing this
// subpackage (even indirectly, via the test blank-import at the top
// of this file) swaps the default stub for a real keychain-capable
// backend — which in turn flips KeychainAvailable and AvailableModes
// over to the three-mode variant.
func TestBlankImport_RegistersBackend(t *testing.T) {
	keyring.MockInit()

	assert.True(t, credentials.KeychainAvailable(),
		"subpackage init() must register a real backend")

	modes := credentials.AvailableModes()
	require.Len(t, modes, 3)
	assert.Equal(t, credentials.ModeEnvVar, modes[0])
	assert.Equal(t, credentials.ModeKeychain, modes[1])
	assert.Equal(t, credentials.ModeLiteral, modes[2])
}

// TestProbe_SucceedsOnMockBackend verifies Probe()'s canary
// round-trip against the mock keyring — the same guarantee the
// setup wizard relies on before offering ModeKeychain.
func TestProbe_SucceedsOnMockBackend(t *testing.T) {
	keyring.MockInit()

	assert.True(t, credentials.Probe(),
		"Probe should succeed when a registered backend is reachable")
}
