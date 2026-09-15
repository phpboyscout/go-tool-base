package version

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	ver "gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// TestVersion_DirtyBuildOfTheLatestReleaseIsCurrent pins that a local build
// stamped v1.0.0+dirty is not reported as behind v1.0.0: build metadata has
// no precedence in semver, so the comparison must not be a string equality.
func TestVersion_DirtyBuildOfTheLatestReleaseIsCurrent(t *testing.T) {
	t.Parallel()

	props := newTestProps(t, releaseProvider("v1.0.0"))
	props.Version = ver.NewInfo("v1.0.0+dirty", "", "")

	out, err := runVersionCmd(t, props, "json", "--check")
	require.NoError(t, err)

	var resp struct {
		Data VersionInfo `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &resp))
	require.Equal(t, "v1.0.0", resp.Data.Latest)
	require.True(t, resp.Data.Current, "a dirty build of the latest release is current, not behind")
	require.Zero(t, warningCount(t, props, "new version is available"))
}
