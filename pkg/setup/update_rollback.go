package setup

import (
	"context"

	"golang.org/x/mod/semver"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	ver "gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// rollbackCandidates bounds the listing a no-tag rollback searches: one page on
// a forge, and one manifest fetch each on the static channel (spec 0104 D2).
const rollbackCandidates = 20

// ErrNoEarlierRelease is a rollback with no release below the running version
// among the newest releases the channel lists.
var ErrNoEarlierRelease = errors.NewSentinel("gtb.setup.no_earlier_release", "no earlier release to roll back to")

// ErrDowngradeNotConfirmed is a downgrade nobody confirmed: declined at the
// prompt, or asked for unattended without --force.
var ErrDowngradeNotConfirmed = errors.NewSentinel("gtb.setup.downgrade_not_confirmed", "a downgrade needs confirmation")

// ReleaseBefore is the highest released version below current, among the
// newest releases the channel lists. From a stable version, prereleases are
// not candidates. Version order rather than the static channel's publish
// order, so a backport published later is still found by its version.
func (s *SelfUpdater) ReleaseBefore(ctx context.Context, current string) (string, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()

	releases, err := s.releaseChannel().List(timeoutCtx, rollbackCandidates)
	if err != nil {
		return "", s.explainRefusal(ctx, errors.Wrap(err, "failed to list releases"))
	}

	stable := semver.Prerelease(current) == ""
	best := ""

	for _, rel := range releases {
		tag := ver.FormatVersionString(rel.GetTagName(), true)

		switch {
		case rel.GetDraft(), !semver.IsValid(tag), stable && semver.Prerelease(tag) != "":
			continue
		case semver.Compare(tag, current) < 0 && (best == "" || semver.Compare(tag, best) > 0):
			best = tag
		}
	}

	if best == "" {
		return "", s.noEarlierRelease(current)
	}

	return best, nil
}

func (s *SelfUpdater) noEarlierRelease(current string) error {
	err := errors.Wrapf(ErrNoEarlierRelease, "nothing below %s among the %d newest releases", current, rollbackCandidates)

	if s.Tool.ReleaseSource.Type == props.ReleaseSourceStatic {
		return errors.WithHint(err,
			"Only releases published since this tool moved to its static release channel can be fetched. "+
				"Name an older one with 'update --rollback=vX.Y.Z', or install it from its archive with 'update --from-file'.")
	}

	return errors.WithHint(err, "Name the release to roll back to with 'update --rollback=vX.Y.Z'.")
}
