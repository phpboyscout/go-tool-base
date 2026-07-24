package trustkeys_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/signing/verify"

	"gitlab.com/phpboyscout/go-tool-base/internal/trustkeys"
)

// expectedFingerprints lists the keys that must be embedded in this
// build. Adding or removing a *.asc under keys/ requires editing this
// list — that change is the explicit signal that a trust-anchor
// rotation has occurred and is being shipped, so the test failing on
// an unrelated PR is exactly the safety net we want.
//
// Fingerprints come from `gtb keys generate` (rotation) and
// `gtb keys mint --backend aws-kms` (signing) on 2026-06-08; v2 was
// minted 2026-07-24 during the dual-anchor key rotation (see infra's
// how-to/rotate-release-signing-keys runbook).
var expectedFingerprints = map[string]struct {
	algorithm   string
	description string
}{
	"2B26658409047ED08B56CEBDCF5B8DBB5D9F19C2": {
		algorithm:   "ed25519",
		description: "rotation-authority (offline, break-glass)",
	},
	"6E2072BBF83DFAAF006300C495DDAC333C37AA35": {
		algorithm:   "rsa",
		description: "signing-key-v1 (AWS KMS alias/gtb-release-signing-v1, old account 049815585546; retired at window close, public key trusted forever)",
	},
	"E969F9AB167897D876B6A93AA44A36F8D7ACF36D": {
		algorithm:   "rsa",
		description: "signing-key-v2 (AWS KMS alias/gtb-release-signing-v2, prod 060730976733 — 2026-07-24 rotation)",
	},
}

// TestKeys_AllParseViaProductionVerifier exercises the embed → parse
// pipeline via NewEmbeddedResolver — the same constructor the running
// gtb binary uses at startup. Any embedded *.asc that isn't valid
// OpenPGP, or fails the minimum-strength policy, panics here.
func TestKeys_AllParseViaProductionVerifier(t *testing.T) {
	t.Parallel()

	keys := trustkeys.Keys()
	require.NotEmpty(t, keys, "internal/trustkeys/keys/ must contain at least one *.asc file")

	// NewEmbeddedResolver panics on malformed or weak keys; if this
	// returns we know every key passed both parse and policy gates.
	r := verify.NewEmbeddedResolver(keys...)
	require.NotNil(t, r)
}

// TestKeys_FingerprintsMatchExpectedSet enumerates the embedded keys
// and asserts the fingerprint set matches expectedFingerprints
// exactly. This is the "trust anchor rotation gate" — anyone changing
// the embedded set must update expectedFingerprints in the same diff,
// which forces a human to acknowledge the rotation.
func TestKeys_FingerprintsMatchExpectedSet(t *testing.T) {
	t.Parallel()

	got := map[string]bool{}

	for _, raw := range trustkeys.Keys() {
		ring, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(raw))
		require.NoError(t, err, "embedded key must be a parseable armored OpenPGP key ring")
		require.Len(t, ring, 1, "each embedded .asc must hold exactly one entity")

		fp := strings.ToUpper(fmt.Sprintf("%X", ring[0].PrimaryKey.Fingerprint))
		got[fp] = true

		t.Logf("embedded fingerprint: %s", fp)
	}

	for fp, meta := range expectedFingerprints {
		assert.Truef(t, got[fp], "expected fingerprint %s (%s — %s) is not embedded", fp, meta.algorithm, meta.description)
	}

	for fp := range got {
		_, expected := expectedFingerprints[fp]
		assert.Truef(t, expected, "embedded fingerprint %s is not in the expected set — if this is a deliberate rotation, update expectedFingerprints in this test", fp)
	}
}
