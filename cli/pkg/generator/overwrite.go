package generator

import "gitlab.com/phpboyscout/go/errors"

// OverwriteMode is the file-conflict policy a generation or regeneration
// runs under.
type OverwriteMode string

// The three overwrite modes; empty parses as OverwriteAsk.
const (
	OverwriteAsk   OverwriteMode = "ask"
	OverwriteAllow OverwriteMode = "allow"
	OverwriteDeny  OverwriteMode = "deny"
)

// ErrInvalidOverwriteMode is returned for an --overwrite value outside the
// three modes.
var ErrInvalidOverwriteMode = errors.NewSentinel("gtb.generator.invalid_overwrite_mode",
	"invalid overwrite mode: must be allow, deny, or ask")

// ParseOverwriteMode maps a flag value onto an OverwriteMode; empty is ask.
func ParseOverwriteMode(s string) (OverwriteMode, error) {
	switch OverwriteMode(s) {
	case "":
		return OverwriteAsk, nil
	case OverwriteAsk, OverwriteAllow, OverwriteDeny:
		return OverwriteMode(s), nil
	default:
		return "", errors.Wrapf(ErrInvalidOverwriteMode, "%q", s)
	}
}
