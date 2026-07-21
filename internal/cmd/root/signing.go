package root

import (
	"gitlab.com/phpboyscout/go/signing/verify"
)

// Phase 1 + Phase 2 of the remote-update-checksum-verification spec.
// gtb-specific signing defaults — separated from root.go so the
// scaffolding generator (which templates root.go) doesn't ship these
// values into downstream tools that have their own signing posture.
//
// Downstream tools that want the same defaults set their own values
// in the equivalent of `internal/cmd/root/signing.go` (or anywhere
// else under their own main package); the framework reads these as
// package-level variables in pkg/setup.
//
// End users override via:
//
//   - `update.require_checksum` / `GTB_UPDATE_REQUIRE_CHECKSUM`
//   - `update.require_signature` / `GTB_UPDATE_REQUIRE_SIGNATURE`
//   - `update.external_key_email` / `GTB_UPDATE_EXTERNAL_KEY_EMAIL`
//   - `update.key_source` / `GTB_UPDATE_KEY_SOURCE` (embedded | external | both)
//
// See [docs/explanation/components/setup/signature-verification.md].
func init() {
	// Phase 1 (checksums) now travels on props.Tool.Signing.RequireChecksum,
	// set where the Props are built — see root.go. It moved off a package
	// variable because process-wide mutable state is not a configuration
	// mechanism, and mutating one from init() raced under t.Parallel.

	// Phase 2 (close-out v0.13.0): v0.12.2 was the first signed release
	// (KMS-held key, OIDC-federated GitLab CI, ASCII-armored
	// checksums.txt.sig attached to the GitLab Release). Every release
	// from this binary's tag onward will carry a signature, so the
	// library default fails closed: a verifier with the embedded trust
	// anchor (internal/trustkeys/keys/*.asc) cross-checked against the
	// externally-served WKD copy at openpgpkey.phpboyscout.uk will
	// refuse to install an update that wasn't signed by the trusted
	// release key.
	verify.DefaultRequireSignature = true

	// Phase 2 (v0.13.1): without this, the verifier silently degrades
	// to embedded-only (logs `resolver=embedded`) because the default
	// `key_source` is "both" but `BuildKeyResolver` falls back to a
	// single resolver when the other source is missing. Setting the
	// email here wires up `CompositeResolver{Embedded, WKD}` so each
	// update cross-checks the embedded trust anchor against the
	// externally-served copy at openpgpkey.phpboyscout.uk — exactly
	// the two-of-three trust-anchor independence the Phase 2 design
	// promises.
	verify.DefaultExternalKeyEmail = "release@phpboyscout.uk"
}
