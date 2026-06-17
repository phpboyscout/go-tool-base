package setup

import (
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/stretchr/testify/assert"
)

// TestSubkeyCanSign exercises the trust-set gate that decides whether a subkey
// may verify update signatures. The security-relevant branches are the two that
// REJECT — a nil public key and a binding signature that explicitly clears the
// Sign flag — which were previously untested.
func TestSubkeyCanSign(t *testing.T) {
	t.Parallel()

	pk := &packet.PublicKey{}

	tests := []struct {
		name string
		sub  openpgp.Subkey
		want bool
	}{
		{
			name: "nil public key is rejected",
			sub:  openpgp.Subkey{PublicKey: nil},
			want: false,
		},
		{
			name: "no binding signature fails closed (treated as signing-capable)",
			sub:  openpgp.Subkey{PublicKey: pk, Sig: nil},
			want: true,
		},
		{
			name: "invalid key flags fail closed (treated as signing-capable)",
			sub:  openpgp.Subkey{PublicKey: pk, Sig: &packet.Signature{FlagsValid: false}},
			want: true,
		},
		{
			name: "valid flags with the sign flag set is accepted",
			sub:  openpgp.Subkey{PublicKey: pk, Sig: &packet.Signature{FlagsValid: true, FlagSign: true}},
			want: true,
		},
		{
			name: "valid flags without the sign flag is rejected",
			sub:  openpgp.Subkey{PublicKey: pk, Sig: &packet.Signature{FlagsValid: true, FlagSign: false}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, subkeyCanSign(tt.sub))
		})
	}
}
