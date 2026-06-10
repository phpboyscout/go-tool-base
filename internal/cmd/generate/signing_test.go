package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateSigningFields covers the key-source guard: it only validates
// when signing is requested (explicitly via --signing or implied by a
// --signing-email), and rejects anything outside embedded/external/both.
func TestValidateSigningFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opts    SkeletonOptions
		wantErr bool
	}{
		{"not signing, no email", SkeletonOptions{}, false},
		{"signing, default (empty) key-source", SkeletonOptions{Signing: true}, false},
		{"signing, embedded", SkeletonOptions{Signing: true, SigningKeySource: "embedded"}, false},
		{"signing, external", SkeletonOptions{Signing: true, SigningKeySource: "external"}, false},
		{"signing, both", SkeletonOptions{Signing: true, SigningKeySource: "both"}, false},
		{"email implies signing, key-source still validated", SkeletonOptions{SigningEmail: "release@example.test", SigningKeySource: "bogus"}, true},
		{"signing, invalid key-source", SkeletonOptions{Signing: true, SigningKeySource: "bogus"}, true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			o := tt.opts
			err := o.validateSigningFields()
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrInvalidSigningKeySource)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
