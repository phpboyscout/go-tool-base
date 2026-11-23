package setup

import (
	"bytes"
	"encoding/hex"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/signing/verify"
)

// ErrDuplicateTrustKey is returned by CheckTrustKeys for a key embedded more
// than once.
var ErrDuplicateTrustKey = errors.NewSentinel("gtb.setup.duplicate_trust_key", "a key is embedded more than once")

// CheckTrustKeys refuses a key embedded more than once across a tool's release
// keys and anchor-only keys, a key bound as another key's subkey, and any key
// that does not load. The trust set counts every copy, so a duplicate stops it
// matching the WKD bucket, and a key bound as a release key's subkey signs
// releases through it. A generated project's trustkeys test calls it, so the
// mistake fails CI rather than a shipped binary's update.
func CheckTrustKeys(release, anchorOnly [][]byte) error {
	seen := map[string]trustKeyHolder{}

	for _, set := range []struct {
		name string
		keys [][]byte
	}{{"release", release}, {"anchor-only", anchorOnly}} {
		if len(set.keys) == 0 {
			continue
		}

		// go/signing's TrustSet reports primary fingerprints only (v0.15.2),
		// so subkeys are read from the same blobs once it has accepted them.
		if _, err := verify.LoadTrustSet(set.keys...); err != nil {
			return errors.Wrapf(err, "loading the %s keys", set.name)
		}

		for _, blob := range set.keys {
			entities, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(blob))
			if err != nil {
				return errors.Wrapf(err, "loading the %s keys", set.name)
			}

			for _, e := range entities {
				if err := holdTrustKey(seen, set.name, e); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// trustKeyHolder is where a fingerprint was first seen: the set, and the
// primary it belongs to when it is a subkey.
type trustKeyHolder struct {
	set, subkeyOf string
}

func holdTrustKey(seen map[string]trustKeyHolder, set string, e *openpgp.Entity) error {
	primary := fingerprint(e.PrimaryKey.Fingerprint)

	// The primary goes first, so a key embedded twice is reported as one
	// rather than through a subkey both copies carry.
	fps := make([]string, 0, 1+len(e.Subkeys))
	fps = append(fps, primary)

	for _, sub := range e.Subkeys {
		fps = append(fps, fingerprint(sub.PublicKey.Fingerprint))
	}

	for i, fp := range fps {
		here := trustKeyHolder{set: set}
		if i > 0 {
			here.subkeyOf = primary
		}

		if first, dup := seen[fp]; dup {
			return duplicateTrustKey(fp, first, here)
		}

		seen[fp] = here
	}

	return nil
}

func fingerprint(raw []byte) string { return strings.ToUpper(hex.EncodeToString(raw)) }

func duplicateTrustKey(fp string, first, second trustKeyHolder) error {
	if first.subkeyOf == "" && second.subkeyOf != "" {
		first, second = second, first
	}

	var err error

	switch {
	case first.subkeyOf != "" && second.subkeyOf != "":
		err = errors.Wrapf(ErrDuplicateTrustKey, "key %s is a subkey of %s key %s and of %s key %s", fp, first.set, first.subkeyOf, second.set, second.subkeyOf)
	case first.subkeyOf != "":
		err = errors.Wrapf(ErrDuplicateTrustKey, "key %s is a subkey of %s key %s and also %s", fp, first.set, first.subkeyOf, aKeyOf(second.set))
	case first.set == second.set:
		err = errors.Wrapf(ErrDuplicateTrustKey, "key %s is embedded twice among the %s keys", fp, first.set)
	default:
		err = errors.Wrapf(ErrDuplicateTrustKey, "key %s is both %s and %s", fp, aKeyOf(first.set), aKeyOf(second.set))
	}

	return errors.WithHint(err,
		"keep one copy: under internal/trustkeys/keys/ if it signs releases, under internal/trustkeys/authority/ if it must not, and bound as no other key's subkey")
}

func aKeyOf(set string) string {
	if strings.ContainsRune("aeiou", rune(set[0])) {
		return "an " + set + " key"
	}

	return "a " + set + " key"
}
