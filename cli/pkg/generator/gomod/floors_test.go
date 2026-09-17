package gomod

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// TestFloors_AreValid holds every declared floor to a module path and a
// canonical semver, so a typo in the table is caught here rather than by the
// first project that regenerates against it.
func TestFloors_AreValid(t *testing.T) {
	t.Parallel()

	for path, version := range Floors {
		require.NoError(t, module.CheckPath(path), path)
		assert.Equal(t, version, semver.Canonical(version), "%s: not canonical semver", path)
	}
}

// TestResolveVersion_PrefersFloorThenBuildInfoThenLatest pins D4's order.
func TestResolveVersion_PrefersFloorThenBuildInfoThenLatest(t *testing.T) {
	t.Parallel()

	src := MapSource{"gitlab.com/phpboyscout/go/forge-gitlab": "v0.21.0"}
	floors := map[string]string{"gitlab.com/phpboyscout/go/forge-gitlab": "v0.23.0", "gitlab.com/phpboyscout/go/chat-gemini": "v0.15.0"}

	got := Requirements([]string{
		"gitlab.com/phpboyscout/go/forge-gitlab",
		"gitlab.com/phpboyscout/go/chat-gemini",
		"gitlab.com/phpboyscout/go/forge-github",
	}, src, floors)

	assert.Equal(t, []Requirement{
		{Path: "gitlab.com/phpboyscout/go/forge-gitlab", Version: "v0.23.0", Floor: true},
		{Path: "gitlab.com/phpboyscout/go/chat-gemini", Version: "v0.15.0", Floor: true},
		{Path: "gitlab.com/phpboyscout/go/forge-github", Version: "latest"},
	}, got)
}

// TestBuildInfoSource_ReadsTheTestBinary: the test binary links x/mod, so
// the source names it; the module under test is (devel) and is not named.
func TestBuildInfoSource_ReadsTheTestBinary(t *testing.T) {
	t.Parallel()

	src := BuildInfoSource()

	v, ok := src.Version("golang.org/x/mod")
	assert.True(t, ok)
	assert.Equal(t, v, semver.Canonical(v))

	_, ok = src.Version("gitlab.com/phpboyscout/go-tool-base/cli")
	assert.False(t, ok, "the main module has no version to seed")
}
