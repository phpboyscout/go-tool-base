package generator

import (
	"context"
	"testing"

	"gitlab.com/phpboyscout/go/errors"

	"github.com/spf13/afero"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

// The backend decides the release source, not the hostname (spec 0195 D2):
// --forge-backend gitlab with a self-managed host used to produce a GitHub
// project because releaseProviderForHost is a substring test.
func TestManifestFromSkeletonConfig_BackendDecidesTheReleaseSource(t *testing.T) {
	t.Parallel()

	m := manifestFromSkeletonConfig(SkeletonConfig{
		Name: "tool", Repo: "org/tool", Host: "code.example.com",
		ForgeBackend: forge.GitlabFeature,
	}, nil, "v1")

	assert.Equal(t, "gitlab", m.ReleaseSource.Type)
	assert.Equal(t, forge.GitlabFeature, m.ReleaseSource.Backend)
	assert.Equal(t, "code.example.com/org/tool", m.Properties.ModulePath, "a hosted project derives its module path")
}

// A direct channel records the direct block and type direct, whatever the
// backend (spec 0195 D7).
func TestManifestFromSkeletonConfig_DirectChannel(t *testing.T) {
	t.Parallel()

	m := manifestFromSkeletonConfig(SkeletonConfig{
		Name: "tool", ModulePath: "myapp",
		ReleaseChannel: ReleaseChannelDirect,
		Direct:         ManifestDirectSource{URLTemplate: "https://dl.example.com/{{.Version}}/{{.Asset}}", VersionURL: "https://dl.example.com/latest"},
	}, nil, "v1")

	assert.Equal(t, "direct", m.ReleaseSource.Type)
	assert.Empty(t, m.ReleaseSource.Backend)
	assert.Equal(t, "https://dl.example.com/latest", m.ReleaseSource.Direct.VersionURL)
	assert.Equal(t, "myapp", m.Properties.ModulePath, "a project that is not hosted names its own module path")
}

// ForgeBackends is every forge the generator can host a project on, taken
// from the feature catalogue so the chooser, the flag and the manifest
// validator cannot disagree.
func TestForgeBackends(t *testing.T) {
	t.Parallel()

	got := ForgeBackends()
	assert.Equal(t, []props.FeatureID{
		forge.GithubFeature, forge.GitlabFeature, forge.GiteaFeature, forge.CodebergFeature, forge.BitbucketFeature,
	}, got)

	for _, id := range got {
		_, ok := forge.DisplayFor(id)
		assert.Truef(t, ok, "backend %q has no display data", id)
	}

	require.NoError(t, ValidateForgeBackend("gitea"))
	require.NoError(t, ValidateForgeBackend(""))
	require.ErrorIs(t, ValidateForgeBackend("sourcehut"), ErrInvalidForgeBackend)
}

// Only GitHub and GitLab have a CI skeleton (spec 0195 D8); the choice follows
// the backend, and a legacy manifest with no backend falls back to the
// release provider it recorded.
func TestCISkeletonFor(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "assets/skeleton-gitlab", ciSkeletonFor(forge.GitlabFeature, "direct").root)
	assert.Equal(t, "assets/skeleton-github", ciSkeletonFor(forge.GithubFeature, "gitlab").root)
	assert.Equal(t, "assets/skeleton-gitlab", ciSkeletonFor("", "gitlab").root, "no backend recorded: the provider decides")
	assert.Empty(t, ciSkeletonFor(forge.GiteaFeature, "gitea").root, "no skeleton for gitea yet")
	assert.Empty(t, ciSkeletonFor("", "direct").root, "not hosted: no CI files")
}

// A manifest from before the backend existed derives it on regenerate (spec
// 0195 D11): one enabled forge feature wins, else the release type, else the
// host substring; two forge features with a type matching neither is refused.
func TestDeriveForgeBackend(t *testing.T) {
	t.Parallel()

	one := Manifest{Properties: ManifestProperties{Features: []ManifestFeature{{Name: "gitlab", Enabled: true}}},
		ReleaseSource: ManifestReleaseSource{Type: "github", Host: "github.com"}}
	got, err := deriveForgeBackend(&one)
	require.NoError(t, err)
	assert.Equal(t, forge.GitlabFeature, got, "the single enabled forge feature wins")

	byType := Manifest{ReleaseSource: ManifestReleaseSource{Type: "gitea", Host: "git.example.com"}}
	got, err = deriveForgeBackend(&byType)
	require.NoError(t, err)
	assert.Equal(t, forge.GiteaFeature, got)

	byHost := Manifest{ReleaseSource: ManifestReleaseSource{Type: "", Host: "gitlab.example.com"}}
	got, err = deriveForgeBackend(&byHost)
	require.NoError(t, err)
	assert.Equal(t, forge.GitlabFeature, got)

	two := Manifest{Properties: ManifestProperties{Features: []ManifestFeature{{Name: "gitlab", Enabled: true}, {Name: "github", Enabled: true}}},
		ReleaseSource: ManifestReleaseSource{Type: "gitea"}}
	_, err = deriveForgeBackend(&two)
	require.ErrorIs(t, err, ErrAmbiguousForgeBackend)

	twoByType := Manifest{Properties: ManifestProperties{Features: []ManifestFeature{{Name: "gitlab", Enabled: true}, {Name: "github", Enabled: true}}},
		ReleaseSource: ManifestReleaseSource{Type: "github"}}
	got, err = deriveForgeBackend(&twoByType)
	require.NoError(t, err)
	assert.Equal(t, forge.GithubFeature, got, "two features: the release type picks the backend, the other is a credential forge")
}

// A manifest that predates the backend and module path gets both derived and
// written back on regenerate (spec 0195 D11), the way the chat block was.
func TestSyncDerivedManifestFields_RecordsBackendAndModulePath(t *testing.T) {
	t.Parallel()

	manifest := "properties:\n  name: mytool\n  features:\n    - name: gitlab\n      enabled: true\n" +
		"release_source:\n  type: github\n  host: code.example.com\n  owner: org\n  repo: mytool\n" +
		"version:\n  gtb: v1.0.0\ncommands: []\n"

	g, fs, _ := newPerimeterTestProject(t, manifest)

	m, err := g.loadManifest()
	require.NoError(t, err)
	require.NoError(t, g.syncDerivedManifestFields(m))

	raw, err := afero.ReadFile(fs, "/work/.gtb/manifest.yaml")
	require.NoError(t, err)
	assert.Contains(t, string(raw), "backend: gitlab", "the single enabled forge feature is recorded as the backend")
	assert.Contains(t, string(raw), "module_path: code.example.com/org/mytool")
	assert.Equal(t, forge.GitlabFeature, m.ReleaseSource.Backend)
}

func TestValidateManifest_RefusesUnknownBackend(t *testing.T) {
	t.Parallel()

	m := &Manifest{Properties: ManifestProperties{Name: "tool"}, ReleaseSource: ManifestReleaseSource{Backend: "sourcehut"}}
	require.ErrorIs(t, ValidateManifest(m), ErrInvalidForgeBackend)

	bad := &Manifest{Properties: ManifestProperties{Name: "tool", ModulePath: "not a module path"}}
	require.ErrorIs(t, ValidateManifest(bad), ErrInvalidModulePath)
}

func TestValidateModulePath(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{"", "myapp", "github.com/acme/tool", "go.acme.dev/tool/v2"} {
		require.NoError(t, ValidateModulePath(ok), ok)
	}

	for _, bad := range []string{"not a module path", "github.com/acme/tool/", "-x"} {
		require.ErrorIs(t, ValidateModulePath(bad), ErrInvalidModulePath, bad)
	}
}

// A project that is not hosted on a forge generates with its own module path
// and no repository (spec 0195 D3, D5).
func TestGenerateSkeleton_NotHosted(t *testing.T) {
	t.Parallel()

	g, fs := newPureGenerator(t, &Config{Path: "/proj", Overwrite: OverwriteAllow})
	g.runCommand = func(_ context.Context, _, _ string, _ ...string) ([]byte, error) { return nil, nil }

	require.NoError(t, g.GenerateSkeleton(context.Background(), SkeletonConfig{
		Name: "myapp", Path: "/proj", ModulePath: "myapp",
		Features: []ManifestFeature{{Name: "docs", Enabled: true}},
	}))

	gomod, err := afero.ReadFile(fs, "/proj/go.mod")
	require.NoError(t, err)
	assert.Contains(t, string(gomod), "module myapp\n")

	m, err := g.decodeManifestFile(ManifestPathFor("/proj"))
	require.NoError(t, err)
	assert.Equal(t, "myapp", m.Properties.ModulePath)
	assert.Empty(t, m.ReleaseSource.Backend)
	assert.Empty(t, m.ReleaseSource.Host)

	_, err = fs.Stat("/proj/.github")
	assert.Error(t, err, "not hosted: no CI skeleton")
}

// Forges leave the generate-time vocabulary (spec 0195 D1): the backend
// chooses one and --forge-credentials the rest. enable/disable keep them,
// because a forge feature is still a feature once the project exists (D6).
func TestSelectableFeatures_HaveNoForge(t *testing.T) {
	t.Parallel()

	for _, id := range ForgeBackends() {
		assert.NotContainsf(t, SelectableFeatures, string(id), "%s is chosen by --forge-backend, not --features", id)
		assert.Containsf(t, ToggleableFeatures, string(id), "%s stays toggleable after generation", id)
	}

	err := ValidateSelectableFeatureName("github")
	require.Error(t, err)
	assert.Contains(t, errors.FlattenHints(err), "--forge-backend", "the refusal names the flag that chooses a forge")
}
