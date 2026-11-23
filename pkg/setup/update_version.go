package setup

import (
	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go-tool-base/internal/binversion"
)

// ErrVersionMismatch is returned when a downloaded release is not the version
// asked for, or cannot show that it is when signatures are required (spec 0220).
var ErrVersionMismatch = errors.NewSentinel("gtb.setup.version_mismatch",
	"the update was stopped because the download is not the version it claims to be")

// WithVersionMismatchAllowed installs a release whose binary is not the version
// asked for, with a warning. Every other check still applies (spec 0220 D8).
func WithVersionMismatchAllowed() UpdaterOption {
	return func(s *SelfUpdater) { s.allowVersionMismatch = true }
}

// wantedVersion is what the binary must report: what was asked for, and
// otherwise the tag the channel names as latest (spec 0220 D3).
func (s *SelfUpdater) wantedVersion(rel forge.Release) string {
	if s.version != "" {
		return s.version
	}

	return rel.GetTagName()
}

// refuseOtherRelease refuses a release fetched by tag that names another tag.
func (s *SelfUpdater) refuseOtherRelease(rel forge.Release) error {
	// A provider that found nothing and said so with no error.
	if rel == nil {
		return errors.Wrapf(forge.ErrNotFound, "release %s", s.version)
	}

	if got := rel.GetTagName(); got != s.version && !binversion.Same(got, s.version) {
		return s.versionMismatch(s.version, "the release is tagged "+got)
	}

	return nil
}

// binaryCheck returns the check that the binary at a path is the version
// wanted. A tag that is not a version leaves nothing to compare, as a binary
// that records none does (spec 0220 D7).
func (s *SelfUpdater) binaryCheck(wanted string) func(path string) error {
	return func(path string) error {
		recorded, err := s.recordedVersion(path)
		if err != nil {
			return err
		}

		unchecked := ""

		switch {
		case !binversion.IsVersion(wanted):
			unchecked = "that tag is not a version"
		case recorded == "":
			unchecked = "its binary records no version"
		}

		switch {
		case unchecked != "" && s.requireSignature:
			return s.versionMismatch(wanted, unchecked)
		case unchecked != "":
			s.logger.Warn("the release's version could not be checked: "+unchecked, "expected", wanted, "recorded", recorded)
		case binversion.Same(recorded, wanted):
		case !s.allowVersionMismatch:
			return s.versionMismatch(wanted, "its binary is "+recorded)
		default:
			s.logger.Warn("installing a release that is not the version asked for",
				"expected", wanted, "recorded", recorded)
		}

		return nil
	}
}

func (s *SelfUpdater) recordedVersion(path string) (string, error) {
	f, err := s.Fs.Open(path)
	if err != nil {
		return "", errors.Wrap(err, "failed to read the version of the downloaded binary")
	}
	defer func() { _ = f.Close() }()

	return binversion.Read(f), nil
}

func (s *SelfUpdater) versionMismatch(wanted, found string) error {
	return errors.WithHintf(errors.WithStack(ErrVersionMismatch),
		"Do not install it by any other means. Report it to the publisher of %s: the update was for %s, and %s. "+
			"Nothing you can configure will fix this.", s.Tool.Name, wanted, found)
}
