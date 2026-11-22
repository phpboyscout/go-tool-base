package setup

import (
	"context"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/forge"
	"gitlab.com/phpboyscout/go/signing/verify"
)

// ErrRotationSignatureMissing is a release that lists a rotation manifest
// without the authority signature beside it.
var ErrRotationSignatureMissing = errors.NewSentinel("gtb.setup.rotation_signature_missing", "the rotation manifest has no signature")

// RotationEnabled reports update.rotation.enabled, which defaults to true
// (spec 0101 D18).
func RotationEnabled(cfg config.Reader) bool {
	if cfg == nil || !cfg.IsSet(ConfigKeyUpdateRotationEnabled) {
		return true
	}

	return cfg.GetBool(ConfigKeyUpdateRotationEnabled)
}

// applyRotation applies the rotation manifest rel carries, before the
// release's own signature is checked, so a release signed by a replacement
// key verifies on the update that introduces it (spec 0101 D4). A release
// that carries none leaves the stored record in force. One that carries a
// manifest which cannot be fetched or does not verify fails the update,
// whatever require_signature says (D7).
func (s *SelfUpdater) applyRotation(ctx context.Context, rel forge.Release) error {
	manifestAsset, listed := findAssetNamed(rel, verify.RotationManifestFile)
	if !listed {
		return nil
	}

	if s.rotationApplier == nil {
		if len(s.rotationAuthorityKeys) > 0 {
			s.logger.Warn(ConfigKeyUpdateRotationEnabled+" is false, so the rotation manifest this release carries is skipped",
				"release", rel.GetName())
		}

		return nil
	}

	manifest, err := s.downloadBoundedAsset(ctx, manifestAsset, verify.MaxRotationManifestSize, verify.ErrRotationManifestTooLarge, "rotation manifest")
	if err != nil {
		return errors.Wrapf(err, "fetching the rotation manifest of release %q", rel.GetName())
	}

	sigAsset, ok := findAssetNamed(rel, verify.RotationSignatureFile)
	if !ok {
		return errors.Wrapf(ErrRotationSignatureMissing, "release %q lists %s without %s",
			rel.GetName(), verify.RotationManifestFile, verify.RotationSignatureFile)
	}

	sig, err := s.downloadBoundedAsset(ctx, sigAsset, verify.MaxSignatureSize, verify.ErrSignatureTooLarge, "rotation signature")
	if err != nil {
		return errors.Wrapf(err, "fetching the rotation signature of release %q", rel.GetName())
	}

	rec, err := s.rotationApplier.ApplyRotation(ctx, manifest, sig)
	if err != nil {
		return errors.WithHint(errors.Wrapf(err, "applying the rotation manifest of release %q", rel.GetName()),
			"The release's key rotation could not be trusted, so the update stops here. Report it to the tool's maintainers.")
	}

	s.logger.Info("rotation manifest in force", "release", rel.GetName(), "epoch", rec.Epoch)

	return nil
}

func findAssetNamed(rel forge.Release, name string) (forge.ReleaseAsset, bool) {
	for _, asset := range rel.GetAssets() {
		if asset.GetName() == name {
			return asset, true
		}
	}

	return nil, false
}
