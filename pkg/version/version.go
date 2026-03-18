package version

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

// Version defines the interface for project version information.
type Version interface {
	GetVersion() string
	GetCommit() string
	GetDate() string
	String() string
	Compare(other string) int
	IsDevelopment() bool
}

// Info holds the concrete version information.
type Info struct {
	Version string `json:"version" yaml:"version"`
	Commit  string `json:"commit" yaml:"commit"`
	Date    string `json:"date" yaml:"date"`
}

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

func (i Info) IsDevelopment() bool {
	return i.Version == "v0.0.0" || i.Version == "dev" || i.Version == "vdev" || strings.Contains(i.Version, "-dev")
}

// FormatVersionString adds or removes a "v" prefix from version string.
func FormatVersionString(version string, prefixWanted bool) string {
	version = strings.TrimLeft(version, "v")
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
