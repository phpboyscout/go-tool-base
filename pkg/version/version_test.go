package version

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInfo_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		info   Info
		expect string
	}{
		{"version with commit", Info{Version: "v1.2.3", Commit: "abc123"}, "v1.2.3 (abc123)"},
		{"version without commit", Info{Version: "v1.2.3", Commit: ""}, "v1.2.3"},
		{"version with none commit", Info{Version: "v1.2.3", Commit: "none"}, "v1.2.3"},
		{"empty version", Info{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expect, tt.info.String())
		})
	}
}

func TestInfo_IsDevelopment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		isDev   bool
	}{
		{"release version", "1.2.3", false},
		{"dev suffix", "1.2.3-dev", true},
		{"dirty suffix", "1.2.3-dirty", true},
		{"empty string", "", true},
		{"invalid version", "not-a-version", true},
		{"prerelease", "1.0.0-beta.1", false},
		{"dirty build metadata", "0.42.0+dirty", true},
		{"dev prerelease segment", "0.42.0-rc.1.dev", true},
		// A go build or go install ...@main stamps a Go pseudo-version, which is
		// valid semver with no dev segment and no build metadata, so it read as a
		// release and ran the update check against an older release source (F19
		// of the v0.43.0 manual round, architecture review A8).
		{"pseudo-version from a commit", "0.42.1-0.20260918131217-27ba9833f144", true},
		{"pseudo-version from a tagged prerelease", "0.43.0-rc.1.0.20260918131217-27ba9833f144", true},
		{"pseudo-version with no prior tag", "0.0.0-20260918131217-27ba9833f144", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			info := NewInfo(tt.version, "", "")
			assert.Equal(t, tt.isDev, info.IsDevelopment())
		})
	}
}

func TestFormatVersionString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		version      string
		prefixWanted bool
		expect       string
	}{
		{"add prefix", "1.2.3", true, "v1.2.3"},
		{"already prefixed", "v1.2.3", true, "v1.2.3"},
		{"remove prefix", "v1.2.3", false, "1.2.3"},
		{"no prefix needed", "1.2.3", false, "1.2.3"},
		{"empty with prefix", "", true, ""},
		{"empty without prefix", "", false, ""},
		// TrimPrefix strips exactly one leading "v" — a literal version part
		// beginning with another "v" must be preserved (TrimLeft would have
		// eaten both, which is the bug this guards against).
		{"strips only one v", "vvendor", false, "vendor"},
		{"strips only one v re-prefixed", "vvendor", true, "vvendor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expect, FormatVersionString(tt.version, tt.prefixWanted))
		})
	}
}

func TestCompareVersions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		a, b     string
		expected int
	}{
		{"equal", "1.0.0", "1.0.0", 0},
		{"a greater major", "2.0.0", "1.0.0", 1},
		{"b greater minor", "1.0.0", "1.1.0", -1},
		{"a greater patch", "1.0.2", "1.0.1", 1},
		{"prerelease vs release", "1.0.0-beta", "1.0.0", -1},
		{"v prefix on a", "v1.0.0", "1.0.0", 0},
		{"v prefix on b", "1.0.0", "v1.0.0", 0},
		{"both empty", "", "", 0},
		{"invalid a treated as empty", "not-a-version", "1.0.0", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, CompareVersions(tt.a, tt.b))
		})
	}
}
