package generator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/signing/verify"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
)

// TestGeneratedOverlapSignatureVerifiesWithEitherKey runs a generated overlap
// signs entry the way goreleaser does, and checks the one signature it writes
// verifies under each key alone (spec 0222). Two goreleaser entries over one
// file looked right and wrote a single packet, so the real signature is the
// thing to check.
func TestGeneratedOverlapSignatureVerifiesWithEitherKey(t *testing.T) {
	t.Parallel()

	testutil.SkipIfNotIntegration(t, "generator", "generator_build")

	if _, err := exec.LookPath("sigillum"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("`sigillum` not on PATH, and the CI image is meant to carry it: %v", err)
		}

		t.Skipf("`sigillum` not on PATH: %v", err)
	}

	path := t.TempDir()
	keysDir := filepath.Join(path, signingKeysDir)
	require.NoError(t, os.MkdirAll(keysDir, 0o755))

	for _, k := range []string{"v1", "v2"} {
		// 3072 bits is the verifier's floor, and the local OpenPGP signer takes RSA only.
		runSigillum(t, path, "--ci", "keys", "generate", "--algorithm", "rsa", "--rsa-bits", "3072",
			"--name", k, "--email", k+"@example.test", "--output", filepath.Join(signingKeysDir, k+".asc"))
	}

	signing := ApplySigningDefaults(ManifestSigning{
		Enabled: true, Backend: "local",
		KeyID: filepath.Join(signingKeysDir, "v1.pem"), PublicKey: filepath.Join(signingKeysDir, "v1.asc"),
		Overlap: &ManifestSigningOverlap{
			KeyID: filepath.Join(signingKeysDir, "v2.pem"), PublicKey: filepath.Join(signingKeysDir, "v2.asc"),
		},
	})

	g := newSkeletonGeneratorForTest(t, afero.NewOsFs())
	require.NoError(t, g.GenerateSkeleton(context.Background(), signingSkeletonConfig(path, signing)))

	out, err := os.ReadFile(filepath.Join(path, goreleaserAssetRelPath))
	require.NoError(t, err)

	entry := renderedSignsEntry(t, string(out))
	args, ok := entry["args"].([]any)
	require.True(t, ok)

	checksums := filepath.Join(path, "checksums.txt")
	require.NoError(t, os.WriteFile(checksums, []byte("abc  tool_linux_amd64\n"), 0o600))

	signature := checksums + ".sig"
	runSignsEntry(t, path, entry["cmd"].(string), args, signature, checksums)

	sig, err := os.ReadFile(signature)
	require.NoError(t, err)

	manifest, err := os.ReadFile(checksums)
	require.NoError(t, err)

	for _, k := range []string{"v1", "v2"} {
		pub, err := os.ReadFile(filepath.Join(keysDir, k+".asc"))
		require.NoError(t, err)

		ts, err := verify.LoadTrustSet(pub)
		require.NoError(t, err)
		require.NoError(t, ts.VerifyManifestSignature(manifest, sig), "the signature must verify under %s alone", k)
	}
}

// runSignsEntry runs a signs entry as goreleaser does: $artifact and $signature
// are expanded first, then the args' goreleaser templates. The only template a
// generated entry holds is the one that emits a dollar.
func runSignsEntry(t *testing.T, dir, cmd string, args []any, signature, artifact string) {
	t.Helper()

	expanded := make([]string, len(args))
	for i, a := range args {
		s := os.Expand(a.(string), func(k string) string {
			return map[string]string{"signature": signature, "artifact": artifact}[k]
		})
		expanded[i] = strings.ReplaceAll(s, "{{ `$` }}", "$")
	}

	c := exec.CommandContext(t.Context(), cmd, expanded...)
	c.Dir = dir

	out, err := c.CombinedOutput()
	require.NoError(t, err, "signs entry failed: %s", out)
}

func runSigillum(t *testing.T, dir string, args ...string) {
	t.Helper()

	c := exec.CommandContext(t.Context(), "sigillum", args...)
	c.Dir = dir

	out, err := c.CombinedOutput()
	require.NoError(t, err, "sigillum %v: %s", args, out)
}
