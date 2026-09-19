package generator

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

func readGenerated(t *testing.T, fs afero.Fs, path string) string {
	t.Helper()

	raw, err := afero.ReadFile(fs, path)
	require.NoError(t, err, path)

	return string(raw)
}

// A project on the static channel (spec 0203 D7) renders the release source
// as the type and the base URL alone, the three release-configuration pieces
// of D5 with the store path derived from the base URL, the pointer script,
// the tool directive that writes the documents, and Pro in its pipeline.
func TestGenerateSkeleton_StaticChannelRendersTheChannel(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := newSkeletonGeneratorForTest(t, fs)

	cfg := SkeletonConfig{
		Name: "sttool", Repo: "acme/sttool", Host: "gitlab.com", ForgeBackend: forge.GitlabFeature,
		Description: "static channel scaffold", Path: "/work",
		Features:       []ManifestFeature{{Name: "changelog", Enabled: false}, {Name: "docs", Enabled: false}, {Name: "gitlab", Enabled: true}},
		ReleaseChannel: ReleaseChannelStatic,
		ReleaseBaseURL: "https://pkg.acme.dev/acme/sttool",
	}
	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

	root := readGenerated(t, fs, "/work/pkg/cmd/root/cmd.go")
	assert.Contains(t, root, "Type:    props.ReleaseSourceStatic")
	assert.Contains(t, root, `BaseURL: "https://pkg.acme.dev/acme/sttool"`)
	assert.NotContains(t, root, `Owner:`, "the static reader consults no repository")

	gr := readGenerated(t, fs, "/work/.goreleaser.yaml")
	assert.Contains(t, gr, "force_token: gitlab", "hosted: the forge token is the backend's")
	assert.Contains(t, gr, "before_publish:")
	assert.Contains(t, gr, "go tool releasemanifest --dist dist --base-url https://pkg.acme.dev/acme/sttool")
	assert.Contains(t, gr, "artifacts: [checksum]")
	assert.Contains(t, gr, `directory: "acme/sttool/{{ .Tag }}"`, "the store prefix is the base URL's path")
	assert.Contains(t, gr, "glob: dist/release.json")
	assert.Contains(t, gr, "bash scripts/move-pointer.sh dist/latest.json {{ .Env.RELEASE_STORE_ENDPOINT }}/{{ .Env.RELEASE_STORE_BUCKET }}/acme/sttool/latest.json")
	assert.Contains(t, gr, "gitlab_urls:", "hosted: the forge release object is still created")

	script := readGenerated(t, fs, "/work/scripts/move-pointer.sh")
	assert.Contains(t, script, "If-Match")

	gomod := readGenerated(t, fs, "/work/go.mod")
	assert.Contains(t, gomod, "gitlab.com/phpboyscout/go-tool-base/cmd/releasemanifest")

	ci := readGenerated(t, fs, "/work/.gitlab-ci.yml")
	assert.Contains(t, ci, "pro: true")
	assert.Contains(t, ci, "RELEASE_STORE_BUCKET: pbs-release-binaries")

	g.config.Path = "/work"
	m, err := g.loadManifest()
	require.NoError(t, err)
	assert.Equal(t, "static", m.ReleaseSource.Type)
	assert.Equal(t, "https://pkg.acme.dev/acme/sttool", m.ReleaseSource.Static.BaseURL)
	assert.Equal(t, forge.GitlabFeature, m.ReleaseSource.Backend)
}

// A project that is not hosted and takes the static channel has no forge
// release object to create and no forge token to read.
func TestGenerateSkeleton_NotHostedStaticChannel(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := newSkeletonGeneratorForTest(t, fs)

	cfg := SkeletonConfig{
		Name: "lonetool", ModulePath: "lonetool", Description: "not hosted", Path: "/work",
		Features:       []ManifestFeature{{Name: "changelog", Enabled: false}, {Name: "docs", Enabled: false}},
		ReleaseChannel: ReleaseChannelStatic,
		ReleaseBaseURL: "https://pkg.acme.dev/lonetool",
	}
	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

	gr := readGenerated(t, fs, "/work/.goreleaser.yaml")
	assert.NotContains(t, gr, "force_token")
	assert.Contains(t, gr, "disable: true")
	assert.NotContains(t, gr, "gitlab_urls:")
	assert.NotContains(t, gr, "github_urls:")
	assert.Contains(t, gr, `directory: "lonetool/{{ .Tag }}"`)

	exists, _ := afero.Exists(fs, "/work/.gitlab-ci.yml")
	assert.False(t, exists, "no backend, no CI skeleton")
}

// The forge channel renders none of it, and a project that leaves the static
// channel loses the pointer script and the tool directive on regenerate.
func TestGenerateSkeleton_ForgeChannelCarriesNoStaticPieces(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := newSkeletonGeneratorForTest(t, fs)

	cfg := SkeletonConfig{
		Name: "fgtool", Repo: "acme/fgtool", Host: "github.com", ForgeBackend: forge.GithubFeature,
		Description: "forge scaffold", Path: "/work",
		Features:       []ManifestFeature{{Name: "changelog", Enabled: false}, {Name: "docs", Enabled: false}, {Name: "github", Enabled: true}},
		ReleaseChannel: ReleaseChannelForge,
	}
	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

	gr := readGenerated(t, fs, "/work/.goreleaser.yaml")
	assert.Contains(t, gr, "force_token: github")
	assert.NotContains(t, gr, "before_publish")
	assert.NotContains(t, gr, "blobs:")
	assert.NotContains(t, gr, "publishers:")
	assert.Contains(t, gr, "github_urls:")

	exists, _ := afero.Exists(fs, "/work/scripts/move-pointer.sh")
	assert.False(t, exists, "a forge project ships no pointer script")
	assert.NotContains(t, readGenerated(t, fs, "/work/go.mod"), "cmd/releasemanifest")

	root := readGenerated(t, fs, "/work/pkg/cmd/root/cmd.go")
	assert.Contains(t, root, `Type:  "github"`)
	assert.NotContains(t, root, "BaseURL")

	// Leave the static channel: the file and the directive go.
	require.NoError(t, afero.WriteFile(fs, "/work/scripts/move-pointer.sh", []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

	exists, _ = afero.Exists(fs, "/work/scripts/move-pointer.sh")
	assert.False(t, exists, "a static-channel file left behind is removed")
}

// The withdrawn direct channel's block is dropped from the manifest on the
// first regenerate (spec 0203 D9) and the write-back says so.
func TestRegenerateProject_DropsTheWithdrawnDirectBlock(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	log := logger.NewBuffer()
	p := &props.Props{FS: fs, Logger: log, Config: emptyTestStore(t), Version: version.NewInfo("v1.0.0", "", "")}

	root := "/work"
	_ = fs.MkdirAll(root+"/.gtb", 0o755)
	_ = afero.WriteFile(fs, root+"/.gtb/manifest.yaml", []byte(`properties:
  name: oldtool
  module_path: github.com/acme/oldtool
  features:
    - name: github
      enabled: true
    - name: changelog
      enabled: false
    - name: docs
      enabled: false
release_source:
  type: github
  backend: github
  host: github.com
  owner: acme
  repo: oldtool
  direct:
    url_template: https://dl.acme.dev/{{.Version}}/{{.Asset}}
    version_url: https://dl.acme.dev/latest
version:
  gtb: v1.0.0
commands: []
`), 0o644)
	_ = afero.WriteFile(fs, root+"/go.mod", []byte("module github.com/acme/oldtool\n"), 0o644)
	_ = fs.MkdirAll(root+"/pkg/cmd/root", 0o755)
	_ = afero.WriteFile(fs, root+"/pkg/cmd/root/cmd.go", []byte("package root\nfunc NewCmdRoot(p interface{}) {}\n"), 0o644)

	g := New(p, &Config{Path: root, Overwrite: OverwriteAllow})
	g.runCommand = func(context.Context, string, string, ...string) ([]byte, error) { return []byte("done"), nil }

	require.NoError(t, g.RegenerateProject(context.Background()))

	m, err := g.loadManifest()
	require.NoError(t, err)
	assert.Equal(t, ManifestDirectSource{}, m.ReleaseSource.Direct)
	assert.NotContains(t, readGenerated(t, fs, filepath.Join(root, ".gtb/manifest.yaml")), "url_template")
	assert.Contains(t, log.String(), "recorded the derived values", "the write-back is logged")
}
