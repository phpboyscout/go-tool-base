package props

import (
	"slices"
	"strings"
	"unicode"

	"gitlab.com/phpboyscout/go/errors"
)

// ErrConfigFileOnly is a file-only pattern that could never match a key.
var ErrConfigFileOnly = errors.NewSentinel("gtb.props.config_file_only", "file-only config pattern")

// validateFileOnly refuses a pattern go/config would match literally or never:
// an empty segment, whitespace, a "*" inside a segment, or anything after "**".
// go/config exposes no pattern validator, so the rules are restated here.
func validateFileOnly(patterns []string) error {
	for _, pattern := range patterns {
		segs := strings.Split(pattern, ".")

		for i, seg := range segs {
			if !validFileOnlySegment(seg, i == len(segs)-1) {
				return errors.WithHint(errors.Wrapf(ErrConfigFileOnly, "%q", pattern),
					"a file-only pattern is dotted key segments without spaces, where * is a whole segment and a final ** is everything below")
			}
		}
	}

	return nil
}

func validFileOnlySegment(seg string, last bool) bool {
	switch {
	case seg == "*":
		return true
	case seg == "**":
		return last
	case seg == "", strings.Contains(seg, "*"):
		return false
	default:
		return !strings.ContainsFunc(seg, unicode.IsSpace)
	}
}

// EnvDenials are the patterns the environment layer denies: each FileOnly
// pattern, and every section above it as an exact path, so a variable cannot
// replace the section wholesale (spec 0214 D2).
func (s ConfigSpec) EnvDenials() []string {
	var out []string

	for _, pattern := range s.FileOnly {
		segs := strings.Split(pattern, ".")

		for i := 1; i <= len(segs); i++ {
			if denial := strings.Join(segs[:i], "."); !slices.Contains(out, denial) {
				out = append(out, denial)
			}
		}
	}

	return out
}
