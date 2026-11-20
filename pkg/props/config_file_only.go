package props

import (
	"slices"
	"strings"

	"gitlab.com/phpboyscout/go/errors"
)

// ErrConfigFileOnly is a file-only pattern that could never match a key.
var ErrConfigFileOnly = errors.NewSentinel("gtb.props.config_file_only", "file-only config pattern")

// validateFileOnly refuses a pattern with an empty segment or anything after
// "**". go/config exposes no pattern validator, so the rules are restated here.
func validateFileOnly(patterns []string) error {
	for _, pattern := range patterns {
		segs := strings.Split(pattern, ".")
		deep := slices.Index(segs, "**")

		if slices.Contains(segs, "") || (deep >= 0 && deep != len(segs)-1) {
			return errors.WithHint(errors.Wrapf(ErrConfigFileOnly, "%q", pattern),
				"a file-only pattern is dotted key segments, where * is one segment and a final ** is everything below")
		}
	}

	return nil
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
