package setup

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/forge"
	forgetest "gitlab.com/phpboyscout/go/forge/test"
	"gitlab.com/phpboyscout/go/signing/minisign"
	"gitlab.com/phpboyscout/go/signing/verify"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/release/static"
)

type rotationAuthority struct {
	signer ed25519.PrivateKey
	public string
}

func newRotationAuthority(t *testing.T) rotationAuthority {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	key, err := minisign.NewPublicKey(pub)
	require.NoError(t, err)

	return rotationAuthority{signer: priv, public: key.Encode()}
}

func newReleaseKey(t *testing.T, name string) testSigningKey {
	t.Helper()

	k, err := generateTestSigningKey(&packet.Config{Algorithm: packet.PubKeyAlgoEdDSA, Curve: packet.Curve25519}, name, "release@test.example")
	require.NoError(t, err)

	return k
}

func fingerprintOf(t *testing.T, k testSigningKey) string {
	t.Helper()

	ts, err := verify.LoadTrustSet(k.armoredPub)
	require.NoError(t, err)

	return ts.Fingerprints()[0]
}

// compromiseManifest adds replacement and revokes compromised, signed by a.
func compromiseManifest(t *testing.T, a rotationAuthority, epoch uint64, replacement, compromised testSigningKey) (manifest, sig []byte) {
	t.Helper()

	now := time.Now()
	manifest, sig, err := verify.SignRotationManifest(a.signer, verify.RotationManifest{
		Schema:   verify.RotationSchema,
		IssuedAt: now.Add(-time.Minute),
		NotAfter: now.Add(24 * time.Hour),
		Reason:   verify.RotationReasonCompromise,
		Add:      []verify.RotationAdd{{Fingerprint: fingerprintOf(t, replacement), Armored: string(replacement.armoredPub)}},
		Revoke:   []verify.RotationRevoke{{Fingerprint: fingerprintOf(t, compromised), Since: now.Add(-time.Hour)}},
		Epoch:    epoch,
	})
	require.NoError(t, err)

	return manifest, sig
}

// rotationRelease is a release whose checksums are signed by signer, carrying
// the rotation files when rotation is non-nil.
func rotationRelease(t *testing.T, signer testSigningKey, rotation, rotationSig []byte) (*fakeRelease, *fakeProvider, []byte) {
	t.Helper()

	checksums := []byte("deadbeef  testtool_Linux_x86_64.tar.gz\n")
	rel := &fakeRelease{name: "v2.0.0", assets: []forge.ReleaseAsset{
		&fakeAsset{name: "testtool_Linux_x86_64.tar.gz"},
		&fakeAsset{name: "checksums.txt"},
		&fakeAsset{name: "checksums.txt.sig"},
	}}
	bodies := map[string][]byte{"checksums.txt.sig": detachSign(t, signer.entity, checksums)}

	if rotation != nil {
		rel.assets = append(rel.assets, &fakeAsset{name: verify.RotationManifestFile})
		bodies[verify.RotationManifestFile] = rotation
	}

	if rotationSig != nil {
		rel.assets = append(rel.assets, &fakeAsset{name: verify.RotationSignatureFile})
		bodies[verify.RotationSignatureFile] = rotationSig
	}

	return rel, &fakeProvider{rel: rel, assetBodies: bodies}, checksums
}

type rotationFixture struct {
	authority   rotationAuthority
	compromised testSigningKey
	replacement testSigningKey
	store       *verify.MemoryRotationStore
}

func newRotationFixture(t *testing.T) rotationFixture {
	t.Helper()

	return rotationFixture{
		authority:   newRotationAuthority(t),
		compromised: newReleaseKey(t, "Compromised Release Key"),
		replacement: newReleaseKey(t, "Replacement Release Key"),
		store:       verify.NewMemoryRotationStore(),
	}
}

// updater trusts the compromised key alone, the way a shipped binary does.
func (f rotationFixture) updater(t *testing.T, p forge.Provider, enabled bool) *SelfUpdater {
	t.Helper()

	s := f.unresolvedUpdater(p, enabled)
	require.NoError(t, s.buildDefaultKeyResolver())

	return s
}

func (f rotationFixture) unresolvedUpdater(p forge.Provider, enabled bool) *SelfUpdater {
	return &SelfUpdater{
		Tool:                  props.Tool{Name: "testtool"},
		logger:                logger.NewNoop(),
		Fs:                    afero.NewMemMapFs(),
		releaseClient:         p,
		requireSignature:      true,
		keySource:             verify.DefaultKeySource,
		embeddedKeys:          [][]byte{f.compromised.armoredPub},
		rotationAuthorityKeys: []string{f.authority.public},
		rotationEnabled:       enabled,
		rotationStore:         f.store,
	}
}

// A release carrying a compromise manifest is applied before its signature is
// checked, so the replacement key verifies on the same update (spec 0101 D4).
func TestVerifyManifestSignature_AppliesARotationBeforeTheSignature(t *testing.T) {
	t.Parallel()

	f := newRotationFixture(t)
	manifest, sig := compromiseManifest(t, f.authority, 1, f.replacement, f.compromised)
	rel, provider, checksums := rotationRelease(t, f.replacement, manifest, sig)

	s := f.updater(t, provider, true)
	require.NoError(t, s.verifyManifestSignature(context.Background(), rel, checksums))

	rec, err := f.store.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(1), rec.Epoch)

	old, oldProvider, oldChecksums := rotationRelease(t, f.compromised, nil, nil)
	s.releaseClient, s.channel = oldProvider, nil
	err = s.verifyManifestSignature(context.Background(), old, oldChecksums)
	require.ErrorIs(t, err, verify.ErrSignatureInvalid, "the revoked key no longer verifies a release")
}

// The stored record holds on a later release that carries no manifest
// (spec 0101 D11).
func TestVerifyManifestSignature_StoredRotationHoldsWithoutAManifest(t *testing.T) {
	t.Parallel()

	f := newRotationFixture(t)
	manifest, sig := compromiseManifest(t, f.authority, 1, f.replacement, f.compromised)
	rel, provider, checksums := rotationRelease(t, f.replacement, manifest, sig)
	require.NoError(t, f.updater(t, provider, true).verifyManifestSignature(context.Background(), rel, checksums))

	later, laterProvider, laterChecksums := rotationRelease(t, f.replacement, nil, nil)
	require.NoError(t, f.updater(t, laterProvider, true).verifyManifestSignature(context.Background(), later, laterChecksums))
}

// A later manifest that omits a stored revoke fails the update and leaves the
// key revoked, because revocation is permanent (spec 0101 D20). go/signing
// v0.15.2 builds the next record from the manifest alone, so the store refuses.
func TestVerifyManifestSignature_AManifestDroppingARevokeFailsTheUpdate(t *testing.T) {
	t.Parallel()

	f := newRotationFixture(t)
	fs := afero.NewMemMapFs()
	store := newRotationStore(fs, rotationTestDir)
	updater := func(p forge.Provider) *SelfUpdater {
		s := f.unresolvedUpdater(p, true)
		s.Fs, s.rotationStore = fs, store
		require.NoError(t, s.buildDefaultKeyResolver())

		return s
	}

	manifest, sig := compromiseManifest(t, f.authority, 1, f.replacement, f.compromised)
	rel, provider, checksums := rotationRelease(t, f.replacement, manifest, sig)
	require.NoError(t, updater(provider).verifyManifestSignature(context.Background(), rel, checksums))

	now := time.Now()
	dropping, droppingSig, err := verify.SignRotationManifest(f.authority.signer, verify.RotationManifest{
		Schema:   verify.RotationSchema,
		IssuedAt: now.Add(-time.Minute),
		NotAfter: now.Add(24 * time.Hour),
		Reason:   verify.RotationReasonScheduled,
		Add:      []verify.RotationAdd{{Fingerprint: fingerprintOf(t, f.replacement), Armored: string(f.replacement.armoredPub)}},
		Epoch:    2,
	})
	require.NoError(t, err)

	later, laterProvider, laterChecksums := rotationRelease(t, f.compromised, dropping, droppingSig)
	err = updater(laterProvider).verifyManifestSignature(context.Background(), later, laterChecksums)
	require.ErrorIs(t, err, verify.ErrRotationRefused)

	rec, err := store.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(1), rec.Epoch)
	assert.Equal(t, []string{fingerprintOf(t, f.compromised)}, rec.Revoked)

	old, oldProvider, oldChecksums := rotationRelease(t, f.compromised, nil, nil)
	err = updater(oldProvider).verifyManifestSignature(context.Background(), old, oldChecksums)
	require.ErrorIs(t, err, verify.ErrSignatureInvalid, "the revoked key still verifies nothing")
}

// A release that lists a manifest which does not verify fails the update,
// whatever require_signature says (spec 0101 D7).
func TestVerifyManifestSignature_ARotationThatFailsFailsTheUpdate(t *testing.T) {
	t.Parallel()

	f := newRotationFixture(t)
	manifest, sig := compromiseManifest(t, f.authority, 1, f.replacement, f.compromised)
	_, forged := compromiseManifest(t, newRotationAuthority(t), 1, f.replacement, f.compromised)

	cases := map[string]struct {
		manifest, sig []byte
		want          error
	}{
		"signed by another authority": {manifest: manifest, sig: forged, want: verify.ErrRotationSignatureInvalid},
		"no signature beside it":      {manifest: manifest, want: ErrRotationSignatureMissing},
		"tampered":                    {manifest: append([]byte(" "), manifest...), sig: sig, want: verify.ErrRotationSignatureInvalid},
		"signed by the release key":   {manifest: manifest, sig: detachSign(t, f.compromised.entity, manifest), want: verify.ErrRotationSignatureInvalid},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := f
			f.store = verify.NewMemoryRotationStore()
			rel, provider, checksums := rotationRelease(t, f.compromised, tc.manifest, tc.sig)

			s := f.updater(t, provider, true)
			s.requireSignature = false
			require.ErrorIs(t, s.verifyManifestSignature(context.Background(), rel, checksums), tc.want)

			rec, err := f.store.Load(context.Background())
			require.NoError(t, err)
			assert.Zero(t, rec.Epoch, "a refused manifest changes nothing")
		})
	}
}

// Rotation files that are listed but cannot be fetched fail the update rather
// than install the release as if it listed none.
func TestVerifyManifestSignature_ARotationThatCannotBeFetchedFailsTheUpdate(t *testing.T) {
	t.Parallel()

	f := newRotationFixture(t)
	manifest, sig := compromiseManifest(t, f.authority, 1, f.replacement, f.compromised)

	cases := map[string]struct {
		file string
		body []byte
		want error
		msg  string
	}{
		"the manifest does not download":  {file: verify.RotationManifestFile, msg: "fetching the rotation manifest"},
		"the signature does not download": {file: verify.RotationSignatureFile, msg: "fetching the rotation signature"},
		"the manifest is too large": {
			file: verify.RotationManifestFile, body: make([]byte, verify.MaxRotationManifestSize+1),
			want: verify.ErrRotationManifestTooLarge, msg: "fetching the rotation manifest",
		},
		"the signature is too large": {
			file: verify.RotationSignatureFile, body: make([]byte, verify.MaxSignatureSize+1),
			want: verify.ErrSignatureTooLarge, msg: "fetching the rotation signature",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := f
			f.store = verify.NewMemoryRotationStore()
			rel, provider, checksums := rotationRelease(t, f.compromised, manifest, sig)

			if tc.body == nil {
				delete(provider.assetBodies, tc.file)
			} else {
				provider.assetBodies[tc.file] = tc.body
			}

			s := f.updater(t, provider, true)
			s.requireSignature = false
			err := s.verifyManifestSignature(context.Background(), rel, checksums)
			require.ErrorContains(t, err, tc.msg)

			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			}

			rec, err := f.store.Load(context.Background())
			require.NoError(t, err)
			assert.Zero(t, rec.Epoch, "a refused manifest changes nothing")
		})
	}
}

// A stored rotation record that cannot be read, or that will not apply, fails
// the update even when signatures are optional: proceeding would drop the
// revokes it holds and install a release signed by an untrusted key.
func TestVerifyManifestSignature_AStoredRecordThatFailsIsAlwaysFatal(t *testing.T) {
	t.Parallel()

	f := newRotationFixture(t)
	recordPath := filepath.Join(rotationTestDir, rotationStoreFilename)

	writeRecord := func(data string) afero.Fs {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, recordPath, []byte(data), filePermTrust))

		return fs
	}

	cases := map[string]struct {
		fs   afero.Fs
		want error
	}{
		"not JSON":                         {writeRecord("{not json"), ErrRotationStoreCorrupt},
		"unreadable":                       {unreadableFs{writeRecord("{}")}, ErrRotationStoreCorrupt},
		"a stored key that does not parse": {writeRecord(`{"epoch":1,"added":["garbage"]}`), ErrRotationStoreCorrupt},
		"revokes every key that may sign": {
			writeRecord(fmt.Sprintf(`{"epoch":1,"revoked":[%q]}`, fingerprintOf(t, f.compromised))),
			verify.ErrRotationRefused,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rel, provider, checksums := rotationRelease(t, newReleaseKey(t, "Untrusted Key"), nil, nil)

			s := &SelfUpdater{
				Tool:                  props.Tool{Name: "testtool"},
				logger:                logger.NewNoop(),
				Fs:                    afero.NewMemMapFs(),
				releaseClient:         provider,
				keySource:             verify.DefaultKeySource,
				embeddedKeys:          [][]byte{f.compromised.armoredPub},
				rotationAuthorityKeys: []string{f.authority.public},
				rotationEnabled:       true,
				rotationStore:         newRotationStore(tc.fs, rotationTestDir),
			}
			require.NoError(t, s.buildDefaultKeyResolver())
			require.False(t, s.requireSignature, "the policy under test is the one that applies without require_signature")

			require.ErrorIs(t, s.verifyManifestSignature(context.Background(), rel, checksums), tc.want)
		})
	}
}

// unreadableFs refuses every open, the way a record at mode 000 or a directory
// in its place does.
type unreadableFs struct{ afero.Fs }

func (unreadableFs) Open(string) (afero.File, error) { return nil, os.ErrPermission }

func (unreadableFs) OpenFile(string, int, os.FileMode) (afero.File, error) {
	return nil, os.ErrPermission
}

// With update.rotation.enabled false, the manifest is skipped with a warning
// and trust is the compiled-in anchors (spec 0101 D18).
func TestVerifyManifestSignature_RotationOffSkipsTheManifestAndSaysSo(t *testing.T) {
	t.Parallel()

	f := newRotationFixture(t)
	manifest, sig := compromiseManifest(t, f.authority, 1, f.replacement, f.compromised)
	rel, provider, checksums := rotationRelease(t, f.replacement, manifest, sig)

	spy := &warnSpyLogger{Logger: logger.NewNoop()}
	s := f.updater(t, provider, false)
	s.logger = spy

	require.ErrorIs(t, s.verifyManifestSignature(context.Background(), rel, checksums), verify.ErrSignatureInvalid,
		"the replacement key is not trusted while rotation is off")
	require.Len(t, spy.warns, 1)
	assert.Contains(t, spy.warns[0], ConfigKeyUpdateRotationEnabled)

	rec, err := f.store.Load(context.Background())
	require.NoError(t, err)
	assert.Zero(t, rec.Epoch)
}

// A record that cannot be kept forgets a revoke, so the update says so whether
// or not its release carries a manifest (spec 0221).
func TestVerifyManifestSignature_WarnsWhenTheRecordCannotBeKept(t *testing.T) {
	t.Parallel()

	testutil.EachConfigDir(t, testVerifyManifestSignature_WarnsWhenTheRecordCannotBeKept)
}

func testVerifyManifestSignature_WarnsWhenTheRecordCannotBeKept(t *testing.T, style props.ConfigDir) {
	t.Parallel()

	f := newRotationFixture(t)
	tool := props.Tool{Name: "testtool", Config: props.ConfigSpec{Dir: style}}
	dir := ConfigDir(tool)
	rotation, rotationSig := compromiseManifest(t, f.authority, 1, f.replacement, f.compromised)

	for name, tc := range map[string]struct {
		rotation, rotationSig []byte
		signer                testSigningKey
		checkout              string
		enabled, authority    bool
		want                  int
	}{
		"a checkout itself, with a manifest": {rotation, rotationSig, f.replacement, dir, true, true, 1},
		"a checkout itself, without one":     {nil, nil, f.compromised, dir, true, true, 1},
		"under a checkout":                   {nil, nil, f.compromised, filepath.Dir(dir), true, true, 1},
		"a checkout, rotation off":           {nil, nil, f.compromised, dir, false, true, 0},
		"a checkout, no authority keys":      {nil, nil, f.compromised, dir, true, false, 0},
		"outside a checkout":                 {rotation, rotationSig, f.replacement, "", true, true, 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			if tc.checkout != "" {
				require.NoError(t, fs.MkdirAll(filepath.Join(tc.checkout, ".git"), 0o700))
			}

			rel, provider, checksums := rotationRelease(t, tc.signer, tc.rotation, tc.rotationSig)
			spy := &warnSpyLogger{Logger: logger.NewNoop()}
			s := f.unresolvedUpdater(provider, tc.enabled)
			s.Tool, s.Fs, s.logger, s.rotationStore = tool, fs, spy, nil

			if !tc.authority {
				s.rotationAuthorityKeys = nil
			}

			require.NoError(t, s.buildDefaultKeyResolver())
			require.NoError(t, s.verifyManifestSignature(context.Background(), rel, checksums))
			require.Len(t, spy.warns, tc.want)

			if tc.want > 0 {
				assert.Contains(t, spy.warns[0], "trusted again")
				assert.Equal(t, []any{"reason", s.rotationNotKept}, spy.attrs[0])
			}
		})
	}
}

// A tool with no authority keys has not taken up rotation: a manifest on its
// release is not read.
func TestVerifyManifestSignature_NoAuthorityKeysIgnoresAManifest(t *testing.T) {
	t.Parallel()

	f := newRotationFixture(t)
	rel, provider, checksums := rotationRelease(t, f.compromised, []byte("not read"), nil)

	s := f.updater(t, provider, true)
	s.rotationAuthorityKeys, s.keyResolver, s.rotationApplier = nil, nil, nil
	require.NoError(t, s.buildDefaultKeyResolver())

	require.NoError(t, s.verifyManifestSignature(context.Background(), rel, checksums))
}

func TestNewUpdater_RotationSettings(t *testing.T) {
	t.Parallel()

	authority := newRotationAuthority(t)
	tool := func() props.Tool {
		return props.Tool{
			Name:            "testtool",
			ReleaseProvider: forgetest.New(forgetest.WithRelease("v1.0.0")),
			Signing: props.SigningConfig{
				EmbeddedKeys:          [][]byte{newReleaseKey(t, "Release").armoredPub},
				RotationAuthorityKeys: []string{authority.public},
			},
		}
	}

	t.Run("on by default", func(t *testing.T) {
		t.Parallel()

		p := injectProps(t, tool())
		p.FS = afero.NewMemMapFs()

		u, err := NewUpdater(context.Background(), p, "", false)
		require.NoError(t, err)
		assert.True(t, u.rotationEnabled)
		assert.NotNil(t, u.rotationApplier)
	})

	t.Run("a malformed authority key fails, and does not turn rotation off", func(t *testing.T) {
		t.Parallel()

		bad := tool()
		bad.Signing.RotationAuthorityKeys = []string{"not a minisign key"}

		p := injectProps(t, bad)
		p.FS = afero.NewMemMapFs()

		_, err := NewUpdater(context.Background(), p, "", false)
		require.Error(t, err)
	})

	t.Run("off from config", func(t *testing.T) {
		t.Parallel()

		p := injectProps(t, tool())
		p.FS = afero.NewMemMapFs()
		p.Config = testutil.StoreFromYAML(t, "update:\n  rotation:\n    enabled: false\n")

		u, err := NewUpdater(context.Background(), p, "", false)
		require.NoError(t, err)
		assert.False(t, u.rotationEnabled)
		assert.Nil(t, u.rotationApplier, "the resolver is built without the authority")
	})
}

// The static channel and go/signing must agree on the file names, because the
// updater finds the rotation files among a release's assets by name.
func TestRotationFileNamesMatchGoSigning(t *testing.T) {
	t.Parallel()

	assert.Equal(t, verify.RotationManifestFile, static.RotationFile)
	assert.Equal(t, verify.RotationSignatureFile, static.RotationSignatureFile)
}

type warnSpyLogger struct {
	logger.Logger
	warns []string
	attrs [][]any
}

func (l *warnSpyLogger) Warn(msg string, attrs ...any) {
	l.warns = append(l.warns, msg)
	l.attrs = append(l.attrs, attrs)
}

// A resolver that cannot apply a rotation is refused when authority keys are
// set, so rotation can never be on in config and off in fact.
func TestRotationApplierOf(t *testing.T) {
	t.Parallel()

	_, err := rotationApplierOf(nonRotatingResolver{})
	require.ErrorContains(t, err, "the plain resolver cannot apply a key rotation")

	f := newRotationFixture(t)
	s := f.updater(t, nil, true)
	require.NotNil(t, s.rotationApplier, "the resolver built with authority keys applies rotations")
}

type nonRotatingResolver struct{}

func (nonRotatingResolver) Name() string { return "plain" }

func (nonRotatingResolver) Resolve(context.Context) (*verify.TrustSet, error) { return nil, nil }
