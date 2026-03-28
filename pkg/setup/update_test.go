package setup

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	mockRelease "github.com/phpboyscout/go-tool-base/mocks/pkg/vcs/release"
	"github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/vcs/release"
	"github.com/phpboyscout/go-tool-base/pkg/version"
)

func createTestRelease(t *testing.T, tagName, body string, draft bool) release.Release {
	rel := mockRelease.NewMockRelease(t)
	rel.EXPECT().GetTagName().Return(tagName).Maybe()
	rel.EXPECT().GetBody().Return(body).Maybe()
	rel.EXPECT().GetDraft().Return(draft).Maybe()
	return rel
}

func TestGetReleaseNotes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		from          string
		to            string
		releases      []release.Release
		expectedNotes []string
	}{
		{
			name: "successful range with multiple releases",
			from: "v1.0.0",
			to:   "v1.2.0",
			releases: []release.Release{
				createTestRelease(t, "v1.3.0", "Future release notes", false),
				createTestRelease(t, "v1.2.0", "Release 1.2.0 notes", false),
				createTestRelease(t, "v1.1.0", "Release 1.1.0 notes", false),
				createTestRelease(t, "v1.0.0", "Release 1.0.0 notes", false),
				createTestRelease(t, "v0.9.0", "Old release notes", false),
			},
			expectedNotes: []string{
				"# v1.2.0\nRelease 1.2.0 notes",
				"# v1.1.0\nRelease 1.1.0 notes",
			},
		},
		{
			name: "skip draft releases",
			from: "v1.0.0",
			to:   "v1.2.0",
			releases: []release.Release{
				createTestRelease(t, "v1.2.0", "Release 1.2.0 notes", false),
				createTestRelease(t, "v1.1.5", "Draft release notes", true), // This should be skipped
				createTestRelease(t, "v1.1.0", "Release 1.1.0 notes", false),
				createTestRelease(t, "v1.0.0", "Release 1.0.0 notes", false),
			},
			expectedNotes: []string{
				"# v1.2.0\nRelease 1.2.0 notes",
				"# v1.1.0\nRelease 1.1.0 notes",
			},
		},
		{
			name: "no releases in range",
			from: "v2.0.0",
			to:   "v2.1.0",
			releases: []release.Release{
				createTestRelease(t, "v1.2.0", "Release 1.2.0 notes", false),
				createTestRelease(t, "v1.1.0", "Release 1.1.0 notes", false),
				createTestRelease(t, "v1.0.0", "Release 1.0.0 notes", false),
			},
			expectedNotes: nil,
		},
		{
			name: "single release at to version",
			from: "v1.0.0",
			to:   "v1.1.0",
			releases: []release.Release{
				createTestRelease(t, "v1.2.0", "Release 1.2.0 notes", false),
				createTestRelease(t, "v1.1.0", "Release 1.1.0 notes", false),
				createTestRelease(t, "v1.0.0", "Release 1.0.0 notes", false),
			},
			expectedNotes: []string{
				"# v1.1.0\nRelease 1.1.0 notes",
			},
		},
		{
			name: "all releases are drafts",
			from: "v1.0.0",
			to:   "v1.2.0",
			releases: []release.Release{
				createTestRelease(t, "v1.2.0", "Draft 1.2.0 notes", true),
				createTestRelease(t, "v1.1.0", "Draft 1.1.0 notes", true),
				createTestRelease(t, "v1.0.0", "Draft 1.0.0 notes", true),
			},
			expectedNotes: nil,
		},
		{
			name: "stop at 'to' version even with more releases",
			from: "v1.0.0",
			to:   "v1.1.0",
			releases: []release.Release{
				createTestRelease(t, "v1.3.0", "Future release notes", false),
				createTestRelease(t, "v1.2.0", "Should not be included", false),
				createTestRelease(t, "v1.1.0", "Release 1.1.0 notes", false),
				createTestRelease(t, "v1.0.0", "Release 1.0.0 notes", false),
				createTestRelease(t, "v0.9.0", "Old release notes", false),
			},
			expectedNotes: []string{
				"# v1.1.0\nRelease 1.1.0 notes",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			updater := &SelfUpdater{}
			result := updater.filterReleaseNotes(tt.releases, tt.from, tt.to)

			assert.Equal(t, tt.expectedNotes, result)
		})
	}
}

func TestFormatVersionString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		version      string
		prefixWanted bool
		expected     string
	}{
		{
			name:         "add prefix to version without v",
			version:      "1.0.0",
			prefixWanted: true,
			expected:     "v1.0.0",
		},
		{
			name:         "keep prefix when wanted",
			version:      "v1.0.0",
			prefixWanted: true,
			expected:     "v1.0.0",
		},
		{
			name:         "remove prefix when not wanted",
			version:      "v1.0.0",
			prefixWanted: false,
			expected:     "1.0.0",
		},
		{
			name:         "no prefix to remove when not wanted",
			version:      "1.0.0",
			prefixWanted: false,
			expected:     "1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := version.FormatVersionString(tt.version, tt.prefixWanted)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCompareVersions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		v1       string
		v2       string
		expected int
	}{
		{
			name:     "v1 less than v2",
			v1:       "1.0.0",
			v2:       "1.1.0",
			expected: -1,
		},
		{
			name:     "v1 equal to v2",
			v1:       "v1.0.0",
			v2:       "1.0.0",
			expected: 0,
		},
		{
			name:     "v1 greater than v2",
			v1:       "v1.1.0",
			v2:       "1.0.0",
			expected: 1,
		},
		{
			name:     "major version difference",
			v1:       "2.0.0",
			v2:       "1.9.9",
			expected: 1,
		},
		{
			name:     "patch version difference",
			v1:       "1.0.1",
			v2:       "1.0.2",
			expected: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := version.CompareVersions(tt.v1, tt.v2)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestShouldIncludeRelease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		releaseVersion string
		fromVersion    string
		toVersion      string
		shouldInclude  bool
	}{
		{
			name:           "version in range",
			releaseVersion: "v1.1.0",
			fromVersion:    "v1.0.0",
			toVersion:      "v1.2.0",
			shouldInclude:  true,
		},
		{
			name:           "version at lower bound - excluded",
			releaseVersion: "v1.0.0",
			fromVersion:    "v1.0.0",
			toVersion:      "v1.2.0",
			shouldInclude:  false, // Actual logic excludes 'from' version (exclusive)
		},
		{
			name:           "version at upper bound",
			releaseVersion: "v1.2.0",
			fromVersion:    "v1.0.0",
			toVersion:      "v1.2.0",
			shouldInclude:  true,
		},
		{
			name:           "version below range",
			releaseVersion: "v0.9.0",
			fromVersion:    "v1.0.0",
			toVersion:      "v1.2.0",
			shouldInclude:  false,
		},
		{
			name:           "version above range",
			releaseVersion: "v2.0.0",
			fromVersion:    "v1.0.0",
			toVersion:      "v1.2.0",
			shouldInclude:  false,
		},
		{
			name:           "version just above from",
			releaseVersion: "v1.0.1",
			fromVersion:    "v1.0.0",
			toVersion:      "v1.2.0",
			shouldInclude:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Now we can call the actual private function directly
			result := shouldIncludeRelease(tt.fromVersion, tt.toVersion, tt.releaseVersion)
			assert.Equal(t, tt.shouldInclude, result, "Version %s should be in range (%s, %s]", tt.releaseVersion, tt.fromVersion, tt.toVersion)
		})
	}
}

func TestFindReleaseAsset(t *testing.T) {
	t.Parallel()

	// Determine current platform's expected asset name
	c := cases.Title(language.Und)
	currentOS := c.String(runtime.GOOS)
	currentArch := runtime.GOARCH
	if currentArch == "amd64" {
		currentArch = "x86_64"
	}

	tests := []struct {
		name        string
		toolName    string
		assets      []release.ReleaseAsset
		expectFound bool
		expectError bool
	}{
		{
			name:     "find exact match for current platform",
			toolName: "mytool",
			assets: []release.ReleaseAsset{
				func() release.ReleaseAsset {
					a := mockRelease.NewMockReleaseAsset(t)
					a.EXPECT().GetName().Return("mytool_Darwin_x86_64.tar.gz").Maybe()
					return a
				}(),
				func() release.ReleaseAsset {
					a := mockRelease.NewMockReleaseAsset(t)
					a.EXPECT().GetName().Return(fmt.Sprintf("mytool_%s_%s.tar.gz", currentOS, currentArch)).Maybe()
					return a
				}(),
				func() release.ReleaseAsset {
					a := mockRelease.NewMockReleaseAsset(t)
					a.EXPECT().GetName().Return("mytool_Windows_x86_64.tar.gz").Maybe()
					return a
				}(),
			},
			expectFound: true,
			expectError: false,
		},
		{
			name:     "no matching asset for current platform",
			toolName: "mytool",
			assets: []release.ReleaseAsset{
				func() release.ReleaseAsset {
					a := mockRelease.NewMockReleaseAsset(t)
					a.EXPECT().GetName().Return("mytool_SomeOtherOS_x86_64.tar.gz").Maybe()
					return a
				}(),
				func() release.ReleaseAsset {
					a := mockRelease.NewMockReleaseAsset(t)
					a.EXPECT().GetName().Return("mytool_AnotherOS_arm64.tar.gz").Maybe()
					return a
				}(),
			},
			expectFound: false,
			expectError: true,
		},
		{
			name:        "empty assets list",
			toolName:    "mytool",
			assets:      []release.ReleaseAsset{},
			expectFound: false,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create a release with the test assets
			rel := mockRelease.NewMockRelease(t)
			rel.EXPECT().GetAssets().Return(tt.assets).Maybe()

			// Create a minimal SelfUpdater
			updater := &SelfUpdater{
				Tool: props.Tool{
					Name: tt.toolName,
				},
			}

			asset, err := updater.findReleaseAsset(rel)

			if tt.expectError {
				require.Error(t, err)
				assert.Nil(t, asset)
			} else {
				assert.NoError(t, err)
				if tt.expectFound {
					assert.NotNil(t, asset)
					expectedName := fmt.Sprintf("%s_%s_%s.tar.gz", tt.toolName, currentOS, currentArch)
					assert.Equal(t, expectedName, asset.GetName())
				}
			}
		})
	}
}

func TestFilterReleaseNotes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		from          string
		to            string
		releases      []release.Release
		expectedCount int
		expectedTags  []string
	}{
		{
			name: "filter releases in range",
			from: "v1.0.0",
			to:   "v1.2.0",
			releases: []release.Release{
				createTestRelease(t, "v1.3.0", "Future release", false),
				createTestRelease(t, "v1.2.0", "To version", false),
				createTestRelease(t, "v1.1.0", "Middle version", false),
				createTestRelease(t, "v1.0.0", "From version", false),
				createTestRelease(t, "v0.9.0", "Old version", false),
			},
			expectedCount: 2, // v1.1.0 and v1.2.0 (exclusive of from, inclusive of to)
			expectedTags:  []string{"v1.1.0", "v1.2.0"},
		},
		{
			name: "skip draft releases",
			from: "v1.0.0",
			to:   "v1.2.0",
			releases: []release.Release{
				createTestRelease(t, "v1.2.0", "To version", false),
				createTestRelease(t, "v1.1.5", "Draft", true), // Should be skipped
				createTestRelease(t, "v1.1.0", "Middle version", false),
			},
			expectedCount: 2, // v1.1.0 and v1.2.0
			expectedTags:  []string{"v1.1.0", "v1.2.0"},
		},
		{
			name: "no releases in range",
			from: "v2.0.0",
			to:   "v2.1.0",
			releases: []release.Release{
				createTestRelease(t, "v1.2.0", "Old", false),
				createTestRelease(t, "v1.1.0", "Older", false),
			},
			expectedCount: 0,
			expectedTags:  []string{},
		},
		{
			name: "single release equals to version",
			from: "v1.0.0",
			to:   "v1.1.0",
			releases: []release.Release{
				createTestRelease(t, "v1.2.0", "Future", false),
				createTestRelease(t, "v1.1.0", "Target", false),
				createTestRelease(t, "v1.0.0", "From", false),
			},
			expectedCount: 1, // Only v1.1.0
			expectedTags:  []string{"v1.1.0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			updater := &SelfUpdater{}
			notes := updater.filterReleaseNotes(tt.releases, tt.from, tt.to)

			assert.Len(t, notes, tt.expectedCount)

			// Check that the expected tags are present in the notes
			for _, expectedTag := range tt.expectedTags {
				found := false
				for _, note := range notes {
					if len(note) > 0 && note[0:2] == "# " && len(note) >= len(expectedTag)+2 {
						// Extract tag from note (format is "# <tag>\n<body>")
						tagInNote := note[2 : 2+len(expectedTag)]
						if tagInNote == expectedTag {
							found = true
							break
						}
					}
				}
				assert.True(t, found, "Expected tag %s not found in release notes", expectedTag)
			}
		})
	}
}
