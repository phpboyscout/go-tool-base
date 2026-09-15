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

// The backend and the credential forges become enabled features (spec 0195
// D1, D6); a not-hosted project enables none.
func TestResolveFeatures_DerivesForgesFromTheBackend(t *testing.T) {
	t.Parallel()

	enabled := func(fs []generator.ManifestFeature) []string {
		var names []string
		for _, f := range fs {
			if f.Enabled {
				names = append(names, f.Name)
			}
		}

		return names
	}

	hosted := &SkeletonOptions{Features: []string{"update", "docs"}, ForgeBackend: "gitlab", ForgeCredentials: []string{"github"}}
	assert.ElementsMatch(t, []string{"update", "docs", "gitlab", "github"}, enabled(hosted.resolveFeatures()))

	bare := &SkeletonOptions{Features: []string{"docs"}, NoForge: true}
	assert.ElementsMatch(t, []string{"docs"}, enabled(bare.resolveFeatures()))

	// A forge name in --features is refused before anything is written.
	stale := &SkeletonOptions{Name: "tool", Repo: "org/tool", ForgeBackend: "github", Features: []string{"update", "github"}}
	require.Error(t, stale.validateFields())
}

// #49: closed sets are enforced at the flag, and a flag whose intent cannot
// be honoured is refused rather than dropped.
func TestSkeletonOptions_ClosedSetsAndCompanions(t *testing.T) {
	t.Parallel()

	base := func() *SkeletonOptions {
		return &SkeletonOptions{Name: "tool", Repo: "org/tool", ForgeBackend: "github", Features: generator.DefaultSelectedFeatures}
	}

	t.Run("an unknown help type is refused", func(t *testing.T) {
		t.Parallel()

		o := base()
		o.HelpType = "bogus"
		require.Error(t, o.validateFields())
	})

	t.Run("slack without a channel is refused", func(t *testing.T) {
		t.Parallel()

		o := base()
		o.HelpType = "slack"
		require.ErrorIs(t, o.validateFields(), ErrHelpChannelRequired)

		o.SlackChannel = "#help"
		require.NoError(t, o.validateFields())
	})

	t.Run("teams without a channel is refused", func(t *testing.T) {
		t.Parallel()

		o := base()
		o.HelpType = "teams"
		require.ErrorIs(t, o.validateFields(), ErrHelpChannelRequired)
	})

	t.Run("a signing key id without signing is refused, not dropped", func(t *testing.T) {
		t.Parallel()

		o := base()
		o.SigningKeyID = "alias/release"
		require.ErrorIs(t, o.validateFields(), ErrSigningKeyWithoutSigning)

		o.Signing = true
		require.NoError(t, o.validateFields())
		assert.Equal(t, "alias/release", o.resolveSigning().KeyID)
	})
}
