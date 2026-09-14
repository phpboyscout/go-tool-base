package generator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestApplySigningDefaults proves the release-pipeline defaulting: a key id
// is what turns the pipeline on, and it pulls aws-kms / eu-west-2 / the
// embedded-key convention behind it, while leaving explicit values and
// non-aws-kms backends alone.
func TestApplySigningDefaults(t *testing.T) {
	t.Parallel()

	t.Run("no key id leaves the pipeline untouched", func(t *testing.T) {
		t.Parallel()

		in := ManifestSigning{Enabled: true, ExternalKeyEmail: "release@example.test"}
		assert.Equal(t, in, ApplySigningDefaults(in))
	})

	t.Run("a key id defaults backend, region and public key", func(t *testing.T) {
		t.Parallel()

		got := ApplySigningDefaults(ManifestSigning{Enabled: true, KeyID: "alias/x"})
		assert.Equal(t, "aws-kms", got.Backend)
		assert.Equal(t, "eu-west-2", got.KMSRegion)
		assert.Equal(t, "internal/trustkeys/keys/signing-key-v1.asc", got.PublicKey)
	})

	t.Run("explicit values are preserved", func(t *testing.T) {
		t.Parallel()

		got := ApplySigningDefaults(ManifestSigning{
			KeyID:     "alias/x",
			Backend:   "aws-kms",
			KMSRegion: "us-east-1",
			PublicKey: "internal/trustkeys/keys/signing-key-v2.asc",
		})
		assert.Equal(t, "us-east-1", got.KMSRegion)
		assert.Equal(t, "internal/trustkeys/keys/signing-key-v2.asc", got.PublicKey)
	})

	t.Run("a non-aws-kms backend gets no region default", func(t *testing.T) {
		t.Parallel()

		got := ApplySigningDefaults(ManifestSigning{KeyID: "./release.pem", Backend: "local"})
		assert.Equal(t, "local", got.Backend)
		assert.Empty(t, got.KMSRegion)
	})
}

// TestManifestSigning_RoundTrip_PipelineFields proves the new release-pipeline
// fields survive a marshal/unmarshal cycle, so `gtb regenerate` reproduces the
// signs block deterministically.
func TestManifestSigning_RoundTrip_PipelineFields(t *testing.T) {
	t.Parallel()

	m := Manifest{}
	m.Properties.Signing = ManifestSigning{
		Enabled:   true,
		KeyID:     "alias/acme-release-signing-v1",
		Backend:   "aws-kms",
		KMSRegion: "eu-west-2",
		PublicKey: "internal/trustkeys/keys/signing-key-v1.asc",
	}

	data, err := yaml.Marshal(m)
	require.NoError(t, err)
	assert.Contains(t, string(data), "key_id: alias/acme-release-signing-v1")
	assert.Contains(t, string(data), "backend: aws-kms")
	assert.Contains(t, string(data), "kms_region: eu-west-2")
	assert.Contains(t, string(data), "public_key: internal/trustkeys/keys/signing-key-v1.asc")

	var got Manifest
	require.NoError(t, yaml.Unmarshal(data, &got))
	assert.Equal(t, m.Properties.Signing, got.Properties.Signing)
}

// generateAndReadGoreleaser scaffolds a project with the given signing
// posture and returns the generated .goreleaser.yaml.
func generateAndReadGoreleaser(t *testing.T, signing ManifestSigning) string {
	t.Helper()

	path := t.TempDir()
	g := newSkeletonGeneratorForTest(t, afero.NewOsFs())

	require.NoError(t, g.GenerateSkeleton(context.Background(), signingSkeletonConfig(path, signing)))

	b, err := os.ReadFile(filepath.Join(path, ".goreleaser.yaml"))
	require.NoError(t, err)

	return string(b)
}

// TestGenerateSkeleton_SignsBlock_AWSKMS asserts the generated .goreleaser.yaml
// carries a complete, direct (shim-free) gtb sign invocation for the aws-kms
// backend, with the concrete manifest values.
func TestGenerateSkeleton_SignsBlock_AWSKMS(t *testing.T) {
	out := generateAndReadGoreleaser(t, ApplySigningDefaults(ManifestSigning{
		Enabled: true,
		KeyID:   "alias/acme-release-signing-v1",
	}))

	assert.Contains(t, out, "signs:")
	assert.Contains(t, out, "cmd: gtb")
	assert.Contains(t, out, `- "aws-kms"`)
	assert.Contains(t, out, `- "--kms-region"`)
	assert.Contains(t, out, `- "eu-west-2"`)
	assert.Contains(t, out, `- "alias/acme-release-signing-v1"`)
	assert.Contains(t, out, `- "internal/trustkeys/keys/signing-key-v1.asc"`)
	assert.Contains(t, out, `- "${signature}"`)
	assert.Contains(t, out, `signature: "${artifact}.sig"`)
	assert.Contains(t, out, "artifacts: checksum")
}

// TestGenerateSkeleton_SignsBlock_OmittedWithoutKeyID asserts that enabling
// signing without a key id (the N+1 rollout step) leaves the release config
// untouched — verify half on, produce half off.
func TestGenerateSkeleton_SignsBlock_OmittedWithoutKeyID(t *testing.T) {
	out := generateAndReadGoreleaser(t, ManifestSigning{
		Enabled:          true,
		ExternalKeyEmail: "release@example.test",
	})

	assert.NotContains(t, out, "signs:")
}

// TestGenerateSkeleton_SignsBlock_LocalBackendNoRegion asserts the backend is
// dynamic: the local backend emits its key id but no aws-kms-only --kms-region.
func TestGenerateSkeleton_SignsBlock_LocalBackendNoRegion(t *testing.T) {
	out := generateAndReadGoreleaser(t, ApplySigningDefaults(ManifestSigning{
		Enabled: true,
		Backend: "local",
		KeyID:   "./release.pem",
	}))

	assert.Contains(t, out, "signs:")
	assert.Contains(t, out, `- "local"`)
	assert.Contains(t, out, `- "./release.pem"`)
	assert.NotContains(t, out, "--kms-region")
}

// TestGenerateSkeleton_SignsBlock_DisabledNoBlock asserts the default
// (disabled) posture renders no signs block at all.
func TestGenerateSkeleton_SignsBlock_DisabledNoBlock(t *testing.T) {
	out := generateAndReadGoreleaser(t, ManifestSigning{})

	assert.NotContains(t, out, "signs:")
}

// TestGenerateSkeleton_SignsBlock_EscapesHostileValues is the
// defence-in-depth half of spec D2: even if a hostile signing value reaches
// rendering (this path deliberately bypasses ValidateManifest), the
// escapeYAML pipe keeps it inside a double-quoted scalar so the GoReleaser
// signs block cannot be broken out of.
func TestGenerateSkeleton_SignsBlock_EscapesHostileValues(t *testing.T) {
	out := generateAndReadGoreleaser(t, ManifestSigning{
		Enabled:   true,
		Backend:   "aws-kms",
		KMSRegion: "eu-west-2",
		KeyID:     "inj\"\n  - artifact: pwned",
		PublicKey: "internal/trustkeys/keys/signing-key-v1.asc",
	})

	// The newline never reaches the output raw — only as the YAML escape
	// sequence inside the quoted scalar.
	assert.NotContains(t, out, "\n  - artifact: pwned")
	assert.Contains(t, out, `\n  - artifact: pwned`)

	// The rendered file must still parse as YAML.
	var doc map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(out), &doc))
}
