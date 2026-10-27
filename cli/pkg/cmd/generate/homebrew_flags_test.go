package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
)

// Spec 0212 D2, D4, D7: --homebrew-tap is accepted on a GitLab or GitHub
// project whose releases Homebrew can download, and reaches the manifest.
func TestSkeletonOptions_HomebrewTap(t *testing.T) {
	t.Parallel()

	t.Run("a GitLab project records its tap", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Name: "tool", Repo: "org/tool", ForgeBackend: "gitlab", Features: []string{"docs"}, HomebrewTap: "org/homebrew"}
		require.NoError(t, o.validateFields())
		assert.Equal(t, "org/homebrew", o.skeletonConfig(nil).Homebrew.Tap)
	})

	t.Run("a project that is not hosted is refused", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Name: "tool", NoForge: true, Module: "myapp", Features: []string{"docs"}, HomebrewTap: "org/homebrew"}
		require.Error(t, o.validateFields())
	})

	t.Run("a private project needs the static channel", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Name: "tool", Repo: "org/tool", ForgeBackend: "github", Private: true, Features: []string{"update"}, HomebrewTap: "org/homebrew-tap"}
		require.Error(t, o.validateFields())

		o.ReleaseChannel = generator.ReleaseChannelStatic
		o.ReleaseBaseURL = "https://pkg.acme.dev/tool"
		require.NoError(t, o.validateFields())
	})

	t.Run("the page shows only for a GitLab or GitHub project", func(t *testing.T) {
		t.Parallel()

		assert.False(t, (&SkeletonOptions{ForgeBackend: "gitlab"}).homebrewHidden())
		assert.False(t, (&SkeletonOptions{ForgeBackend: "github"}).homebrewHidden())
		assert.True(t, (&SkeletonOptions{ForgeBackend: "gitea"}).homebrewHidden())
		assert.True(t, (&SkeletonOptions{ForgeBackend: "gitlab", NoForge: true}).homebrewHidden())
	})

	t.Run("the flag exists", func(t *testing.T) {
		t.Parallel()

		assert.NotNil(t, NewCmdSkeleton(nobodyTyping(), &SharedFlags{}).Flags().Lookup("homebrew-tap"))
	})

	t.Run("revisiting a project keeps its tap", func(t *testing.T) {
		t.Parallel()

		m := generator.Manifest{Properties: generator.ManifestProperties{Name: "tool", Homebrew: generator.ManifestHomebrew{Tap: "org/homebrew"}}}
		assert.Equal(t, "org/homebrew", optionsFromManifest(m).HomebrewTap)
	})
}
