package setup

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/signing/verify"

	"gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// signatureFakeProvider implements [forge.SignatureProvider] to
// exercise the provider-preferred fetch path.
type signatureFakeProvider struct {
	fakeProvider
	sig []byte
	err error
}

func (p *signatureFakeProvider) DownloadSignature(_ context.Context, _ forge.Release, _ int64) ([]byte, error) {
	if p.err != nil {
		return nil, p.err
	}

	return p.sig, nil
}

func TestUpdaterOptions_SetFields(t *testing.T) {
	t.Parallel()

	r := &fakeResolver{name: "x"}
	s := &SelfUpdater{}
	WithKeyResolver(r)(s)
	assert.Same(t, r, s.keyResolver)

	s2 := &SelfUpdater{}
	WithEmbeddedKeys([]byte("a"), []byte("b"))(s2)
	assert.Len(t, s2.embeddedKeys, 2)
}

// TestEmbeddedKeys_BuildResolverAndVerify exercises the path that
// props.Tool.Signing.EmbeddedKeys feeds: NewUpdater seeds s.embeddedKeys
// from the tool's compile-time keys (e.g. internal/trustkeys), and
// buildDefaultKeyResolver turns them into a verify.TrustSet that verifies a
// real detached signature. This is the consume side of the
// embed -> sign -> verify loop.
func TestEmbeddedKeys_BuildResolverAndVerify(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	// Mirrors NewUpdater's seed: embeddedKeys come from the tool's
	// trustkeys package; key_source defaults to "both" but degrades to
	// embedded-only when no external email is set.
	s := &SelfUpdater{
		logger:       logger.NewNoop(),
		embeddedKeys: [][]byte{testEd25519.armoredPub},
		keySource:    verify.DefaultKeySource,
	}

	require.NoError(t, s.buildDefaultKeyResolver())
	require.NotNil(t, s.keyResolver, "embedded keys must produce a resolver")
	assert.Equal(t, "embedded", s.keyResolver.Name())

	ts, err := s.keyResolver.Resolve(context.Background())
	require.NoError(t, err)

	manifest := []byte("deadbeef  gtb_Linux_x86_64.tar.gz\n")
	sig := detachSign(t, testEd25519.entity, manifest)
	require.NoError(t, ts.VerifyManifestSignature(manifest, sig))
}

func TestFetchSignature_ProviderPath(t *testing.T) {
	t.Parallel()

	rel := &fakeRelease{name: "v1.0.0"}

	// Provider supplies the signature directly.
	got, err := newSigningUpdater(
		&signatureFakeProvider{fakeProvider: fakeProvider{rel: rel}, sig: []byte("SIG")}, nil, false,
	).fetchSignature(context.Background(), rel)
	require.NoError(t, err)
	assert.Equal(t, []byte("SIG"), got)

	// Provider opts out -> asset fallback finds nothing -> (nil, nil).
	got2, err := newSigningUpdater(
		&signatureFakeProvider{fakeProvider: fakeProvider{rel: rel}, err: forge.ErrNotSupported}, nil, false,
	).fetchSignature(context.Background(), rel)
	require.NoError(t, err)
	assert.Nil(t, got2)

	// Provider hard error -> propagated.
	_, err = newSigningUpdater(
		&signatureFakeProvider{fakeProvider: fakeProvider{rel: rel}, err: errors.New("boom")}, nil, false,
	).fetchSignature(context.Background(), rel)
	require.Error(t, err)
}

func TestVerifyManifestSignature_FetchError(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	manifest := []byte("aa  f\n")
	rel := &fakeRelease{name: "v1.0.0", assets: []forge.ReleaseAsset{
		&fakeAsset{name: "checksums.txt.sig"},
	}}
	provider := &fakeProvider{rel: rel, downloadErr: errors.New("network down")}
	resolver := verify.NewEmbeddedResolver(testEd25519.armoredPub)

	// Required: download failure aborts.
	err := newSigningUpdater(provider, resolver, true).
		verifyManifestSignature(context.Background(), rel, manifest)
	require.Error(t, err)

	// Not required: warn + proceed.
	require.NoError(t, newSigningUpdater(provider, resolver, false).
		verifyManifestSignature(context.Background(), rel, manifest))
}

type fakeStringConfig struct {
	set  map[string]bool
	strs map[string]string
}

func (c *fakeStringConfig) IsSet(key string) bool       { return c.set[key] }
func (c *fakeStringConfig) GetString(key string) string { return c.strs[key] }

func TestBuildKeyResolver(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	cases := []struct {
		name     string
		cfg      verify.KeyResolverConfig
		keys     [][]byte
		wantName string
		wantErr  bool
	}{
		{
			name:     "embedded",
			cfg:      verify.KeyResolverConfig{KeySource: "embedded"},
			keys:     [][]byte{testEd25519.armoredPub},
			wantName: "embedded",
		},
		{
			name:    "embedded-without-keys-errors",
			cfg:     verify.KeyResolverConfig{KeySource: "embedded"},
			wantErr: true,
		},
		{
			name:     "external",
			cfg:      verify.KeyResolverConfig{KeySource: "external", ExternalKeyEmail: "release@phpboyscout.uk"},
			wantName: "wkd:openpgpkey.phpboyscout.uk",
		},
		{
			name:    "external-without-email-errors",
			cfg:     verify.KeyResolverConfig{KeySource: "external"},
			wantErr: true,
		},
		{
			name:     "both-keys-and-email-composite",
			cfg:      verify.KeyResolverConfig{KeySource: "both", ExternalKeyEmail: "release@phpboyscout.uk"},
			keys:     [][]byte{testEd25519.armoredPub},
			wantName: "composite[embedded,wkd:openpgpkey.phpboyscout.uk]",
		},
		{
			name:     "both-keys-only-degrades-to-embedded",
			cfg:      verify.KeyResolverConfig{KeySource: "both"},
			keys:     [][]byte{testEd25519.armoredPub},
			wantName: "embedded",
		},
		{
			name:     "both-email-only-degrades-to-wkd",
			cfg:      verify.KeyResolverConfig{KeySource: "both", ExternalKeyEmail: "release@phpboyscout.uk"},
			wantName: "wkd:openpgpkey.phpboyscout.uk",
		},
		{
			name:    "both-neither-errors",
			cfg:     verify.KeyResolverConfig{KeySource: "both"},
			wantErr: true,
		},
		{
			name:     "empty-source-defaults-to-both",
			cfg:      verify.KeyResolverConfig{},
			keys:     [][]byte{testEd25519.armoredPub},
			wantName: "embedded",
		},
		{
			name:    "unknown-source-errors",
			cfg:     verify.KeyResolverConfig{KeySource: "carrier-pigeon"},
			keys:    [][]byte{testEd25519.armoredPub},
			wantErr: true,
		},
		{
			name:    "weak-embedded-key-errors-not-panics",
			cfg:     verify.KeyResolverConfig{KeySource: "embedded"},
			keys:    [][]byte{testRSA1024.armoredPub},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r, err := verify.BuildKeyResolver(tc.cfg, tc.keys...)
			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantName, r.Name())
		})
	}
}

func TestBuildKeyResolver_CompositeHonoursRequireAll(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	r, err := verify.BuildKeyResolver(verify.KeyResolverConfig{
		KeySource:                 "both",
		ExternalKeyEmail:          "release@phpboyscout.uk",
		RequireExternalCrosscheck: true,
	}, testEd25519.armoredPub)
	require.NoError(t, err)

	composite, ok := r.(*verify.CompositeResolver)
	require.True(t, ok, "both with keys+email must yield a verify.CompositeResolver")
	assert.True(t, composite.RequireAll, "RequireExternalCrosscheck must map to RequireAll")
}

func TestResolveSigningConfig_Precedence(t *testing.T) {
	// Mutates package-level Default* sentinels — must not run in parallel.
	oldSig, oldSrc, oldEmail, oldCross := verify.DefaultRequireSignature, verify.DefaultKeySource, verify.DefaultExternalKeyEmail, verify.DefaultRequireExternalCrosscheck
	t.Cleanup(func() {
		verify.DefaultRequireSignature = oldSig
		verify.DefaultKeySource = oldSrc
		verify.DefaultExternalKeyEmail = oldEmail
		verify.DefaultRequireExternalCrosscheck = oldCross
	})

	t.Run("require_signature_falls_back_to_default", func(t *testing.T) {
		verify.DefaultRequireSignature = true
		assert.True(t, resolveRequireSignature(nil))

		verify.DefaultRequireSignature = false
		assert.False(t, resolveRequireSignature(&fakeBoolConfig{}))
	})

	t.Run("require_signature_explicit_wins", func(t *testing.T) {
		verify.DefaultRequireSignature = false
		cfg := &fakeBoolConfig{
			set:  map[string]bool{"update.require_signature": true},
			vals: map[string]bool{"update.require_signature": true},
		}
		assert.True(t, resolveRequireSignature(cfg))
	})

	t.Run("key_source_default_and_explicit", func(t *testing.T) {
		verify.DefaultKeySource = "both"
		assert.Equal(t, "both", resolveKeySource(&fakeStringConfig{}))

		cfg := &fakeStringConfig{
			set:  map[string]bool{"update.key_source": true},
			strs: map[string]string{"update.key_source": "embedded"},
		}
		assert.Equal(t, "embedded", resolveKeySource(cfg))
	})

	t.Run("external_key_email_default_and_explicit", func(t *testing.T) {
		verify.DefaultExternalKeyEmail = "fallback@example.com"
		assert.Equal(t, "fallback@example.com", resolveExternalKeyEmail(&fakeStringConfig{}))

		cfg := &fakeStringConfig{
			set:  map[string]bool{"update.external_key_email": true},
			strs: map[string]string{"update.external_key_email": "release@phpboyscout.uk"},
		}
		assert.Equal(t, "release@phpboyscout.uk", resolveExternalKeyEmail(cfg))
	})

	t.Run("require_external_crosscheck_default_and_explicit", func(t *testing.T) {
		verify.DefaultRequireExternalCrosscheck = true
		assert.True(t, resolveRequireExternalCrosscheck(&fakeBoolConfig{}))

		cfg := &fakeBoolConfig{
			set:  map[string]bool{"update.require_external_crosscheck": true},
			vals: map[string]bool{"update.require_external_crosscheck": false},
		}
		assert.False(t, resolveRequireExternalCrosscheck(cfg))
	})
}

func TestSignatureAssetName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "checksums.txt.sig", (&SelfUpdater{}).SignatureAssetName())
	assert.Equal(t, "custom.sig", (&SelfUpdater{signatureAssetName: "custom.sig"}).SignatureAssetName())
}

// newSigningUpdater builds a minimal SelfUpdater for signature-gate tests.
func newSigningUpdater(p forge.Provider, resolver verify.KeyResolver, require bool) *SelfUpdater {
	return &SelfUpdater{
		Tool:             props.Tool{Name: "testtool"},
		logger:           logger.NewNoop(),
		releaseClient:    p,
		keyResolver:      resolver,
		requireSignature: require,
	}
}

func relWithSig(sig []byte) (*fakeRelease, *fakeProvider) {
	rel := &fakeRelease{
		name: "v1.0.0",
		assets: []forge.ReleaseAsset{
			&fakeAsset{name: "testtool_Linux_x86_64.tar.gz"},
			&fakeAsset{name: "checksums.txt"},
			&fakeAsset{name: "checksums.txt.sig"},
		},
	}
	bodies := map[string][]byte{}
	if sig != nil {
		bodies["checksums.txt.sig"] = sig
	}

	return rel, &fakeProvider{rel: rel, assetBodies: bodies}
}

func TestVerifyManifestSignature_NoResolver(t *testing.T) {
	t.Parallel()

	manifest := []byte("aa  f\n")
	rel, provider := relWithSig(nil)

	// Not required: skip silently.
	require.NoError(t, newSigningUpdater(provider, nil, false).
		verifyManifestSignature(context.Background(), rel, manifest))

	// Required: actionable error.
	err := newSigningUpdater(provider, nil, true).
		verifyManifestSignature(context.Background(), rel, manifest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no signing key")
}

func TestVerifyManifestSignature_ValidPasses(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	manifest := []byte("deadbeef  testtool_Linux_x86_64.tar.gz\n")
	sig := detachSign(t, testEd25519.entity, manifest)
	rel, provider := relWithSig(sig)

	s := newSigningUpdater(provider, verify.NewEmbeddedResolver(testEd25519.armoredPub), true)
	require.NoError(t, s.verifyManifestSignature(context.Background(), rel, manifest))
}

// infoSpyLogger embeds a noop Logger and records Info calls so tests can
// assert the verifying key fingerprint is logged on a successful verification.
type infoSpyLogger struct {
	logger.Logger
	infos []logEntry
}

type logEntry struct {
	msg     string
	keyvals []any
}

func (l *infoSpyLogger) Info(msg string, keyvals ...any) {
	l.infos = append(l.infos, logEntry{msg: msg, keyvals: keyvals})
}

func TestVerifyManifestSignature_LogsVerifyingFingerprint(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	manifest := []byte("deadbeef  testtool_Linux_x86_64.tar.gz\n")
	sig := detachSign(t, testEd25519.entity, manifest)
	rel, provider := relWithSig(sig)

	spy := &infoSpyLogger{Logger: logger.NewNoop()}
	s := newSigningUpdater(provider, verify.NewEmbeddedResolver(testEd25519.armoredPub), true)
	s.logger = spy

	require.NoError(t, s.verifyManifestSignature(context.Background(), rel, manifest))

	// Expected fingerprint comes from the trust set of the verifying key.
	ts, err := verify.LoadTrustSet(testEd25519.armoredPub)
	require.NoError(t, err)
	require.Len(t, ts.Fingerprints(), 1)
	wantFP := ts.Fingerprints()[0]

	var found bool

	for _, e := range spy.infos {
		if e.msg != "signature verified" {
			continue
		}

		for i := 0; i+1 < len(e.keyvals); i += 2 {
			if e.keyvals[i] == "fingerprint" && e.keyvals[i+1] == wantFP {
				found = true
			}
		}
	}

	assert.True(t, found, "successful verification must log the verifying key fingerprint %s for the audit trail", wantFP)
}

func TestVerifyManifestSignature_InvalidAlwaysFatal(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	signed := []byte("deadbeef  testtool_Linux_x86_64.tar.gz\n")
	sig := detachSign(t, testEd25519.entity, signed)
	rel, provider := relWithSig(sig)

	// requireSignature=false, but a present signature that does not
	// verify against the (tampered) manifest is still fatal.
	s := newSigningUpdater(provider, verify.NewEmbeddedResolver(testEd25519.armoredPub), false)
	tampered := []byte("beefdead  testtool_Linux_x86_64.tar.gz\n")

	err := s.verifyManifestSignature(context.Background(), rel, tampered)
	require.Error(t, err)
	assert.ErrorIs(t, err, verify.ErrSignatureInvalid)
}

func TestVerifyManifestSignature_MissingSignature(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	manifest := []byte("aa  testtool_Linux_x86_64.tar.gz\n")

	// No sig asset present.
	rel := &fakeRelease{name: "v1.0.0", assets: []forge.ReleaseAsset{
		&fakeAsset{name: "checksums.txt"},
	}}
	provider := &fakeProvider{rel: rel, assetBodies: map[string][]byte{}}
	resolver := verify.NewEmbeddedResolver(testEd25519.armoredPub)

	// Not required: skip.
	require.NoError(t, newSigningUpdater(provider, resolver, false).
		verifyManifestSignature(context.Background(), rel, manifest))

	// Required: verify.ErrSignatureMissing.
	err := newSigningUpdater(provider, resolver, true).
		verifyManifestSignature(context.Background(), rel, manifest)
	require.Error(t, err)
	assert.ErrorIs(t, err, verify.ErrSignatureMissing)
}

func TestVerifyManifestSignature_ResolverUnavailable(t *testing.T) {
	t.Parallel()

	manifest := []byte("aa  f\n")
	rel, provider := relWithSig([]byte("sig"))
	resolver := &fakeResolver{name: "wkd", err: verify.ErrKeyResolverUnavailable}

	// Not required: warn + proceed.
	require.NoError(t, newSigningUpdater(provider, resolver, false).
		verifyManifestSignature(context.Background(), rel, manifest))

	// Required: abort.
	err := newSigningUpdater(provider, resolver, true).
		verifyManifestSignature(context.Background(), rel, manifest)
	require.Error(t, err)
	assert.ErrorIs(t, err, verify.ErrKeyResolverUnavailable)
}

func TestVerifyManifestSignature_MismatchAlwaysFatal(t *testing.T) {
	t.Parallel()

	manifest := []byte("aa  f\n")
	rel, provider := relWithSig([]byte("sig"))
	resolver := &fakeResolver{name: "composite", err: verify.ErrKeyResolverMismatch}

	// Even with requireSignature=false, a trust-anchor mismatch aborts.
	err := newSigningUpdater(provider, resolver, false).
		verifyManifestSignature(context.Background(), rel, manifest)
	require.Error(t, err)
	assert.ErrorIs(t, err, verify.ErrKeyResolverMismatch)
}

func TestVerifyAssetChecksum_SignatureGate_Integration(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	binary := []byte("binary-body")
	manifest := manifestFor("testtool_Linux_x86_64.tar.gz", binary)
	sig := detachSign(t, testEd25519.entity, manifest)

	rel := &fakeRelease{
		name: "v1.0.0",
		assets: []forge.ReleaseAsset{
			&fakeAsset{name: "testtool_Linux_x86_64.tar.gz"},
			&fakeAsset{name: "checksums.txt"},
			&fakeAsset{name: "checksums.txt.sig"},
		},
	}
	provider := &fakeProvider{rel: rel, assetBodies: map[string][]byte{
		"checksums.txt":     manifest,
		"checksums.txt.sig": sig,
	}}

	s := newSigningUpdater(provider, verify.NewEmbeddedResolver(testEd25519.armoredPub), true)
	s.requireChecksum = true

	asset := &fakeAsset{name: "testtool_Linux_x86_64.tar.gz"}
	require.NoError(t, s.verifyAssetChecksum(context.Background(), rel, asset, binary))
}

func TestVerifyAssetChecksum_BadSignatureBlocksBeforeChecksum(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	binary := []byte("binary-body")
	manifest := manifestFor("testtool_Linux_x86_64.tar.gz", binary)
	// Signature over different bytes — invalid for this manifest.
	sig := detachSign(t, testEd25519.entity, []byte("not the manifest"))

	rel := &fakeRelease{
		name: "v1.0.0",
		assets: []forge.ReleaseAsset{
			&fakeAsset{name: "testtool_Linux_x86_64.tar.gz"},
			&fakeAsset{name: "checksums.txt"},
			&fakeAsset{name: "checksums.txt.sig"},
		},
	}
	provider := &fakeProvider{rel: rel, assetBodies: map[string][]byte{
		"checksums.txt":     manifest,
		"checksums.txt.sig": sig,
	}}

	s := newSigningUpdater(provider, verify.NewEmbeddedResolver(testEd25519.armoredPub), true)

	asset := &fakeAsset{name: "testtool_Linux_x86_64.tar.gz"}
	err := s.verifyAssetChecksum(context.Background(), rel, asset, binary)
	require.ErrorIs(t, err, verify.ErrSignatureInvalid,
		"a bad signature must abort before the checksum is even parsed")
	assert.NotContains(t, err.Error(), "checksum mismatch",
		"failure must be the signature, not the checksum")
}

// TestBuildDefaultKeyResolver_ConfigError covers the verification-config error
// path: key_source=external with no external email is an invalid posture, so
// verify.BuildKeyResolver rejects it and buildDefaultKeyResolver wraps the error
// (and leaves no resolver set).
func TestBuildDefaultKeyResolver_ConfigError(t *testing.T) {
	mustInitTestSigningKeys(t)

	s := &SelfUpdater{
		logger:       logger.NewNoop(),
		embeddedKeys: [][]byte{testEd25519.armoredPub},
		keySource:    "external", // external requires an email; none set → error
	}

	err := s.buildDefaultKeyResolver()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuring update signature verification")
	assert.Nil(t, s.keyResolver, "no resolver should be set when configuration fails")
}
