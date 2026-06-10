package enable

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
)

// TestMergeSigning proves that re-running `gtb enable signing` to change one
// field leaves the rest of the existing signing block intact — the property
// the N+1 → N+2 → N+3 rollout depends on.
func TestMergeSigning(t *testing.T) {
	t.Parallel()

	current := generator.ManifestSigning{
		Enabled:          true,
		ExternalKeyEmail: "release@acme.dev",
		KeySource:        "external",
	}

	t.Run("adding a key id preserves the existing email", func(t *testing.T) {
		t.Parallel()

		opts := &signingOptions{KeyID: "alias/x"}
		got := mergeSigning(current, opts, signingFlagSet{keyID: true})

		assert.Equal(t, "release@acme.dev", got.ExternalKeyEmail)
		assert.Equal(t, "external", got.KeySource)
		assert.Equal(t, "alias/x", got.KeyID)
	})

	t.Run("require-signature only preserves email, key and backend", func(t *testing.T) {
		t.Parallel()

		base := generator.ManifestSigning{
			Enabled:          true,
			ExternalKeyEmail: "release@acme.dev",
			KeyID:            "alias/x",
			Backend:          "aws-kms",
		}
		opts := &signingOptions{RequireSignature: true}
		got := mergeSigning(base, opts, signingFlagSet{requireSignature: true})

		assert.True(t, got.RequireSignature)
		assert.Equal(t, "release@acme.dev", got.ExternalKeyEmail)
		assert.Equal(t, "alias/x", got.KeyID)
		assert.Equal(t, "aws-kms", got.Backend)
	})

	t.Run("nothing provided leaves the block untouched", func(t *testing.T) {
		t.Parallel()

		opts := &signingOptions{Email: "ignored@x", KeyID: "ignored"}
		got := mergeSigning(current, opts, signingFlagSet{})

		assert.Equal(t, current, got)
	})

	t.Run("a provided email overrides", func(t *testing.T) {
		t.Parallel()

		opts := &signingOptions{Email: "new@acme.dev"}
		got := mergeSigning(current, opts, signingFlagSet{email: true})

		assert.Equal(t, "new@acme.dev", got.ExternalKeyEmail)
	})

	t.Run("key-source both normalises to empty", func(t *testing.T) {
		t.Parallel()

		opts := &signingOptions{KeySource: "both"}
		got := mergeSigning(current, opts, signingFlagSet{keySource: true})

		assert.Empty(t, got.KeySource)
	})
}
