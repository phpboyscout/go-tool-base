package generator

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/errors"
)

// ErrOverlapNeedsPrimary is returned when a rotation overlap is opened on a
// project that does not yet sign its releases.
var ErrOverlapNeedsPrimary = errors.NewSentinel("gtb.generator.overlap_needs_primary", "a rotation overlap needs a primary signing key")

// ErrOverlapKeyNotReleaseKey is returned when the overlap key's public key is
// not a release key the tool already embeds.
var ErrOverlapKeyNotReleaseKey = errors.NewSentinel("gtb.generator.overlap_key_not_release_key", "the overlap public key is not a release key")

// positionalArg is the shell's nth positional parameter, quoted. goreleaser
// expands $name in signs args before running them, so the dollar comes from a
// goreleaser template, which runs after that expansion.
func positionalArg(n string) string {
	return "\"{{ `$` }}" + n + "\""
}

// OverlapSignScript is the sh -c script that signs the checksums file with the
// primary key and then appends the overlap key's signature (spec 0222 D3).
// goreleaser passes the signature and artifact paths as $1 and $2.
func (s ManifestSigning) OverlapSignScript() string {
	paths := " --output " + positionalArg("1") + " " + positionalArg("2")

	return "set -e; sigillum --ci sign " + s.signArgs(s.KeyID, s.PublicKey, s.KMSRegion) + paths +
		"; sigillum --ci sign --append " + s.signArgs(s.Overlap.KeyID, s.Overlap.PublicKey, s.overlapRegion()) + paths
}

func (s ManifestSigning) signArgs(keyID, publicKey, region string) string {
	args := []string{"--backend", escapeShellArg(s.Backend)}
	if s.Backend == defaultSigningBackend {
		args = append(args, "--kms-region", escapeShellArg(region))
	}

	args = append(args, "--key-id", escapeShellArg(keyID), "--public-key", escapeShellArg(publicKey))

	return strings.Join(args, " ")
}

func (s ManifestSigning) overlapRegion() string {
	if s.Overlap.KMSRegion != "" {
		return s.Overlap.KMSRegion
	}

	return s.KMSRegion
}

// PromoteOverlap closes the rotation window on the new key: the overlap key
// becomes the primary, taking its region when it has one.
func (s ManifestSigning) PromoteOverlap() ManifestSigning {
	if s.Overlap == nil {
		return s
	}

	s.KeyID = s.Overlap.KeyID
	s.PublicKey = s.Overlap.PublicKey
	s.KMSRegion = s.overlapRegion()
	s.Overlap = nil

	return s
}

// validateOverlap checks an open overlap's values, and that its public key is
// a release key under internal/trustkeys/keys/ (spec 0222 D4).
func validateOverlap(s ManifestSigning) error {
	o := s.Overlap
	if o == nil {
		return nil
	}

	if s.KeyID == "" {
		return errors.WithHint(ErrOverlapNeedsPrimary,
			"record the current key with --key-id (signing.key_id) before opening an overlap; "+
				"when one is already open, close it first with gtb enable signing --promote-overlap or --end-overlap")
	}

	if o.KeyID == "" || o.PublicKey == "" {
		return errors.WithHint(ErrInvalidInput,
			"an overlap needs both --overlap-key-id and --overlap-public-key")
	}

	if err := ValidateSigningKeyID(o.KeyID); err != nil {
		return err
	}

	if err := ValidateSigningKMSRegion(o.KMSRegion); err != nil {
		return err
	}

	if err := ValidateSigningPublicKey(o.PublicKey); err != nil {
		return err
	}

	return overlapKeyIsReleaseKey(strings.TrimPrefix(o.PublicKey, "./"))
}

func overlapKeyIsReleaseKey(publicKey string) error {
	switch path.Dir(publicKey) {
	case signingKeysDir:
		return nil
	case signingAuthorityDir:
		return errors.WithHintf(ErrOverlapKeyNotReleaseKey,
			"%s is in %s/, whose keys anchor trust and must never sign a release; put the new release key in %s/",
			publicKey, signingAuthorityDir, signingKeysDir)
	default:
		return errors.WithHintf(ErrOverlapKeyNotReleaseKey,
			"%s must be in %s/, where the tool embeds the keys that may sign a release", publicKey, signingKeysDir)
	}
}

// requireOverlapKeyFile refuses an overlap whose public key is not in the tree.
// It cannot see whether a released version embeds the key yet, so the hint
// states the order.
func (g *Generator) requireOverlapKeyFile(s ManifestSigning) error {
	if s.Overlap == nil {
		return nil
	}

	exists, err := afero.Exists(g.props.FS, filepath.Join(g.config.Path, filepath.FromSlash(s.Overlap.PublicKey)))
	if err != nil {
		return errors.Newf("failed to check %s: %w", s.Overlap.PublicKey, err)
	}

	if !exists {
		return errors.WithHintf(ErrOverlapKeyNotReleaseKey,
			"%s does not exist. Commit the new public key there and release it before opening the overlap, so the copies people run trust it",
			s.Overlap.PublicKey)
	}

	return nil
}
