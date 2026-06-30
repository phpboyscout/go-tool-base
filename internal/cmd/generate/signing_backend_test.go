package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Register the compiled-in signing backends so signing.Names() (used by
	// validateSigningFields) resolves aws-kms and local in the test binary,
	// mirroring cmd/gtb.
	_ "gitlab.com/phpboyscout/signing-aws-kms"
	_ "gitlab.com/phpboyscout/signing/local"
)

// TestValidateSigningFields_Backend covers the --signing-backend guard: an
// empty backend is fine (the generator defaults it when a key id is set), the
// registered backends pass, and an unregistered one is rejected.
func TestValidateSigningFields_Backend(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opts    SkeletonOptions
		wantErr bool
	}{
		{"no backend (defaulted later)", SkeletonOptions{Signing: true}, false},
		{"valid aws-kms", SkeletonOptions{Signing: true, SigningBackend: "aws-kms"}, false},
		{"valid local", SkeletonOptions{Signing: true, SigningBackend: "local"}, false},
		{"unregistered backend", SkeletonOptions{Signing: true, SigningBackend: "bogus"}, true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.opts.validateSigningFields()
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrInvalidSigningBackend)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
