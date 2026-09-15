package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

// The flag path for spec 0195 step 1: the backend, module path and release
// channel are validated as a set, and reach SkeletonConfig as recorded.
func TestSkeletonOptions_ForgeFlags(t *testing.T) {
	t.Parallel()

	t.Run("an unknown backend is refused", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Name: "tool", Repo: "org/tool", ForgeBackend: "sourcehut", Features: generator.DefaultSelectedFeatures}
		require.ErrorIs(t, o.validateFields(), generator.ErrInvalidForgeBackend)
	})

	t.Run("not hosted needs a module path", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Name: "tool", NoForge: true, Features: []string{"docs"}}
		require.ErrorIs(t, o.validateFields(), ErrModuleRequired)

		o.Module = "myapp"
		require.NoError(t, o.validateFields())
		assert.Equal(t, "myapp", o.skeletonConfig(nil).ModulePath)
		assert.Empty(t, o.skeletonConfig(nil).ForgeBackend)
	})

	t.Run("update without a channel is refused when not hosted", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Name: "tool", NoForge: true, Module: "myapp", Features: []string{"update"}}
		require.ErrorIs(t, o.validateFields(), ErrReleaseChannelRequired)

		o.ReleaseChannel = generator.ReleaseChannelDirect
		require.ErrorIs(t, o.validateFields(), ErrDirectSourceIncomplete, "direct needs a URL template and a version URL")

		o.Direct.URLTemplate = "https://dl.example.com/{{.Version}}/{{.Asset}}"
		o.Direct.VersionURL = "https://dl.example.com/latest"
		require.NoError(t, o.validateFields())
		assert.Equal(t, generator.ReleaseChannelDirect, o.skeletonConfig(nil).ReleaseChannel)
	})

	t.Run("hosted defaults to the forge channel and records the backend", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Name: "tool", Repo: "org/tool", ForgeBackend: "gitlab", Host: "code.example.com",
			Features: generator.DefaultSelectedFeatures, ForgeCredentials: []string{"github"}}
		require.NoError(t, o.validateFields())

		cfg := o.skeletonConfig(nil)
		assert.Equal(t, forge.GitlabFeature, cfg.ForgeBackend)
		assert.Equal(t, "code.example.com", cfg.Host)
		assert.Equal(t, generator.ReleaseChannelForge, cfg.ReleaseChannel)
		assert.Equal(t, []props.FeatureID{forge.GithubFeature}, cfg.ForgeCredentials)
	})
}
