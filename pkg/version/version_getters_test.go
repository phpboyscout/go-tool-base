package version_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

func TestInfo_Getters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		version    string
		commit     string
		date       string
		wantVer    string
		wantCommit string
		wantDate   string
	}{
		{
			name:       "fully populated",
			version:    "1.2.3",
			commit:     "abc123",
			date:       "2026-06-19",
			wantVer:    "v1.2.3",
			wantCommit: "abc123",
			wantDate:   "2026-06-19",
		},
		{
			name:       "already prefixed version",
			version:    "v2.0.0",
			commit:     "deadbeef",
			date:       "2026-01-01",
			wantVer:    "v2.0.0",
			wantCommit: "deadbeef",
			wantDate:   "2026-01-01",
		},
		{
			name:       "empty fields",
			version:    "",
			commit:     "",
			date:       "",
			wantVer:    "",
			wantCommit: "",
			wantDate:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			info := version.NewInfo(tt.version, tt.commit, tt.date)
			assert.Equal(t, tt.wantVer, info.GetVersion())
			assert.Equal(t, tt.wantCommit, info.GetCommit())
			assert.Equal(t, tt.wantDate, info.GetDate())
		})
	}
}

func TestInfo_Compare(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		other   string
		expect  int
	}{
		{"equal", "1.0.0", "1.0.0", 0},
		{"equal with prefix mismatch", "1.0.0", "v1.0.0", 0},
		{"receiver greater", "2.0.0", "1.0.0", 1},
		{"receiver lesser", "1.0.0", "1.1.0", -1},
		{"receiver lesser patch", "1.0.1", "1.0.2", -1},
		{"prerelease lower than release", "1.0.0-beta", "1.0.0", -1},
		{"both empty", "", "", 0},
		{"invalid other treated as empty", "1.0.0", "not-a-version", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			info := version.NewInfo(tt.version, "", "")
			assert.Equal(t, tt.expect, info.Compare(tt.other))
		})
	}
}
