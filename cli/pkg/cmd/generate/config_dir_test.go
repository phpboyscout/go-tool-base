package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
)

func TestSkeletonOptions_ConfigDirIsValidatedAndNormalised(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Name: "tool", Repo: "org/tool", ForgeBackend: "github", Features: generator.DefaultSelectedFeatures}
	o.ConfigDir = "/etc/tool"
	require.Error(t, o.validateFields())

	o.ConfigDir = "xdg"
	require.NoError(t, o.validateFields())
	assert.Equal(t, "xdg", o.skeletonConfig(nil).ConfigDir)

	o.ConfigDir = "home"
	require.NoError(t, o.afterWizard())
	assert.Empty(t, o.ConfigDir)
}

// The wizard asks no question about the directory, so a revisit must carry it.
func TestOptionsFromManifest_KeepsTheConfigDir(t *testing.T) {
	t.Parallel()

	start := &SkeletonOptions{Name: "tool", Repo: "org/tool", ForgeBackend: "github",
		Features: generator.DefaultSelectedFeatures, ConfigDir: "xdg"}
	require.NoError(t, start.validateFields())

	m := generator.ManifestFromSkeletonConfig(start.skeletonConfig(nil), nil, "v1.0.0")
	require.Equal(t, "xdg", m.Properties.Config.Dir)

	revisited := optionsFromManifest(m)
	require.Equal(t, "xdg", revisited.ConfigDir)

	m.Properties.Config.Dir = ""
	require.NoError(t, generator.ApplyAuthorSettingsTo(&m, revisited.skeletonConfig(nil)))
	assert.Equal(t, "xdg", m.Properties.Config.Dir)
}

func TestWizard_NamesTheUserFileInTheChosenDirectory(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "~/.mytool/config.yaml", (&SkeletonOptions{}).userConfigFile("MyTool"))
	assert.Equal(t, "~/.config/mytool/config.toml", (&SkeletonOptions{ConfigDir: "xdg", ConfigFormat: "toml"}).userConfigFile("MyTool"))
	assert.Contains(t, shownNote(t, (&SkeletonOptions{Name: "my-app", Repo: "org/my-app", ConfigDir: "xdg"}).wizardSummary()), "xdg")
}
