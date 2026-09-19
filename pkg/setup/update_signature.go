package setup

import (
	"context"
	"strings"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/signing/verify"

	"gitlab.com/phpboyscout/go/forge"
)

// signatureDefaultAssetName is the filename GoReleaser produces for the
// detached signature over the checksums manifest. Override per-tool via
// the `update.signature_asset_name` config key.
const signatureDefaultAssetName = "checksums.txt.sig"

// withKeyResolver overrides the default key resolver used for signature
// verification. When set, the config-driven default (built from the tool's
// embedded keys and the update.key_source family) is bypassed entirely.
func withKeyResolver(r verify.KeyResolver) UpdaterOption {
	return func(s *SelfUpdater) { s.keyResolver = r }
}

// withEmbeddedKeys supplies release public keys (in ASCII-armored form) in
// place of props.Tool.Signing.EmbeddedKeys. NewUpdater builds the default
// resolver from these keys and the resolved update.key_source /
// external_key_email / require_external_crosscheck config. Ignored when
// withKeyResolver is also supplied.
func withEmbeddedKeys(armoredKeys ...[]byte) UpdaterOption {
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

// resolveRequireSignature mirrors [resolveRequireChecksum]: explicit
// config (which Viper AutomaticEnv also pulls from a prefixed env var)
// first, otherwise the compile-time [verify.DefaultRequireSignature].
func resolveRequireSignature(cfg config.Reader) bool {
	if cfg == nil {
		return verify.DefaultRequireSignature
	}

	if cfg.IsSet(ConfigKeyUpdateRequireSignature) {
		return cfg.GetBool(ConfigKeyUpdateRequireSignature)
	}

	return verify.DefaultRequireSignature
}

// resolveRequireExternalCrosscheck applies the same precedence for the
// WKD cross-check enforcement flag.
func resolveRequireExternalCrosscheck(cfg config.Reader) bool {
	if cfg == nil {
		return verify.DefaultRequireExternalCrosscheck
	}

	if cfg.IsSet(ConfigKeyUpdateRequireExternalCrosscheck) {
		return cfg.GetBool(ConfigKeyUpdateRequireExternalCrosscheck)
	}

	return verify.DefaultRequireExternalCrosscheck
}

// resolveKeySource resolves update.key_source, defaulting to
// [verify.DefaultKeySource].
func resolveKeySource(cfg config.Reader) string {
	if cfg == nil {
		return verify.DefaultKeySource
	}

	if cfg.IsSet(ConfigKeyUpdateKeySource) {
		if v := strings.TrimSpace(cfg.GetString(ConfigKeyUpdateKeySource)); v != "" {
			return v
		}
	}

	return verify.DefaultKeySource
}

// resolveExternalKeyEmail resolves update.external_key_email, defaulting
// to [verify.DefaultExternalKeyEmail].
func resolveExternalKeyEmail(cfg config.Reader) string {
	if cfg == nil {
		return verify.DefaultExternalKeyEmail
	}

	if cfg.IsSet(ConfigKeyUpdateExternalKeyEmail) {
		if v := strings.TrimSpace(cfg.GetString(ConfigKeyUpdateExternalKeyEmail)); v != "" {
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
func (s *SelfUpdater) verifyManifestSignature(ctx context.Context, rel forge.Release, manifest []byte) error {
	if s.keyResolver == nil {
		if s.requireSignature {
			return errors.WithHint(
				errors.New("update.require_signature is enabled but no signing key is configured"),
				"Supply the release public key via props.Tool.Signing.EmbeddedKeys, set "+
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
func (s *SelfUpdater) verifyAgainstTrustSet(ctx context.Context, rel forge.Release, manifest []byte, trustSet *verify.TrustSet) error {
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
// [forge.SignatureProvider] when the provider opts in and falling
// back to asset-list lookup otherwise. Returns (nil, nil) when no
// signature is available by either route.
func (s *SelfUpdater) fetchSignature(ctx context.Context, rel forge.Release) ([]byte, error) {
	sig, err := s.releaseChannel().Signature(ctx, rel, verify.MaxSignatureSize)
	if err == nil {
		return sig, nil
	}

	if !errors.Is(err, forge.ErrNotSupported) {
		return nil, err
	}
	// ErrNotSupported: the channel has no direct route. Fall through to asset lookup.

	sigAsset, found := s.findSignatureAsset(rel)
	if !found {
		return nil, nil
	}

	return s.downloadBoundedAsset(ctx, sigAsset, verify.MaxSignatureSize, verify.ErrSignatureTooLarge, "signature")
}

// findSignatureAsset returns the release asset whose name matches the
// configured signature filename. The second return value is false when
// no matching asset is present.
func (s *SelfUpdater) findSignatureAsset(rel forge.Release) (forge.ReleaseAsset, bool) {
	name := s.SignatureAssetName()

	for _, asset := range rel.GetAssets() {
		if asset.GetName() == name {
			return asset, true
		}
	}

	return nil, false
}
