package setup

import (
	"context"
	"strings"

	"github.com/cockroachdb/errors"
	"gitlab.com/phpboyscout/go/signing/verify"

	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

// signatureDefaultAssetName is the filename GoReleaser produces for the
// detached signature over the checksums manifest. Override per-tool via
// the `update.signature_asset_name` config key.
const signatureDefaultAssetName = "checksums.txt.sig"

// WithKeyResolver overrides the default key resolver used for signature
// verification. When set, the config-driven default (built from
// WithEmbeddedKeys and the update.key_source family) is bypassed
// entirely — the tool author owns the resolver chain.
func WithKeyResolver(r verify.KeyResolver) UpdaterOption {
	return func(s *SelfUpdater) { s.keyResolver = r }
}

// WithEmbeddedKeys supplies the tool's embedded release public keys (in
// ASCII-armored form). NewUpdater builds the default resolver from
// these keys and the resolved update.key_source / external_key_email /
// require_external_crosscheck config. Ignored when WithKeyResolver is
// also supplied.
func WithEmbeddedKeys(armoredKeys ...[]byte) UpdaterOption {
	return func(s *SelfUpdater) { s.embeddedKeys = armoredKeys }
}

// SignatureAssetName returns the configured signature filename, or the
// GoReleaser default "checksums.txt.sig" when unset.
func (s *SelfUpdater) SignatureAssetName() string {
	if s.signatureAssetName != "" {
		return s.signatureAssetName
	}

	return signatureDefaultAssetName
}

// stringConfig is the narrow subset of [config.Containable] the
// string-valued signing resolvers depend on, declared so tests can
// pass a small fake.
type stringConfig interface {
	IsSet(key string) bool
	GetString(key string) string
}

// resolveRequireSignature mirrors [resolveRequireChecksum]: explicit
// config (which Viper AutomaticEnv also pulls from a prefixed env var)
// first, otherwise the compile-time [verify.DefaultRequireSignature].
func resolveRequireSignature(cfg boolConfig) bool {
	if cfg == nil || reflectIsNil(cfg) {
		return verify.DefaultRequireSignature
	}

	if cfg.IsSet("update.require_signature") {
		return cfg.GetBool("update.require_signature")
	}

	return verify.DefaultRequireSignature
}

// resolveRequireExternalCrosscheck applies the same precedence for the
// WKD cross-check enforcement flag.
func resolveRequireExternalCrosscheck(cfg boolConfig) bool {
	if cfg == nil || reflectIsNil(cfg) {
		return verify.DefaultRequireExternalCrosscheck
	}

	if cfg.IsSet("update.require_external_crosscheck") {
		return cfg.GetBool("update.require_external_crosscheck")
	}

	return verify.DefaultRequireExternalCrosscheck
}

// resolveKeySource resolves update.key_source, defaulting to
// [verify.DefaultKeySource].
func resolveKeySource(cfg stringConfig) string {
	if cfg == nil || reflectIsNil(cfg) {
		return verify.DefaultKeySource
	}

	if cfg.IsSet("update.key_source") {
		if v := strings.TrimSpace(cfg.GetString("update.key_source")); v != "" {
			return v
		}
	}

	return verify.DefaultKeySource
}

// resolveExternalKeyEmail resolves update.external_key_email, defaulting
// to [verify.DefaultExternalKeyEmail].
func resolveExternalKeyEmail(cfg stringConfig) string {
	if cfg == nil || reflectIsNil(cfg) {
		return verify.DefaultExternalKeyEmail
	}

	if cfg.IsSet("update.external_key_email") {
		if v := strings.TrimSpace(cfg.GetString("update.external_key_email")); v != "" {
			return v
		}
	}

	return verify.DefaultExternalKeyEmail
}

// verifyManifestSignature resolves the trust set, fetches the detached
// signature, and verifies it against the raw manifest bytes. It runs
// before the manifest is parsed (spec decision 17).
//
// Failure handling distinguishes three cases:
//   - A fingerprint mismatch between trust anchors (verify.ErrKeyResolverMismatch)
//     is an active-tampering signal and is ALWAYS fatal.
//   - A present-but-invalid signature is ALWAYS fatal — a forged or
//     corrupted signature must never be accepted.
//   - An absent signature, an unreachable resolver, or no configured
//     resolver is gated by requireSignature: fail-closed aborts,
//     fail-open logs a warning and proceeds.
func (s *SelfUpdater) verifyManifestSignature(ctx context.Context, rel release.Release, manifest []byte) error {
	if s.keyResolver == nil {
		if s.requireSignature {
			return errors.WithHint(
				errors.New("update.require_signature is enabled but no signing key is configured"),
				"Supply the release public key via WithEmbeddedKeys or WithKeyResolver, set "+
					"update.external_key_email for a WKD-sourced key, or disable update.require_signature.",
			)
		}

		return nil
	}

	trustSet, err := s.keyResolver.Resolve(ctx)
	if err != nil {
		return s.handleResolveError(err)
	}

	return s.verifyAgainstTrustSet(ctx, rel, manifest, trustSet)
}

// handleResolveError applies the trust-set resolution failure policy: a
// fingerprint mismatch is always fatal; any other resolver error is
// gated by requireSignature.
func (s *SelfUpdater) handleResolveError(err error) error {
	if errors.Is(err, verify.ErrKeyResolverMismatch) {
		return err
	}

	if s.requireSignature {
		return errors.Wrap(err, "resolving signing trust set (require_signature is enabled)")
	}

	s.logger.Warn("could not resolve signing trust set; proceeding without signature verification",
		"resolver", s.keyResolver.Name(), "error", err)

	return nil
}

// verifyAgainstTrustSet fetches the detached signature and verifies it
// against the resolved trust set. A present-but-invalid signature is
// always fatal; an absent signature is gated by requireSignature.
func (s *SelfUpdater) verifyAgainstTrustSet(ctx context.Context, rel release.Release, manifest []byte, trustSet *verify.TrustSet) error {
	sig, err := s.fetchSignature(ctx, rel)
	if err != nil {
		if s.requireSignature {
			return errors.Wrap(err, "failed to retrieve signature (require_signature is enabled)")
		}

		s.logger.Warn("failed to retrieve signature; proceeding without signature verification",
			"release", rel.GetName(), "error", err)

		return nil
	}

	if sig == nil {
		if s.requireSignature {
			return errors.WithHintf(verify.ErrSignatureMissing,
				"No %q was found in release %q and update.require_signature is enabled.",
				s.SignatureAssetName(), rel.GetName())
		}

		s.logger.Warn("no signature found; skipping signature verification",
			"release", rel.GetName(), "expected", s.SignatureAssetName())

		return nil
	}

	fingerprint, err := trustSet.VerifyManifestSignatureSigner(manifest, sig)
	if err != nil {
		return err
	}

	// Record the verifying key's fingerprint for the audit trail: it
	// identifies exactly which trust-anchor key authorised this update.
	s.logger.Info("signature verified",
		"resolver", s.keyResolver.Name(), "fingerprint", fingerprint)

	return nil
}

// fetchSignature retrieves the detached signature for rel, preferring
// [release.SignatureProvider] when the provider opts in and falling
// back to asset-list lookup otherwise. Returns (nil, nil) when no
// signature is available by either route.
func (s *SelfUpdater) fetchSignature(ctx context.Context, rel release.Release) ([]byte, error) {
	if sp, ok := s.releaseClient.(release.SignatureProvider); ok {
		sig, err := sp.DownloadSignature(ctx, rel, verify.MaxSignatureSize)
		if err == nil {
			return sig, nil
		}

		if !errors.Is(err, release.ErrNotSupported) {
			return nil, err
		}
		// ErrNotSupported: provider opted out. Fall through to asset lookup.
	}

	sigAsset, found := s.findSignatureAsset(rel)
	if !found {
		return nil, nil
	}

	return s.downloadBoundedAsset(ctx, sigAsset, verify.MaxSignatureSize, verify.ErrSignatureTooLarge, "signature")
}

// findSignatureAsset returns the release asset whose name matches the
// configured signature filename. The second return value is false when
// no matching asset is present.
func (s *SelfUpdater) findSignatureAsset(rel release.Release) (release.ReleaseAsset, bool) {
	name := s.SignatureAssetName()

	for _, asset := range rel.GetAssets() {
		if asset.GetName() == name {
			return asset, true
		}
	}

	return nil, false
}
