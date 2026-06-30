package setup

import (
	"bytes"
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/signing/verify"
)

// Test fixtures for the update-signature flow. The signing/verification logic
// itself lives in (and is exhaustively tested by) gitlab.com/phpboyscout/signing;
// these are the minimal fixtures gtb's own SelfUpdater tests need.

// testSigningKey bundles a generated entity with its ASCII-armored public-key
// blob, so a test can sign with the entity or load the public half into a
// TrustSet without re-armoring.
type testSigningKey struct {
	entity     *openpgp.Entity
	armoredPub []byte
}

//nolint:gochecknoglobals // once-initialised test fixtures shared across the package's tests
var (
	testKeysOnce sync.Once
	testKeysErr  error
	testEd25519  testSigningKey
	testRSA1024  testSigningKey
)

// mustInitTestSigningKeys generates the fixture keys exactly once per test
// binary and fails the calling test if generation errors.
func mustInitTestSigningKeys(t *testing.T) {
	t.Helper()
	testKeysOnce.Do(func() {
		testEd25519, testKeysErr = generateTestSigningKey(&packet.Config{
			Algorithm: packet.PubKeyAlgoEdDSA,
			Curve:     packet.Curve25519,
		}, "Ed25519 Test", "ed25519@test.example")
		if testKeysErr != nil {
			return
		}

		testRSA1024, testKeysErr = generateTestSigningKey(&packet.Config{
			Algorithm: packet.PubKeyAlgoRSA,
			RSABits:   1024,
		}, "RSA 1024 Test", "rsa1024@test.example")
	})
	require.NoError(t, testKeysErr, "test key generation must succeed")
}

func generateTestSigningKey(cfg *packet.Config, name, email string) (testSigningKey, error) {
	ent, err := openpgp.NewEntity(name, "", email, cfg)
	if err != nil {
		return testSigningKey{}, err
	}

	var buf bytes.Buffer

	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	if err != nil {
		return testSigningKey{}, err
	}

	if err := ent.Serialize(w); err != nil {
		return testSigningKey{}, err
	}

	if err := w.Close(); err != nil {
		return testSigningKey{}, err
	}

	return testSigningKey{entity: ent, armoredPub: buf.Bytes()}, nil
}

// detachSign produces an ASCII-armored detached signature over data using the
// fixture entity — the publisher side of the round-trip in these tests.
func detachSign(t *testing.T, ent *openpgp.Entity, data []byte) []byte {
	t.Helper()

	var buf bytes.Buffer

	require.NoError(t, openpgp.ArmoredDetachSign(&buf, ent, bytes.NewReader(data), nil))

	return buf.Bytes()
}

// fakeResolver is a test-only verify.KeyResolver returning a preconfigured
// (TrustSet, error), with an optional delay for cancellation tests.
type fakeResolver struct {
	name  string
	ts    *verify.TrustSet
	err   error
	delay time.Duration
	calls int32
}

func (f *fakeResolver) Name() string { return f.name }

func (f *fakeResolver) Resolve(ctx context.Context) (*verify.TrustSet, error) {
	atomic.AddInt32(&f.calls, 1)

	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return f.ts, f.err
}
