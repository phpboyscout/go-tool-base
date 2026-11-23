package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

func TestDefaultHost(t *testing.T) {
	t.Parallel()

	for backend, want := range map[props.FeatureID]string{
		"":                    "github.com",
		forge.GithubFeature:   "github.com",
		forge.GitlabFeature:   "gitlab.com",
		forge.GiteaFeature:    "",
		forge.CodebergFeature: "codeberg.org",
		"not-a-forge-at-all":  "",
	} {
		assert.Equal(t, want, DefaultHost(backend), "backend %q", backend)
	}
}

// #217: a Gitea project with no host was generated against github.com.
func TestGenerateSkeleton_AHostlessForgeWithNoHostIsRefused(t *testing.T) {
	t.Parallel()

	g, _ := newPureGenerator(t, &Config{Path: "/work"})

	err := g.generateSkeletonFiles(SkeletonConfig{
		Name: "tool", Repo: "org/tool", ForgeBackend: forge.GiteaFeature, ReleaseChannel: ReleaseChannelForge,
	})

	require.ErrorIs(t, err, ErrHostRequired)
	assert.Contains(t, errors.FlattenHints(err), "--host")
	assert.Contains(t, err.Error(), "forge Gitea", "named as the CLI names it")
}
