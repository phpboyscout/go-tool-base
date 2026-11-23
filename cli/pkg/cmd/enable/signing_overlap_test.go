package enable

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
)

const (
	signingManifest = "properties:\n  module_path: github.com/acme/tool\n  signing:\n    enabled: true\n    key_id: alias/acme-v1\n" +
		"commands:\n  - name: post\n    description: publish\n"

	overlapManifest = "properties:\n  module_path: github.com/acme/tool\n  signing:\n    enabled: true\n    key_id: alias/acme-v1\n" +
		"    overlap:\n      key_id: alias/acme-v2\n      public_key: internal/trustkeys/keys/signing-key-v2.asc\n      kms_region: us-east-1\n" +
		"commands:\n  - name: post\n    description: publish\n"

	newKeyPath = "internal/trustkeys/keys/signing-key-v2.asc"
)

func runEnableSigningArgs(t *testing.T, fs afero.Fs, args ...string) (generator.ManifestSigning, error) {
	t.Helper()

	p := newTestProps(fs)
	cmd := NewCmdEnableSigning(p).Command
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(append([]string{"--path", "/work"}, args...))

	if err := cmd.Execute(); err != nil {
		return generator.ManifestSigning{}, err
	}

	return generator.New(p, &generator.Config{Path: "/work"}).CurrentSigning()
}

func projectWithNewKey(t *testing.T, manifest string) afero.Fs {
	t.Helper()

	fs := afero.NewMemMapFs()
	writeProject(t, fs, manifest)
	require.NoError(t, afero.WriteFile(fs, "/work/"+newKeyPath, []byte("key"), 0o600))

	return fs
}

func TestEnableSigning_OverlapFlagsOpenTheWindow(t *testing.T) {
	t.Parallel()

	fs := projectWithNewKey(t, signingManifest)

	got, err := runEnableSigningArgs(t, fs, "--overlap-key-id", "alias/acme-v2", "--overlap-public-key", newKeyPath,
		"--overlap-kms-region", "us-east-1")
	require.NoError(t, err)

	assert.Equal(t, "alias/acme-v1", got.KeyID)
	assert.Equal(t, &generator.ManifestSigningOverlap{KeyID: "alias/acme-v2", PublicKey: newKeyPath, KMSRegion: "us-east-1"}, got.Overlap)
	assert.Contains(t, readFile(t, fs, "/work/.goreleaser.yaml"), "cmd: sh")
}

func TestEnableSigning_PromoteOverlapClosesOnTheNewKey(t *testing.T) {
	t.Parallel()

	fs := projectWithNewKey(t, overlapManifest)

	got, err := runEnableSigningArgs(t, fs, "--promote-overlap")
	require.NoError(t, err)

	assert.Nil(t, got.Overlap)
	assert.Equal(t, "alias/acme-v2", got.KeyID)
	assert.Equal(t, newKeyPath, got.PublicKey)
	assert.Equal(t, "us-east-1", got.KMSRegion)
	assert.Contains(t, readFile(t, fs, "/work/.goreleaser.yaml"), "cmd: sigillum")
}

func TestEnableSigning_EndOverlapClosesOnTheOldKey(t *testing.T) {
	t.Parallel()

	fs := projectWithNewKey(t, overlapManifest)

	got, err := runEnableSigningArgs(t, fs, "--end-overlap")
	require.NoError(t, err)

	assert.Nil(t, got.Overlap)
	assert.Equal(t, "alias/acme-v1", got.KeyID)
}

func TestEnableSigning_OverlapRerunKeepsTheOpenWindow(t *testing.T) {
	t.Parallel()

	fs := projectWithNewKey(t, overlapManifest)

	got, err := runEnableSigningArgs(t, fs, "--overlap-kms-region", "eu-west-1")
	require.NoError(t, err)
	require.NotNil(t, got.Overlap)
	assert.Equal(t, "alias/acme-v2", got.Overlap.KeyID)
	assert.Equal(t, "eu-west-1", got.Overlap.KMSRegion)

	got, err = runEnableSigningArgs(t, fs, "--require-signature")
	require.NoError(t, err)
	assert.NotNil(t, got.Overlap, "an unrelated re-run leaves the overlap open")
}

func TestEnableSigning_OverlapRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		manifest string
		args     []string
		want     error
	}{
		{
			name: "promote and end together", manifest: overlapManifest,
			args: []string{"--promote-overlap", "--end-overlap"}, want: ErrOverlapFlagsConflict,
		},
		{
			name: "close and open together", manifest: overlapManifest,
			args: []string{"--promote-overlap", "--overlap-key-id", "alias/acme-v3"}, want: ErrOverlapFlagsConflict,
		},
		{
			name: "promote with nothing open", manifest: signingManifest,
			args: []string{"--promote-overlap"}, want: ErrNoOverlapOpen,
		},
		{
			name: "end with nothing open", manifest: signingManifest,
			args: []string{"--end-overlap"}, want: ErrNoOverlapOpen,
		},
		{
			name: "no primary key", manifest: plainManifest,
			args: []string{"--overlap-key-id", "alias/acme-v2", "--overlap-public-key", newKeyPath},
			want: generator.ErrOverlapNeedsPrimary,
		},
		{
			name: "only half the overlap", manifest: signingManifest,
			args: []string{"--overlap-key-id", "alias/acme-v2"}, want: generator.ErrInvalidInput,
		},
		{
			name: "an authority key", manifest: signingManifest,
			args: []string{"--overlap-key-id", "alias/acme-v2", "--overlap-public-key", "internal/trustkeys/authority/rotation-authority.asc"},
			want: generator.ErrOverlapKeyNotReleaseKey,
		},
		{
			name: "a key not in the tree", manifest: signingManifest,
			args: []string{"--overlap-key-id", "alias/acme-v2", "--overlap-public-key", "internal/trustkeys/keys/signing-key-v3.asc"},
			want: generator.ErrOverlapKeyNotReleaseKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fs := projectWithNewKey(t, tt.manifest)
			before := readFile(t, fs, "/work/.gtb/manifest.yaml")

			_, err := runEnableSigningArgs(t, fs, tt.args...)
			require.ErrorIs(t, err, tt.want)
			assert.Equal(t, before, readFile(t, fs, "/work/.gtb/manifest.yaml"), "a refused run writes nothing")
		})
	}
}
