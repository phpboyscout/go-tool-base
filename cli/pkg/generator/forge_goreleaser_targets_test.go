package generator

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

// Each forge's release config names the API goreleaser releases through
// there, and a forge goreleaser cannot release to gets none (#213).
func TestGenerateSkeleton_ReleaseConfigMatchesTheForge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		backend props.FeatureID
		host    string
		want    []string
		absent  []string
	}{
		{
			backend: forge.GiteaFeature, host: "gitea.com",
			want:   []string{"force_token: gitea", "gitea_urls:", "api: https://gitea.com/api/v1/", "download: https://gitea.com\n", "mode: keep-existing"},
			absent: []string{"gitlab_urls:", "github_urls:"},
		},
		{
			backend: forge.CodebergFeature, host: "codeberg.org",
			want:   []string{"force_token: gitea", "gitea_urls:", "api: https://codeberg.org/api/v1/", "download: https://codeberg.org\n"},
			absent: []string{"force_token: codeberg", "gitlab_urls:", "github_urls:"},
		},
		{
			backend: forge.BitbucketFeature, host: "bitbucket.org",
			want:   []string{"disable: true"},
			absent: []string{"force_token", "gitlab_urls:", "github_urls:", "gitea_urls:", "mode: keep-existing"},
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.backend), func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			g := newSkeletonGeneratorForTest(t, fs)

			cfg := SkeletonConfig{
				Name: "fgtool", Repo: "acme/fgtool", Host: tt.host, ForgeBackend: tt.backend,
				Description: "forge scaffold", Path: "/work",
				Features:       []ManifestFeature{{Name: "changelog", Enabled: false}, {Name: "docs", Enabled: false}, {Name: string(tt.backend), Enabled: true}},
				ReleaseChannel: ReleaseChannelForge,
			}
			require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

			gr := readGenerated(t, fs, "/work/.goreleaser.yaml")
			for _, w := range tt.want {
				assert.Contains(t, gr, w)
			}

			for _, a := range tt.absent {
				assert.NotContains(t, gr, a)
			}
		})
	}
}

func TestManifestWarnings_AForgeGoreleaserCannotReleaseTo(t *testing.T) {
	t.Parallel()

	m := &Manifest{}
	m.ReleaseSource.Type = string(forge.BitbucketFeature)
	assert.Contains(t, ManifestWarnings(m), `release_source.type is "bitbucket": goreleaser cannot publish a release there, so the release config disables it; set release_source.type to static with release_source.static.base_url`)

	for _, typ := range []string{"github", "gitlab", "gitea", "codeberg", props.ReleaseSourceStatic, ""} {
		m.ReleaseSource.Type = typ
		assert.Empty(t, ManifestWarnings(m), typ)
	}
}
