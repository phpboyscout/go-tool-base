package changelog

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatSummary_WithBreaking(t *testing.T) {
	t.Parallel()

	cl := &Changelog{
		Releases: []Release{
			{Version: "v2.0.0", Entries: []Entry{
				{Category: CategoryBreaking, Scope: "config", Description: "rename ConfigPath to ConfigDir"},
				{Category: CategoryFeature, Scope: "http", Description: "add TLS support"},
			}},
		},
	}

	output := FormatSummary(cl)
	assert.Contains(t, output, "WARNING: Breaking changes detected!")
	assert.Contains(t, output, "BREAKING: config: rename ConfigPath to ConfigDir")
	assert.Contains(t, output, "Features:")
	assert.Contains(t, output, "  - http: add TLS support")
}

func TestFormatSummary_NoBreaking(t *testing.T) {
	t.Parallel()

	cl := &Changelog{
		Releases: []Release{
			{Version: "v1.1.0", Entries: []Entry{
				{Category: CategoryFeature, Description: "new feature"},
				{Category: CategoryFix, Scope: "http", Description: "fix timeout"},
			}},
		},
	}

	output := FormatSummary(cl)
	assert.NotContains(t, output, "WARNING")
	assert.NotContains(t, output, "BREAKING")
	assert.Contains(t, output, "Features:")
	assert.Contains(t, output, "  - new feature")
	assert.Contains(t, output, "Bug Fixes:")
	assert.Contains(t, output, "  - http: fix timeout")
}

func TestFormatSummary_AllCategories(t *testing.T) {
	t.Parallel()

	cl := &Changelog{
		Releases: []Release{
			{Version: "v1.0.0", Entries: []Entry{
				{Category: CategoryBreaking, Description: "removed old API"},
				{Category: CategoryFeature, Description: "new API"},
				{Category: CategoryFix, Description: "bug fix"},
				{Category: CategoryPerformance, Description: "faster cache"},
				{Category: CategoryOther, Description: "updated deps"},
			}},
		},
	}

	output := FormatSummary(cl)
	assert.Contains(t, output, "WARNING: Breaking changes detected!")
	assert.Contains(t, output, "BREAKING: removed old API")
	assert.Contains(t, output, "Features:")
	assert.Contains(t, output, "Bug Fixes:")
	assert.Contains(t, output, "Performance:")
	assert.Contains(t, output, "Other:")
}

func TestFormatSummary_UnscopedBreaking(t *testing.T) {
	t.Parallel()

	cl := &Changelog{
		Releases: []Release{
			{Version: "v2.0.0", Entries: []Entry{
				{Category: CategoryBreaking, Description: "removed deprecated API"},
			}},
		},
	}

	output := FormatSummary(cl)
	assert.Contains(t, output, "BREAKING: removed deprecated API")
}

func TestFormatSummary_MultiReleaseFixture(t *testing.T) {
	t.Parallel()

	// Parse the test fixture and format it
	input := `# v1.3.0

### Features

* **http:** add middleware chaining

### Bug Fixes

* fix race condition

# v1.2.0

### BREAKING CHANGES

* **config:** rename ConfigPath to ConfigDir
`

	cl := Parse(input)
	output := FormatSummary(cl)

	assert.Contains(t, output, "WARNING: Breaking changes detected!")
	assert.Contains(t, output, "BREAKING: config: rename ConfigPath to ConfigDir")
	assert.Contains(t, output, "Features:")
	assert.Contains(t, output, "  - http: add middleware chaining")
	assert.Contains(t, output, "Bug Fixes:")
	assert.Contains(t, output, "  - fix race condition")
}
