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
// The static channel (spec 0203 D1) is a release source type of its own with
// one setting; a project that is not hosted can take it, and a hosted one
// that opts in keeps its backend beside it.
func TestManifestFromSkeletonConfig_StaticChannel(t *testing.T) {
	t.Parallel()

	m := manifestFromSkeletonConfig(SkeletonConfig{
		Name: "tool", ModulePath: "myapp",
		ReleaseChannel: ReleaseChannelStatic,
		ReleaseBaseURL: "https://pkg.acme.dev/tool",
	}, nil, "v1")

	assert.Equal(t, "static", m.ReleaseSource.Type)
	assert.Empty(t, m.ReleaseSource.Backend)
	assert.Equal(t, "https://pkg.acme.dev/tool", m.ReleaseSource.Static.BaseURL)
	assert.Equal(t, "myapp", m.Properties.ModulePath, "a project that is not hosted names its own module path")
	require.NoError(t, ValidateManifest(&m))

	hosted := manifestFromSkeletonConfig(SkeletonConfig{
		Name: "tool", Repo: "acme/tool", Host: "gitlab.com", ForgeBackend: forge.GitlabFeature,
		ReleaseChannel: ReleaseChannelStatic, ReleaseBaseURL: "https://pkg.acme.dev/tool",
	}, nil, "v1")

	assert.Equal(t, "static", hosted.ReleaseSource.Type)
	assert.Equal(t, forge.GitlabFeature, hosted.ReleaseSource.Backend, "the backend stays for init and credentials")
	assert.Equal(t, "acme", hosted.ReleaseSource.Owner)

	onForge := manifestFromSkeletonConfig(SkeletonConfig{
		Name: "tool", Repo: "acme/tool", Host: "gitlab.com", ForgeBackend: forge.GitlabFeature,
		ReleaseChannel: ReleaseChannelForge, ReleaseBaseURL: "https://left.over.example.com",
	}, nil, "v1")
	assert.Empty(t, onForge.ReleaseSource.Static.BaseURL, "a base URL is recorded only on the static channel")
}

// The withdrawn direct channel (spec 0203 D9): its block is dropped by the
// derivation pass so the write-back removes it, and its type is refused with
// a message that names the replacement.
func TestDirectChannelIsWithdrawn(t *testing.T) {
	t.Parallel()

	m := &Manifest{
		Properties: ManifestProperties{Name: "tool", ModulePath: "myapp"},
		ReleaseSource: ManifestReleaseSource{Type: "github", Backend: forge.GithubFeature, Host: "github.com", Owner: "o", Repo: "r",
			Direct: ManifestDirectSource{URLTemplate: "https://dl.example.com/{{.Version}}/{{.Asset}}"}},
	}
	m.Properties.Features = []ManifestFeature{{Name: "github", Enabled: true}}

	changed, err := deriveMissingManifestFields(m)
	require.NoError(t, err)
	assert.True(t, changed, "dropping the block is a change the manifest is written back for")
	assert.Equal(t, ManifestDirectSource{}, m.ReleaseSource.Direct)

	err = ValidateReleaseChannel("direct")
	require.ErrorIs(t, err, ErrInvalidReleaseChannel)
	assert.Contains(t, err.Error(), "static")

	err = ValidateReleaseSourceType("direct")
	require.Error(t, err)
	assert.Contains(t, errors.FlattenHints(err), "release_source.static.base_url")
}

// The static channel's base URL is held to the provider-endpoint rule (spec
// 0203 D1): https, no userinfo, no placeholder host, nothing after the path.
func TestValidateReleaseBaseURL(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateReleaseBaseURL("https://pkg.acme.dev/tool"))

	for _, bad := range []string{"", "http://pkg.acme.dev/tool", "https://user:pw@pkg.acme.dev/tool", "https://example.com/tool", "https://pkg.acme.dev/tool?x=1", "pkg.acme.dev/tool"} {
		require.Error(t, ValidateReleaseBaseURL(bad), bad)
	}

	static := &ManifestReleaseSource{Type: "static"}
	require.Error(t, validateManifestStaticSource(static), "static needs a base URL")

	onForge := &Manifest{Properties: ManifestProperties{Name: "tool", ModulePath: "example.com/tool"},
		ReleaseSource: ManifestReleaseSource{Type: "github", Backend: forge.GithubFeature, Static: ManifestStaticSource{BaseURL: "https://pkg.acme.dev/tool"}}}
	require.NoError(t, ValidateManifest(onForge), "a base URL beside a forge is ignored, so set can record it before the type")
	require.Len(t, ManifestWarnings(onForge), 1)
	assert.Contains(t, ManifestWarnings(onForge)[0], "read only on the static channel")
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

// A manifest from before spec 0195 names its forge only as the release type.
// Deriving the backend and stopping there left forge.go empty and the tool
// unable to update (keryx's v0.40.0 -> v0.43.0 regenerate): the backend
// implies the forge (D1), so the derivation enables its feature too.
func TestSyncDerivedManifestFields_ADerivedBackendEnablesItsForgeFeature(t *testing.T) {
	t.Parallel()

	manifest := "properties:\n  name: mytool\n  features:\n    - name: update\n      enabled: true\n" +
		"release_source:\n  type: gitlab\n  host: gitlab.com\n  owner: org\n  repo: mytool\n" +
		"version:\n  gtb: v0.40.0\ncommands: []\n"

	g, fs, _ := newPerimeterTestProject(t, manifest)

	m, err := g.loadManifest()
	require.NoError(t, err)
	require.NoError(t, g.syncDerivedManifestFields(m))

	assert.Equal(t, forge.GitlabFeature, m.ReleaseSource.Backend)
	assert.True(t, featureEnabledIn(m.Properties.Features, string(forge.GitlabFeature)), "the derived backend's forge is enabled")
	assert.Equal(t, []string{string(forge.GitlabFeature)}, enabledForges(m.Properties.Features))

	raw, err := afero.ReadFile(fs, "/work/.gtb/manifest.yaml")
	require.NoError(t, err)
	assert.Contains(t, string(raw), "- name: gitlab\n      enabled: true", "and recorded, so the project states its choice from then on")
}

// A manifest already naming a forge feature is read, not migrated (D11): a
// second forge is never added on its behalf.
func TestSyncDerivedManifestFields_AnEnabledForgeIsLeftAlone(t *testing.T) {
	t.Parallel()

	manifest := "properties:\n  name: mytool\n  features:\n    - name: github\n      enabled: true\n" +
		"release_source:\n  type: github\n  host: github.com\n  owner: org\n  repo: mytool\n" +
		"version:\n  gtb: v0.42.0\ncommands: []\n"

	g, _, _ := newPerimeterTestProject(t, manifest)

	m, err := g.loadManifest()
	require.NoError(t, err)
	require.NoError(t, g.syncDerivedManifestFields(m))

	assert.Equal(t, []string{string(forge.GithubFeature)}, enabledForges(m.Properties.Features))
}

// A scaffold from before the manifest recorded keychain carries the file and
// no entry. Spec 0197 D8 removes a file the manifest does not call for, which
// would silently drop a tool's keychain on its first regenerate, so the
// derivation reads the file as the record once and writes the entry.
func TestSyncDerivedManifestFields_AKeychainFileWithNoEntryIsRecorded(t *testing.T) {
	t.Parallel()

	manifest := "properties:\n  name: mytool\n  features:\n    - name: update\n      enabled: true\n" +
		"release_source:\n  type: gitlab\n  host: gitlab.com\n  owner: org\n  repo: mytool\n" +
		"version:\n  gtb: v0.40.0\ncommands: []\n"

	g, fs, _ := newPerimeterTestProject(t, manifest)
	require.NoError(t, afero.WriteFile(fs, "/work/cmd/mytool/keychain.go",
		[]byte("package main\n\nimport _ \"gitlab.com/phpboyscout/go/credentials/keychain\"\n"), 0o644))

	m, err := g.loadManifest()
	require.NoError(t, err)
	require.NoError(t, g.syncDerivedManifestFields(m))

	assert.True(t, featureEnabledIn(m.Properties.Features, KeychainFeature))

	raw, err := afero.ReadFile(fs, "/work/.gtb/manifest.yaml")
	require.NoError(t, err)
	assert.Contains(t, string(raw), "- name: keychain\n      enabled: true")

	// An explicit entry, either way, is the record and the file does not override it.
	off := &Manifest{Properties: ManifestProperties{Name: "mytool",
		Features: []ManifestFeature{{Name: KeychainFeature, Enabled: false}}}}
	assert.False(t, g.recordKeychainFromFile(off))
	assert.False(t, featureEnabledIn(off.Properties.Features, KeychainFeature))

	// A file the current generator wrote is the sync's own: `disable keychain`
	// removes the entry as the default state, and the file must not vote.
	require.NoError(t, afero.WriteFile(fs, "/work/cmd/mytool/keychain.go",
		[]byte("package main\n\nimport _ \"gitlab.com/phpboyscout/go-tool-base/pkg/setup/keychain\"\n"), 0o644))
	current := &Manifest{Properties: ManifestProperties{Name: "mytool"}}
	assert.False(t, g.recordKeychainFromFile(current))
}

func TestValidateManifest_RefusesUnknownBackend(t *testing.T) {
	t.Parallel()

	m := &Manifest{Properties: ManifestProperties{Name: "tool"}, ReleaseSource: ManifestReleaseSource{Backend: "sourcehut"}}
	require.ErrorIs(t, ValidateManifest(m), ErrInvalidForgeBackend)

	bad := &Manifest{Properties: ManifestProperties{Name: "tool", ModulePath: "not a module path"}}
	require.ErrorIs(t, ValidateManifest(bad), ErrInvalidModulePath)
}

// TestValidateManifest_RefusesABackendContradictingTheReleaseType (F21 of the
// v0.43.0 manual round): gtb set release_source.backend accepted a forge
// that contradicted release_source.type, so the tool would link one adapter
// and release from another. The backend implies the forge (spec 0195), so a
// release type that names a forge must be that backend.
func TestValidateManifest_RefusesABackendContradictingTheReleaseType(t *testing.T) {
	t.Parallel()

	contradiction := &Manifest{Properties: ManifestProperties{Name: "tool"},
		ReleaseSource: ManifestReleaseSource{Type: "github", Backend: "gitlab", Host: "gitlab.com", Owner: "o", Repo: "r"}}
	require.ErrorIs(t, ValidateManifest(contradiction), ErrBackendContradictsType)

	agree := &Manifest{Properties: ManifestProperties{Name: "tool"},
		ReleaseSource: ManifestReleaseSource{Type: "gitlab", Backend: "gitlab", Host: "gitlab.com", Owner: "o", Repo: "r"}}
	require.NoError(t, ValidateManifest(agree))

	static := &Manifest{Properties: ManifestProperties{Name: "tool", ModulePath: "example.com/tool"},
		ReleaseSource: ManifestReleaseSource{Type: "static", Backend: "gitlab", Static: ManifestStaticSource{BaseURL: "https://pkg.acme.dev/tool"}}}
	require.NoError(t, ValidateManifest(static), "a type that names no forge constrains nothing")
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
