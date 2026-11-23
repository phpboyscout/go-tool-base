package setup

import (
	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/signing/verify"
)

// ErrDuplicateTrustKey is returned by CheckTrustKeys for a key embedded more
// than once.
var ErrDuplicateTrustKey = errors.NewSentinel("gtb.setup.duplicate_trust_key", "a key is embedded more than once")

// CheckTrustKeys refuses a key embedded more than once across a tool's release
// keys and anchor-only keys, and any key that does not load. The trust set
// counts every copy, so a duplicate stops it matching the WKD bucket. A
// generated project's trustkeys test calls it, so the mistake fails CI rather
// than a shipped binary's update.
func CheckTrustKeys(release, anchorOnly [][]byte) error {
	seen := map[string]string{}

	for _, set := range []struct {
		name string
		keys [][]byte
	}{{"release", release}, {"anchor-only", anchorOnly}} {
		if len(set.keys) == 0 {
			continue
		}

		ts, err := verify.LoadTrustSet(set.keys...)
		if err != nil {
			return errors.Wrapf(err, "loading the %s keys", set.name)
		}

		for _, fp := range ts.Fingerprints() {
			if first, dup := seen[fp]; dup {
				return duplicateTrustKey(fp, first, set.name)
			}

			seen[fp] = set.name
		}
	}

	return nil
}

func duplicateTrustKey(fingerprint, first, second string) error {
	err := errors.Wrapf(ErrDuplicateTrustKey, "key %s is both a %s key and an %s key", fingerprint, first, second)
	if first == second {
		err = errors.Wrapf(ErrDuplicateTrustKey, "key %s is embedded twice among the %s keys", fingerprint, first)
	}

	return errors.WithHint(err,
		"keep one copy: under internal/trustkeys/keys/ if it signs releases, under internal/trustkeys/authority/ if it must not")
}
