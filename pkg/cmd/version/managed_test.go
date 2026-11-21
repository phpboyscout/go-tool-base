package version

import (
	"bytes"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errorhandling"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	ver "gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// Spec 0217 D3: a managed install's version makes no probe and gives no
// reminder; --check is an explicit question, so it still asks, and both say
// who manages the install.
func TestNewCmdVersion_ManagedInstall(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv(setup.InstallManagerEnvVariable, "mise")
	t.Setenv(setup.InstallUpgradeEnvVariable, "mise upgrade test-tool")

	run := func(t *testing.T, args ...string) (string, interface{ Contains(string) bool }) {
		t.Helper()

		l := logger.NewBuffer()
		props := &p.Props{
			Tool: p.Tool{Name: "test-tool", ReleaseSource: p.ReleaseSource{Type: "github", Owner: "owner", Repo: "repo"},
				ReleaseProvider: releaseProvider("v9.0.0")},
			Logger: l, FS: afero.NewMemMapFs(), Config: testutil.StoreFromYAML(t, testConfig),
			Version:      ver.NewInfo("v1.0.0", "", ""),
			ErrorHandler: errorhandling.New(logger.ToSlog(l), nil),
		}

		var out bytes.Buffer

		cmd := NewCmdVersion(props)
		cmd.Flags().String("output", "json", "")
		cmd.SetOut(&out)
		cmd.SetArgs(args)
		require.NoError(t, cmd.Execute())

		return out.String(), l
	}

	out, l := run(t)
	assert.NotContains(t, out, `"latest"`, "no release-source call was made")
	assert.False(t, l.Contains("a new version is available"))
	assert.Contains(t, out, `"managed_by": "mise"`)
	assert.Contains(t, out, `"upgrade": "mise upgrade test-tool"`)

	out, _ = run(t, "--check")
	assert.Contains(t, out, `"latest": "v9.0.0"`, "--check still asks")
	assert.Contains(t, out, `"managed_by": "mise"`)
}

func TestPrintVersionText_Managed(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	printVersionText(&out, &VersionInfo{Version: "v1.0.0", Latest: "v9.0.0", ManagedBy: "mise", Upgrade: "mise upgrade test-tool"})
	assert.Contains(t, out.String(), "Latest:  v9.0.0 (update available)")
	assert.Contains(t, out.String(), "Managed: mise (upgrade with: mise upgrade test-tool)")

	out.Reset()
	printVersionText(&out, &VersionInfo{Version: "v1.0.0", Current: true, ManagedBy: "mise"})
	assert.Contains(t, out.String(), "Managed: mise\n")
}
