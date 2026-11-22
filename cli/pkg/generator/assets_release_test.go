package generator

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

func gitlabReleaseConfig(channel string) SkeletonConfig {
	cfg := SkeletonConfig{
		Name: "astool", Repo: "acme/as-tool-repo", Host: "gitlab.com", ForgeBackend: forge.GitlabFeature,
		Description: "assets scaffold", Path: "/work",
		Features:       []ManifestFeature{{Name: "changelog", Enabled: false}, {Name: "docs", Enabled: false}, {Name: "gitlab", Enabled: true}},
		ReleaseChannel: channel,
	}
	if channel == ReleaseChannelStatic {
		cfg.ReleaseBaseURL = "https://pkg.acme.dev/acme/astool"
	}

	return cfg
}

// A GitLab project on the forge channel uploads to its generic package
// registry under the tool's name, not the repository's, and colophon links
// the same directory (spec 0219 D1, D2).
func TestGenerateSkeleton_GitLabForgeChannelTakesTheAssetsFlow(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := newSkeletonGeneratorForTest(t, fs)
	require.NoError(t, g.GenerateSkeleton(context.Background(), gitlabReleaseConfig(ReleaseChannelForge)))

	gr := readGenerated(t, fs, "/work/.goreleaser.yaml")
	assert.Contains(t, gr, "so goreleaser creates none.\n  disable: true")
	assert.NotContains(t, gr, "keep-existing")
	assert.NotContains(t, gr, "make_latest")
	assert.Contains(t, gr, "uploads:")
	assert.Contains(t, gr, `target: "{{ .Env.CI_API_V4_URL }}/projects/{{ .Env.CI_PROJECT_ID }}/packages/generic/astool/{{ .Version }}/{{ .ArtifactName }}"`)
	assert.Contains(t, gr, "custom_artifact_name: true", "the target names the file; goreleaser would append it again")
	assert.Contains(t, gr, "mode: archive")
	assert.Contains(t, gr, "checksum: true")
	assert.Contains(t, gr, "signature: true")
	assert.Contains(t, gr, `JOB-TOKEN: "{{ .Env.CI_JOB_TOKEN }}"`)

	ci := readGenerated(t, fs, "/work/.gitlab-ci.yml")
	assert.Contains(t, ci, "assets: true")
	assert.Contains(t, ci, `asset_base: "$CI_API_V4_URL/projects/$CI_PROJECT_ID/packages/generic/astool/$COLOPHON_VERSION"`)
	assert.NotContains(t, ci, "and creates the release when it merges")
	assert.Contains(t, ci, "The package registry enabled")
}

// A GitLab project on the static channel already uploads through blobs:, so
// it gains no uploads: entry, and colophon links the tag's directory in the
// store (spec 0219 D3).
func TestGenerateSkeleton_GitLabStaticChannelTakesTheAssetsFlow(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := newSkeletonGeneratorForTest(t, fs)
	require.NoError(t, g.GenerateSkeleton(context.Background(), gitlabReleaseConfig(ReleaseChannelStatic)))

	gr := readGenerated(t, fs, "/work/.goreleaser.yaml")
	assert.Contains(t, gr, "so goreleaser creates none.\n  disable: true")
	assert.NotContains(t, gr, "keep-existing")
	assert.NotContains(t, gr, "uploads:")
	assert.Contains(t, gr, `directory: "acme/astool/{{ .Tag }}"`)

	ci := readGenerated(t, fs, "/work/.gitlab-ci.yml")
	assert.Contains(t, ci, "assets: true")
	assert.Contains(t, ci, `asset_base: "https://pkg.acme.dev/acme/astool/$CI_COMMIT_TAG"`)
	assert.NotContains(t, ci, "The package registry enabled")
}

// GitHub keeps today's flow until colophon can attach files there (spec 0219,
// Not yet specified).
func TestGenerateSkeleton_GitHubKeepsTheAttachFlow(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := newSkeletonGeneratorForTest(t, fs)

	cfg := gitlabReleaseConfig(ReleaseChannelForge)
	cfg.Host, cfg.ForgeBackend = "github.com", forge.GithubFeature
	cfg.Features = []ManifestFeature{{Name: "changelog", Enabled: false}, {Name: "docs", Enabled: false}, {Name: "github", Enabled: true}}
	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

	gr := readGenerated(t, fs, "/work/.goreleaser.yaml")
	assert.Contains(t, gr, "mode: keep-existing")
	assert.NotContains(t, gr, "uploads:")
	assert.NotContains(t, gr, "goreleaser creates none")
}

// The upload target and colophon's asset base come from one method, so they
// cannot name different directories.
func TestSkeletonTemplateData_UploadTargetSitsUnderTheAssetBase(t *testing.T) {
	t.Parallel()

	d := skeletonTemplateData{Name: "astool", ForgeBackend: forge.GitlabFeature, Host: "gitlab.com"}
	assert.Equal(t,
		"{{ .Env.CI_API_V4_URL }}/projects/{{ .Env.CI_PROJECT_ID }}/packages/generic/astool/{{ .Version }}/{{ .ArtifactName }}",
		d.GenericPackageUploadTarget())
	assert.Equal(t,
		"$CI_API_V4_URL/projects/$CI_PROJECT_ID/packages/generic/astool/$COLOPHON_VERSION",
		d.ColophonAssetBase())
}

const preAssetsReleaseBlock = `release:
  use_existing_draft: true
  replace_existing_artifacts: true
  make_latest: true
  mode: keep-existing
`

// seedPreAssetsGoreleaser stands in for a project generated before spec 0219:
// its .goreleaser.yaml carries the old release block, and the manifest records
// that file's hash, so regenerate sees it as unedited.
func seedPreAssetsGoreleaser(t *testing.T, g *Generator, fs afero.Fs) string {
	t.Helper()

	old := "version: 2\nproject_name: \"widget\"\n" + preAssetsReleaseBlock
	require.NoError(t, afero.WriteFile(fs, settingsSkeletonRoot+"/.goreleaser.yaml", []byte(old), 0o644))

	m, err := g.loadManifest()
	require.NoError(t, err)
	m.Hashes[".goreleaser.yaml"] = calculateHash([]byte(old))
	require.NoError(t, g.writeManifest(m))

	return old
}

// An unedited release file from before the assets flow moves across on the
// next regenerate without asking, even under --overwrite deny (spec 0219 D5).
func TestRegenerateProject_MovesAnUneditedReleaseFileToTheAssetsFlow(t *testing.T) {
	t.Parallel()

	g, fs, _ := newSettingsSkeletonProject(t)
	g.config.Overwrite = OverwriteDeny
	seedPreAssetsGoreleaser(t, g, fs)

	require.NoError(t, g.RegenerateProject(context.Background()))

	gr := readGenerated(t, fs, settingsSkeletonRoot+"/.goreleaser.yaml")
	assert.NotContains(t, gr, "keep-existing")
	assert.Contains(t, gr, "packages/generic/widget/")
}

// An edited one is the developer's, and --overwrite deny keeps it.
func TestRegenerateProject_KeepsAnEditedReleaseFileUnderDeny(t *testing.T) {
	t.Parallel()

	g, fs, _ := newSettingsSkeletonProject(t)
	g.config.Overwrite = OverwriteDeny
	old := seedPreAssetsGoreleaser(t, g, fs)

	edited := old + "# hand-edited\n"
	require.NoError(t, afero.WriteFile(fs, settingsSkeletonRoot+"/.goreleaser.yaml", []byte(edited), 0o644))

	require.NoError(t, g.RegenerateProject(context.Background()))

	assert.Equal(t, edited, readGenerated(t, fs, settingsSkeletonRoot+"/.goreleaser.yaml"))
}
