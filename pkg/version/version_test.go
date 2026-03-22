package version

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewInfo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		version, commit string
		date            string
		wantVersion     string
	}{
		{"adds v prefix", "1.2.3", "abc", "2026-01-01", "v1.2.3"},
		{"preserves v prefix", "v1.2.3", "abc", "2026-01-01", "v1.2.3"},
		{"empty version", "", "", "", ""},
		{"prerelease", "1.0.0-beta.1", "def", "2026-02-01", "v1.0.0-beta.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			info := NewInfo(tt.version, tt.commit, tt.date)
			assert.Equal(t, tt.wantVersion, info.GetVersion())
			assert.Equal(t, tt.commit, info.GetCommit())
			assert.Equal(t, tt.date, info.GetDate())
		})
	}
}

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

func TestInfo_Compare(t *testing.T) {
	t.Parallel()

	info := NewInfo("1.2.3", "", "")
	assert.Equal(t, 0, info.Compare("v1.2.3"))
	assert.Equal(t, 1, info.Compare("v1.2.2"))
	assert.Equal(t, -1, info.Compare("v1.2.4"))
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

// Compile-time interface satisfaction check.
var _ Version = Info{}
