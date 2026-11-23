package generator

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go/errors"
)

func overlapSigning() ManifestSigning {
	return ApplySigningDefaults(ManifestSigning{
		Enabled: true,
		KeyID:   "alias/acme-v1",
		Overlap: &ManifestSigningOverlap{
			KeyID:     "alias/acme-v2",
			PublicKey: "internal/trustkeys/keys/signing-key-v2.asc",
		},
	})
}

// renderedSignsEntry parses the generated .goreleaser.yaml and returns its one
// signs entry.
func renderedSignsEntry(t *testing.T, out string) map[string]any {
	t.Helper()

	var doc struct {
		Signs []map[string]any `yaml:"signs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(out), &doc))
	require.Len(t, doc.Signs, 1, "the overlap must sign from one entry, since goreleaser orders two entries over a file arbitrarily")

	return doc.Signs[0]
}

func TestGenerateSkeleton_SignsBlock_OverlapSignsTwiceFromOneEntry(t *testing.T) {
	t.Parallel()

	entry := renderedSignsEntry(t, generateAndReadGoreleaser(t, overlapSigning()))

	assert.Equal(t, "sh", entry["cmd"])

	args, ok := entry["args"].([]any)
	require.True(t, ok)
	require.Len(t, args, 5)
	assert.Equal(t, []any{"-c", "sh", "${signature}", "${artifact}"}, []any{args[0], args[2], args[3], args[4]})

	assert.Equal(t,
		"set -e; "+
			"sigillum --ci sign --backend 'aws-kms' --kms-region 'eu-west-2' --key-id 'alias/acme-v1' "+
			"--public-key 'internal/trustkeys/keys/signing-key-v1.asc' --output \"{{ `$` }}1\" \"{{ `$` }}2\"; "+
			"sigillum --ci sign --append --backend 'aws-kms' --kms-region 'eu-west-2' --key-id 'alias/acme-v2' "+
			"--public-key 'internal/trustkeys/keys/signing-key-v2.asc' --output \"{{ `$` }}1\" \"{{ `$` }}2\"",
		args[1])
}

func TestOverlapSignScript_OverlapRegionAndLocalBackend(t *testing.T) {
	t.Parallel()

	kms := overlapSigning()
	kms.Overlap.KMSRegion = "us-east-1"
	assert.Contains(t, kms.OverlapSignScript(), "--append --backend 'aws-kms' --kms-region 'us-east-1'")
	assert.Contains(t, kms.OverlapSignScript(), "sign --backend 'aws-kms' --kms-region 'eu-west-2'")

	local := ApplySigningDefaults(ManifestSigning{
		Enabled: true, Backend: "local", KeyID: "v1.pem",
		Overlap: &ManifestSigningOverlap{KeyID: "v2.pem", PublicKey: "internal/trustkeys/keys/v2.asc"},
	})
	assert.NotContains(t, local.OverlapSignScript(), "--kms-region")
}

// TestOverlapSignScript_QuotesEveryValue is defence in depth: the validators
// refuse spaces and quotes, but a hand-edited manifest could hold one, and the
// script is parsed by sh.
func TestOverlapSignScript_QuotesEveryValue(t *testing.T) {
	t.Parallel()

	s := overlapSigning()
	s.Overlap.KeyID = "it's a key; rm -rf /"

	script := s.OverlapSignScript()
	assert.Contains(t, script, `--key-id 'it'\''s a key; rm -rf /'`)

	assert.Equal(t, s.Overlap.KeyID+"\n", shellOutput(t, "-c", "printf '%s\\n' "+escapeShellArg(s.Overlap.KeyID)))
}

func shellOutput(t *testing.T, args ...string) string {
	t.Helper()

	out, err := exec.CommandContext(t.Context(), "sh", args...).Output()
	require.NoError(t, err)

	return string(out)
}

// TestGenerateSkeleton_SignsBlock_OverlapEscapesHostileValues keeps the script
// inside its YAML scalar, as the single-key block does.
func TestGenerateSkeleton_SignsBlock_OverlapEscapesHostileValues(t *testing.T) {
	t.Parallel()

	s := overlapSigning()
	s.Overlap.KeyID = "inj\"\n  - artifact: pwned"

	out := generateAndReadGoreleaser(t, s)

	assert.NotContains(t, out, "\n  - artifact: pwned")
	renderedSignsEntry(t, out)
}

func TestGenerateSkeleton_SignsBlock_GitLabKeepsTheKMSEnvBlanking(t *testing.T) {
	t.Parallel()

	path := t.TempDir()
	cfg := signingSkeletonConfig(path, overlapSigning())
	cfg.Host = "gitlab.com"

	g := newSkeletonGeneratorForTest(t, afero.NewOsFs())
	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

	b, err := afero.ReadFile(afero.NewOsFs(), filepath.Join(path, ".goreleaser.yaml"))
	require.NoError(t, err)

	entry := renderedSignsEntry(t, string(b))
	assert.Equal(t, []any{"AWS_ACCESS_KEY_ID=", "AWS_SECRET_ACCESS_KEY="}, entry["env"])
}

func TestPromoteOverlap(t *testing.T) {
	t.Parallel()

	s := overlapSigning()
	s.Overlap.KMSRegion = "us-east-1"

	got := s.PromoteOverlap()
	assert.Nil(t, got.Overlap)
	assert.Equal(t, "alias/acme-v2", got.KeyID)
	assert.Equal(t, "internal/trustkeys/keys/signing-key-v2.asc", got.PublicKey)
	assert.Equal(t, "us-east-1", got.KMSRegion)

	s.Overlap.KMSRegion = ""
	assert.Equal(t, "eu-west-2", s.PromoteOverlap().KMSRegion, "an overlap with no region of its own keeps the primary's")
}

func TestValidateOverlap(t *testing.T) {
	t.Parallel()

	withOverlap := func(edit func(*ManifestSigning)) ManifestSigning {
		s := overlapSigning()
		edit(&s)

		return s
	}

	tests := []struct {
		name    string
		signing ManifestSigning
		want    error
		hint    string
	}{
		{name: "a release key", signing: overlapSigning()},
		{name: "a leading ./", signing: withOverlap(func(s *ManifestSigning) {
			s.Overlap.PublicKey = "./internal/trustkeys/keys/signing-key-v2.asc"
		})},
		{name: "no primary", want: ErrOverlapNeedsPrimary, signing: withOverlap(func(s *ManifestSigning) { s.KeyID = "" })},
		{name: "no overlap key id", want: ErrInvalidInput, signing: withOverlap(func(s *ManifestSigning) { s.Overlap.KeyID = "" })},
		{name: "no overlap public key", want: ErrInvalidInput, signing: withOverlap(func(s *ManifestSigning) { s.Overlap.PublicKey = "" })},
		{name: "an authority key", want: ErrOverlapKeyNotReleaseKey, hint: "must never sign a release", signing: withOverlap(func(s *ManifestSigning) {
			s.Overlap.PublicKey = "internal/trustkeys/authority/rotation-authority.asc"
		})},
		{name: "outside the key directory", want: ErrOverlapKeyNotReleaseKey, hint: "internal/trustkeys/keys/", signing: withOverlap(func(s *ManifestSigning) {
			s.Overlap.PublicKey = "keys/signing-key-v2.asc"
		})},
		{name: "a nested directory", want: ErrOverlapKeyNotReleaseKey, signing: withOverlap(func(s *ManifestSigning) {
			s.Overlap.PublicKey = "internal/trustkeys/keys/old/signing-key-v2.asc"
		})},
		{name: "a bad key id", want: ErrInvalidInput, signing: withOverlap(func(s *ManifestSigning) { s.Overlap.KeyID = "a b" })},
		{name: "a bad region", want: ErrInvalidInput, signing: withOverlap(func(s *ManifestSigning) { s.Overlap.KMSRegion = "Mars" })},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateManifestSigning(&tt.signing)
			if tt.want == nil {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, tt.want)

			if tt.hint != "" {
				assert.Contains(t, strings.Join(errors.GetAllHints(err), " "), tt.hint)
			}
		})
	}
}

func TestEnableSigning_RefusesAnOverlapKeyNotInTheTree(t *testing.T) {
	t.Parallel()

	g, _ := newSigningProject(t)

	err := g.EnableSigning(context.Background(), overlapSigning())
	require.ErrorIs(t, err, ErrOverlapKeyNotReleaseKey)
	assert.Contains(t, strings.Join(errors.GetAllHints(err), " "), "release it before opening the overlap")
}

// TestEnableSigning_OpensAndPromotesAnOverlap walks the rotation through an
// existing project's release config: gtb's own signs block is rewritten each
// time, so nothing goes under .gtb/ignore.
func TestEnableSigning_OpensAndPromotesAnOverlap(t *testing.T) {
	t.Parallel()

	g, fs := newSigningProject(t)
	ctx := context.Background()
	goreleaser := "/work/" + goreleaserAssetRelPath

	primary := ManifestSigning{KeyID: "alias/acme-v1"}
	require.NoError(t, g.EnableSigning(ctx, primary))

	require.NoError(t, afero.WriteFile(fs, "/work/internal/trustkeys/keys/signing-key-v2.asc", []byte("key"), 0o600))

	open := primary
	open.Overlap = &ManifestSigningOverlap{KeyID: "alias/acme-v2", PublicKey: "internal/trustkeys/keys/signing-key-v2.asc"}
	require.NoError(t, g.EnableSigning(ctx, open))

	b, err := afero.ReadFile(fs, goreleaser)
	require.NoError(t, err)
	entry := renderedSignsEntry(t, string(b))
	assert.Equal(t, "sh", entry["cmd"])
	assert.Contains(t, entry["args"].([]any)[1], "--append --backend 'aws-kms' --kms-region 'eu-west-2' --key-id 'alias/acme-v2'")

	current, err := g.CurrentSigning()
	require.NoError(t, err)
	require.NotNil(t, current.Overlap)
	require.NoError(t, g.EnableSigning(ctx, current.PromoteOverlap()))

	b, err = afero.ReadFile(fs, goreleaser)
	require.NoError(t, err)
	entry = renderedSignsEntry(t, string(b))
	assert.Equal(t, "sigillum", entry["cmd"])
	assert.Contains(t, entry["args"], "alias/acme-v2")
	assert.NotContains(t, entry["args"], "alias/acme-v1")

	m, err := DecodeManifestFile(fs, ManifestPathFor("/work"))
	require.NoError(t, err)
	assert.Nil(t, m.Properties.Signing.Overlap)
	assert.Equal(t, "internal/trustkeys/keys/signing-key-v2.asc", m.Properties.Signing.PublicKey)
}
