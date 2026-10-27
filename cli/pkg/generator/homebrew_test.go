package generator

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

func homebrewConfig(backend props.FeatureID, host, tap string) SkeletonConfig {
	return SkeletonConfig{
		Name: "hbtool", Repo: "acme/hbtool", Host: host, ForgeBackend: backend,
		Description: "homebrew scaffold", Path: "/work",
		Features:       []ManifestFeature{{Name: "changelog", Enabled: false}, {Name: "docs", Enabled: false}, {Name: string(backend), Enabled: true}},
		ReleaseChannel: ReleaseChannelForge,
		Homebrew:       ManifestHomebrew{Tap: tap},
	}
}

// A GitLab project with a tap renders gtb's three pieces (spec 0212 D3): the
// cask GoReleaser only renders, the overlay that keeps it, and the job that
// pushes it with the job token through the scaffolded script.
func TestGenerateSkeleton_HomebrewTapOnGitLab(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := newSkeletonGeneratorForTest(t, fs)

	require.NoError(t, g.GenerateSkeleton(context.Background(), homebrewConfig(forge.GitlabFeature, "gitlab.com", "acme/tools/homebrew")))

	gr := readGenerated(t, fs, "/work/.goreleaser.yaml")
	assert.Contains(t, gr, "homebrew_casks:")
	assert.Contains(t, gr, "skip_upload: true")
	assert.Contains(t, gr, `owner: "acme/tools"`, "a subgroup is the owner")
	assert.Contains(t, gr, `name: "homebrew"`)
	assert.Contains(t, gr, `homepage: "https://gitlab.com/acme/hbtool"`)
	assert.Contains(t, gr, `description: "homebrew scaffold"`)
	assert.NotContains(t, gr, "url:\n", "a public forge release supplies GoReleaser's default URL")
	assert.NotContains(t, gr, "commit_author:", "GoReleaser makes no commit")
	assert.NotContains(t, gr, "HOMEBREW_TAP_TOKEN")
	assert.Contains(t, gr, "- id: hbtool")
	assert.Contains(t, gr, `enabled: '{{ isEnvSet "APPLE_DEV_CERT" }}'`)
	assert.Contains(t, gr, "        - hbtool\n", "notarize names the build")

	ci := readGenerated(t, fs, "/work/.gitlab-ci.yml")
	assert.Contains(t, ci, "homebrew-tap:")
	assert.Contains(t, ci, "bash scripts/push-homebrew-cask.sh 'hbtool' 'acme/tools/homebrew'")
	assert.Contains(t, ci, "- dist/artifacts.json")
	assert.Contains(t, ci, "- dist/**/Casks/hbtool.rb")
	assert.Contains(t, ci, `$CI_COMMIT_TAG =~ /^v[0-9]+\.[0-9]+\.[0-9]+$/`)
	assert.Contains(t, ci, "job token allowlist")
	assert.Contains(t, ci, "APPLE_DEV_CERT")

	script := readGenerated(t, fs, "/work/scripts/push-homebrew-cask.sh")
	assert.Contains(t, script, "CI_JOB_TOKEN")

	g.config.Path = "/work"
	m, err := g.loadManifest()
	require.NoError(t, err)
	assert.Equal(t, "acme/tools/homebrew", m.Properties.Homebrew.Tap)
}

// A GitHub project with a tap uses GoReleaser's own push with a tap-token
// secret (spec 0212 D3, D8): no script, and prereleases leave the cask alone.
func TestGenerateSkeleton_HomebrewTapOnGitHub(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := newSkeletonGeneratorForTest(t, fs)

	require.NoError(t, g.GenerateSkeleton(context.Background(), homebrewConfig(forge.GithubFeature, "github.com", "acme/homebrew-tap")))

	gr := readGenerated(t, fs, "/work/.goreleaser.yaml")
	assert.Contains(t, gr, "homebrew_casks:")
	assert.Contains(t, gr, "skip_upload: auto")
	assert.Contains(t, gr, `token: "{{ .Env.HOMEBREW_TAP_TOKEN }}"`)
	assert.Contains(t, gr, `owner: "acme"`)
	assert.Contains(t, gr, `name: "homebrew-tap"`)
	assert.Contains(t, gr, `homepage: "https://github.com/acme/hbtool"`)

	wf := readGenerated(t, fs, "/work/.github/workflows/goreleaser.yaml")
	assert.Contains(t, wf, "HOMEBREW_TAP_TOKEN: ${{ secrets.HOMEBREW_TAP_TOKEN }}")
	assert.Contains(t, wf, "APPLE_DEV_CERT: ${{ secrets.APPLE_DEV_CERT }}")

	exists, _ := afero.Exists(fs, "/work/scripts/push-homebrew-cask.sh")
	assert.False(t, exists, "GoReleaser pushes on GitHub")
}

// On the static channel the cask downloads from the channel's store, and the
// GitLab overlay is one job carrying both the store variables and the cask.
func TestGenerateSkeleton_HomebrewTapOnTheStaticChannel(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := newSkeletonGeneratorForTest(t, fs)

	cfg := homebrewConfig(forge.GitlabFeature, "gitlab.com", "acme/homebrew")
	cfg.ReleaseChannel = ReleaseChannelStatic
	cfg.ReleaseBaseURL = "https://pkg.acme.dev/acme/hbtool"
	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

	gr := readGenerated(t, fs, "/work/.goreleaser.yaml")
	assert.Contains(t, gr, `template: "https://pkg.acme.dev/acme/hbtool/{{ .Tag }}/{{ .ArtifactName }}"`)

	ci := readGenerated(t, fs, "/work/.gitlab-ci.yml")
	assert.Equal(t, 1, strings.Count(ci, "\ngoreleaser:\n"), "one overlay")
	assert.Contains(t, ci, "RELEASE_STORE_BUCKET: pbs-release-binaries")
	assert.Contains(t, ci, "- dist/**/Casks/hbtool.rb")
}

// No tap, no change: none of the pieces render, and a script an earlier tap
// left behind is removed (spec 0212 D1, D5).
func TestGenerateSkeleton_NoHomebrewTapRendersNothing(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := newSkeletonGeneratorForTest(t, fs)

	cfg := homebrewConfig(forge.GitlabFeature, "gitlab.com", "")
	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

	gr := readGenerated(t, fs, "/work/.goreleaser.yaml")
	assert.NotContains(t, gr, "homebrew_casks")
	assert.NotContains(t, gr, "notarize")
	assert.NotContains(t, gr, "- id:")

	ci := readGenerated(t, fs, "/work/.gitlab-ci.yml")
	assert.NotContains(t, ci, "homebrew")
	assert.NotContains(t, ci, "\ngoreleaser:")

	exists, _ := afero.Exists(fs, "/work/scripts/push-homebrew-cask.sh")
	assert.False(t, exists)

	require.NoError(t, afero.WriteFile(fs, "/work/scripts/push-homebrew-cask.sh", []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

	exists, _ = afero.Exists(fs, "/work/scripts/push-homebrew-cask.sh")
	assert.False(t, exists, "a tap's script left behind is removed")
}

func TestValidateHomebrewTap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		tap     string
		backend props.FeatureID
		private bool
		static  bool
		wantErr bool
	}{
		{name: "no tap", tap: "", backend: "", wantErr: false},
		{name: "gitlab", tap: "phpboyscout/homebrew", backend: forge.GitlabFeature},
		{name: "gitlab subgroup", tap: "acme/tools/homebrew", backend: forge.GitlabFeature},
		{name: "github", tap: "acme/homebrew-tap", backend: forge.GithubFeature},
		{name: "github subgroup", tap: "acme/tools/homebrew", backend: forge.GithubFeature, wantErr: true},
		{name: "not hosted", tap: "acme/homebrew", backend: "", wantErr: true},
		{name: "other forge", tap: "acme/homebrew", backend: "gitea", wantErr: true},
		{name: "private forge release", tap: "acme/homebrew", backend: forge.GitlabFeature, private: true, wantErr: true},
		{name: "private static channel", tap: "acme/homebrew", backend: forge.GitlabFeature, private: true, static: true},
		{name: "no owner", tap: "homebrew", backend: forge.GitlabFeature, wantErr: true},
		{name: "host", tap: "gitlab.com/acme/homebrew", backend: forge.GithubFeature, wantErr: true},
		{name: "scheme", tap: "https://gitlab.com/acme/homebrew", backend: forge.GitlabFeature, wantErr: true},
		{name: "space", tap: "acme/home brew", backend: forge.GitlabFeature, wantErr: true},
		{name: "shell", tap: "acme/homebrew;rm", backend: forge.GitlabFeature, wantErr: true},
		{name: "quote", tap: "acme/home'brew", backend: forge.GitlabFeature, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateHomebrewTap(tt.tap, tt.backend, tt.private, tt.static)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// The refusal is a manifest rule, so a later forge change cannot leave a tap
// behind that no pipeline pushes (spec 0212 D2).
func TestValidateManifest_HomebrewTapNeedsItsForge(t *testing.T) {
	t.Parallel()

	m := Manifest{
		Properties:    ManifestProperties{Name: "hbtool", Homebrew: ManifestHomebrew{Tap: "acme/homebrew"}},
		ReleaseSource: ManifestReleaseSource{Type: "gitlab", Backend: forge.GitlabFeature, Host: "gitlab.com", Owner: "acme", Repo: "hbtool"},
	}
	require.NoError(t, ValidateManifest(&m))

	m.ReleaseSource.Private = true
	require.Error(t, ValidateManifest(&m), "a private forge release cannot serve a cask")
}

func TestSetSetting_HomebrewTap(t *testing.T) {
	t.Parallel()

	const project = "properties:\n  name: mytool\n" +
		"release_source:\n  type: gitlab\n  backend: gitlab\n  host: gitlab.com\n  owner: org\n  repo: mytool\n" +
		"version:\n  gtb: v1.0.0\n  go: \"1.26\"\ncommands: []\n"

	g, fs, _ := newPerimeterTestProject(t, project)

	require.NoError(t, g.SetSetting(context.Background(), "homebrew.tap", []string{"org/homebrew"}))

	m, err := g.loadManifest()
	require.NoError(t, err)
	assert.Equal(t, "org/homebrew", m.Properties.Homebrew.Tap)

	require.Error(t, g.SetSetting(context.Background(), "homebrew.tap", []string{"org/home brew"}))

	require.NoError(t, g.UnsetSetting(context.Background(), "homebrew.tap"))

	raw, err := afero.ReadFile(fs, "/work/.gtb/manifest.yaml")
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "homebrew", "unset removes the block")
}
