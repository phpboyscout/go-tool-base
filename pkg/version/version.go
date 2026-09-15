package version

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

// Info holds a build's version information. Its zero value means the
// binary was not stamped; see IsZero.
type Info struct {
	Version string `json:"version" yaml:"version"`
	Commit  string `json:"commit" yaml:"commit"`
	Date    string `json:"date" yaml:"date"`
}

// IsZero reports whether no version was stamped into the binary.
func (i Info) IsZero() bool { return i == Info{} }

// NewInfo creates a new Info instance.
func NewInfo(v, c, d string) Info {
	return Info{
		Version: FormatVersionString(v, true),
		Commit:  c,
		Date:    d,
	}
}

func (i Info) GetVersion() string { return i.Version }
func (i Info) GetCommit() string  { return i.Commit }
func (i Info) GetDate() string    { return i.Date }

func (i Info) String() string {
	if i.Commit != "" && i.Commit != "none" {
		return fmt.Sprintf("%s (%s)", i.Version, i.Commit)
	}

	return i.Version
}

func (i Info) Compare(other string) int {
	return CompareVersions(i.Version, other)
}

// IsDevelopment reports whether this is not a release build: an invalid
// version, a prerelease carrying a dev or dirty segment, or any build
// metadata (a VCS-stamped module build reports the tag plus "+dirty").
func (i Info) IsDevelopment() bool {
	v := FormatVersionString(i.Version, true)
	if !semver.IsValid(v) {
		return true
	}

	if semver.Build(v) != "" {
		return true
	}

	pre := semver.Prerelease(v)

	return strings.Contains(pre, "dev") || strings.Contains(pre, "dirty")
}

// FormatVersionString adds or removes a "v" prefix from version string.
func FormatVersionString(version string, prefixWanted bool) string {
	version = strings.TrimPrefix(version, "v")
	if prefixWanted && version != "" {
		version = fmt.Sprintf("v%s", version)
	}

	return version
}

// CompareVersions returns an integer comparing two versions according to semantic version precedence.
func CompareVersions(v, w string) int {
	v = FormatVersionString(v, true)
	w = FormatVersionString(w, true)

	return semver.Compare(v, w)
}
