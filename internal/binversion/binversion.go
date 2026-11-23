// Package binversion reads the release version the Go toolchain recorded in a
// binary, without running it (spec 0220 D1).
package binversion

import (
	"debug/buildinfo"
	"io"
	"runtime/debug"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"

	ver "gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// Read returns the release version recorded in the binary at r, or "" when it
// records none or cannot be read. The three shapes are one rule (spec 0220 D2).
func Read(r io.ReaderAt) string {
	info, err := buildinfo.Read(r)
	if err != nil {
		return ""
	}

	return Recorded(info)
}

// Recorded is the release version in info, or "" when it records none.
func Recorded(info *debug.BuildInfo) string {
	v := info.Main.Version
	if !semver.IsValid(v) || module.IsPseudoVersion(v) {
		return ""
	}

	return v
}

// IsVersion reports whether v is a full version, with or without its v prefix.
func IsVersion(v string) bool {
	return canonical(v) != ""
}

// Same reports whether two versions name one release. Build metadata does not
// count, because a release build may be stamped +dirty (spec 0220 D2).
func Same(v, w string) bool {
	v, w = canonical(v), canonical(w)

	return v != "" && v == w
}

// canonical is v without its build metadata, or "" when v is not a full
// version. semver takes v1 and v20261124, which no tagged build records.
func canonical(v string) string {
	v = ver.FormatVersionString(v, true)

	c := semver.Canonical(v)
	if c+semver.Build(v) != v {
		return ""
	}

	return c
}
